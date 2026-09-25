package service_test

import (
	"context"
	"errors"
	"testing"

	"github.com/InzKazik/mealplanner/backend/internal/service"
)

// A partner resolves ingredients with their own visibility: the owner's custom
// ingredient is not theirs to reference, and the answer must not reveal it.
func TestShoppingListsPartnerCannotAddAnItemOnTheOwnersCustomIngredient(t *testing.T) {
	f := newSharedListFixture(t, "e1")
	ctx := context.Background()
	list := f.createList(t, f.alice, "Shared", true)
	secret := mustCreateIngredient(t, f.ing, f.alice, service.CreateIngredientInput{Name: "Alice's Secret Sauce", Category: "condiments_oils"})

	_, err := f.lists.AddItem(ctx, f.bob, list.ID, service.CreateShoppingItemInput{IngredientID: &secret.ID, Name: "Sauce"})
	if !errors.Is(err, service.ErrShoppingItemIngredientNotFound) {
		t.Errorf("partner AddItem on the owner's custom ingredient: err = %v, want ErrShoppingItemIngredientNotFound", err)
	}
	if _, err := f.lists.AddItem(ctx, f.alice, list.ID, service.CreateShoppingItemInput{IngredientID: &secret.ID, Name: "Sauce"}); err != nil {
		t.Errorf("the owner's AddItem on their own ingredient: %v", err)
	}
}

// When the owner deletes a shared list while the partner watches it, the
// partner's stream ends with list_deleted like the owner's.
func TestShoppingListsDeletingASharedListEndsThePartnersStream(t *testing.T) {
	f := newSharedListFixture(t, "e2")
	ctx := context.Background()
	list := f.createList(t, f.alice, "Shared", true)
	partnerSub, err := f.lists.Subscribe(ctx, f.bob, list.ID)
	if err != nil {
		t.Fatalf("partner Subscribe: %v", err)
	}
	defer partnerSub.Close()

	if err := f.lists.Delete(ctx, f.alice, list.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if ev := nextEvent(t, partnerSub); ev.Type != service.ListEventListDeleted || ev.ListID != list.ID {
		t.Errorf("partner's stream got %+v, want list_deleted for the list", ev)
	}
	if !closedWithin(partnerSub) {
		t.Error("the partner's stream stayed open after list_deleted")
	}
}
