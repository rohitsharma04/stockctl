package screener

import (
	"context"
	"fmt"
	"math"

	"github.com/rohitsharma04/stockctl/internal/config"
	"github.com/rohitsharma04/stockctl/internal/indicators"
	"github.com/rohitsharma04/stockctl/internal/marketdata"
)

// RSIBounce identifies stocks bouncing off oversold RSI levels.
//
// Filters:
//  1. RSI(14) crossed above 30 in the last 5 days (oversold recovery)
//  2. Current RSI(14) is between 30 and 60 (not overbought)
//  3. Price above SMA(50) (uptrend support)
//  4. Volume > 1.2x 20-day average (participation confirmation)
type RSIBounce struct {
	scoring config.ScoringConfig
}

func NewRSIBounce(scoring config.ScoringConfig) *RSIBounce {
	return &RSIBounce{scoring: scoring}
}

func (r *RSIBounce) Name() string        { return "rsi-bounce" }
func (r *RSIBounce) Description() string  { return "RSI oversold bounce with volume confirmation" }

func (r *RSIBounce) Screen(ctx context.Context, data []marketdata.OHLCV, benchmark []marketdata.OHLCV) (*ScreenResult, error) {
	if len(data) < 60 {
		return nil, fmt.Errorf("need at least 60 bars, got %d", len(data))
	}

	closes := marketdata.Closes(data)
	volumes := marketdata.Volumes(data)
	n := len(closes)

	// Calculate indicators
	rsi := indicators.RSI(closes, 14)
	sma50 := indicators.SMA(closes, 50)
	avgVol := indicators.Mean(indicators.Tail(volumes, 20))

	// Filter 1: RSI crossed above 30 in last 5 days (critical)
	rsiBounced := false
	for i := n - 5; i < n; i++ {
		if i > 0 && !math.IsNaN(rsi[i]) && !math.IsNaN(rsi[i-1]) {
			if rsi[i-1] < 30 && rsi[i] >= 30 {
				rsiBounced = true
				break
			}
		}
	}

	currentRSI := rsi[n-1]
	if math.IsNaN(currentRSI) {
		return NewScreenResultWeighted([]FilterResult{
			MakeUnknownFilter("rsi_bounce", ImportanceCritical, "RSI not available"),
			MakeUnknownFilter("rsi_in_range", ImportanceMajor, "RSI not available"),
			MakeUnknownFilter("price_above_sma50", ImportanceMajor, "depends on RSI availability"),
			MakeUnknownFilter("volume_confirmation", ImportanceCritical, "depends on RSI availability"),
		}, r.scoring), nil
	}

	var filters []FilterResult

	filters = append(filters, MakeFilter(
		"rsi_bounce", rsiBounced, currentRSI, 30, ImportanceCritical,
		fmt.Sprintf("RSI=%.1f", currentRSI),
	))

	// Filter 2: Current RSI between 30 and 60 (major)
	rsiInRange := currentRSI >= 30 && currentRSI <= 60
	filters = append(filters, MakeFilter(
		"rsi_in_range", rsiInRange, currentRSI, 60, ImportanceMajor,
		fmt.Sprintf("RSI=%.1f", currentRSI),
	))

	// Filter 3: Price above SMA(50) (major)
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

	// Filter 4: Volume confirmation (critical)
	volRatio := 0.0
	if avgVol > 0 {
		volRatio = volumes[n-1] / avgVol
	}
	volumeSpike := volRatio >= 1.2
	filters = append(filters, MakeFilter(
		"volume_confirmation", volumeSpike, volRatio, 1.2, ImportanceCritical,
		fmt.Sprintf("%.1fx avg", volRatio),
	))

	return NewScreenResultWeighted(filters, r.scoring), nil
}
