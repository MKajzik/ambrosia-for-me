package service_test

import (
	"context"
	"sync"
	"testing"
)

// A double tap on "invite" sends two requests at once. Both must succeed, and
// only one invite may be left alive: the newest replaces the others.
func TestPartnersConcurrentInvitesByOneUserLeaveExactlyOnePendingInvite(t *testing.T) {
	f := newPartnersFixture(t)
	ctx := context.Background()
	alice := newNamedUser(t, f.st, "alice-edge1@example.com", "Alice")

	const taps = 6
	errs := make([]error, taps)
	var wg sync.WaitGroup
	for i := range taps {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, errs[i] = f.partners.Invite(ctx, alice)
		}()
	}
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Errorf("concurrent Invite %d: %v, want every request to succeed", i, err)
		}
	}
	rows, err := f.st.DeletePartnershipsForUser(ctx, alice)
	if err != nil || len(rows) != 1 || rows[0].Status != "pending" {
		t.Errorf("Alice's partnership rows after %d concurrent invites = %+v (err %v), want exactly one pending invite", taps, rows, err)
	}
}
