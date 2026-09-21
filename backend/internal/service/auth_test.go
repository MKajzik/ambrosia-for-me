package service_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/InzKazik/mealplanner/backend/internal/auth"
	"github.com/InzKazik/mealplanner/backend/internal/db"
	"github.com/InzKazik/mealplanner/backend/internal/service"
	"github.com/InzKazik/mealplanner/backend/internal/store"
	"github.com/InzKazik/mealplanner/backend/internal/testutil"
)

type fixture struct {
	svc   *service.Auth
	store *store.Store
	clock *time.Time
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	pool, err := db.Connect(context.Background(), testutil.NewMigratedDatabase(t))
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(pool.Close)

	clock := time.Date(2026, 9, 21, 12, 0, 0, 0, time.UTC)
	f := &fixture{store: store.New(pool), clock: &clock}
	now := func() time.Time { return *f.clock }
	f.svc = service.NewAuth(f.store,
		auth.NewHasher(auth.HashParams{MemoryKiB: 8, Iterations: 1, Parallelism: 1}),
		auth.NewTokenIssuer([]byte("0123456789abcdef0123456789abcdef"), 15*time.Minute, now),
		30*24*time.Hour, now)
	return f
}

func register(t *testing.T, f *fixture, email string) service.Session {
	t.Helper()
	s, err := f.svc.Register(context.Background(), service.RegisterInput{
		Email: email, Password: "a-long-enough-password", DisplayName: "Test User",
	})
	if err != nil {
		t.Fatalf("Register(%s): %v", email, err)
	}
	return s
}

func TestRegisterReturnsSessionAndStoresUser(t *testing.T) {
	f := newFixture(t)

	s := register(t, f, "Alice@Example.com")

	if s.AccessToken == "" || s.RefreshToken == "" || s.ExpiresIn != 15*time.Minute {
		t.Errorf("session = %+v, want tokens and a 15m lifetime", s)
	}
	if s.User.Email != "Alice@Example.com" || s.User.DisplayName != "Test User" {
		t.Errorf("user = %+v", s.User)
	}
	if s.User.TargetKcal != nil {
		t.Errorf("new user has a kcal target %v, want none", *s.User.TargetKcal)
	}
	stored, err := f.store.GetUserByID(context.Background(), s.User.ID)
	if err != nil {
		t.Fatalf("stored user: %v", err)
	}
	if stored.PasswordHash == nil || *stored.PasswordHash == "a-long-enough-password" {
		t.Error("password was not hashed before storing")
	}
}

func TestRegisterRejectsDuplicateEmailCaseInsensitively(t *testing.T) {
	f := newFixture(t)
	register(t, f, "alice@example.com")

	_, err := f.svc.Register(context.Background(), service.RegisterInput{
		Email: "ALICE@example.com", Password: "another-long-password", DisplayName: "Other",
	})

	if !errors.Is(err, service.ErrEmailTaken) {
		t.Errorf("err = %v, want ErrEmailTaken", err)
	}
}

func TestLogin(t *testing.T) {
	f := newFixture(t)
	register(t, f, "alice@example.com")

	tests := []struct {
		name, email, password string
		wantErr               error
	}{
		{"correct credentials", "alice@example.com", "a-long-enough-password", nil},
		{"email is case-insensitive", "ALICE@example.com", "a-long-enough-password", nil},
		{"wrong password", "alice@example.com", "not-the-password", service.ErrInvalidCredentials},
		{"unknown email", "nobody@example.com", "a-long-enough-password", service.ErrInvalidCredentials},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s, err := f.svc.Login(context.Background(), tt.email, tt.password)

			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("err = %v, want %v", err, tt.wantErr)
			}
			if tt.wantErr == nil && (s.AccessToken == "" || s.RefreshToken == "") {
				t.Errorf("session missing tokens: %+v", s)
			}
		})
	}
}

func TestRefreshRotatesAndDetectsReuse(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	first := register(t, f, "alice@example.com")

	second, err := f.svc.Refresh(ctx, first.RefreshToken)
	if err != nil {
		t.Fatalf("first Refresh: %v", err)
	}
	if second.RefreshToken == first.RefreshToken {
		t.Error("refresh token was not rotated")
	}

	// Replaying the old token is theft: it must fail and revoke the whole family.
	if _, err := f.svc.Refresh(ctx, first.RefreshToken); !errors.Is(err, service.ErrInvalidRefreshToken) {
		t.Errorf("replayed token: err = %v, want ErrInvalidRefreshToken", err)
	}
	if _, err := f.svc.Refresh(ctx, second.RefreshToken); !errors.Is(err, service.ErrInvalidRefreshToken) {
		t.Errorf("newest token after reuse: err = %v, want ErrInvalidRefreshToken (family revoked)", err)
	}
}

func TestRefreshRejectsUnknownAndExpiredTokens(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	s := register(t, f, "alice@example.com")

	if _, err := f.svc.Refresh(ctx, "definitely-not-a-real-token"); !errors.Is(err, service.ErrInvalidRefreshToken) {
		t.Errorf("unknown token: err = %v, want ErrInvalidRefreshToken", err)
	}

	*f.clock = f.clock.Add(31 * 24 * time.Hour)
	if _, err := f.svc.Refresh(ctx, s.RefreshToken); !errors.Is(err, service.ErrInvalidRefreshToken) {
		t.Errorf("expired token: err = %v, want ErrInvalidRefreshToken", err)
	}
}

func TestLogoutRevokesTheSessionAndIsIdempotent(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	s := register(t, f, "alice@example.com")

	if err := f.svc.Logout(ctx, s.RefreshToken); err != nil {
		t.Fatalf("Logout: %v", err)
	}
	if _, err := f.svc.Refresh(ctx, s.RefreshToken); !errors.Is(err, service.ErrInvalidRefreshToken) {
		t.Errorf("Refresh after Logout: err = %v, want ErrInvalidRefreshToken", err)
	}
	if err := f.svc.Logout(ctx, s.RefreshToken); err != nil {
		t.Errorf("second Logout: %v, want nil (idempotent)", err)
	}
	if err := f.svc.Logout(ctx, "unknown-token"); err != nil {
		t.Errorf("Logout(unknown token): %v, want nil", err)
	}
}

func TestUpdateUserAppliesOnlySpecifiedFields(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	s := register(t, f, "alice@example.com")
	kcal, protein := 2200.0, 150.0

	u, err := f.svc.UpdateUser(ctx, s.User.ID, service.UpdateInput{
		DisplayName:    ptr("Alice"),
		TargetKcal:     service.Set(&kcal),
		TargetProteinG: service.Set(&protein),
	})
	if err != nil {
		t.Fatalf("UpdateUser: %v", err)
	}
	if u.DisplayName != "Alice" || u.TargetKcal == nil || *u.TargetKcal != 2200 || u.TargetProteinG == nil || *u.TargetProteinG != 150 {
		t.Errorf("after first update: %+v", u)
	}

	// Only the kcal target is specified (cleared): the others must stay.
	u, err = f.svc.UpdateUser(ctx, s.User.ID, service.UpdateInput{TargetKcal: service.Set[float64](nil)})
	if err != nil {
		t.Fatalf("second UpdateUser: %v", err)
	}
	if u.TargetKcal != nil {
		t.Errorf("kcal target = %v, want cleared", *u.TargetKcal)
	}
	if u.TargetProteinG == nil || *u.TargetProteinG != 150 || u.DisplayName != "Alice" {
		t.Errorf("unspecified fields changed: %+v", u)
	}
}

func TestGetUpdateDeleteUserNotFound(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	missing := uuid.New()

	if _, err := f.svc.GetUser(ctx, missing); !errors.Is(err, service.ErrNotFound) {
		t.Errorf("GetUser: err = %v, want ErrNotFound", err)
	}
	if _, err := f.svc.UpdateUser(ctx, missing, service.UpdateInput{DisplayName: ptr("x")}); !errors.Is(err, service.ErrNotFound) {
		t.Errorf("UpdateUser: err = %v, want ErrNotFound", err)
	}
	if err := f.svc.DeleteUser(ctx, missing); !errors.Is(err, service.ErrNotFound) {
		t.Errorf("DeleteUser: err = %v, want ErrNotFound", err)
	}
}

func TestDeleteUserRemovesAccountAndSessions(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	s := register(t, f, "alice@example.com")

	if err := f.svc.DeleteUser(ctx, s.User.ID); err != nil {
		t.Fatalf("DeleteUser: %v", err)
	}

	if _, err := f.svc.GetUser(ctx, s.User.ID); !errors.Is(err, service.ErrNotFound) {
		t.Errorf("GetUser after delete: err = %v, want ErrNotFound", err)
	}
	if _, err := f.svc.Refresh(ctx, s.RefreshToken); !errors.Is(err, service.ErrInvalidRefreshToken) {
		t.Errorf("Refresh after delete: err = %v, want ErrInvalidRefreshToken", err)
	}
	if _, err := f.svc.Login(ctx, "alice@example.com", "a-long-enough-password"); !errors.Is(err, service.ErrInvalidCredentials) {
		t.Errorf("Login after delete: err = %v, want ErrInvalidCredentials", err)
	}
}

func ptr[T any](v T) *T { return &v }
