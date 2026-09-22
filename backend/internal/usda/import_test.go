package usda_test

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/InzKazik/mealplanner/backend/internal/db"
	"github.com/InzKazik/mealplanner/backend/internal/store"
	"github.com/InzKazik/mealplanner/backend/internal/testutil"
	"github.com/InzKazik/mealplanner/backend/internal/usda"
)

const fixturePage1 = `{"foods":[
	{"fdcId":1750340,"description":"Apples, fuji, with skin, raw","foodCategory":"Fruits and Fruit Juices","foodNutrients":[
		{"nutrientNumber":"208","value":64.7},{"nutrientNumber":"203","value":0.15}
	]},
	{"fdcId":9999999,"description":"Mystery Food","foodCategory":"Some Future Category","foodNutrients":[
		{"nutrientNumber":"208","value":10}
	]}
]}`

func fixtureServer(t *testing.T) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Query().Get("pageNumber") == "1" {
			_, _ = w.Write([]byte(fixturePage1))
			return
		}
		_, _ = w.Write([]byte(`{"foods":[]}`))
	}))
	t.Cleanup(server.Close)
	return server
}

func TestImportIsIdempotentAndMapsUnknownCategoriesToOther(t *testing.T) {
	pool, err := db.Connect(context.Background(), testutil.NewMigratedDatabase(t))
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(pool.Close)
	st := store.New(pool)

	server := fixtureServer(t)
	client := usda.NewClient("test-key")
	client.BaseURL = server.URL
	client.HTTP = server.Client()
	logger := slog.New(slog.DiscardHandler)

	stats, err := usda.Import(context.Background(), st, client, logger)
	if err != nil {
		t.Fatalf("Import: %v", err)
	}
	if stats.Imported != 2 || stats.UnknownCategory != 1 {
		t.Errorf("stats = %+v, want Imported=2 UnknownCategory=1", stats)
	}

	var appleCategory, mysteryCategory string
	conn, err := pool.Acquire(context.Background())
	if err != nil {
		t.Fatalf("acquire: %v", err)
	}
	defer conn.Release()
	if err := conn.QueryRow(context.Background(), `SELECT category FROM ingredients WHERE usda_fdc_id = 1750340`).Scan(&appleCategory); err != nil {
		t.Fatalf("look up imported apple: %v", err)
	}
	if appleCategory != "produce" {
		t.Errorf("apple category = %q, want produce", appleCategory)
	}
	if err := conn.QueryRow(context.Background(), `SELECT category FROM ingredients WHERE usda_fdc_id = 9999999`).Scan(&mysteryCategory); err != nil {
		t.Fatalf("look up imported mystery food: %v", err)
	}
	if mysteryCategory != "other" {
		t.Errorf("mystery category = %q, want other (unmapped USDA group)", mysteryCategory)
	}

	// Rerunning is idempotent: same row count, values updated in place.
	stats2, err := usda.Import(context.Background(), st, client, logger)
	if err != nil {
		t.Fatalf("second Import: %v", err)
	}
	if stats2.Imported != 2 {
		t.Errorf("second run imported = %d, want 2 (upsert, not duplicate)", stats2.Imported)
	}
}
