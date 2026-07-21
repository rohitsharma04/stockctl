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
	cfg     config.ScreenerConfig
	scoring config.ScoringConfig
}

func NewBreakoutCaution(cfg config.ScreenerConfig, scoring config.ScoringConfig) *BreakoutCaution {
	if cfg.MinPrice == 0 {
		cfg.MinPrice = 5.0
	}
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
	return &BreakoutCaution{cfg: cfg, scoring: scoring}
}

func (b *BreakoutCaution) Name() string { return "breakout-caution" }
func (b *BreakoutCaution) Description() string {
	return "Bollinger Band breakout + volume + relative strength"
}

func (b *BreakoutCaution) Screen(_ context.Context, data []marketdata.OHLCV, benchmark []marketdata.OHLCV) (*ScreenResult, error) {
	var filters []FilterResult

	// Need at least 252 trading days (1 year)
	if len(data) < 252 {
		return &ScreenResult{Pass: false, Score: 0, Filters: []FilterResult{
			MakeFilter("min_data", false, float64(len(data)), 252, ImportanceMinor, "insufficient data"),
		}}, nil
	}

	closes := marketdata.Closes(data)
	highs := marketdata.Highs(data)
	lows := marketdata.Lows(data)
	volumes := marketdata.Volumes(data)
	n := len(closes)

	// 1. Min price (minor)
	price := closes[n-1]
	filters = append(filters, MakeFilter(
		"min_price", price >= b.cfg.MinPrice, price, b.cfg.MinPrice, ImportanceMinor,
		fmt.Sprintf("%.2f", price),
	))

	// 2. Momentum: 10% rise in last month (major)
	momentum := 0.0
	if n >= b.cfg.MomentumPeriod+1 {
		momentum = (closes[n-1] - closes[n-1-b.cfg.MomentumPeriod]) / closes[n-1-b.cfg.MomentumPeriod]
	}
	filters = append(filters, MakeFilter(
		"momentum", momentum > b.cfg.MomentumThreshold, momentum, b.cfg.MomentumThreshold, ImportanceMajor,
		fmt.Sprintf("%.1f%% vs %.1f%% required", momentum*100, b.cfg.MomentumThreshold*100),
	))

	// 3. Bollinger breakout: daily high above upper band (critical)
	upper, _, _ := indicators.BollingerBands(closes, b.cfg.BollingerPeriod, b.cfg.BollingerStd)
	bbPass := !math.IsNaN(upper[n-1]) && highs[n-1] > upper[n-1]
	bbVal := highs[n-1]
	bbThreshold := upper[n-1]
	if math.IsNaN(upper[n-1]) {
		filters = append(filters, MakeUnknownFilter(
			"bollinger_breakout", ImportanceCritical, "bollinger band not available",
		))
	} else {
		filters = append(filters, MakeFilter(
			"bollinger_breakout", bbPass, bbVal, bbThreshold, ImportanceCritical,
			fmt.Sprintf("high %.2f vs upper band %.2f (diff: %.2f)", highs[n-1], upper[n-1], highs[n-1]-upper[n-1]),
		))
	}

	// 4. Volume spike: volume > 1.5x 10-day average (critical)
	avgVol := indicators.SMA(volumes, 10)
	volRatio := 0.0
	if !math.IsNaN(avgVol[n-1]) && avgVol[n-1] > 0 {
		volRatio = volumes[n-1] / avgVol[n-1]
	}
	if math.IsNaN(avgVol[n-1]) {
		filters = append(filters, MakeUnknownFilter(
			"volume_spike", ImportanceCritical, "volume average not available",
		))
	} else {
		volPass := volumes[n-1] > avgVol[n-1]*b.cfg.VolumeMultiplier
		filters = append(filters, MakeFilter(
			"volume_spike", volPass, volRatio, b.cfg.VolumeMultiplier, ImportanceCritical,
			fmt.Sprintf("%.2fx vs %.2fx required", volRatio, b.cfg.VolumeMultiplier),
		))
	}

	// 5. Dynamic SMA: close above SMA + 0.5 * ATR (major)
	sma := indicators.SMA(closes, b.cfg.SMAWindow)
	atr := indicators.ATR(highs, lows, closes, b.cfg.ATRWindow)
	if math.IsNaN(sma[n-1]) || math.IsNaN(atr[n-1]) {
		filters = append(filters, MakeUnknownFilter(
			"dynamic_sma", ImportanceMajor, "SMA or ATR not available",
		))
	} else {
		dynamicSMA := sma[n-1] + 0.5*atr[n-1]
		dynamicPass := closes[n-1] > dynamicSMA
		filters = append(filters, MakeFilter(
			"dynamic_sma", dynamicPass, closes[n-1], dynamicSMA, ImportanceMajor,
			fmt.Sprintf("close %.2f vs dynamic SMA %.2f", closes[n-1], dynamicSMA),
		))
	}

	// 6. Relative strength vs benchmark > threshold (major)
	// FIX: When benchmark is nil or too short, mark as "unknown" instead of defaulting to pass.
	if benchmark == nil || len(benchmark) < len(data) {
		detail := "benchmark data not available"
		if benchmark != nil && len(benchmark) < len(data) {
			detail = fmt.Sprintf("benchmark has %d bars, need %d", len(benchmark), len(data))
		}
		filters = append(filters, MakeUnknownFilter(
			"relative_strength", ImportanceMajor, detail,
		))
	} else {
		benchCloses := marketdata.Closes(benchmark)
		offset := len(benchCloses) - n
		benchAligned := benchCloses[offset:]

		stockReturns := indicators.PctChange(closes)
		benchReturns := indicators.PctChange(benchAligned)
		rs := indicators.RelativeStrength(stockReturns, benchReturns, 20)
		if len(rs) > 0 && !math.IsNaN(rs[len(rs)-1]) {
			rsVal := rs[len(rs)-1]
			rsPass := rsVal > b.cfg.RSThreshold
			filters = append(filters, MakeFilter(
				"relative_strength", rsPass, rsVal, b.cfg.RSThreshold, ImportanceMajor,
				fmt.Sprintf("RS %.4f vs %.4f required", rsVal, b.cfg.RSThreshold),
			))
		} else {
			filters = append(filters, MakeUnknownFilter(
				"relative_strength", ImportanceMajor, "RS calculation returned NaN",
			))
		}
	}

	return NewScreenResultWeighted(filters, b.scoring), nil
}
