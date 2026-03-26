package screener

import (
	"context"
	"fmt"
	"math"

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
type MACDCrossover struct{}

func NewMACDCrossover() *MACDCrossover {
	return &MACDCrossover{}
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

	// Filter 1: MACD crossed above signal in last 3 days
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

	// Filter 2: Histogram turning positive
	currentHistogram := 0.0
	if !math.IsNaN(macdLine[n-1]) && !math.IsNaN(signalLine[n-1]) {
		currentHistogram = macdLine[n-1] - signalLine[n-1]
	}
	histPositive := currentHistogram > 0

	// Filter 3: Price above SMA(200)
	aboveSMA200 := !math.IsNaN(sma200[n-1]) && closes[n-1] > sma200[n-1]

	// Filter 4: Volume confirmation
	volRatio := 0.0
	if avgVol > 0 {
		volRatio = volumes[n-1] / avgVol
	}
	volumeOk := volRatio >= 1.0

	filters := []FilterResult{
		{Name: "MACD Crossover", Pass: crossover, Value: macdLine[n-1], Threshold: signalLine[n-1], Detail: "MACD > Signal"},
		{Name: "Histogram Positive", Pass: histPositive, Value: currentHistogram, Threshold: 0, Detail: fmt.Sprintf("hist=%.4f", currentHistogram)},
		{Name: "Price > SMA(200)", Pass: aboveSMA200, Value: closes[n-1], Threshold: sma200[n-1]},
		{Name: "Volume ≥ 1.0x Avg", Pass: volumeOk, Value: volRatio, Threshold: 1.0, Detail: fmt.Sprintf("%.1fx avg", volRatio)},
	}

	return NewScreenResult(filters), nil
}
