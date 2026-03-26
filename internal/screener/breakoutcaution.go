package screener

import (
	"context"
	"math"

	"github.com/rohitsharma04/stockctl/internal/config"
	"github.com/rohitsharma04/stockctl/internal/indicators"
	"github.com/rohitsharma04/stockctl/internal/marketdata"
)

// BreakoutCaution screens for Bollinger Band breakouts with volume confirmation
// and relative strength against a benchmark.
// Port of breakoutcaution.py.
type BreakoutCaution struct {
	cfg config.ScreenerConfig
}

func NewBreakoutCaution(cfg config.ScreenerConfig) *BreakoutCaution {
	if cfg.BollingerPeriod == 0 {
		cfg.BollingerPeriod = 20
	}
	if cfg.BollingerStd == 0 {
		cfg.BollingerStd = 2.0
	}
	if cfg.VolumeMultiplier == 0 {
		cfg.VolumeMultiplier = 1.5
	}
	if cfg.SMAWindow == 0 {
		cfg.SMAWindow = 10
	}
	if cfg.ATRWindow == 0 {
		cfg.ATRWindow = 14
	}
	if cfg.RSThreshold == 0 {
		cfg.RSThreshold = 1.05
	}
	if cfg.MomentumPeriod == 0 {
		cfg.MomentumPeriod = 22
	}
	if cfg.MomentumThreshold == 0 {
		cfg.MomentumThreshold = 0.10
	}
	return &BreakoutCaution{cfg: cfg}
}

func (b *BreakoutCaution) Name() string        { return "breakout-caution" }
func (b *BreakoutCaution) Description() string  { return "Bollinger Band breakout + volume + relative strength" }

func (b *BreakoutCaution) Screen(_ context.Context, data []marketdata.OHLCV, benchmark []marketdata.OHLCV) (bool, error) {
	// Need at least 252 trading days (1 year)
	if len(data) < 252 {
		return false, nil
	}

	closes := marketdata.Closes(data)
	highs := marketdata.Highs(data)
	lows := marketdata.Lows(data)
	volumes := marketdata.Volumes(data)
	n := len(closes)

	// Price above $5
	if closes[n-1] <= 5.0 {
		return false, nil
	}

	// 10% rise in last month (22 trading days)
	if n >= b.cfg.MomentumPeriod+1 {
		momentum := (closes[n-1] - closes[n-1-b.cfg.MomentumPeriod]) / closes[n-1-b.cfg.MomentumPeriod]
		if momentum <= b.cfg.MomentumThreshold {
			return false, nil
		}
	}

	// Daily high above Bollinger upper band
	upper, _, _ := indicators.BollingerBands(closes, b.cfg.BollingerPeriod, b.cfg.BollingerStd)
	if math.IsNaN(upper[n-1]) || highs[n-1] <= upper[n-1] {
		return false, nil
	}

	// Volume > 1.5x 10-day average
	avgVol := indicators.SMA(volumes, 10)
	if math.IsNaN(avgVol[n-1]) || volumes[n-1] <= avgVol[n-1]*b.cfg.VolumeMultiplier {
		return false, nil
	}

	// Close above dynamic SMA (SMA + 0.5 * ATR)
	sma := indicators.SMA(closes, b.cfg.SMAWindow)
	atr := indicators.ATR(highs, lows, closes, b.cfg.ATRWindow)
	if math.IsNaN(sma[n-1]) || math.IsNaN(atr[n-1]) {
		return false, nil
	}
	dynamicSMA := sma[n-1] + 0.5*atr[n-1]
	if closes[n-1] <= dynamicSMA {
		return false, nil
	}

	// Relative strength vs benchmark > threshold
	if benchmark != nil && len(benchmark) >= len(data) {
		benchCloses := marketdata.Closes(benchmark)
		// Align lengths
		offset := len(benchCloses) - n
		benchAligned := benchCloses[offset:]

		stockReturns := indicators.PctChange(closes)
		benchReturns := indicators.PctChange(benchAligned)
		rs := indicators.RelativeStrength(stockReturns, benchReturns, 20)
		if len(rs) > 0 && !math.IsNaN(rs[len(rs)-1]) && rs[len(rs)-1] <= b.cfg.RSThreshold {
			return false, nil
		}
	}

	return true, nil
}
