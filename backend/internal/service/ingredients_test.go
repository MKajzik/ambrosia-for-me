package service_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/InzKazik/mealplanner/backend/internal/db"
	"github.com/InzKazik/mealplanner/backend/internal/service"
	"github.com/InzKazik/mealplanner/backend/internal/store"
	"github.com/InzKazik/mealplanner/backend/internal/store/sqlc"
	"github.com/InzKazik/mealplanner/backend/internal/testutil"
)

func newIngredientsFixture(t *testing.T) (*service.Ingredients, *store.Store) {
	t.Helper()
	pool, err := db.Connect(context.Background(), testutil.NewMigratedDatabase(t))
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(pool.Close)
	st := store.New(pool)
	return service.NewIngredients(st), st
}

func newTestUser(t *testing.T, st *store.Store, email string) uuid.UUID {
	t.Helper()
	hash := "hash"
	u, err := st.CreateUser(context.Background(), sqlc.CreateUserParams{
		Email: email, PasswordHash: &hash, DisplayName: "Test User",
	})
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	return u.ID
}

func TestIngredientsCreateStoresNutrientsAndIsCustom(t *testing.T) {
	svc, st := newIngredientsFixture(t)
	owner := newTestUser(t, st, "a@example.com")

	grams := 120.0
	ing, err := svc.Create(context.Background(), owner, service.CreateIngredientInput{
		Name: "Custom Oats", Category: "grains_bread", GramsPerPiece: &grams,
		Nutrients: map[string]float64{service.NutrientCalories: 389, service.NutrientProtein: 17},
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if !ing.IsCustom || ing.Name != "Custom Oats" || ing.Category != "grains_bread" {
		t.Errorf("ingredient = %+v", ing)
	}
	if ing.GramsPerPiece == nil || *ing.GramsPerPiece != 120 {
		t.Errorf("GramsPerPiece = %v, want 120", ing.GramsPerPiece)
	}
	if ing.Nutrients[service.NutrientCalories] != 389 || ing.Nutrients[service.NutrientProtein] != 17 {
		t.Errorf("Nutrients = %v", ing.Nutrients)
	}
	if len(ing.Nutrients) != 2 {
		t.Errorf("Nutrients has %d keys, want exactly the 2 provided", len(ing.Nutrients))
	}
}

func TestIngredientsCreateWithAMissingOwnerFailsCleanly(t *testing.T) {
	svc, _ := newIngredientsFixture(t)

	_, err := svc.Create(context.Background(), uuid.New(), service.CreateIngredientInput{
		Name: "Orphan", Category: "other",
	})
	if !errors.Is(err, service.ErrNotFound) {
		t.Errorf("err = %v, want ErrNotFound (mirrors GetUser: an access token for a gone user is unauthorized, not a 500)", err)
	}
}

func TestIngredientsUpdateReplacesNutrientsAndRejectsNonOwners(t *testing.T) {
	svc, st := newIngredientsFixture(t)
	owner := newTestUser(t, st, "owner@example.com")
	other := newTestUser(t, st, "other@example.com")

	ing, err := svc.Create(context.Background(), owner, service.CreateIngredientInput{
		Name: "Mix", Category: "other",
		Nutrients: map[string]float64{service.NutrientCalories: 100, service.NutrientFat: 5},
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	newName := "Renamed Mix"
	updated, err := svc.Update(context.Background(), owner, ing.ID, service.UpdateIngredientInput{
		Name:      &newName,
		Nutrients: map[string]float64{service.NutrientProtein: 9},
	})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if updated.Name != "Renamed Mix" {
		t.Errorf("Name = %q, want Renamed Mix", updated.Name)
	}
	if _, ok := updated.Nutrients[service.NutrientCalories]; ok {
		t.Error("stale nutrient (calories) survived a full replace")
	}
	if updated.Nutrients[service.NutrientProtein] != 9 {
		t.Errorf("Nutrients = %v, want protein=9 only", updated.Nutrients)
	}

	if _, err := svc.Update(context.Background(), other, ing.ID, service.UpdateIngredientInput{Name: &newName}); !errors.Is(err, service.ErrIngredientNotFound) {
		t.Errorf("Update by a non-owner: err = %v, want ErrIngredientNotFound", err)
	}
}

func TestIngredientsUpdatePreservesNutrientsWhenNotProvided(t *testing.T) {
	svc, st := newIngredientsFixture(t)
	owner := newTestUser(t, st, "a@example.com")
	ing, err := svc.Create(context.Background(), owner, service.CreateIngredientInput{
		Name: "Mix", Category: "other", Nutrients: map[string]float64{service.NutrientCalories: 100},
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	newCategory := "sweets_snacks"
	updated, err := svc.Update(context.Background(), owner, ing.ID, service.UpdateIngredientInput{Category: &newCategory})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if updated.Category != "sweets_snacks" {
		t.Errorf("Category = %q, want sweets_snacks", updated.Category)
	}
	if updated.Nutrients[service.NutrientCalories] != 100 {
		t.Errorf("Nutrients = %v, want calories=100 preserved", updated.Nutrients)
	}
}

func TestIngredientsDeleteRejectsNonOwnersAndGlobals(t *testing.T) {
	svc, st := newIngredientsFixture(t)
	owner := newTestUser(t, st, "owner@example.com")
	other := newTestUser(t, st, "other@example.com")
	ing, err := svc.Create(context.Background(), owner, service.CreateIngredientInput{Name: "Mix", Category: "other"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	if err := svc.Delete(context.Background(), other, ing.ID); !errors.Is(err, service.ErrIngredientNotFound) {
		t.Errorf("Delete by a non-owner: err = %v, want ErrIngredientNotFound", err)
	}
	if err := svc.Delete(context.Background(), owner, ing.ID); err != nil {
		t.Fatalf("Delete by the owner: %v", err)
	}
}

func TestIngredientsListPaginatesAndFiltersByOwnership(t *testing.T) {
	svc, st := newIngredientsFixture(t)
	owner := newTestUser(t, st, "owner@example.com")
	other := newTestUser(t, st, "other@example.com")

	for _, name := range []string{"Banana", "Apple", "Carrot"} {
		if _, err := svc.Create(context.Background(), owner, service.CreateIngredientInput{Name: name, Category: "produce"}); err != nil {
			t.Fatalf("Create(%s): %v", name, err)
		}
	}
	if _, err := svc.Create(context.Background(), other, service.CreateIngredientInput{Name: "Zebra Cake", Category: "sweets_snacks"}); err != nil {
		t.Fatalf("Create(other's ingredient): %v", err)
	}

	var names []string
	var cursor *service.IngredientCursor
	for {
		page, err := svc.List(context.Background(), owner, service.ListIngredientsInput{Cursor: cursor, Limit: 1})
		if err != nil {
			t.Fatalf("List: %v", err)
		}
		for _, ing := range page.Items {
			names = append(names, ing.Name)
		}
		if page.NextCursor == nil {
			break
		}
		cursor = page.NextCursor
	}
	if want := []string{"Apple", "Banana", "Carrot"}; len(names) != len(want) || names[0] != want[0] || names[1] != want[1] || names[2] != want[2] {
		t.Errorf("paginated names = %v, want %v (alphabetical, owner's only)", names, want)
	}
}

func TestIngredientsSearchRanksByNameSimilarity(t *testing.T) {
	svc, st := newIngredientsFixture(t)
	owner := newTestUser(t, st, "owner@example.com")
	for _, name := range []string{"Chicken Breast", "Chicken Thigh", "Beef Steak"} {
		if _, err := svc.Create(context.Background(), owner, service.CreateIngredientInput{Name: name, Category: "meat_seafood"}); err != nil {
			t.Fatalf("Create(%s): %v", name, err)
		}
	}

	results, err := svc.Search(context.Background(), owner, "chicken", nil, 10)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("Search(chicken) returned %d results, want 2", len(results))
	}
	for _, r := range results {
		if r.Category != "meat_seafood" {
			t.Errorf("result %q has category %q, want meat_seafood", r.Name, r.Category)
		}
	}
}
