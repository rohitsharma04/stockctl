package screener

import (
	"context"

	"github.com/rohitsharma04/stockctl/internal/config"
	"github.com/rohitsharma04/stockctl/internal/marketdata"
)

// DescendingBreakout screens for descending triangle breakouts.
// A descending triangle has progressively lower highs with flat support.
// When price breaks above the descending trendline with volume, it's bullish.
// Port of descendingbreakout.py.
type DescendingBreakout struct {
	cfg config.ScreenerConfig
}

func NewDescendingBreakout(cfg config.ScreenerConfig) *DescendingBreakout {
	if cfg.Months == 0 {
		cfg.Months = 36
	}
	if cfg.FalseBreakoutTolerance == 0 {
		cfg.FalseBreakoutTolerance = 6
	}
	if cfg.VolumeMultiplier == 0 {
		cfg.VolumeMultiplier = 1.5
	}
	if cfg.MinDataDays == 0 {
		cfg.MinDataDays = 756
	}
	return &DescendingBreakout{cfg: cfg}
}

func (d *DescendingBreakout) Name() string        { return "descending-breakout" }
func (d *DescendingBreakout) Description() string  { return "Descending triangle breakout with volume confirmation" }

func (d *DescendingBreakout) Screen(_ context.Context, data []marketdata.OHLCV, _ []marketdata.OHLCV) (bool, error) {
	if len(data) < d.cfg.MinDataDays {
		return false, nil
	}

	closes := marketdata.Closes(data)
	n := len(closes)

	// Price > $5
	if closes[n-1] <= 5.0 {
		return false, nil
	}

	// Resample to monthly
	monthly := marketdata.ToMonthly(data)
	mn := len(monthly)
	if mn < d.cfg.Months {
		return false, nil
	}

	// Take last N months
	triangleData := monthly[mn-d.cfg.Months:]

	// Check descending highs with tolerance for false breakouts
	peak := triangleData[0].High
	falseBreakouts := 0
	for i := 1; i < len(triangleData); i++ {
		if triangleData[i].High > peak {
			falseBreakouts++
			if falseBreakouts > d.cfg.FalseBreakoutTolerance {
				return false, nil
			}
		} else {
			peak = triangleData[i].High
		}
	}

	// Calculate descending trendline
	highFirst := triangleData[0].High
	highLast := triangleData[len(triangleData)-2].High // second to last
	numPoints := float64(len(triangleData) - 1)
	trendlineSlope := (highLast - highFirst) / numPoints
	trendlineValue := highFirst + trendlineSlope*numPoints

	// Current close must break ABOVE the trendline
	currentClose := triangleData[len(triangleData)-1].Close
	if currentClose <= trendlineValue {
		return false, nil
	}

	// Volume confirmation: last month volume > 1.5x average
	avgVolume := 0.0
	for i := 0; i < len(triangleData)-1; i++ {
		avgVolume += triangleData[i].Volume
	}
	avgVolume /= float64(len(triangleData) - 1)

	lastVolume := triangleData[len(triangleData)-1].Volume
	if lastVolume <= avgVolume*d.cfg.VolumeMultiplier {
		return false, nil
	}

	return true, nil
}
