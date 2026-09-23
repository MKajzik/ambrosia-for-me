// Package service holds business rules. It calls the store and never speaks HTTP.
package service

import (
	"context"
	"errors"
	"fmt"
	"sort"
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

// GenerateShoppingListInput selects the plan range to shop for. With ListID
// nil a new list is created (named Name, or a default); with ListID set,
// that list's generated items are replaced and Name is ignored.
type GenerateShoppingListInput struct {
	From   time.Time
	To     time.Time
	Name   *string
	ListID *uuid.UUID
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

// Generate builds generated items from the caller's plan entries in [From,
// To] inclusive (at most maxPlanRangeDays days, like GET /plan). Without
// ListID it creates a new list and reports created = true. With ListID it
// replaces that list's generated items, keeps its manual items untouched,
// records the new source range, and tells the list's event streams.
func (s *ShoppingLists) Generate(ctx context.Context, ownerID uuid.UUID, in GenerateShoppingListInput) (list ShoppingList, created bool, err error) {
	if in.To.Before(in.From) {
		return ShoppingList{}, false, ErrPlanRangeInvalid
	}
	if diffDays := int(in.To.Sub(in.From).Hours() / 24); diffDays >= maxPlanRangeDays {
		return ShoppingList{}, false, ErrPlanRangeTooLong
	}
	created = in.ListID == nil

	err = s.st.InTx(ctx, func(q *sqlc.Queries) error {
		var row sqlc.ShoppingList
		if created {
			name := fmt.Sprintf("Shopping %s to %s", in.From.Format(time.DateOnly), in.To.Format(time.DateOnly))
			if in.Name != nil {
				name = *in.Name
			}
			var err error
			row, err = q.CreateShoppingList(ctx, sqlc.CreateShoppingListParams{
				OwnerID: ownerID, Name: name, SourceFrom: toPgDate(in.From), SourceTo: toPgDate(in.To),
			})
			if store.IsForeignKeyViolation(err, "shopping_lists_owner_id_fkey") {
				return ErrNotFound
			}
			if err != nil {
				return fmt.Errorf("create shopping list: %w", err)
			}
		} else {
			// SetShoppingListSourceForUser is an UPDATE, so besides recording
			// the new range it takes the list row's write lock before the
			// DELETE+INSERT below: two concurrent regenerations of one list
			// serialize instead of both inserting a full generated set (the
			// second one's DELETE cannot see rows the first one inserts).
			// Same fix as TouchMealForUser / TouchDietTemplateForUser.
			var err error
			row, err = q.SetShoppingListSourceForUser(ctx, sqlc.SetShoppingListSourceForUserParams{
				ID: *in.ListID, UserID: ownerID, SourceFrom: toPgDate(in.From), SourceTo: toPgDate(in.To),
			})
			if store.IsNotFound(err) {
				return ErrShoppingListNotFound
			}
			if err != nil {
				return fmt.Errorf("set shopping list source: %w", err)
			}
			if err := q.DeleteGeneratedShoppingItems(ctx, row.ID); err != nil {
				return fmt.Errorf("clear generated items: %w", err)
			}
		}

		lines, err := generateLines(ctx, q, ownerID, in.From, in.To)
		if err != nil {
			return err
		}
		// Generated items go after whatever manual items the list keeps.
		base, err := q.NextShoppingItemPosition(ctx, row.ID)
		if err != nil {
			return fmt.Errorf("next item position: %w", err)
		}
		for i, l := range lines {
			ingredientID, quantity, unit := l.IngredientID, l.Quantity, l.Unit
			if _, err := q.InsertShoppingItem(ctx, sqlc.InsertShoppingItemParams{
				ListID: row.ID, IngredientID: &ingredientID, Name: l.Name, Quantity: &quantity, Unit: &unit,
				Category: l.Category, Position: base + toInt32(i), Origin: "generated",
			}); err != nil {
				return fmt.Errorf("insert generated item: %w", err)
			}
		}
		items, err := q.GetShoppingItems(ctx, row.ID)
		if err != nil {
			return fmt.Errorf("get shopping items: %w", err)
		}
		list = toShoppingList(row, items)
		return nil
	})
	if err != nil {
		return ShoppingList{}, false, err
	}
	if !created {
		s.events.Publish(ListEvent{Type: ListEventListChanged, ListID: list.ID})
	}
	return list, created, nil
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

// generatedLine is one item Generate is about to write.
type generatedLine struct {
	IngredientID uuid.UUID
	Name         string
	Category     string
	Quantity     float64
	Unit         string
}

// generateLines sums every ingredient line of every meal scheduled in [from,
// to] (each scaled by the entry's portion over the meal's servings, the same
// per-serving rule Plan uses for nutrition), merges them per ingredient with
// mergeUnits, and orders the result by category, then name, then unit.
func generateLines(ctx context.Context, q *sqlc.Queries, ownerID uuid.UUID, from, to time.Time) ([]generatedLine, error) {
	entries, err := q.GetPlanEntriesForUserInRange(ctx, sqlc.GetPlanEntriesForUserInRangeParams{
		UserID: ownerID, FromDate: toPgDate(from), ToDate: toPgDate(to),
	})
	if err != nil {
		return nil, fmt.Errorf("get plan entries: %w", err)
	}
	if len(entries) == 0 {
		return nil, nil
	}

	mealIDs := uniqueUUIDs(entries, func(e sqlc.PlanEntry) uuid.UUID { return e.MealID })
	mealRows, err := q.GetMealsForUser(ctx, sqlc.GetMealsForUserParams{Ids: mealIDs, UserID: ownerID})
	if err != nil {
		return nil, fmt.Errorf("get meals: %w", err)
	}
	servings := make(map[uuid.UUID]float64, len(mealRows))
	for _, m := range mealRows {
		servings[m.ID] = m.Servings
	}
	lineRows, err := q.GetMealIngredientsForMeals(ctx, mealIDs)
	if err != nil {
		return nil, fmt.Errorf("get meal ingredients: %w", err)
	}
	linesByMeal := make(map[uuid.UUID][]sqlc.MealIngredient, len(mealIDs))
	for _, l := range lineRows {
		linesByMeal[l.MealID] = append(linesByMeal[l.MealID], l)
	}
	ingredientIDs := uniqueUUIDs(lineRows, func(l sqlc.MealIngredient) uuid.UUID { return l.IngredientID })
	if len(ingredientIDs) == 0 {
		return nil, nil
	}
	ingredientRows, err := q.GetIngredientsForUser(ctx, sqlc.GetIngredientsForUserParams{Ids: ingredientIDs, UserID: &ownerID})
	if err != nil {
		return nil, fmt.Errorf("get ingredients: %w", err)
	}
	ingredients := make(map[uuid.UUID]sqlc.Ingredient, len(ingredientRows))
	for _, ing := range ingredientRows {
		ingredients[ing.ID] = ing
	}

	// sums[ingredient][unit] is the total quantity needed in that unit.
	sums := make(map[uuid.UUID]map[string]float64, len(ingredientIDs))
	for _, e := range entries {
		mealServings, ok := servings[e.MealID]
		if !ok {
			// plan_entries.meal_id is a NO ACTION foreign key and plan
			// entries are owner-only, so every entry's meal exists and is the
			// caller's own; this is a defensive check, not a reachable path.
			return nil, fmt.Errorf("meal %s of a plan entry is not visible to its owner", e.MealID)
		}
		for _, l := range linesByMeal[e.MealID] {
			if sums[l.IngredientID] == nil {
				sums[l.IngredientID] = make(map[string]float64, 1)
			}
			sums[l.IngredientID][l.Unit] += l.Quantity * e.Portion / mealServings
		}
	}

	var lines []generatedLine
	for ingredientID, byUnit := range sums {
		ing, ok := ingredients[ingredientID]
		if !ok {
			// Unreachable for the same reason as ErrMealIngredientNotFound
			// in meals.go: an ingredient a meal line references cannot be
			// deleted, and a custom ingredient never changes owner.
			return nil, ErrMealIngredientNotFound
		}
		merged, err := mergeUnits(ing, byUnit)
		if err != nil {
			return nil, err
		}
		lines = append(lines, merged...)
	}
	sort.Slice(lines, func(i, j int) bool {
		a, b := lines[i], lines[j]
		if a.Category != b.Category {
			return a.Category < b.Category
		}
		if a.Name != b.Name {
			return a.Name < b.Name
		}
		return a.Unit < b.Unit
	})
	return lines, nil
}

// mergeUnits turns one ingredient's per-unit totals into shopping lines.
// When every line used the same unit, that unit is kept as is (six eggs stay
// "6 piece", not "300 g"). When units are mixed, every total that can be
// converted to grams (with gramsFor, the same conversion meal nutrition
// uses) is merged into one "g" line, and every total that cannot (the
// ingredient lacks grams_per_piece or density_g_per_ml) stays a line of its
// own in its own unit rather than being merged incorrectly.
func mergeUnits(ing sqlc.Ingredient, byUnit map[string]float64) ([]generatedLine, error) {
	line := func(unit string, quantity float64) generatedLine {
		return generatedLine{IngredientID: ing.ID, Name: ing.Name, Category: ing.Category, Quantity: quantity, Unit: unit}
	}
	if len(byUnit) == 1 {
		for unit, quantity := range byUnit {
			return []generatedLine{line(unit, quantity)}, nil
		}
	}
	var (
		out      []generatedLine
		grams    float64
		hasGrams bool
	)
	// A fixed unit order keeps the floating-point sum deterministic.
	for _, unit := range []string{"g", "ml", "piece"} {
		quantity, ok := byUnit[unit]
		if !ok {
			continue
		}
		g, err := gramsFor(quantity, unit, ing.GramsPerPiece, ing.DensityGPerMl)
		if errors.Is(err, ErrUnitNotConvertible) {
			out = append(out, line(unit, quantity))
			continue
		}
		if err != nil {
			return nil, err
		}
		grams += g
		hasGrams = true
	}
	if hasGrams {
		out = append(out, line("g", grams))
	}
	return out, nil
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
