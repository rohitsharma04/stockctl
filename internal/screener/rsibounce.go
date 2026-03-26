package screener

import (
	"context"
	"fmt"
	"math"

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
type RSIBounce struct{}

func NewRSIBounce() *RSIBounce {
	return &RSIBounce{}
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

	// Filter 1: RSI crossed above 30 in last 5 days
	rsiBounced := false
	for i := n - 5; i < n; i++ {
		if i > 0 && !math.IsNaN(rsi[i]) && !math.IsNaN(rsi[i-1]) {
			if rsi[i-1] < 30 && rsi[i] >= 30 {
				rsiBounced = true
				break
			}
		}
	}

	// Filter 2: Current RSI between 30 and 60
	currentRSI := rsi[n-1]
	rsiInRange := !math.IsNaN(currentRSI) && currentRSI >= 30 && currentRSI <= 60

	// Filter 3: Price above SMA(50)
	aboveSMA50 := !math.IsNaN(sma50[n-1]) && closes[n-1] > sma50[n-1]

	// Filter 4: Volume confirmation
	volRatio := 0.0
	if avgVol > 0 {
		volRatio = volumes[n-1] / avgVol
	}
	volumeSpike := volRatio >= 1.2

	filters := []FilterResult{
		{Name: "RSI Bounce (crossed 30)", Pass: rsiBounced, Value: currentRSI, Threshold: 30, Detail: fmt.Sprintf("RSI=%.1f", currentRSI)},
		{Name: "RSI in Range (30-60)", Pass: rsiInRange, Value: currentRSI, Threshold: 60, Detail: fmt.Sprintf("RSI=%.1f", currentRSI)},
		{Name: "Price > SMA(50)", Pass: aboveSMA50, Value: closes[n-1], Threshold: sma50[n-1]},
		{Name: "Volume > 1.2x Avg", Pass: volumeSpike, Value: volRatio, Threshold: 1.2, Detail: fmt.Sprintf("%.1fx avg", volRatio)},
	}

	return NewScreenResult(filters), nil
}
