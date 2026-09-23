// Package service holds business rules. It calls the store and never speaks HTTP.
package service

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/InzKazik/mealplanner/backend/internal/store"
	"github.com/InzKazik/mealplanner/backend/internal/store/sqlc"
)

// Errors returned by ShoppingLists. Handlers map them to problem responses.
var (
	// ErrShoppingListNotFound means the list does not exist or is not owned
	// by the caller. Partner visibility is not implemented yet (see the
	// ShoppingLists doc comment).
	ErrShoppingListNotFound = errors.New("shopping list not found")
	// ErrShoppingItemNotFound means the item does not exist, is not on the
	// given list, or the list is not visible to the caller.
	ErrShoppingItemNotFound = errors.New("shopping item not found")
	// ErrShoppingItemIngredientNotFound means a new item's ingredient_id does
	// not exist or is not visible to the caller.
	ErrShoppingItemIngredientNotFound = errors.New("ingredient does not exist or is not visible to you")
	// ErrShoppingItemVersionRequired means an item edit changes name,
	// quantity, unit or category without saying which version it edits.
	ErrShoppingItemVersionRequired = errors.New("version is required to change an item's name, quantity, unit or category")
)

// ShoppingItemVersionConflictError means an item edit carried a version that
// is no longer current. Current is the item as it is now, so the client can
// re-apply its change on top of it (spec §4.3).
type ShoppingItemVersionConflictError struct {
	Current ShoppingItem
}

func (e *ShoppingItemVersionConflictError) Error() string {
	return fmt.Sprintf("shopping item %s is at version %d", e.Current.ID, e.Current.Version)
}

// ShoppingItem is one line of a shopping list. Name, Quantity, Unit and
// Category are the item's own copy, not read live from the ingredient:
// IngredientID becomes nil if the ingredient is later deleted, and the item
// keeps working exactly like a free-text item.
type ShoppingItem struct {
	ID           uuid.UUID
	ListID       uuid.UUID
	IngredientID *uuid.UUID
	Name         string
	Quantity     *float64
	Unit         *string
	Category     string
	Checked      bool
	CheckedBy    *uuid.UUID
	Position     int
	Version      int
	Origin       string // "generated" | "manual"
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

// ShoppingList is a list with its items, ordered by position.
type ShoppingList struct {
	ID                uuid.UUID
	Name              string
	SharedWithPartner bool
	SourceFrom        *time.Time
	SourceTo          *time.Time
	Items             []ShoppingItem
	CreatedAt         time.Time
	UpdatedAt         time.Time
}

// ShoppingListSummary is a list without its items, for the list endpoint.
type ShoppingListSummary struct {
	ID                uuid.UUID
	Name              string
	SharedWithPartner bool
	SourceFrom        *time.Time
	SourceTo          *time.Time
	CreatedAt         time.Time
	UpdatedAt         time.Time
}

// ShoppingListCursor is an opaque position in the newest-first list.
type ShoppingListCursor struct {
	CreatedAt time.Time
	ID        uuid.UUID
}

// ListShoppingListsInput selects a page of the caller's newest-first lists.
type ListShoppingListsInput struct {
	Cursor *ShoppingListCursor
	Limit  int
}

// ShoppingListPage is one page of summaries plus the cursor for the next one
// (nil on the last page).
type ShoppingListPage struct {
	Items      []ShoppingListSummary
	NextCursor *ShoppingListCursor
}

// CreateShoppingListInput creates an empty list.
type CreateShoppingListInput struct {
	Name              string
	SharedWithPartner bool
}

// UpdateShoppingListInput is a partial update of a list's own fields.
type UpdateShoppingListInput struct {
	Name              *string
	SharedWithPartner *bool
}

// CreateShoppingItemInput adds a manual item. Category defaults to the
// ingredient's category when IngredientID is set, and to "other" otherwise.
type CreateShoppingItemInput struct {
	IngredientID *uuid.UUID
	Name         string
	Quantity     *float64
	Unit         *string
	Category     *string
}

// UpdateShoppingItemInput is a partial item update. Changing Name, Quantity,
// Unit or Category is an edit and needs Version (optimistic concurrency);
// changing only Checked is last-write-wins and ignores Version (spec §4.3).
type UpdateShoppingItemInput struct {
	Version  *int
	Name     *string
	Quantity Optional[float64]
	Unit     Optional[string]
	Category *string
	Checked  *bool
}

// ShoppingLists implements shopping lists owned by a single user, their
// generation from the plan, item edits with optimistic concurrency, and the
// live event streams. Partner sharing is not implemented: shared_with_partner
// is stored, but every read and write here checks owner_id only, and only
// the owner's own edits reach a list's event streams. See the "Not built
// yet" note in backend/CLAUDE.md.
type ShoppingLists struct {
	st     *store.Store
	events *ListEventHub
}

// NewShoppingLists returns a ShoppingLists service that publishes to events.
func NewShoppingLists(st *store.Store, events *ListEventHub) *ShoppingLists {
	return &ShoppingLists{st: st, events: events}
}

// Create adds an empty list owned by ownerID.
func (s *ShoppingLists) Create(ctx context.Context, ownerID uuid.UUID, in CreateShoppingListInput) (ShoppingList, error) {
	row, err := s.st.CreateShoppingList(ctx, sqlc.CreateShoppingListParams{
		OwnerID: ownerID, Name: in.Name, SharedWithPartner: in.SharedWithPartner,
	})
	if store.IsForeignKeyViolation(err, "shopping_lists_owner_id_fkey") {
		// Mirrors Ingredients.Create: an access token for a user that no
		// longer exists is unauthorized, not a 500.
		return ShoppingList{}, ErrNotFound
	}
	if err != nil {
		return ShoppingList{}, fmt.Errorf("create shopping list: %w", err)
	}
	return toShoppingList(row, nil), nil
}

// Get returns a list owned by ownerID, with its items.
func (s *ShoppingLists) Get(ctx context.Context, ownerID, id uuid.UUID) (ShoppingList, error) {
	row, err := s.st.GetShoppingListForUser(ctx, sqlc.GetShoppingListForUserParams{ID: id, UserID: ownerID})
	if store.IsNotFound(err) {
		return ShoppingList{}, ErrShoppingListNotFound
	}
	if err != nil {
		return ShoppingList{}, fmt.Errorf("get shopping list: %w", err)
	}
	items, err := s.st.GetShoppingItems(ctx, id)
	if err != nil {
		return ShoppingList{}, fmt.Errorf("get shopping items: %w", err)
	}
	return toShoppingList(row, items), nil
}

// List returns a page of the caller's lists, newest first.
func (s *ShoppingLists) List(ctx context.Context, ownerID uuid.UUID, in ListShoppingListsInput) (ShoppingListPage, error) {
	if in.Limit < 1 {
		in.Limit = 1
	}
	params := sqlc.ListShoppingListsForUserParams{UserID: ownerID, RowLimit: toRowLimit(in.Limit + 1)}
	if in.Cursor != nil {
		params.HasCursor = true
		params.CursorCreatedAt = in.Cursor.CreatedAt
		params.CursorID = in.Cursor.ID
	}
	rows, err := s.st.ListShoppingListsForUser(ctx, params)
	if err != nil {
		return ShoppingListPage{}, fmt.Errorf("list shopping lists: %w", err)
	}
	var next *ShoppingListCursor
	if len(rows) > in.Limit {
		last := rows[in.Limit-1]
		next = &ShoppingListCursor{CreatedAt: last.CreatedAt, ID: last.ID}
		rows = rows[:in.Limit]
	}
	items := make([]ShoppingListSummary, len(rows))
	for i, r := range rows {
		items[i] = ShoppingListSummary{
			ID: r.ID, Name: r.Name, SharedWithPartner: r.SharedWithPartner,
			SourceFrom: fromPgDatePtr(r.SourceFrom), SourceTo: fromPgDatePtr(r.SourceTo),
			CreatedAt: r.CreatedAt, UpdatedAt: r.UpdatedAt,
		}
	}
	return ShoppingListPage{Items: items, NextCursor: next}, nil
}

// Update applies a partial update to a list owned by ownerID and tells its
// event streams the list changed.
func (s *ShoppingLists) Update(ctx context.Context, ownerID, id uuid.UUID, in UpdateShoppingListInput) (ShoppingList, error) {
	var list ShoppingList
	err := s.st.InTx(ctx, func(q *sqlc.Queries) error {
		row, err := q.UpdateShoppingList(ctx, sqlc.UpdateShoppingListParams{
			ID: id, UserID: ownerID, Name: in.Name, SharedWithPartner: in.SharedWithPartner,
		})
		if store.IsNotFound(err) {
			return ErrShoppingListNotFound
		}
		if err != nil {
			return fmt.Errorf("update shopping list: %w", err)
		}
		items, err := q.GetShoppingItems(ctx, id)
		if err != nil {
			return fmt.Errorf("get shopping items: %w", err)
		}
		list = toShoppingList(row, items)
		return nil
	})
	if err != nil {
		return ShoppingList{}, err
	}
	s.events.Publish(ListEvent{Type: ListEventListChanged, ListID: id})
	return list, nil
}

// Delete removes a list owned by ownerID (its items go with it, ON DELETE
// CASCADE) and ends its event streams with a list_deleted event.
func (s *ShoppingLists) Delete(ctx context.Context, ownerID, id uuid.UUID) error {
	n, err := s.st.DeleteShoppingList(ctx, sqlc.DeleteShoppingListParams{ID: id, UserID: ownerID})
	if err != nil {
		return fmt.Errorf("delete shopping list: %w", err)
	}
	if n == 0 {
		return ErrShoppingListNotFound
	}
	s.events.Publish(ListEvent{Type: ListEventListDeleted, ListID: id})
	return nil
}

// AddItem appends a manual item to a list owned by ownerID.
func (s *ShoppingLists) AddItem(ctx context.Context, ownerID, listID uuid.UUID, in CreateShoppingItemInput) (ShoppingItem, error) {
	var item ShoppingItem
	err := s.st.InTx(ctx, func(q *sqlc.Queries) error {
		// TouchShoppingListForUser checks ownership and takes the list row's
		// write lock, so two concurrent adds (or an add racing a
		// regeneration) cannot both read the same next position.
		if _, err := q.TouchShoppingListForUser(ctx, sqlc.TouchShoppingListForUserParams{ID: listID, UserID: ownerID}); err != nil {
			if store.IsNotFound(err) {
				return ErrShoppingListNotFound
			}
			return fmt.Errorf("get shopping list: %w", err)
		}
		category := "other"
		if in.IngredientID != nil {
			rows, err := q.GetIngredientsForUser(ctx, sqlc.GetIngredientsForUserParams{Ids: []uuid.UUID{*in.IngredientID}, UserID: &ownerID})
			if err != nil {
				return fmt.Errorf("get ingredient: %w", err)
			}
			if len(rows) == 0 {
				return ErrShoppingItemIngredientNotFound
			}
			category = rows[0].Category
		}
		if in.Category != nil {
			category = *in.Category
		}
		pos, err := q.NextShoppingItemPosition(ctx, listID)
		if err != nil {
			return fmt.Errorf("next item position: %w", err)
		}
		row, err := q.InsertShoppingItem(ctx, sqlc.InsertShoppingItemParams{
			ListID: listID, IngredientID: in.IngredientID, Name: in.Name, Quantity: in.Quantity, Unit: in.Unit,
			Category: category, Position: pos, Origin: "manual",
		})
		// The ingredient was visible a moment ago in this same transaction;
		// a concurrent delete of it can still win the race. Answer as if it
		// never existed rather than with a raw foreign-key 500.
		if store.IsForeignKeyViolation(err, "shopping_items_ingredient_id_fkey") {
			return ErrShoppingItemIngredientNotFound
		}
		if err != nil {
			return fmt.Errorf("insert shopping item: %w", err)
		}
		item = toShoppingItem(row)
		return nil
	})
	if err != nil {
		return ShoppingItem{}, err
	}
	s.publishItem(ListEventItemChanged, item.ListID, item.ID, item.Version)
	return item, nil
}

// UpdateItem edits or checks off an item on a list owned by ownerID.
//
// An edit (Name, Quantity, Unit or Category present) needs in.Version and
// fails with *ShoppingItemVersionConflictError, carrying the current item,
// if the version is stale. A request that only sets Checked is
// last-write-wins: its Version is ignored, and setting Checked to the value
// it already has changes nothing (no version bump, no event), so retries
// and offline replays are safe. Every real change bumps the version by one
// and returns the item with its new version for the client to chain on.
func (s *ShoppingLists) UpdateItem(ctx context.Context, ownerID, listID, itemID uuid.UUID, in UpdateShoppingItemInput) (ShoppingItem, error) {
	isEdit := in.Name != nil || in.Quantity.Specified || in.Unit.Specified || in.Category != nil
	if isEdit && in.Version == nil {
		return ShoppingItem{}, ErrShoppingItemVersionRequired
	}
	var (
		item    ShoppingItem
		changed bool
	)
	err := s.st.InTx(ctx, func(q *sqlc.Queries) error {
		// FOR UPDATE: the version check below and the UPDATE after it must
		// see the same row, so a concurrent edit waits here instead of
		// slipping in between them.
		cur, err := q.GetShoppingItemForUserForUpdate(ctx, sqlc.GetShoppingItemForUserForUpdateParams{
			ID: itemID, ListID: listID, UserID: ownerID,
		})
		if store.IsNotFound(err) {
			return ErrShoppingItemNotFound
		}
		if err != nil {
			return fmt.Errorf("get shopping item: %w", err)
		}
		if isEdit && int(cur.Version) != *in.Version {
			return &ShoppingItemVersionConflictError{Current: toShoppingItem(cur)}
		}
		if !isEdit && (in.Checked == nil || *in.Checked == cur.Checked) {
			item = toShoppingItem(cur)
			return nil
		}
		row, err := q.UpdateShoppingItem(ctx, sqlc.UpdateShoppingItemParams{
			ID: itemID, Name: in.Name,
			SetQuantity: in.Quantity.Specified, Quantity: in.Quantity.Value,
			SetUnit: in.Unit.Specified, Unit: in.Unit.Value,
			Category: in.Category, Checked: in.Checked, CheckedBy: &ownerID,
		})
		if err != nil {
			return fmt.Errorf("update shopping item: %w", err)
		}
		item, changed = toShoppingItem(row), true
		return nil
	})
	if err != nil {
		return ShoppingItem{}, err
	}
	if changed {
		s.publishItem(ListEventItemChanged, item.ListID, item.ID, item.Version)
	}
	return item, nil
}

// DeleteItem removes an item from a list owned by ownerID, whatever its
// version: removes merge (spec §4.3), so a remove is never a conflict.
func (s *ShoppingLists) DeleteItem(ctx context.Context, ownerID, listID, itemID uuid.UUID) error {
	row, err := s.st.DeleteShoppingItemForUser(ctx, sqlc.DeleteShoppingItemForUserParams{
		ID: itemID, ListID: listID, UserID: ownerID,
	})
	if store.IsNotFound(err) {
		return ErrShoppingItemNotFound
	}
	if err != nil {
		return fmt.Errorf("delete shopping item: %w", err)
	}
	s.publishItem(ListEventItemDeleted, listID, row.ID, int(row.Version))
	return nil
}

// Subscribe opens an event stream for a list owned by ownerID. It subscribes
// before checking ownership, so a delete that commits between the two can
// never be missed: either the check sees the list gone (404), or the
// subscription is already registered when the list_deleted event is
// published.
func (s *ShoppingLists) Subscribe(ctx context.Context, ownerID, listID uuid.UUID) (*ListSubscription, error) {
	sub, err := s.events.Subscribe(listID)
	if err != nil {
		return nil, err
	}
	if _, err := s.st.GetShoppingListForUser(ctx, sqlc.GetShoppingListForUserParams{ID: listID, UserID: ownerID}); err != nil {
		sub.Close()
		if store.IsNotFound(err) {
			return nil, ErrShoppingListNotFound
		}
		return nil, fmt.Errorf("get shopping list: %w", err)
	}
	return sub, nil
}

func (s *ShoppingLists) publishItem(typ string, listID, itemID uuid.UUID, version int) {
	s.events.Publish(ListEvent{Type: typ, ListID: listID, ItemID: &itemID, Version: &version})
}

func toShoppingList(row sqlc.ShoppingList, itemRows []sqlc.ShoppingItem) ShoppingList {
	items := make([]ShoppingItem, len(itemRows))
	for i, r := range itemRows {
		items[i] = toShoppingItem(r)
	}
	return ShoppingList{
		ID: row.ID, Name: row.Name, SharedWithPartner: row.SharedWithPartner,
		SourceFrom: fromPgDatePtr(row.SourceFrom), SourceTo: fromPgDatePtr(row.SourceTo),
		Items: items, CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt,
	}
}

func toShoppingItem(r sqlc.ShoppingItem) ShoppingItem {
	return ShoppingItem{
		ID: r.ID, ListID: r.ListID, IngredientID: r.IngredientID, Name: r.Name,
		Quantity: r.Quantity, Unit: r.Unit, Category: r.Category,
		Checked: r.Checked, CheckedBy: r.CheckedBy, Position: int(r.Position), Version: int(r.Version),
		Origin: r.Origin, CreatedAt: r.CreatedAt, UpdatedAt: r.UpdatedAt,
	}
}

// fromPgDatePtr is fromPgDate for a nullable date column: nil when the
// column is NULL.
func fromPgDatePtr(d pgtype.Date) *time.Time {
	if !d.Valid {
		return nil
	}
	t := d.Time
	return &t
}
