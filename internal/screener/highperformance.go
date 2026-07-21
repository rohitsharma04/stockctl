package screener

import (
	"context"
	"fmt"
	"math"

	"github.com/rohitsharma04/stockctl/internal/config"
	"github.com/rohitsharma04/stockctl/internal/indicators"
	"github.com/rohitsharma04/stockctl/internal/marketdata"
)

// HighPerformance screens for stocks in a sustained, powerful uptrend.
type HighPerformance struct {
	cfg     config.ScreenerConfig
	scoring config.ScoringConfig
}

func NewHighPerformance(cfg config.ScreenerConfig, scoring config.ScoringConfig) *HighPerformance {
	if cfg.MinPrice == 0 {
		cfg.MinPrice = 5.0
	}
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
	return &HighPerformance{cfg: cfg, scoring: scoring}
}

func (h *HighPerformance) Name() string        { return "high-performance" }
func (h *HighPerformance) Description() string { return "Sustained uptrend with consistent new highs" }

func (h *HighPerformance) Screen(_ context.Context, data []marketdata.OHLCV, _ []marketdata.OHLCV) (*ScreenResult, error) {
	if len(data) < h.cfg.MinDataDays {
		return &ScreenResult{Pass: false, Score: 0, Filters: []FilterResult{
			MakeFilter("min_data", false, float64(len(data)), float64(h.cfg.MinDataDays), ImportanceMinor, "insufficient data"),
		}}, nil
	}

	closes := marketdata.Closes(data)
	n := len(closes)
	var filters []FilterResult

	// 1. Min price (minor)
	price := closes[n-1]
	filters = append(filters, MakeFilter(
		"min_price", price >= h.cfg.MinPrice, price, h.cfg.MinPrice, ImportanceMinor, "",
	))

	// 2. Golden cross: SMA(50) > SMA(200) (critical)
	sma200 := indicators.SMA(closes, 200)
	sma50 := indicators.SMA(closes, 50)
	if math.IsNaN(sma200[n-1]) || math.IsNaN(sma50[n-1]) {
		filters = append(filters, MakeUnknownFilter(
			"golden_cross", ImportanceCritical, "SMA data not available",
		))
	} else {
		gcPass := sma50[n-1] > sma200[n-1]
		filters = append(filters, MakeFilter(
			"golden_cross", gcPass, sma50[n-1], sma200[n-1], ImportanceCritical,
			fmt.Sprintf("SMA50=%.2f vs SMA200=%.2f", sma50[n-1], sma200[n-1]),
		))
	}

	// 3. Price above SMA(50) (major)
	if math.IsNaN(sma50[n-1]) {
		filters = append(filters, MakeUnknownFilter(
			"price_above_sma50", ImportanceMajor, "SMA50 not available",
		))
	} else {
		aboveSMA50 := closes[n-1] > sma50[n-1]
		filters = append(filters, MakeFilter(
			"price_above_sma50", aboveSMA50, closes[n-1], sma50[n-1], ImportanceMajor, "",
		))
	}

	// 4. Double from low: close > 2x the 252-day low (major)
	tail252 := indicators.Tail(closes, 252)
	minClose := indicators.Min(tail252)
	filters = append(filters, MakeFilter(
		"double_from_low", closes[n-1] > 2*minClose, closes[n-1], 2*minClose, ImportanceMajor,
		fmt.Sprintf("%.2f vs 2×%.2f=%.2f", closes[n-1], minClose, 2*minClose),
	))

	// 5. Consistent new highs at 4 checkpoints (critical)
	consistentPass := h.checkConsistentMaxClose(closes)
	filters = append(filters, MakeFilter(
		"consistent_new_highs", consistentPass, 1, 1, ImportanceCritical,
		"126d max = 252d max at 4 checkpoints",
	))

	// 6. SMA(200) monotonically increasing for 90 days (major)
	sma200Tail := indicators.Tail(sma200, h.cfg.SMA200IncreaseDays)
	var validSMA []float64
	for _, v := range sma200Tail {
		if !math.IsNaN(v) {
			validSMA = append(validSMA, v)
		}
	}
	monoPass := len(validSMA) >= h.cfg.SMA200IncreaseDays && indicators.IsMonotonicallyIncreasing(validSMA)
	filters = append(filters, MakeFilter(
		"sma200_monotonic", monoPass, float64(len(validSMA)), float64(h.cfg.SMA200IncreaseDays), ImportanceMajor,
		fmt.Sprintf("%d valid days, monotonic=%v", len(validSMA), monoPass),
	))

	// 7. Proximity to high: close >= 75% of 252-day high (minor)
	maxClose252 := indicators.Max(tail252)
	proxPass := closes[n-1] >= h.cfg.HighFloor*maxClose252
	filters = append(filters, MakeFilter(
		"proximity_to_high", proxPass, closes[n-1], h.cfg.HighFloor*maxClose252, ImportanceMinor,
		fmt.Sprintf("%.2f vs %.0f%% of %.2f", closes[n-1], h.cfg.HighFloor*100, maxClose252),
	))

	// 8. Drawdown floor: never below 70% of 126-day max in last 252 days (major)
	ddPass := h.checkDrawdownFloor(closes)
	filters = append(filters, MakeFilter(
		"drawdown_floor", ddPass, h.cfg.DrawdownFloor, h.cfg.DrawdownFloor, ImportanceMajor,
		fmt.Sprintf("never below %.0f%% of 126d max", h.cfg.DrawdownFloor*100),
	))

	return NewScreenResultWeighted(filters, h.scoring), nil
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
