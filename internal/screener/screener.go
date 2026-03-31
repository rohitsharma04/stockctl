package screener

import (
	"context"
	"math"

	"github.com/rohitsharma04/stockctl/internal/config"
	"github.com/rohitsharma04/stockctl/internal/marketdata"
)

// Filter status constants.
const (
	StatusPass    = "pass"
	StatusFail    = "fail"
	StatusUnknown = "unknown"
)

// Filter importance constants.
const (
	ImportanceCritical = "critical"
	ImportanceMajor    = "major"
	ImportanceMinor    = "minor"
)

// FilterResult represents the outcome of a single filter check within a screener.
type FilterResult struct {
	Name       string  `json:"name"`
	Pass       bool    `json:"pass"`
	Status     string  `json:"status"`               // "pass", "fail", "unknown"
	Importance string  `json:"importance"`            // "critical", "major", "minor"
	Value      float64 `json:"value"`
	Threshold  float64 `json:"threshold"`
	Detail     string  `json:"detail,omitempty"`
}

// ScreenResult holds the scored result of running a screener on a stock.
type ScreenResult struct {
	Pass               bool           `json:"pass"`
	Score              float64        `json:"score"`               // 0.0–1.0 (filters_passed / total_filters)
	WeightedScore      float64        `json:"weighted_score"`      // importance-weighted 0.0–1.0
	DataConfidence     float64        `json:"data_confidence"`     // 0.0–1.0 (valid data ratio)
	ActionabilityScore float64        `json:"actionability_score"` // combined quality signal
	Filters            []FilterResult `json:"filters"`
}

// sanitizeFloat replaces NaN and Inf with 0 for JSON compatibility.
func sanitizeFloat(v float64) float64 {
	if math.IsNaN(v) || math.IsInf(v, 0) {
		return 0
	}
	return v
}

// MakeFilter creates a FilterResult with explicit status and importance.
// If the underlying value is NaN/Inf, status is forced to "unknown".
func MakeFilter(name string, pass bool, value, threshold float64, importance, detail string) FilterResult {
	status := StatusFail
	if pass {
		status = StatusPass
	}
	// Force unknown when value is invalid
	if math.IsNaN(value) || math.IsInf(value, 0) {
		status = StatusUnknown
		pass = false
	}
	return FilterResult{
		Name:       name,
		Pass:       pass,
		Status:     status,
		Importance: importance,
		Value:      sanitizeFloat(value),
		Threshold:  sanitizeFloat(threshold),
		Detail:     detail,
	}
}

// MakeUnknownFilter creates a FilterResult with status "unknown".
func MakeUnknownFilter(name, importance, detail string) FilterResult {
	return FilterResult{
		Name:       name,
		Pass:       false,
		Status:     StatusUnknown,
		Importance: importance,
		Value:      0,
		Threshold:  0,
		Detail:     detail,
	}
}

// importanceWeight returns the numeric weight for an importance level.
func importanceWeight(importance string, scoring config.ScoringConfig) float64 {
	switch importance {
	case ImportanceCritical:
		return scoring.CriticalWeight
	case ImportanceMajor:
		return scoring.MajorWeight
	case ImportanceMinor:
		return scoring.MinorWeight
	default:
		return 1.0
	}
}

// NewScreenResult builds a ScreenResult from a list of filter results.
// Uses default scoring weights.
func NewScreenResult(filters []FilterResult) *ScreenResult {
	return NewScreenResultWeighted(filters, config.ScoringConfig{
		CriticalWeight: 3.0,
		MajorWeight:    2.0,
		MinorWeight:    1.0,
	})
}

// NewScreenResultWeighted builds a ScreenResult with configurable importance weights.
func NewScreenResultWeighted(filters []FilterResult, scoring config.ScoringConfig) *ScreenResult {
	passed := 0
	unknowns := 0
	weightedPassSum := 0.0
	weightedTotalSum := 0.0

	for i := range filters {
		filters[i].Value = sanitizeFloat(filters[i].Value)
		filters[i].Threshold = sanitizeFloat(filters[i].Threshold)

		w := importanceWeight(filters[i].Importance, scoring)
		weightedTotalSum += w

		switch filters[i].Status {
		case StatusPass:
			passed++
			weightedPassSum += w
		case StatusUnknown:
			unknowns++
			// unknowns do NOT contribute to pass count or weighted pass
			filters[i].Pass = false
		default:
			// StatusFail — no contribution
		}
	}

	total := len(filters)
	allPass := passed == total && unknowns == 0

	score := 0.0
	if total > 0 {
		score = float64(passed) / float64(total)
	}

	weightedScore := 0.0
	if weightedTotalSum > 0 {
		weightedScore = weightedPassSum / weightedTotalSum
	}

	dataConfidence := 1.0
	if total > 0 {
		dataConfidence = float64(total-unknowns) / float64(total)
	}

	actionability := weightedScore * dataConfidence

	return &ScreenResult{
		Pass:               allPass,
		Score:              score,
		WeightedScore:      weightedScore,
		DataConfidence:     dataConfidence,
		ActionabilityScore: actionability,
		Filters:            filters,
	}
}

// Screener is the interface that all stock screening strategies implement.
type Screener interface {
	// Name returns the screener identifier (e.g., "breakout-caution").
	Name() string

	// Description returns a human-readable description.
	Description() string

	// Screen evaluates whether a stock passes all filters.
	// data: the stock's daily OHLCV history
	// benchmark: benchmark index data (e.g., S&P 500), may be nil if not needed
	Screen(ctx context.Context, data []marketdata.OHLCV, benchmark []marketdata.OHLCV) (*ScreenResult, error)
}

// Registry returns all available screeners.
func Registry(cfg *config.Config) map[string]Screener {
	screeners := make(map[string]Screener)

	bcCfg := cfg.Screeners["breakout_caution"]
	screeners["breakout-caution"] = NewBreakoutCaution(bcCfg, cfg.Scoring)

	hpCfg := cfg.Screeners["high_performance"]
	screeners["high-performance"] = NewHighPerformance(hpCfg, cfg.Scoring)

	sbCfg := cfg.Screeners["stellar_breakout"]
	screeners["stellar-breakout"] = NewStellarBreakout(sbCfg, cfg.Scoring)

	dbCfg := cfg.Screeners["descending_breakout"]
	screeners["descending-breakout"] = NewDescendingBreakout(dbCfg, cfg.Scoring)

	screeners["rsi-bounce"] = NewRSIBounce(cfg.Scoring)
	screeners["macd-crossover"] = NewMACDCrossover(cfg.Scoring)

	return screeners
}
