// Package service holds business rules. It calls the store and never speaks HTTP.
package service

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"errors"
	"fmt"
	"math/big"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/InzKazik/mealplanner/backend/internal/store"
	"github.com/InzKazik/mealplanner/backend/internal/store/sqlc"
)

// Errors returned by Partners and by the partner listings of the other
// services. Handlers map them to problem responses.
var (
	// ErrPartnerNotLinked means the caller has no active partner (or, for
	// Get, no live invite).
	ErrPartnerNotLinked = errors.New("no linked partner")
	// ErrPartnerAlreadyLinked means the caller already has an active partner.
	ErrPartnerAlreadyLinked = errors.New("already linked to a partner")
	// ErrInviteInvalid means the invite code is wrong, expired, the caller's
	// own, or already used. Those cases are deliberately indistinguishable.
	ErrInviteInvalid = errors.New("invite code is invalid")
)

const (
	// inviteAlphabet has no 0, O, 1, I or L, so a code read aloud or from a
	// screenshot is hard to mistype.
	inviteAlphabet   = "23456789ABCDEFGHJKMNPQRSTUVWXYZ"
	inviteCodeLength = 8
	inviteTTL        = 48 * time.Hour
	// inviteCodeAttempts is how often Invite retries when a freshly generated
	// code collides with another live invite's hash (about 1 in 10^12).
	inviteCodeAttempts = 3
)

// Invite is a freshly created invite. Code is shown once: only its hash is
// stored.
type Invite struct {
	Code      string
	ExpiresAt time.Time
}

// Partnership is a user's partnership as they see it. DisplayName and
// LinkedAt are set while Status is "active", ExpiresAt while it is "pending".
type Partnership struct {
	Status      string // "pending" | "active"
	DisplayName *string
	LinkedAt    *time.Time
	ExpiresAt   *time.Time
}

// Partners links a user with one partner through an invite code, and ends the
// link. The visibility rules that follow from a link live in Meals,
// DietTemplates and ShoppingLists, which resolve the partner with
// activePartnerID.
type Partners struct {
	st     *store.Store
	events *ListEventHub
	now    func() time.Time
}

// NewPartners returns a Partners service. events is closed on unlink so a
// partner's open shopping-list streams end with the link.
func NewPartners(st *store.Store, events *ListEventHub, now func() time.Time) *Partners {
	return &Partners{st: st, events: events, now: now}
}

// Invite creates an invite for callerID, replacing any pending one, and
// returns its code. It fails with ErrPartnerAlreadyLinked if the caller is
// linked.
func (p *Partners) Invite(ctx context.Context, callerID uuid.UUID) (Invite, error) {
	var inv Invite
	err := p.st.InTx(ctx, func(q *sqlc.Queries) error {
		// Serialize with Accept and with a concurrent Invite by the same user.
		if err := lockUsers(ctx, q, callerID); err != nil {
			return err
		}
		if err := requireUnlinked(ctx, q, callerID, ErrPartnerAlreadyLinked); err != nil {
			return err
		}
		if err := q.DeletePendingPartnershipForUser(ctx, callerID); err != nil {
			return fmt.Errorf("replace pending invite: %w", err)
		}
		expires := p.now().Add(inviteTTL)
		for range inviteCodeAttempts {
			code, err := newInviteCode()
			if err != nil {
				return err
			}
			_, err = q.CreatePartnerInvite(ctx, sqlc.CreatePartnerInviteParams{
				UserID: callerID, InviteCodeHash: hashInviteCode(code), InviteExpiresAt: &expires,
			})
			if store.IsUniqueViolation(err, "partnerships_invite_code_hash_idx") {
				continue
			}
			if store.IsForeignKeyViolation(err, "partnerships_user_a_fkey") {
				return ErrNotFound
			}
			if err != nil {
				return fmt.Errorf("create invite: %w", err)
			}
			inv = Invite{Code: code, ExpiresAt: expires}
			return nil
		}
		return errors.New("could not generate a unique invite code")
	})
	if err != nil {
		return Invite{}, err
	}
	return inv, nil
}

// Accept links callerID with the user who issued code. A wrong, expired, own
// or used code is ErrInviteInvalid, all alike; a caller who already has a
// partner gets ErrPartnerAlreadyLinked whatever the code, so a linked user
// cannot use Accept to test codes.
func (p *Partners) Accept(ctx context.Context, callerID uuid.UUID, code string) (Partnership, error) {
	hash := hashInviteCode(code)
	var out Partnership
	err := p.st.InTx(ctx, func(q *sqlc.Queries) error {
		if err := requireUnlinked(ctx, q, callerID, ErrPartnerAlreadyLinked); err != nil {
			return err
		}
		invite, err := q.GetPendingPartnershipByCodeHash(ctx, hash)
		if store.IsNotFound(err) {
			return ErrInviteInvalid
		}
		if err != nil {
			return fmt.Errorf("find invite: %w", err)
		}
		if invite.UserA == callerID {
			return ErrInviteInvalid
		}

		// Lock both users in id order, then re-check everything: two accepts
		// by different users, or an accept racing an invite replace or an
		// unlink, must not both win. A user appearing once across both
		// columns of active rows is not expressible as an index.
		if err := lockUsers(ctx, q, callerID, invite.UserA); err != nil {
			return err
		}
		if err := requireUnlinked(ctx, q, callerID, ErrPartnerAlreadyLinked); err != nil {
			return err
		}
		if err := requireUnlinked(ctx, q, invite.UserA, ErrInviteInvalid); err != nil {
			return err // the inviter got linked meanwhile
		}
		invite, err = q.GetPendingPartnershipByCodeHash(ctx, hash)
		if store.IsNotFound(err) {
			return ErrInviteInvalid
		}
		if err != nil {
			return fmt.Errorf("find invite: %w", err)
		}
		if invite.InviteExpiresAt == nil || !p.now().Before(*invite.InviteExpiresAt) {
			return ErrInviteInvalid
		}

		// The caller's own pending invite dies with their becoming linked, so
		// no stale code stays valid.
		if err := q.DeletePendingPartnershipForUser(ctx, callerID); err != nil {
			return fmt.Errorf("drop own pending invite: %w", err)
		}
		row, err := q.ActivatePartnership(ctx, sqlc.ActivatePartnershipParams{ID: invite.ID, UserB: &callerID})
		if store.IsNotFound(err) {
			return ErrInviteInvalid
		}
		if store.IsUniqueViolation(err, "partnerships_active_user_a_idx") || store.IsUniqueViolation(err, "partnerships_active_user_b_idx") {
			return ErrPartnerAlreadyLinked
		}
		if store.IsForeignKeyViolation(err, "partnerships_user_b_fkey") {
			return ErrNotFound
		}
		if err != nil {
			return fmt.Errorf("activate partnership: %w", err)
		}
		inviter, err := q.GetUserByID(ctx, invite.UserA)
		if err != nil {
			return fmt.Errorf("get inviter: %w", err)
		}
		out = Partnership{Status: row.Status, DisplayName: &inviter.DisplayName, LinkedAt: &row.UpdatedAt}
		return nil
	})
	if err != nil {
		return Partnership{}, err
	}
	return out, nil
}

// Get returns callerID's partnership: the active partner, or a pending invite
// that has not expired. Anything else is ErrPartnerNotLinked.
func (p *Partners) Get(ctx context.Context, callerID uuid.UUID) (Partnership, error) {
	row, err := p.st.GetPartnershipForUser(ctx, callerID)
	if store.IsNotFound(err) {
		return Partnership{}, ErrPartnerNotLinked
	}
	if err != nil {
		return Partnership{}, fmt.Errorf("get partnership: %w", err)
	}
	if row.Status == "active" {
		return Partnership{Status: row.Status, DisplayName: row.PartnerDisplayName, LinkedAt: &row.UpdatedAt}, nil
	}
	if row.InviteExpiresAt == nil || !p.now().Before(*row.InviteExpiresAt) {
		return Partnership{}, ErrPartnerNotLinked
	}
	return Partnership{Status: row.Status, ExpiresAt: row.InviteExpiresAt}, nil
}

// Unlink ends callerID's active partnership, or cancels their pending invite
// (an expired one too). Either side may unlink. Access ends at once: the
// partner's next read finds no partnership, and their open shopping-list
// streams are closed.
func (p *Partners) Unlink(ctx context.Context, callerID uuid.UUID) error {
	rows, err := p.st.DeletePartnershipsForUser(ctx, callerID)
	if err != nil {
		return fmt.Errorf("delete partnership: %w", err)
	}
	if len(rows) == 0 {
		return ErrPartnerNotLinked
	}
	for _, r := range rows {
		if r.Status == "active" && r.UserB != nil {
			p.events.CloseAccess(r.UserA, *r.UserB)
		}
	}
	return nil
}

// activePartnerID returns the id of userID's active partner, or
// ErrPartnerNotLinked. With forShare it also takes a shared lock on the
// partnership row, so an Unlink's DELETE waits for the caller's transaction.
// The Meals, DietTemplates and ShoppingLists services call it once at the
// start of a transaction and pass the result into their visibility queries;
// nothing is cached, so an unlink is visible to the next transaction.
func activePartnerID(ctx context.Context, q *sqlc.Queries, userID uuid.UUID, forShare bool) (uuid.UUID, error) {
	var (
		id  uuid.UUID
		err error
	)
	if forShare {
		id, err = q.GetActivePartnerIDForShare(ctx, userID)
	} else {
		id, err = q.GetActivePartnerID(ctx, userID)
	}
	if store.IsNotFound(err) {
		return uuid.Nil, ErrPartnerNotLinked
	}
	if err != nil {
		return uuid.Nil, fmt.Errorf("get active partner: %w", err)
	}
	return id, nil
}

// partnerOrNil is activePartnerID for the read predicate: nil means "no
// partner", so the predicate's partner branch matches nothing.
func partnerOrNil(ctx context.Context, q *sqlc.Queries, userID uuid.UUID, forShare bool) (*uuid.UUID, error) {
	id, err := activePartnerID(ctx, q, userID, forShare)
	if errors.Is(err, ErrPartnerNotLinked) {
		return nil, nil //nolint:nilnil // nil is the "no partner" value the visibility queries take
	}
	if err != nil {
		return nil, err
	}
	return &id, nil
}

// requireUnlinked returns nil if userID has no active partner and linkedErr
// if they do.
func requireUnlinked(ctx context.Context, q *sqlc.Queries, userID uuid.UUID, linkedErr error) error {
	_, err := activePartnerID(ctx, q, userID, false)
	switch {
	case err == nil:
		return linkedErr
	case errors.Is(err, ErrPartnerNotLinked):
		return nil
	default:
		return err
	}
}

func lockUsers(ctx context.Context, q *sqlc.Queries, ids ...uuid.UUID) error {
	locked, err := q.LockUsersForUpdate(ctx, ids)
	if err != nil {
		return fmt.Errorf("lock users: %w", err)
	}
	if len(locked) != len(ids) {
		return ErrNotFound // a user vanished (account deleted)
	}
	return nil
}

// newInviteCode returns inviteCodeLength characters drawn uniformly from
// inviteAlphabet.
func newInviteCode() (string, error) {
	var b strings.Builder
	limit := big.NewInt(int64(len(inviteAlphabet)))
	for range inviteCodeLength {
		n, err := rand.Int(rand.Reader, limit)
		if err != nil {
			return "", fmt.Errorf("generate invite code: %w", err)
		}
		b.WriteByte(inviteAlphabet[n.Int64()])
	}
	return b.String(), nil
}

// hashInviteCode hashes a code after normalizing what a person types: case,
// spaces and dashes do not matter. SHA-256 is enough because the code is
// random, short-lived (inviteTTL) and rate limited; it is never a password.
func hashInviteCode(code string) []byte {
	normalized := strings.ToUpper(strings.NewReplacer(" ", "", "-", "").Replace(code))
	sum := sha256.Sum256([]byte(normalized))
	return sum[:]
}
