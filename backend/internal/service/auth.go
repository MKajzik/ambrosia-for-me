// Package service holds business rules. It calls the store and never speaks HTTP.
package service

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/InzKazik/mealplanner/backend/internal/auth"
	"github.com/InzKazik/mealplanner/backend/internal/store"
	"github.com/InzKazik/mealplanner/backend/internal/store/sqlc"
)

// Errors returned by Auth. Handlers map them to problem responses.
var (
	ErrEmailTaken          = errors.New("email is already registered")
	ErrInvalidCredentials  = errors.New("invalid email or password")
	ErrInvalidRefreshToken = errors.New("invalid refresh token")
	ErrNotFound            = errors.New("not found")
)

// User is an account as the rest of the application sees it.
type User struct {
	ID             uuid.UUID
	Email          string
	DisplayName    string
	TargetKcal     *float64
	TargetProteinG *float64
	TargetCarbsG   *float64
	TargetFatG     *float64
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

// Session is a signed-in user with a fresh token pair.
type Session struct {
	AccessToken  string
	RefreshToken string
	ExpiresIn    time.Duration
	User         User
}

// RegisterInput is the data needed to create an account.
type RegisterInput struct {
	Email       string
	Password    string
	DisplayName string
}

// Optional distinguishes "leave unchanged" (zero value) from "set to Value",
// where a nil Value clears a nullable column.
type Optional[T any] struct {
	Specified bool
	Value     *T
}

// Set returns an Optional that assigns v (nil clears the column).
func Set[T any](v *T) Optional[T] { return Optional[T]{Specified: true, Value: v} }

// UpdateInput is a partial profile update: unspecified fields are unchanged.
type UpdateInput struct {
	DisplayName    *string
	TargetKcal     Optional[float64]
	TargetProteinG Optional[float64]
	TargetCarbsG   Optional[float64]
	TargetFatG     Optional[float64]
}

// Auth implements registration, login, refresh-token sessions and the
// signed-in user's own account.
type Auth struct {
	st         *store.Store
	hasher     *auth.Hasher
	tokens     *auth.TokenIssuer
	refreshTTL time.Duration
	now        func() time.Time
}

// NewAuth returns an Auth service. now is injected so tests control time.
func NewAuth(st *store.Store, hasher *auth.Hasher, tokens *auth.TokenIssuer, refreshTTL time.Duration, now func() time.Time) *Auth {
	return &Auth{st: st, hasher: hasher, tokens: tokens, refreshTTL: refreshTTL, now: now}
}

// Register creates an account and signs it in.
func (a *Auth) Register(ctx context.Context, in RegisterInput) (Session, error) {
	hash, err := a.hasher.Hash(in.Password)
	if err != nil {
		return Session{}, err
	}

	var sess Session
	err = a.st.InTx(ctx, func(q *sqlc.Queries) error {
		user, err := q.CreateUser(ctx, sqlc.CreateUserParams{
			Email: in.Email, PasswordHash: &hash, DisplayName: in.DisplayName,
		})
		if store.IsUniqueViolation(err, "users_email_key") {
			return ErrEmailTaken
		}
		if err != nil {
			return fmt.Errorf("create user: %w", err)
		}
		sess, err = a.newSession(ctx, q, user, uuid.New())
		return err
	})
	if err != nil {
		return Session{}, err
	}
	return sess, nil
}

// Login checks an email and password and starts a new session.
func (a *Auth) Login(ctx context.Context, email, password string) (Session, error) {
	user, err := a.st.GetUserByEmail(ctx, email)
	if store.IsNotFound(err) {
		a.hasher.Burn(password)
		return Session{}, ErrInvalidCredentials
	}
	if err != nil {
		return Session{}, fmt.Errorf("get user: %w", err)
	}
	if user.PasswordHash == nil { // an account that only signs in with Apple
		a.hasher.Burn(password)
		return Session{}, ErrInvalidCredentials
	}
	ok, err := a.hasher.Verify(password, *user.PasswordHash)
	if err != nil {
		return Session{}, err
	}
	if !ok {
		return Session{}, ErrInvalidCredentials
	}
	return a.newSession(ctx, a.st.Queries, user, uuid.New())
}

// Refresh exchanges a refresh token for a new token pair and revokes the old
// token. Presenting a token that was already used or revoked is treated as
// theft: the whole session family is revoked.
func (a *Auth) Refresh(ctx context.Context, rawToken string) (Session, error) {
	var (
		sess   Session
		reused bool
	)
	err := a.st.InTx(ctx, func(q *sqlc.Queries) error {
		rt, err := q.GetRefreshTokenByHashForUpdate(ctx, auth.HashRefreshToken(rawToken))
		if store.IsNotFound(err) {
			return ErrInvalidRefreshToken
		}
		if err != nil {
			return fmt.Errorf("get refresh token: %w", err)
		}
		if rt.RevokedAt != nil {
			// Commit the family revocation, then report the failure.
			reused = true
			if err := q.RevokeRefreshTokenFamily(ctx, rt.FamilyID); err != nil {
				return fmt.Errorf("revoke session family: %w", err)
			}
			return nil
		}
		if !rt.ExpiresAt.After(a.now()) {
			return ErrInvalidRefreshToken
		}
		if err := q.RevokeRefreshToken(ctx, rt.ID); err != nil {
			return fmt.Errorf("revoke refresh token: %w", err)
		}
		user, err := q.GetUserByID(ctx, rt.UserID)
		if err != nil {
			return fmt.Errorf("get user: %w", err)
		}
		sess, err = a.newSession(ctx, q, user, rt.FamilyID)
		return err
	})
	if err != nil {
		return Session{}, err
	}
	if reused {
		return Session{}, ErrInvalidRefreshToken
	}
	return sess, nil
}

// Logout revokes the whole session that rawToken belongs to. It is atomic with Refresh:
// a Refresh racing on the same token either finishes first, in which case Logout revokes
// its new token too (they share the family), or fails with ErrInvalidRefreshToken. Either
// way no live token is left. It is idempotent: an unknown or already-revoked token is not
// an error.
func (a *Auth) Logout(ctx context.Context, rawToken string) error {
	err := a.st.InTx(ctx, func(q *sqlc.Queries) error {
		rt, err := q.GetRefreshTokenByHashForUpdate(ctx, auth.HashRefreshToken(rawToken))
		if store.IsNotFound(err) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("get refresh token: %w", err)
		}
		if err := q.RevokeRefreshTokenFamily(ctx, rt.FamilyID); err != nil {
			return fmt.Errorf("revoke session: %w", err)
		}
		return nil
	})
	return err
}

// GetUser returns the account with the given ID.
func (a *Auth) GetUser(ctx context.Context, id uuid.UUID) (User, error) {
	u, err := a.st.GetUserByID(ctx, id)
	if store.IsNotFound(err) {
		return User{}, ErrNotFound
	}
	if err != nil {
		return User{}, fmt.Errorf("get user: %w", err)
	}
	return toUser(u), nil
}

// UpdateUser applies a partial profile update.
func (a *Auth) UpdateUser(ctx context.Context, id uuid.UUID, in UpdateInput) (User, error) {
	u, err := a.st.UpdateUserProfile(ctx, sqlc.UpdateUserProfileParams{
		ID:                id,
		DisplayName:       in.DisplayName,
		SetTargetKcal:     in.TargetKcal.Specified,
		TargetKcal:        in.TargetKcal.Value,
		SetTargetProteinG: in.TargetProteinG.Specified,
		TargetProteinG:    in.TargetProteinG.Value,
		SetTargetCarbsG:   in.TargetCarbsG.Specified,
		TargetCarbsG:      in.TargetCarbsG.Value,
		SetTargetFatG:     in.TargetFatG.Specified,
		TargetFatG:        in.TargetFatG.Value,
	})
	if store.IsNotFound(err) {
		return User{}, ErrNotFound
	}
	if err != nil {
		return User{}, fmt.Errorf("update user: %w", err)
	}
	return toUser(u), nil
}

// DeleteUser permanently deletes the account and, through cascading foreign
// keys, everything that belongs to it.
//
// The caller's plan entries, diet templates and meals go first, in that
// order, in the same transaction. users cascades to ingredients, meals,
// diet_templates and plan_entries, but meal_ingredients.ingredient_id,
// template_slots.meal_id and plan_entries.meal_id are all deliberately NO
// ACTION (it is what makes deleting an in-use ingredient or meal a 409), and
// Postgres does not run those cascades in an order that respects those
// references. Without these explicit pre-deletes, an account that owns a
// custom ingredient its own meal references fails with a foreign-key
// violation on meal_ingredients_ingredient_id_fkey, and an account whose
// diet template or plan entry references its own meal fails with a
// violation on template_slots_meal_id_fkey or plan_entries_meal_id_fkey.
// Deleting plan entries first is not strictly required (their
// from_template_id would otherwise just be set to NULL by the existing ON
// DELETE SET NULL when the template is deleted next), but it keeps the
// intent explicit. Diet templates must go before meals (a template's slots
// reference meals with NO ACTION, and deleting the template cascades its
// slots away first). Meals must go before users, as before.
func (a *Auth) DeleteUser(ctx context.Context, id uuid.UUID) error {
	return a.st.InTx(ctx, func(q *sqlc.Queries) error {
		if err := q.DeletePlanEntriesForUser(ctx, id); err != nil {
			return fmt.Errorf("delete plan entries: %w", err)
		}
		if err := q.DeleteDietTemplatesForUser(ctx, id); err != nil {
			return fmt.Errorf("delete diet templates: %w", err)
		}
		if err := q.DeleteMealsForUser(ctx, id); err != nil {
			return fmt.Errorf("delete meals: %w", err)
		}
		n, err := q.DeleteUser(ctx, id)
		if err != nil {
			return fmt.Errorf("delete user: %w", err)
		}
		if n == 0 {
			return ErrNotFound
		}
		return nil
	})
}

func (a *Auth) newSession(ctx context.Context, q *sqlc.Queries, user sqlc.User, family uuid.UUID) (Session, error) {
	access, ttl, err := a.tokens.IssueAccess(user.ID)
	if err != nil {
		return Session{}, err
	}
	raw, hash, err := auth.NewRefreshToken()
	if err != nil {
		return Session{}, err
	}
	_, err = q.CreateRefreshToken(ctx, sqlc.CreateRefreshTokenParams{
		UserID: user.ID, FamilyID: family, TokenHash: hash, ExpiresAt: a.now().Add(a.refreshTTL),
	})
	if err != nil {
		return Session{}, fmt.Errorf("store refresh token: %w", err)
	}
	return Session{AccessToken: access, RefreshToken: raw, ExpiresIn: ttl, User: toUser(user)}, nil
}

func toUser(u sqlc.User) User {
	return User{
		ID: u.ID, Email: u.Email, DisplayName: u.DisplayName,
		TargetKcal: u.TargetKcal, TargetProteinG: u.TargetProteinG,
		TargetCarbsG: u.TargetCarbsG, TargetFatG: u.TargetFatG,
		CreatedAt: u.CreatedAt, UpdatedAt: u.UpdatedAt,
	}
}
