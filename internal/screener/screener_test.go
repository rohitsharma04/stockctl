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

func defaultScoringConfig() config.ScoringConfig {
	return config.ScoringConfig{
		CriticalWeight: 3.0,
		MajorWeight:    2.0,
		MinorWeight:    1.0,
	}
}

func TestBreakoutCautionReturnsScreenResult(t *testing.T) {
	data := generateSyntheticOHLCV(300, 10.0, 0.002) // uptrending
	cfg := config.ScreenerConfig{}
	scr := NewBreakoutCaution(cfg, defaultScoringConfig())

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
	scr := NewHighPerformance(cfg, defaultScoringConfig())

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
	scr := NewStellarBreakout(cfg, defaultScoringConfig())

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
	scr := NewDescendingBreakout(cfg, defaultScoringConfig())

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

	scr := NewBreakoutCaution(cfg, defaultScoringConfig())
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
		{Name: "a", Pass: true, Status: StatusPass, Importance: ImportanceMajor},
		{Name: "b", Pass: false, Status: StatusFail, Importance: ImportanceMajor},
		{Name: "c", Pass: true, Status: StatusPass, Importance: ImportanceMajor},
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

// --- New tests for tri-state scoring ---

func TestNewScreenResult_WithUnknowns(t *testing.T) {
	filters := []FilterResult{
		{Name: "a", Pass: true, Status: StatusPass, Importance: ImportanceCritical},
		{Name: "b", Pass: false, Status: StatusUnknown, Importance: ImportanceMajor},
		{Name: "c", Pass: true, Status: StatusPass, Importance: ImportanceMinor},
	}
	result := NewScreenResult(filters)

	if result.Pass {
		t.Error("should not pass when unknowns are present")
	}
	// Score: 2 pass / 3 total = 0.667 (unknowns don't count as pass)
	if result.Score < 0.66 || result.Score > 0.67 {
		t.Errorf("score should be ~0.667, got %f", result.Score)
	}
	// DataConfidence: 2 valid / 3 total = 0.667
	if result.DataConfidence < 0.66 || result.DataConfidence > 0.67 {
		t.Errorf("data_confidence should be ~0.667, got %f", result.DataConfidence)
	}
	// Unknown filter's Pass should be forced to false
	for _, f := range result.Filters {
		if f.Status == StatusUnknown && f.Pass {
			t.Errorf("filter %q has status=unknown but pass=true", f.Name)
		}
	}
}

func TestNewScreenResult_WeightedScoring(t *testing.T) {
	// critical (3x) passes, major (2x) fails, minor (1x) passes
	// weighted = (3 + 1) / (3 + 2 + 1) = 4/6 = 0.667
	filters := []FilterResult{
		{Name: "a", Pass: true, Status: StatusPass, Importance: ImportanceCritical},
		{Name: "b", Pass: false, Status: StatusFail, Importance: ImportanceMajor},
		{Name: "c", Pass: true, Status: StatusPass, Importance: ImportanceMinor},
	}
	result := NewScreenResult(filters)

	if result.WeightedScore < 0.66 || result.WeightedScore > 0.67 {
		t.Errorf("weighted_score should be ~0.667 (4/6), got %f", result.WeightedScore)
	}
}

func TestDataConfidence_AllValid(t *testing.T) {
	filters := []FilterResult{
		{Name: "a", Pass: true, Status: StatusPass, Importance: ImportanceCritical},
		{Name: "b", Pass: false, Status: StatusFail, Importance: ImportanceMajor},
	}
	result := NewScreenResult(filters)

	if result.DataConfidence != 1.0 {
		t.Errorf("data_confidence should be 1.0 when no unknowns, got %f", result.DataConfidence)
	}
}

func TestDataConfidence_WithMissing(t *testing.T) {
	filters := []FilterResult{
		{Name: "a", Pass: true, Status: StatusPass, Importance: ImportanceCritical},
		{Name: "b", Pass: false, Status: StatusUnknown, Importance: ImportanceMajor},
		{Name: "c", Pass: false, Status: StatusUnknown, Importance: ImportanceMinor},
		{Name: "d", Pass: true, Status: StatusPass, Importance: ImportanceMajor},
	}
	result := NewScreenResult(filters)

	// 2 unknowns out of 4 = confidence 0.5
	if result.DataConfidence != 0.5 {
		t.Errorf("data_confidence should be 0.5, got %f", result.DataConfidence)
	}
}

func TestBreakoutCaution_NoBenchmark_RSUnknown(t *testing.T) {
	data := generateSyntheticOHLCV(300, 10.0, 0.002)
	cfg := config.ScreenerConfig{}
	scr := NewBreakoutCaution(cfg, defaultScoringConfig())

	// Pass nil benchmark — RS should be unknown, not pass
	result, err := scr.Screen(context.Background(), data, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	for _, f := range result.Filters {
		if f.Name == "relative_strength" {
			if f.Status != StatusUnknown {
				t.Errorf("relative_strength should be status=%q when no benchmark, got %q", StatusUnknown, f.Status)
			}
			if f.Pass {
				t.Error("relative_strength should NOT pass when no benchmark")
			}
			return
		}
	}
	t.Error("relative_strength filter not found in results")
}

func TestActionabilityScore(t *testing.T) {
	// All pass, all known → actionability = 1.0
	filters := []FilterResult{
		{Name: "a", Pass: true, Status: StatusPass, Importance: ImportanceCritical},
		{Name: "b", Pass: true, Status: StatusPass, Importance: ImportanceMajor},
	}
	result := NewScreenResult(filters)

	if result.ActionabilityScore != 1.0 {
		t.Errorf("actionability should be 1.0 when all pass and known, got %f", result.ActionabilityScore)
	}

	// One unknown → actionability < 1.0
	filters2 := []FilterResult{
		{Name: "a", Pass: true, Status: StatusPass, Importance: ImportanceCritical},
		{Name: "b", Pass: false, Status: StatusUnknown, Importance: ImportanceMajor},
	}
	result2 := NewScreenResult(filters2)

	if result2.ActionabilityScore >= 1.0 {
		t.Errorf("actionability should be < 1.0 with unknowns, got %f", result2.ActionabilityScore)
	}
}
