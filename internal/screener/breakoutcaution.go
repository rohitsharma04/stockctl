package screener

import (
	"context"
	"fmt"
	"math"

	"github.com/rohitsharma04/stockctl/internal/config"
	"github.com/rohitsharma04/stockctl/internal/indicators"
	"github.com/rohitsharma04/stockctl/internal/marketdata"
)

// BreakoutCaution screens for Bollinger Band breakouts with volume confirmation
// and relative strength against a benchmark.
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

func (b *BreakoutCaution) Screen(_ context.Context, data []marketdata.OHLCV, benchmark []marketdata.OHLCV) (*ScreenResult, error) {
	var filters []FilterResult

	// Need at least 252 trading days (1 year)
	if len(data) < 252 {
		return &ScreenResult{Pass: false, Score: 0, Filters: []FilterResult{
			{Name: "min_data", Pass: false, Value: float64(len(data)), Threshold: 252, Detail: "insufficient data"},
		}}, nil
	}

	closes := marketdata.Closes(data)
	highs := marketdata.Highs(data)
	lows := marketdata.Lows(data)
	volumes := marketdata.Volumes(data)
	n := len(closes)

	// 1. Min price
	price := closes[n-1]
	filters = append(filters, FilterResult{
		Name:      "min_price",
		Pass:      price > 5.0,
		Value:     price,
		Threshold: 5.0,
		Detail:    fmt.Sprintf("$%.2f", price),
	})

	// 2. Momentum: 10% rise in last month (22 trading days)
	momentum := 0.0
	if n >= b.cfg.MomentumPeriod+1 {
		momentum = (closes[n-1] - closes[n-1-b.cfg.MomentumPeriod]) / closes[n-1-b.cfg.MomentumPeriod]
	}
	filters = append(filters, FilterResult{
		Name:      "momentum",
		Pass:      momentum > b.cfg.MomentumThreshold,
		Value:     momentum,
		Threshold: b.cfg.MomentumThreshold,
		Detail:    fmt.Sprintf("%.1f%% vs %.1f%% required", momentum*100, b.cfg.MomentumThreshold*100),
	})

	// 3. Bollinger breakout: daily high above upper band
	upper, _, _ := indicators.BollingerBands(closes, b.cfg.BollingerPeriod, b.cfg.BollingerStd)
	bbPass := !math.IsNaN(upper[n-1]) && highs[n-1] > upper[n-1]
	bbVal := 0.0
	if !math.IsNaN(upper[n-1]) {
		bbVal = highs[n-1] - upper[n-1]
	}
	filters = append(filters, FilterResult{
		Name:      "bollinger_breakout",
		Pass:      bbPass,
		Value:     highs[n-1],
		Threshold: upper[n-1],
		Detail:    fmt.Sprintf("high %.2f vs upper band %.2f (diff: %.2f)", highs[n-1], upper[n-1], bbVal),
	})

	// 4. Volume spike: volume > 1.5x 10-day average
	avgVol := indicators.SMA(volumes, 10)
	volPass := !math.IsNaN(avgVol[n-1]) && volumes[n-1] > avgVol[n-1]*b.cfg.VolumeMultiplier
	volRatio := 0.0
	if !math.IsNaN(avgVol[n-1]) && avgVol[n-1] > 0 {
		volRatio = volumes[n-1] / avgVol[n-1]
	}
	filters = append(filters, FilterResult{
		Name:      "volume_spike",
		Pass:      volPass,
		Value:     volRatio,
		Threshold: b.cfg.VolumeMultiplier,
		Detail:    fmt.Sprintf("%.2fx vs %.2fx required", volRatio, b.cfg.VolumeMultiplier),
	})

	// 5. Dynamic SMA: close above SMA + 0.5 * ATR
	sma := indicators.SMA(closes, b.cfg.SMAWindow)
	atr := indicators.ATR(highs, lows, closes, b.cfg.ATRWindow)
	dynamicPass := false
	dynamicSMA := math.NaN()
	if !math.IsNaN(sma[n-1]) && !math.IsNaN(atr[n-1]) {
		dynamicSMA = sma[n-1] + 0.5*atr[n-1]
		dynamicPass = closes[n-1] > dynamicSMA
	}
	filters = append(filters, FilterResult{
		Name:      "dynamic_sma",
		Pass:      dynamicPass,
		Value:     closes[n-1],
		Threshold: dynamicSMA,
		Detail:    fmt.Sprintf("close %.2f vs dynamic SMA %.2f", closes[n-1], dynamicSMA),
	})

	// 6. Relative strength vs benchmark > threshold
	rsPass := true // default pass if no benchmark
	rsVal := math.NaN()
	if benchmark != nil && len(benchmark) >= len(data) {
		benchCloses := marketdata.Closes(benchmark)
		offset := len(benchCloses) - n
		benchAligned := benchCloses[offset:]

		stockReturns := indicators.PctChange(closes)
		benchReturns := indicators.PctChange(benchAligned)
		rs := indicators.RelativeStrength(stockReturns, benchReturns, 20)
		if len(rs) > 0 && !math.IsNaN(rs[len(rs)-1]) {
			rsVal = rs[len(rs)-1]
			rsPass = rsVal > b.cfg.RSThreshold
		}
	}
	filters = append(filters, FilterResult{
		Name:      "relative_strength",
		Pass:      rsPass,
		Value:     rsVal,
		Threshold: b.cfg.RSThreshold,
		Detail:    fmt.Sprintf("RS %.4f vs %.4f required", rsVal, b.cfg.RSThreshold),
	})

	return NewScreenResult(filters), nil
}
