// Package service holds business rules. It calls the store and never speaks HTTP.
package service

import (
	"math"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
)

// uniqueUUIDs returns the distinct ids get extracts from items, in
// first-seen order. Shared by every domain that needs to dedupe a batch of
// referenced ids before a single batch lookup: meals' ingredient lines, a
// diet template's slot meal references, and a plan range's entry meal
// references.
func uniqueUUIDs[T any](items []T, get func(T) uuid.UUID) []uuid.UUID {
	seen := make(map[uuid.UUID]bool, len(items))
	ids := make([]uuid.UUID, 0, len(items))
	for _, it := range items {
		id := get(it)
		if !seen[id] {
			seen[id] = true
			ids = append(ids, id)
		}
	}
	return ids
}

// toPgDate and fromPgDate convert between the service layer's plain
// time.Time (used on every public Diet Templates/Plan type, per the plan's
// interfaces) and sqlc's pgtype.Date (the generated type for
// plan_entries.date, the only "date" column either domain has —
// template_slots targets a day by day_index, an integer, not a date).
// plan_entries.sql.go does not use time.Time directly for that column, so
// every service that reads or writes plan_entries needs this conversion at
// the store boundary.
func toPgDate(t time.Time) pgtype.Date {
	return pgtype.Date{Time: t, Valid: true}
}

func fromPgDate(d pgtype.Date) time.Time {
	return d.Time
}

// toInt32 clamps a caller-supplied int to the int32 range before it reaches
// a sqlc int32 column parameter (day_count, day_index), the same
// overflow-avoidance pattern ingredients.go's toRowLimit already uses
// (gosec G115): a plain int(n) conversion is flagged wherever gosec can't
// prove n is already in range, which a bare struct field coming from a
// request body never is.
func toInt32(n int) int32 {
	switch {
	case n > math.MaxInt32:
		return math.MaxInt32
	case n < math.MinInt32:
		return math.MinInt32
	default:
		return int32(n)
	}
}
