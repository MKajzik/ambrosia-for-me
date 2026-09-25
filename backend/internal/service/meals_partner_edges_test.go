package service_test

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/InzKazik/mealplanner/backend/internal/service"
)

// A partner's meal is read-only + copy. Scheduling it directly would put a
// row in the plan that points at someone else's meal, so it is refused like
// any meal the caller cannot see: copy it first.
func TestMealsAPartnersSharedMealCannotBeScheduledInThePlanDirectly(t *testing.T) {
	meals, _, st := newMealsFixture(t)
	plan := service.NewPlan(st, meals)
	ctx := context.Background()
	alice := newTestUser(t, st, "alice-edge2@example.com")
	bob := newTestUser(t, st, "bob-edge2@example.com")
	linkPartners(t, st, alice, bob)
	shared, err := meals.Create(ctx, alice, service.CreateMealInput{Name: "Shared Dinner", Servings: 1, SharedWithPartner: true})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	date := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)

	if _, err := plan.SetEntry(ctx, bob, date, "dinner", service.SetPlanEntryInput{MealID: shared.ID, Portion: 1}); !errors.Is(err, service.ErrPlanMealNotFound) {
		t.Errorf("SetEntry with the partner's shared meal: err = %v, want ErrPlanMealNotFound", err)
	}
	cp, err := meals.Copy(ctx, bob, shared.ID)
	if err != nil {
		t.Fatalf("Copy: %v", err)
	}
	if _, err := plan.SetEntry(ctx, bob, date, "dinner", service.SetPlanEntryInput{MealID: cp.ID, Portion: 1}); err != nil {
		t.Errorf("SetEntry with Bob's own copy: %v", err)
	}
}

// Paging through the partner's meals with a small limit returns every shared
// meal exactly once, in order, and never one of the caller's own.
func TestMealsListPartnerPaginatesTheSharedMealsOnly(t *testing.T) {
	meals, _, st := newMealsFixture(t)
	ctx := context.Background()
	alice := newTestUser(t, st, "alice-edge3@example.com")
	bob := newTestUser(t, st, "bob-edge3@example.com")
	linkPartners(t, st, alice, bob)
	for name, shared := range map[string]bool{"Apple Pie": true, "Bagel": true, "Curry": true, "Dumplings": false} {
		if _, err := meals.Create(ctx, alice, service.CreateMealInput{Name: name, Servings: 1, SharedWithPartner: shared}); err != nil {
			t.Fatalf("Create %s: %v", name, err)
		}
	}
	if _, err := meals.Create(ctx, bob, service.CreateMealInput{Name: "Bob's Own", Servings: 1, SharedWithPartner: true}); err != nil {
		t.Fatalf("Create Bob's meal: %v", err)
	}

	var names []string
	var cursor *service.MealCursor
	for range 10 {
		page, err := meals.ListPartner(ctx, bob, service.ListMealsInput{Cursor: cursor, Limit: 1})
		if err != nil {
			t.Fatalf("ListPartner: %v", err)
		}
		for _, m := range page.Items {
			names = append(names, m.Name)
		}
		if page.NextCursor == nil {
			break
		}
		cursor = page.NextCursor
	}
	if want := []string{"Apple Pie", "Bagel", "Curry"}; !slices.Equal(names, want) {
		t.Errorf("paged partner meals = %v, want %v (each shared meal once, none unshared, none of Bob's own)", names, want)
	}
}
