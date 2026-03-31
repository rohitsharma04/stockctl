package screener

import (
	"context"
	"fmt"
	"math"

	"github.com/rohitsharma04/stockctl/internal/config"
	"github.com/rohitsharma04/stockctl/internal/indicators"
	"github.com/rohitsharma04/stockctl/internal/marketdata"
)

// MACDCrossover identifies stocks with bullish MACD signal-line crossovers.
//
// Filters:
//  1. MACD crossed above signal in the last 3 days
//  2. MACD histogram turning positive (momentum shifting)
//  3. Price above SMA(200) (major uptrend)
//  4. Volume > 1.0x 20-day average (basic participation)
type MACDCrossover struct {
	scoring config.ScoringConfig
}

func NewMACDCrossover(scoring config.ScoringConfig) *MACDCrossover {
	return &MACDCrossover{scoring: scoring}
}

func (m *MACDCrossover) Name() string        { return "macd-crossover" }
func (m *MACDCrossover) Description() string  { return "MACD bullish crossover with trend confirmation" }

func (m *MACDCrossover) Screen(ctx context.Context, data []marketdata.OHLCV, benchmark []marketdata.OHLCV) (*ScreenResult, error) {
	if len(data) < 210 {
		return nil, fmt.Errorf("need at least 210 bars for MACD + SMA(200), got %d", len(data))
	}

	closes := marketdata.Closes(data)
	volumes := marketdata.Volumes(data)
	n := len(closes)

	// Calculate indicators
	macdLine, signalLine := indicators.MACD(closes)
	sma200 := indicators.SMA(closes, 200)
	avgVol := indicators.Mean(indicators.Tail(volumes, 20))

	var filters []FilterResult

	// Filter 1: MACD crossed above signal in last 3 days (critical)
	if math.IsNaN(macdLine[n-1]) || math.IsNaN(signalLine[n-1]) {
		filters = append(filters, MakeUnknownFilter(
			"macd_crossover", ImportanceCritical, "MACD not available (insufficient data for warmup)",
		))
	} else {
		crossover := false
		for i := n - 3; i < n; i++ {
			if i > 0 && !math.IsNaN(macdLine[i]) && !math.IsNaN(signalLine[i]) &&
				!math.IsNaN(macdLine[i-1]) && !math.IsNaN(signalLine[i-1]) {
				if macdLine[i-1] < signalLine[i-1] && macdLine[i] >= signalLine[i] {
					crossover = true
					break
				}
			}
		}
		filters = append(filters, MakeFilter(
			"macd_crossover", crossover, macdLine[n-1], signalLine[n-1], ImportanceCritical,
			"MACD > Signal",
		))
	}

	// Filter 2: Histogram turning positive (major)
	if math.IsNaN(macdLine[n-1]) || math.IsNaN(signalLine[n-1]) {
		filters = append(filters, MakeUnknownFilter(
			"histogram_positive", ImportanceMajor, "MACD not available",
		))
	} else {
		currentHistogram := macdLine[n-1] - signalLine[n-1]
		histPositive := currentHistogram > 0
		filters = append(filters, MakeFilter(
			"histogram_positive", histPositive, currentHistogram, 0, ImportanceMajor,
			fmt.Sprintf("hist=%.4f", currentHistogram),
		))
	}

	// Filter 3: Price above SMA(200) (major)
	if math.IsNaN(sma200[n-1]) {
		filters = append(filters, MakeUnknownFilter(
			"price_above_sma200", ImportanceMajor, "SMA200 not available",
		))
	} else {
		aboveSMA200 := closes[n-1] > sma200[n-1]
		filters = append(filters, MakeFilter(
			"price_above_sma200", aboveSMA200, closes[n-1], sma200[n-1], ImportanceMajor, "",
		))
	}

	// Filter 4: Volume confirmation (minor)
	volRatio := 0.0
	if avgVol > 0 {
		volRatio = volumes[n-1] / avgVol
	}
	volumeOk := volRatio >= 1.0
	filters = append(filters, MakeFilter(
		"volume_confirmation", volumeOk, volRatio, 1.0, ImportanceMinor,
		fmt.Sprintf("%.1fx avg", volRatio),
	))

	return NewScreenResultWeighted(filters, m.scoring), nil
}
