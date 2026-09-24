package service_test

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/InzKazik/mealplanner/backend/internal/service"
	"github.com/InzKazik/mealplanner/backend/internal/store"
	"github.com/InzKazik/mealplanner/backend/internal/store/sqlc"
)

// partnersFixture is a Partners service over a real database with a clock the
// test can move.
type partnersFixture struct {
	partners *service.Partners
	events   *service.ListEventHub
	st       *store.Store
	clock    *fakeClock
}

type fakeClock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *fakeClock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.t = c.t.Add(d)
}

func newPartnersFixture(t *testing.T) partnersFixture {
	t.Helper()
	_, st := newIngredientsFixture(t)
	clock := &fakeClock{t: time.Date(2026, 9, 23, 12, 0, 0, 0, time.UTC)}
	events := service.NewListEventHub()
	return partnersFixture{partners: service.NewPartners(st, events, clock.Now), events: events, st: st, clock: clock}
}

// newNamedUser creates a user with its own display name, for tests that read
// the partner's name back.
func newNamedUser(t *testing.T, st *store.Store, email, displayName string) uuid.UUID {
	t.Helper()
	hash := "hash"
	u, err := st.CreateUser(context.Background(), sqlc.CreateUserParams{Email: email, PasswordHash: &hash, DisplayName: displayName})
	if err != nil {
		t.Fatalf("create user %s: %v", email, err)
	}
	return u.ID
}

// link makes a and b partners through the real invite flow.
func (f partnersFixture) link(t *testing.T, a, b uuid.UUID) {
	t.Helper()
	ctx := context.Background()
	inv, err := f.partners.Invite(ctx, a)
	if err != nil {
		t.Fatalf("Invite: %v", err)
	}
	if _, err := f.partners.Accept(ctx, b, inv.Code); err != nil {
		t.Fatalf("Accept: %v", err)
	}
}

func TestPartnersInviteAndAcceptLinkTwoUsers(t *testing.T) {
	f := newPartnersFixture(t)
	ctx := context.Background()
	alice := newNamedUser(t, f.st, "alice1@example.com", "Alice")
	bob := newNamedUser(t, f.st, "bob1@example.com", "Bob")

	inv, err := f.partners.Invite(ctx, alice)
	if err != nil {
		t.Fatalf("Invite: %v", err)
	}
	if len(inv.Code) != 8 || strings.Trim(inv.Code, "23456789ABCDEFGHJKMNPQRSTUVWXYZ") != "" {
		t.Errorf("code %q, want 8 characters from the unambiguous alphabet", inv.Code)
	}
	if want := f.clock.Now().Add(48 * time.Hour); !inv.ExpiresAt.Equal(want) {
		t.Errorf("ExpiresAt = %v, want %v", inv.ExpiresAt, want)
	}

	pending, err := f.partners.Get(ctx, alice)
	if err != nil {
		t.Fatalf("Get while pending: %v", err)
	}
	if pending.Status != "pending" || pending.ExpiresAt == nil || pending.DisplayName != nil || pending.LinkedAt != nil {
		t.Errorf("pending partnership = %+v, want status pending with only ExpiresAt set", pending)
	}

	// People type codes in lower case, with a space or a dash in the middle.
	typed := strings.ToLower(inv.Code[:4]) + " - " + inv.Code[4:]
	accepted, err := f.partners.Accept(ctx, bob, typed)
	if err != nil {
		t.Fatalf("Accept: %v", err)
	}
	if accepted.Status != "active" || accepted.DisplayName == nil || *accepted.DisplayName != "Alice" || accepted.LinkedAt == nil {
		t.Errorf("accepted = %+v, want an active partnership with Alice", accepted)
	}

	forAlice, err := f.partners.Get(ctx, alice)
	if err != nil {
		t.Fatalf("Get for the inviter: %v", err)
	}
	if forAlice.Status != "active" || forAlice.DisplayName == nil || *forAlice.DisplayName != "Bob" {
		t.Errorf("the inviter sees %+v, want an active partnership with Bob", forAlice)
	}
}

func TestPartnersInviteRules(t *testing.T) {
	f := newPartnersFixture(t)
	ctx := context.Background()
	alice := newNamedUser(t, f.st, "alice2@example.com", "Alice")
	bob := newNamedUser(t, f.st, "bob2@example.com", "Bob")
	carol := newNamedUser(t, f.st, "carol2@example.com", "Carol")

	first, err := f.partners.Invite(ctx, alice)
	if err != nil {
		t.Fatalf("Invite: %v", err)
	}
	second, err := f.partners.Invite(ctx, alice)
	if err != nil {
		t.Fatalf("second Invite: %v", err)
	}
	if first.Code == second.Code {
		t.Fatal("a new invite returned the old code")
	}
	if _, err := f.partners.Accept(ctx, bob, first.Code); !errors.Is(err, service.ErrInviteInvalid) {
		t.Errorf("Accept of a replaced code: err = %v, want ErrInviteInvalid", err)
	}

	tests := map[string]struct {
		caller uuid.UUID
		code   string
	}{
		"a wrong code":     {bob, "ZZZZZZZZ"},
		"an empty code":    {bob, ""},
		"the caller's own": {alice, second.Code},
	}
	for name, tt := range tests {
		if _, err := f.partners.Accept(ctx, tt.caller, tt.code); !errors.Is(err, service.ErrInviteInvalid) {
			t.Errorf("Accept of %s: err = %v, want ErrInviteInvalid", name, err)
		}
	}

	f.clock.Advance(48*time.Hour - time.Second)
	if _, err := f.partners.Get(ctx, alice); err != nil {
		t.Errorf("Get just before the invite expires: %v", err)
	}
	f.clock.Advance(time.Second)
	if _, err := f.partners.Get(ctx, alice); !errors.Is(err, service.ErrPartnerNotLinked) {
		t.Errorf("Get of an expired invite: err = %v, want ErrPartnerNotLinked", err)
	}
	if _, err := f.partners.Accept(ctx, bob, second.Code); !errors.Is(err, service.ErrInviteInvalid) {
		t.Errorf("Accept of an expired code: err = %v, want ErrInviteInvalid", err)
	}

	fresh, err := f.partners.Invite(ctx, alice)
	if err != nil {
		t.Fatalf("Invite after expiry: %v", err)
	}
	if _, err := f.partners.Accept(ctx, bob, fresh.Code); err != nil {
		t.Fatalf("Accept: %v", err)
	}
	if _, err := f.partners.Accept(ctx, carol, fresh.Code); !errors.Is(err, service.ErrInviteInvalid) {
		t.Errorf("Accept of a used code: err = %v, want ErrInviteInvalid", err)
	}
	if _, err := f.partners.Invite(ctx, alice); !errors.Is(err, service.ErrPartnerAlreadyLinked) {
		t.Errorf("Invite while linked: err = %v, want ErrPartnerAlreadyLinked", err)
	}
	if _, err := f.partners.Invite(ctx, bob); !errors.Is(err, service.ErrPartnerAlreadyLinked) {
		t.Errorf("Invite by the accepting side while linked: err = %v, want ErrPartnerAlreadyLinked", err)
	}
}

func TestPartnersAcceptWhileLinkedIsAConflictWhateverTheCode(t *testing.T) {
	f := newPartnersFixture(t)
	ctx := context.Background()
	alice := newNamedUser(t, f.st, "alice3@example.com", "Alice")
	bob := newNamedUser(t, f.st, "bob3@example.com", "Bob")
	carol := newNamedUser(t, f.st, "carol3@example.com", "Carol")
	f.link(t, alice, bob)
	carolInvite, err := f.partners.Invite(ctx, carol)
	if err != nil {
		t.Fatalf("Invite: %v", err)
	}

	for name, code := range map[string]string{"a valid code": carolInvite.Code, "a wrong code": "ZZZZZZZZ"} {
		if _, err := f.partners.Accept(ctx, alice, code); !errors.Is(err, service.ErrPartnerAlreadyLinked) {
			t.Errorf("a linked caller accepting %s: err = %v, want ErrPartnerAlreadyLinked (no oracle for guessing codes)", name, err)
		}
	}
}

func TestPartnersAcceptDropsTheCallersOwnPendingInvite(t *testing.T) {
	f := newPartnersFixture(t)
	ctx := context.Background()
	xena := newNamedUser(t, f.st, "xena4@example.com", "Xena")
	yuri := newNamedUser(t, f.st, "yuri4@example.com", "Yuri")
	zed := newNamedUser(t, f.st, "zed4@example.com", "Zed")

	xenaInvite, err := f.partners.Invite(ctx, xena)
	if err != nil {
		t.Fatalf("Invite by Xena: %v", err)
	}
	yuriInvite, err := f.partners.Invite(ctx, yuri)
	if err != nil {
		t.Fatalf("Invite by Yuri: %v", err)
	}
	if _, err := f.partners.Accept(ctx, xena, yuriInvite.Code); err != nil {
		t.Fatalf("Xena accepting Yuri's code: %v", err)
	}

	if _, err := f.partners.Accept(ctx, zed, xenaInvite.Code); !errors.Is(err, service.ErrInviteInvalid) {
		t.Errorf("Accept of Xena's old code after she linked: err = %v, want ErrInviteInvalid (not a conflict that would reveal the code was once valid)", err)
	}
	got, err := f.partners.Get(ctx, xena)
	if err != nil || got.Status != "active" || got.DisplayName == nil || *got.DisplayName != "Yuri" {
		t.Errorf("Xena's partnership = %+v (err %v), want the single active one with Yuri", got, err)
	}
}

func TestPartnersConcurrentAcceptsOfOneCodeHaveExactlyOneWinner(t *testing.T) {
	f := newPartnersFixture(t)
	ctx := context.Background()
	inviter := newNamedUser(t, f.st, "inviter5@example.com", "Inviter")
	inv, err := f.partners.Invite(ctx, inviter)
	if err != nil {
		t.Fatalf("Invite: %v", err)
	}

	const racers = 6
	users := make([]uuid.UUID, racers)
	for i := range users {
		users[i] = newNamedUser(t, f.st, "racer5-"+string(rune('a'+i))+"@example.com", "Racer")
	}
	errs := make([]error, racers)
	var wg sync.WaitGroup
	for i := range racers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, errs[i] = f.partners.Accept(ctx, users[i], inv.Code)
		}()
	}
	wg.Wait()

	winners := 0
	for i, err := range errs {
		switch {
		case err == nil:
			winners++
		case !errors.Is(err, service.ErrInviteInvalid) && !errors.Is(err, service.ErrPartnerAlreadyLinked):
			t.Errorf("racer %d: unexpected error %v", i, err)
		}
	}
	if winners != 1 {
		t.Fatalf("%d accepts of one code succeeded, want exactly 1", winners)
	}
}

func TestPartnersCrossedAcceptsLinkTwoUsersOnlyOnce(t *testing.T) {
	// Alice accepts Bob's code while Bob accepts Alice's. Each activation
	// would satisfy the unique indexes on its own (they sit in different
	// columns), so only Accept's locks and re-checks keep a user out of two
	// active rows.
	for round := range 5 {
		f := newPartnersFixture(t)
		ctx := context.Background()
		alice := newNamedUser(t, f.st, "alice6@example.com", "Alice")
		bob := newNamedUser(t, f.st, "bob6@example.com", "Bob")
		aliceInvite, err := f.partners.Invite(ctx, alice)
		if err != nil {
			t.Fatalf("round %d: Invite by Alice: %v", round, err)
		}
		bobInvite, err := f.partners.Invite(ctx, bob)
		if err != nil {
			t.Fatalf("round %d: Invite by Bob: %v", round, err)
		}

		var wg sync.WaitGroup
		errs := make([]error, 2)
		wg.Add(2)
		go func() { defer wg.Done(); _, errs[0] = f.partners.Accept(ctx, alice, bobInvite.Code) }()
		go func() { defer wg.Done(); _, errs[1] = f.partners.Accept(ctx, bob, aliceInvite.Code) }()
		wg.Wait()

		winners := 0
		for _, err := range errs {
			if err == nil {
				winners++
			}
		}
		if winners != 1 {
			t.Fatalf("round %d: %d of the two crossed accepts succeeded (errors %v), want exactly 1", round, winners, errs)
		}
		// Deleting Alice's partnership rows shows how many she is in.
		rows, err := f.st.DeletePartnershipsForUser(ctx, alice)
		if err != nil || len(rows) != 1 {
			t.Fatalf("round %d: Alice is in %d partnership rows (err %v), want 1", round, len(rows), err)
		}
	}
}

func TestPartnersUnlinkEndsTheLinkForBothSidesAndClosesStreams(t *testing.T) {
	f := newPartnersFixture(t)
	ctx := context.Background()
	alice := newNamedUser(t, f.st, "alice7@example.com", "Alice")
	bob := newNamedUser(t, f.st, "bob7@example.com", "Bob")
	f.link(t, alice, bob)

	list := uuid.New()
	bobWatching, err := f.events.Subscribe(list, bob, alice)
	if err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	defer bobWatching.Close()
	aliceWatching, err := f.events.Subscribe(list, alice, alice)
	if err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	defer aliceWatching.Close()

	if err := f.partners.Unlink(ctx, bob); err != nil {
		t.Fatalf("Unlink by the accepting side: %v", err)
	}
	for name, id := range map[string]uuid.UUID{"the accepting side": bob, "the inviter": alice} {
		if _, err := f.partners.Get(ctx, id); !errors.Is(err, service.ErrPartnerNotLinked) {
			t.Errorf("Get by %s after unlink: err = %v, want ErrPartnerNotLinked", name, err)
		}
	}
	if err := f.partners.Unlink(ctx, alice); !errors.Is(err, service.ErrPartnerNotLinked) {
		t.Errorf("second Unlink: err = %v, want ErrPartnerNotLinked", err)
	}
	if !closedWithin(bobWatching) {
		t.Error("the partner's event stream stayed open after the unlink")
	}
	if !stillOpen(aliceWatching) {
		t.Error("the owner's own event stream was closed by the unlink")
	}

	// Re-linking is a fresh invite.
	f.link(t, bob, alice)
}

func TestPartnersUnlinkCancelsAPendingInvite(t *testing.T) {
	f := newPartnersFixture(t)
	ctx := context.Background()
	alice := newNamedUser(t, f.st, "alice8@example.com", "Alice")
	bob := newNamedUser(t, f.st, "bob8@example.com", "Bob")
	inv, err := f.partners.Invite(ctx, alice)
	if err != nil {
		t.Fatalf("Invite: %v", err)
	}
	if err := f.partners.Unlink(ctx, alice); err != nil {
		t.Fatalf("cancel: %v", err)
	}
	if _, err := f.partners.Accept(ctx, bob, inv.Code); !errors.Is(err, service.ErrInviteInvalid) {
		t.Errorf("Accept of a cancelled invite: err = %v, want ErrInviteInvalid", err)
	}
}

func TestPartnersDeletingAnAccountEndsItsPartnership(t *testing.T) {
	f := newPartnersFixture(t)
	ctx := context.Background()
	alice := newNamedUser(t, f.st, "alice9@example.com", "Alice")
	bob := newNamedUser(t, f.st, "bob9@example.com", "Bob")
	f.link(t, alice, bob)

	if n, err := f.st.DeleteUser(ctx, alice); err != nil || n != 1 {
		t.Fatalf("delete user: n = %d, err = %v", n, err)
	}
	if _, err := f.partners.Get(ctx, bob); !errors.Is(err, service.ErrPartnerNotLinked) {
		t.Errorf("Get by the surviving partner: err = %v, want ErrPartnerNotLinked", err)
	}
	if _, err := f.partners.Invite(ctx, bob); err != nil {
		t.Errorf("Invite by the surviving partner: %v", err)
	}
}

// A user who is user_a of one active row and user_b of another cannot be
// caught by the unique indexes, so Accept must lock both users and look again.
// This test holds Bob's users row in another transaction, links Bob to Carol
// there, and only then lets Accept (Bob taking Alice's code) continue.
func TestPartnersAcceptWaitsForTheUserLocksAndChecksAgainAfterThem(t *testing.T) {
	f := newPartnersFixture(t)
	ctx := context.Background()
	alice := newNamedUser(t, f.st, "alice10@example.com", "Alice")
	bob := newNamedUser(t, f.st, "bob10@example.com", "Bob")
	carol := newNamedUser(t, f.st, "carol10@example.com", "Carol")
	aliceInvite, err := f.partners.Invite(ctx, alice)
	if err != nil {
		t.Fatalf("Invite by Alice: %v", err)
	}
	if _, err := f.partners.Invite(ctx, bob); err != nil {
		t.Fatalf("Invite by Bob: %v", err)
	}
	bobsInvite, err := f.st.GetPartnershipForUser(ctx, bob)
	if err != nil {
		t.Fatalf("read Bob's invite: %v", err)
	}

	locked, release := make(chan struct{}), make(chan struct{})
	var releaseOnce sync.Once
	releaseTx := func() { releaseOnce.Do(func() { close(release) }) }
	// A failed assertion below must not leave the transaction open: closing
	// the pool would wait for it forever.
	t.Cleanup(releaseTx)
	txDone := make(chan error, 1)
	go func() {
		txDone <- f.st.InTx(ctx, func(q *sqlc.Queries) error {
			if _, err := q.LockUsersForUpdate(ctx, []uuid.UUID{bob, carol}); err != nil {
				return err
			}
			close(locked)
			<-release
			// Carol takes Bob's code: Bob is now user_a of an active row.
			_, err := q.ActivatePartnership(ctx, sqlc.ActivatePartnershipParams{ID: bobsInvite.ID, UserB: &carol})
			return err
		})
	}()
	<-locked

	accepted := make(chan error, 1)
	go func() {
		_, err := f.partners.Accept(ctx, bob, aliceInvite.Code)
		accepted <- err
	}()
	select {
	case err := <-accepted:
		t.Fatalf("Accept finished (err %v) while another transaction held Bob's users row, want it to wait for the lock", err)
	case <-time.After(300 * time.Millisecond):
	}

	releaseTx()
	if err := <-txDone; err != nil {
		t.Fatalf("linking Bob and Carol: %v", err)
	}
	if err := <-accepted; !errors.Is(err, service.ErrPartnerAlreadyLinked) {
		t.Errorf("Accept after Bob got linked meanwhile: err = %v, want ErrPartnerAlreadyLinked", err)
	}
	rows, err := f.st.DeletePartnershipsForUser(ctx, bob)
	if err != nil || len(rows) != 1 {
		t.Errorf("Bob is in %d partnership rows (err %v), want 1", len(rows), err)
	}
}
