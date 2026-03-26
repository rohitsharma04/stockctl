package screener

import (
	"context"
	"testing"
	"time"

	"github.com/rohitsharma04/stockctl/internal/config"
	"github.com/rohitsharma04/stockctl/internal/marketdata"
)

// generateSyntheticOHLCV creates synthetic daily OHLCV data for testing.
// Price starts at base and trends upward by pctPerDay.
func generateSyntheticOHLCV(days int, base, pctPerDay float64) []marketdata.OHLCV {
	data := make([]marketdata.OHLCV, days)
	start := time.Date(2021, 1, 4, 0, 0, 0, 0, time.UTC) // Monday
	price := base

	for i := 0; i < days; i++ {
		// Skip weekends
		for start.Weekday() == time.Saturday || start.Weekday() == time.Sunday {
			start = start.AddDate(0, 0, 1)
		}

		data[i] = marketdata.OHLCV{
			Date:   start,
			Open:   price,
			High:   price * 1.02,
			Low:    price * 0.98,
			Close:  price * (1 + pctPerDay),
			Volume: 1_000_000 + float64(i)*1000,
		}
		price = data[i].Close
		start = start.AddDate(0, 0, 1)
	}
	return data
}

func TestBreakoutCautionReturnsScreenResult(t *testing.T) {
	data := generateSyntheticOHLCV(300, 10.0, 0.002) // uptrending
	cfg := config.ScreenerConfig{}
	scr := NewBreakoutCaution(cfg)

	result, err := scr.Screen(context.Background(), data, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result == nil {
		t.Fatal("result should not be nil")
	}

	if len(result.Filters) == 0 {
		t.Error("result should have at least one filter")
	}

	if result.Score < 0 || result.Score > 1 {
		t.Errorf("score should be 0-1, got %f", result.Score)
	}

	// Verify filter names
	expectedNames := map[string]bool{
		"min_price": false, "momentum": false, "bollinger_breakout": false,
		"volume_spike": false, "dynamic_sma": false, "relative_strength": false,
	}
	for _, f := range result.Filters {
		if _, ok := expectedNames[f.Name]; ok {
			expectedNames[f.Name] = true
		}
	}
	for name, found := range expectedNames {
		if !found {
			t.Errorf("missing filter: %s", name)
		}
	}
}

func TestHighPerformanceReturnsScreenResult(t *testing.T) {
	data := generateSyntheticOHLCV(800, 10.0, 0.001)
	cfg := config.ScreenerConfig{}
	scr := NewHighPerformance(cfg)

	result, err := scr.Screen(context.Background(), data, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result == nil {
		t.Fatal("result should not be nil")
	}
	if len(result.Filters) == 0 {
		t.Error("result should have filters")
	}

	expectedNames := []string{"min_price", "golden_cross", "price_above_sma50",
		"double_from_low", "consistent_new_highs", "sma200_monotonic",
		"proximity_to_high", "drawdown_floor"}
	for _, name := range expectedNames {
		found := false
		for _, f := range result.Filters {
			if f.Name == name {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("missing filter: %s", name)
		}
	}
}

func TestStellarBreakoutReturnsScreenResult(t *testing.T) {
	data := generateSyntheticOHLCV(800, 10.0, 0.001)
	cfg := config.ScreenerConfig{}
	scr := NewStellarBreakout(cfg)

	result, err := scr.Screen(context.Background(), data, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result == nil {
		t.Fatal("result should not be nil")
	}
	if result.Score < 0 || result.Score > 1 {
		t.Errorf("score should be 0-1, got %f", result.Score)
	}
}

func TestDescendingBreakoutReturnsScreenResult(t *testing.T) {
	data := generateSyntheticOHLCV(800, 50.0, -0.0005) // downtrending
	cfg := config.ScreenerConfig{}
	scr := NewDescendingBreakout(cfg)

	result, err := scr.Screen(context.Background(), data, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result == nil {
		t.Fatal("result should not be nil")
	}
	if result.Score < 0 || result.Score > 1 {
		t.Errorf("score should be 0-1, got %f", result.Score)
	}
}

func TestInsufficientData(t *testing.T) {
	data := generateSyntheticOHLCV(50, 10.0, 0.001) // too few
	cfg := config.ScreenerConfig{}

	scr := NewBreakoutCaution(cfg)
	result, err := scr.Screen(context.Background(), data, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Pass {
		t.Error("should not pass with insufficient data")
	}
}

func TestNewScreenResult(t *testing.T) {
	filters := []FilterResult{
		{Name: "a", Pass: true}, {Name: "b", Pass: false}, {Name: "c", Pass: true},
	}
	result := NewScreenResult(filters)

	if result.Pass {
		t.Error("should not pass when not all filters pass")
	}
	if result.Score < 0.66 || result.Score > 0.67 {
		t.Errorf("score should be ~0.667, got %f", result.Score)
	}
	if len(result.Filters) != 3 {
		t.Errorf("expected 3 filters, got %d", len(result.Filters))
	}
}
