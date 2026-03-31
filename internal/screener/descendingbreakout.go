package screener

import (
	"context"
	"fmt"

	"github.com/rohitsharma04/stockctl/internal/config"
	"github.com/rohitsharma04/stockctl/internal/marketdata"
)

// DescendingBreakout screens for descending triangle breakouts.
type DescendingBreakout struct {
	cfg     config.ScreenerConfig
	scoring config.ScoringConfig
}

func NewDescendingBreakout(cfg config.ScreenerConfig, scoring config.ScoringConfig) *DescendingBreakout {
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
	return &DescendingBreakout{cfg: cfg, scoring: scoring}
}

func (d *DescendingBreakout) Name() string        { return "descending-breakout" }
func (d *DescendingBreakout) Description() string  { return "Descending triangle breakout with volume confirmation" }

func (d *DescendingBreakout) Screen(_ context.Context, data []marketdata.OHLCV, _ []marketdata.OHLCV) (*ScreenResult, error) {
	if len(data) < d.cfg.MinDataDays {
		return &ScreenResult{Pass: false, Score: 0, Filters: []FilterResult{
			MakeFilter("min_data", false, float64(len(data)), float64(d.cfg.MinDataDays), ImportanceMinor, "insufficient data"),
		}}, nil
	}

	closes := marketdata.Closes(data)
	n := len(closes)
	var filters []FilterResult

	// 1. Min price (minor)
	filters = append(filters, MakeFilter(
		"min_price", closes[n-1] > 5.0, closes[n-1], 5.0, ImportanceMinor, "",
	))

	// Resample to monthly
	monthly := marketdata.ToMonthly(data)
	mn := len(monthly)
	if mn < d.cfg.Months {
		return &ScreenResult{Pass: false, Score: 0, Filters: []FilterResult{
			MakeFilter("min_monthly_data", false, float64(mn), float64(d.cfg.Months), ImportanceMinor, "insufficient monthly data"),
		}}, nil
	}

	triangleData := monthly[mn-d.cfg.Months:]

	// 2. Descending highs (critical)
	peak := triangleData[0].High
	falseBreakouts := 0
	descPass := true
	for i := 1; i < len(triangleData); i++ {
		if triangleData[i].High > peak {
			falseBreakouts++
			if falseBreakouts > d.cfg.FalseBreakoutTolerance {
				descPass = false
				break
			}
		} else {
			peak = triangleData[i].High
		}
	}
	filters = append(filters, MakeFilter(
		"descending_highs", descPass, float64(falseBreakouts), float64(d.cfg.FalseBreakoutTolerance), ImportanceCritical,
		fmt.Sprintf("%d false breakouts (max %d)", falseBreakouts, d.cfg.FalseBreakoutTolerance),
	))

	// 3. Trendline breakout (critical)
	highFirst := triangleData[0].High
	highLast := triangleData[len(triangleData)-2].High
	numPoints := float64(len(triangleData) - 1)
	trendlineSlope := (highLast - highFirst) / numPoints
	trendlineValue := highFirst + trendlineSlope*numPoints

	currentClose := triangleData[len(triangleData)-1].Close
	trendPass := currentClose > trendlineValue
	filters = append(filters, MakeFilter(
		"trendline_breakout", trendPass, currentClose, trendlineValue, ImportanceCritical,
		fmt.Sprintf("close %.2f vs trendline %.2f", currentClose, trendlineValue),
	))

	// 4. Volume confirmation (critical)
	avgVolume := 0.0
	for i := 0; i < len(triangleData)-1; i++ {
		avgVolume += triangleData[i].Volume
	}
	avgVolume /= float64(len(triangleData) - 1)

	lastVolume := triangleData[len(triangleData)-1].Volume
	volRatio := 0.0
	if avgVolume > 0 {
		volRatio = lastVolume / avgVolume
	}
	volPass := lastVolume > avgVolume*d.cfg.VolumeMultiplier
	filters = append(filters, MakeFilter(
		"volume_confirmation", volPass, volRatio, d.cfg.VolumeMultiplier, ImportanceCritical,
		fmt.Sprintf("%.2fx vs %.2fx required", volRatio, d.cfg.VolumeMultiplier),
	))

	return NewScreenResultWeighted(filters, d.scoring), nil
}
