package service_test

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/InzKazik/mealplanner/backend/internal/auth"
	"github.com/InzKazik/mealplanner/backend/internal/db"
	"github.com/InzKazik/mealplanner/backend/internal/service"
	"github.com/InzKazik/mealplanner/backend/internal/store"
	"github.com/InzKazik/mealplanner/backend/internal/store/sqlc"
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
	kcal, protein, carbs, fat := 2200.0, 150.0, 250.0, 70.0

	u, err := f.svc.UpdateUser(ctx, s.User.ID, service.UpdateInput{
		DisplayName:    ptr("Alice"),
		TargetKcal:     service.Set(&kcal),
		TargetProteinG: service.Set(&protein),
		TargetCarbsG:   service.Set(&carbs),
		TargetFatG:     service.Set(&fat),
	})
	if err != nil {
		t.Fatalf("UpdateUser: %v", err)
	}
	if u.DisplayName != "Alice" || u.TargetKcal == nil || *u.TargetKcal != 2200 || u.TargetProteinG == nil || *u.TargetProteinG != 150 {
		t.Errorf("after first update: %+v", u)
	}
	if u.TargetCarbsG == nil || *u.TargetCarbsG != 250 || u.TargetFatG == nil || *u.TargetFatG != 70 {
		t.Errorf("after first update, carbs and fat: %+v", u)
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
	if u.TargetCarbsG == nil || *u.TargetCarbsG != 250 || u.TargetFatG == nil || *u.TargetFatG != 70 {
		t.Errorf("carbs and fat should be unchanged: %+v", u)
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

func TestConcurrentRefreshOfOneTokenSucceedsExactlyOnce(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	first := register(t, f, "alice@example.com")

	const numGoroutines = 8
	var (
		wg      sync.WaitGroup
		barrier = make(chan struct{})
		results = make([]struct {
			session service.Session
			err     error
		}, numGoroutines)
	)

	wg.Add(numGoroutines)
	for i := 0; i < numGoroutines; i++ {
		go func(idx int) {
			defer wg.Done()
			<-barrier // Wait for all goroutines to be ready
			sess, err := f.svc.Refresh(ctx, first.RefreshToken)
			results[idx].session = sess
			results[idx].err = err
		}(i)
	}

	close(barrier) // Release all goroutines at once
	wg.Wait()

	// Count successes
	var successCount int
	var winnerToken string
	for _, r := range results {
		if r.err == nil {
			successCount++
			winnerToken = r.session.RefreshToken
		} else if !errors.Is(r.err, service.ErrInvalidRefreshToken) {
			t.Errorf("unexpected error: %v", r.err)
		}
	}

	if successCount != 1 {
		t.Errorf("expected exactly 1 success, got %d", successCount)
	}

	// The winner's token should now also fail (family was revoked when losers detected reuse).
	if _, err := f.svc.Refresh(ctx, winnerToken); !errors.Is(err, service.ErrInvalidRefreshToken) {
		t.Errorf("winner's new token should also fail: err = %v, want ErrInvalidRefreshToken", err)
	}
}

func TestLogoutRacingWithRefreshLeavesNoLiveToken(t *testing.T) {
	// This is a probabilistic race test: we repeat 25 times to catch timing issues.
	for iteration := 0; iteration < 25; iteration++ {
		f := newFixture(t)
		ctx := context.Background()
		email := fmt.Sprintf("race%d@example.com", iteration)
		first := register(t, f, email)

		var (
			wg            sync.WaitGroup
			barrier       = make(chan struct{})
			refreshResult struct {
				session service.Session
				err     error
			}
			logoutErr error
		)

		wg.Add(2)

		// Refresh goroutine
		go func() {
			defer wg.Done()
			<-barrier
			sess, err := f.svc.Refresh(ctx, first.RefreshToken)
			refreshResult.session = sess
			refreshResult.err = err
		}()

		// Logout goroutine
		go func() {
			defer wg.Done()
			<-barrier
			logoutErr = f.svc.Logout(ctx, first.RefreshToken)
		}()

		close(barrier)
		wg.Wait()

		// Both must not return unexpected errors
		if logoutErr != nil {
			t.Errorf("iteration %d: Logout returned unexpected error: %v", iteration, logoutErr)
		}
		if refreshResult.err != nil && !errors.Is(refreshResult.err, service.ErrInvalidRefreshToken) {
			t.Errorf("iteration %d: Refresh returned unexpected error: %v", iteration, refreshResult.err)
		}

		// If Refresh succeeded, its token must now also fail (logout must have revoked the family).
		if refreshResult.err == nil {
			if _, err := f.svc.Refresh(ctx, refreshResult.session.RefreshToken); !errors.Is(err, service.ErrInvalidRefreshToken) {
				t.Errorf("iteration %d: refresh winner's new token should also fail: err = %v", iteration, err)
			}
		}
	}
}

func TestRefreshWaitsForTheTokenRowLock(t *testing.T) {
	t.Helper()
	f := newFixture(t)
	ctx := context.Background()
	first := register(t, f, "alice@example.com")

	hash := auth.HashRefreshToken(first.RefreshToken)

	var (
		locked  = make(chan struct{})
		release = make(chan struct{})
		g1done  = make(chan error, 1)
		done    = make(chan struct {
			session service.Session
			err     error
		}, 1)
		releaseOnce sync.Once
	)

	// Ensure release is closed exactly once to prevent panic
	t.Cleanup(func() {
		releaseOnce.Do(func() {
			close(release)
		})
	})

	// G1: Hold the row lock
	go func() {
		err := f.store.InTx(ctx, func(q *sqlc.Queries) error {
			_, err := q.GetRefreshTokenByHashForUpdate(ctx, hash)
			if err != nil {
				return fmt.Errorf("get token for lock: %w", err)
			}
			close(locked)
			<-release
			return nil
		})
		g1done <- err
	}()

	// Wait for G1 to acquire the lock
	<-locked

	// G2: Try to refresh (should block waiting for the lock)
	go func() {
		sess, err := f.svc.Refresh(ctx, first.RefreshToken)
		done <- struct {
			session service.Session
			err     error
		}{sess, err}
	}()

	// Assert that Refresh is blocked (still waiting for the row lock)
	select {
	case result := <-done:
		t.Fatalf("Refresh finished while another transaction held the token's row lock: it does not lock the row. Result: %+v", result)
	case <-time.After(500 * time.Millisecond):
		// Expected: Refresh is still blocked
	}

	// Release the lock and wait for both goroutines to complete
	releaseOnce.Do(func() {
		close(release)
	})

	// G1 must complete with no error
	g1Err := <-g1done
	if g1Err != nil {
		t.Fatalf("G1 error: %v", g1Err)
	}

	// G2 must complete within 5 seconds and succeed
	select {
	case result := <-done:
		if result.err != nil {
			t.Fatalf("Refresh failed: %v", result.err)
		}
		// Verify the token was rotated (new token different from old)
		if result.session.RefreshToken == first.RefreshToken {
			t.Error("refresh token was not rotated")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Refresh did not complete within 5 seconds")
	}
}

func ptr[T any](v T) *T { return &v }
