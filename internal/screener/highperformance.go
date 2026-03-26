package screener

import (
	"context"
	"math"

	"github.com/rohitsharma04/stockctl/internal/config"
	"github.com/rohitsharma04/stockctl/internal/indicators"
	"github.com/rohitsharma04/stockctl/internal/marketdata"
)

// HighPerformance screens for stocks in a sustained, powerful uptrend.
// Port of highperformance.py.
type HighPerformance struct {
	cfg config.ScreenerConfig
}

func NewHighPerformance(cfg config.ScreenerConfig) *HighPerformance {
	if cfg.MinDataDays == 0 {
		cfg.MinDataDays = 756
	}
	if cfg.SMA200IncreaseDays == 0 {
		cfg.SMA200IncreaseDays = 90
	}
	if cfg.DrawdownFloor == 0 {
		cfg.DrawdownFloor = 0.70
	}
	if cfg.HighFloor == 0 {
		cfg.HighFloor = 0.75
	}
	return &HighPerformance{cfg: cfg}
}

func (h *HighPerformance) Name() string        { return "high-performance" }
func (h *HighPerformance) Description() string  { return "Sustained uptrend with consistent new highs" }

func (h *HighPerformance) Screen(_ context.Context, data []marketdata.OHLCV, _ []marketdata.OHLCV) (bool, error) {
	if len(data) < h.cfg.MinDataDays {
		return false, nil
	}

	closes := marketdata.Closes(data)
	n := len(closes)

	// Price > $5
	if closes[n-1] <= 5.0 {
		return false, nil
	}

	// SMA(200) below SMA(50) — golden cross
	sma200 := indicators.SMA(closes, 200)
	sma50 := indicators.SMA(closes, 50)
	if math.IsNaN(sma200[n-1]) || math.IsNaN(sma50[n-1]) || sma200[n-1] >= sma50[n-1] {
		return false, nil
	}

	// Close above SMA(50)
	if closes[n-1] <= sma50[n-1] {
		return false, nil
	}

	// Close > 2x the 252-day low
	tail252 := indicators.Tail(closes, 252)
	minClose := indicators.Min(tail252)
	if closes[n-1] <= 2*minClose {
		return false, nil
	}

	// Consistent max close at 4 checkpoints
	if !h.checkConsistentMaxClose(closes) {
		return false, nil
	}

	// SMA(200) monotonically increasing for 90 days
	sma200Tail := indicators.Tail(sma200, h.cfg.SMA200IncreaseDays)
	// Filter out NaN values
	var validSMA []float64
	for _, v := range sma200Tail {
		if !math.IsNaN(v) {
			validSMA = append(validSMA, v)
		}
	}
	if len(validSMA) < h.cfg.SMA200IncreaseDays || !indicators.IsMonotonicallyIncreasing(validSMA) {
		return false, nil
	}

	// Close >= 75% of 252-day high
	maxClose252 := indicators.Max(tail252)
	if closes[n-1] < h.cfg.HighFloor*maxClose252 {
		return false, nil
	}

	// Never below 70% of 126-day max in last 252 days
	if !h.checkDrawdownFloor(closes) {
		return false, nil
	}

	return true, nil
}

// checkConsistentMaxClose verifies that at 4 checkpoints (now, -126, -252, -378),
// the 126-day max equals the 252-day max — meaning new highs every 6 months.
func (h *HighPerformance) checkConsistentMaxClose(closes []float64) bool {
	n := len(closes)
	offsets := []int{0, 126, 252, 378}

	for _, offset := range offsets {
		end := n - offset
		if end < 252 {
			return false
		}
		data := closes[:end]
		dn := len(data)

		max126 := indicators.Max(data[dn-126:])
		max252 := indicators.Max(data[dn-252:])

		if max126 != max252 {
			return false
		}
	}
	return true
}

// checkDrawdownFloor ensures the close never dropped below 70% of the
// trailing 126-day max over the last 252 trading days.
func (h *HighPerformance) checkDrawdownFloor(closes []float64) bool {
	n := len(closes)
	for i := n - 252; i < n; i++ {
		start := i - 126
		if start < 0 {
			start = 0
		}
		max126 := indicators.Max(closes[start:i])
		if closes[i] < h.cfg.DrawdownFloor*max126 {
			return false
		}
	}
	return true
}
