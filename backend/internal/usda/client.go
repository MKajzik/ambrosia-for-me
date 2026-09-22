package usda

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"time"
)

// Food is one Foundation Foods item as returned by the search API.
type Food struct {
	FdcID         int32
	Description   string
	FoodCategory  string
	FoodNutrients []FoodNutrient
}

// FoodNutrient is one nutrient value for a Food, per 100 g of the food.
type FoodNutrient struct {
	NutrientNumber string
	Value          float64
	UnitName       string
}

type searchResponse struct {
	Foods []struct {
		FdcID         int32  `json:"fdcId"`
		Description   string `json:"description"`
		FoodCategory  string `json:"foodCategory"`
		FoodNutrients []struct {
			NutrientNumber string  `json:"nutrientNumber"`
			Value          float64 `json:"value"`
			UnitName       string  `json:"unitName"`
		} `json:"foodNutrients"`
	} `json:"foods"`
}

const (
	defaultBaseURL = "https://api.nal.usda.gov/fdc/v1"
	pageSize       = 200
	maxAttempts    = 5
)

// Client fetches Foundation Foods from the FoodData Central API. BaseURL and
// HTTP are exported so tests can point at an httptest.Server.
type Client struct {
	BaseURL string
	APIKey  string
	HTTP    *http.Client
}

// NewClient returns a Client that calls the live FoodData Central API.
func NewClient(apiKey string) *Client {
	return &Client{BaseURL: defaultBaseURL, APIKey: apiKey, HTTP: &http.Client{Timeout: 30 * time.Second}}
}

// FetchFoundationFoods returns every food in the Foundation Foods dataset,
// paging through the search endpoint until a page comes back empty.
func (c *Client) FetchFoundationFoods(ctx context.Context) ([]Food, error) {
	var all []Food
	for page := 1; ; page++ {
		batch, err := c.fetchPage(ctx, page)
		if err != nil {
			return nil, fmt.Errorf("fetch page %d: %w", page, err)
		}
		if len(batch) == 0 {
			return all, nil
		}
		all = append(all, batch...)
	}
}

func (c *Client) fetchPage(ctx context.Context, page int) ([]Food, error) {
	u := fmt.Sprintf("%s/foods/search?dataType=Foundation&pageSize=%d&pageNumber=%d&api_key=%s",
		c.BaseURL, pageSize, page, url.QueryEscape(c.APIKey))

	var last error
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		if attempt > 1 {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(time.Duration(attempt) * time.Second):
			}
		}
		foods, retryable, err := c.doFetch(ctx, u)
		if err == nil {
			return foods, nil
		}
		last = err
		if !retryable {
			return nil, err
		}
	}
	return nil, fmt.Errorf("giving up after %d attempts: %w", maxAttempts, last)
}

func (c *Client) doFetch(ctx context.Context, u string) ([]Food, bool, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, false, err
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, true, err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500 {
		return nil, true, fmt.Errorf("unexpected status %d", resp.StatusCode)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, false, fmt.Errorf("unexpected status %d", resp.StatusCode)
	}

	var parsed searchResponse
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		return nil, false, fmt.Errorf("decode response: %w", err)
	}
	foods := make([]Food, len(parsed.Foods))
	for i, f := range parsed.Foods {
		nutrients := make([]FoodNutrient, len(f.FoodNutrients))
		for j, n := range f.FoodNutrients {
			nutrients[j] = FoodNutrient{NutrientNumber: n.NutrientNumber, Value: n.Value, UnitName: n.UnitName}
		}
		foods[i] = Food{FdcID: f.FdcID, Description: f.Description, FoodCategory: f.FoodCategory, FoodNutrients: nutrients}
	}
	return foods, false, nil
}
