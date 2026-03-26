package screener

import (
	"context"

	"github.com/rohitsharma04/stockctl/internal/config"
	"github.com/rohitsharma04/stockctl/internal/marketdata"
)

// FilterResult represents the outcome of a single filter check within a screener.
type FilterResult struct {
	Name      string  `json:"name"`
	Pass      bool    `json:"pass"`
	Value     float64 `json:"value"`
	Threshold float64 `json:"threshold"`
	Detail    string  `json:"detail,omitempty"`
}

// ScreenResult holds the scored result of running a screener on a stock.
type ScreenResult struct {
	Pass    bool           `json:"pass"`
	Score   float64        `json:"score"` // 0.0–1.0 (filters_passed / total_filters)
	Filters []FilterResult `json:"filters"`
}

// NewScreenResult builds a ScreenResult from a list of filter results.
func NewScreenResult(filters []FilterResult) *ScreenResult {
	passed := 0
	for _, f := range filters {
		if f.Pass {
			passed++
		}
	}
	allPass := passed == len(filters)
	score := 0.0
	if len(filters) > 0 {
		score = float64(passed) / float64(len(filters))
	}
	return &ScreenResult{
		Pass:    allPass,
		Score:   score,
		Filters: filters,
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
	screeners["breakout-caution"] = NewBreakoutCaution(bcCfg)

	hpCfg := cfg.Screeners["high_performance"]
	screeners["high-performance"] = NewHighPerformance(hpCfg)

	sbCfg := cfg.Screeners["stellar_breakout"]
	screeners["stellar-breakout"] = NewStellarBreakout(sbCfg)

	dbCfg := cfg.Screeners["descending_breakout"]
	screeners["descending-breakout"] = NewDescendingBreakout(dbCfg)

	return screeners
}
