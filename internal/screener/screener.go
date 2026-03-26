package screener

import (
	"context"

	"github.com/rohitsharma04/stockctl/internal/config"
	"github.com/rohitsharma04/stockctl/internal/marketdata"
)

// Screener is the interface that all stock screening strategies implement.
type Screener interface {
	// Name returns the screener identifier (e.g., "breakout-caution").
	Name() string

	// Description returns a human-readable description.
	Description() string

	// Screen evaluates whether a stock passes all filters.
	// data: the stock's daily OHLCV history
	// benchmark: benchmark index data (e.g., S&P 500), may be nil if not needed
	Screen(ctx context.Context, data []marketdata.OHLCV, benchmark []marketdata.OHLCV) (bool, error)
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
