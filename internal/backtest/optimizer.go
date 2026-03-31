package backtest

import (
	"math"
	"sync"

	"github.com/rohitsharma04/stockctl/internal/indicators"
)

// TradeResult represents the result of a single trade simulation.
type TradeResult struct {
	TP          float64
	SL          float64
	TotalInit   float64
	FinalAmount float64
}

// StrategyMetrics holds performance metrics for a TP/SL combination.
type StrategyMetrics struct {
	TP          float64 `json:"tp"`
	SL          float64 `json:"sl"`
	Sharpe      float64 `json:"sharpe"`
	AvgReturn   float64 `json:"avg_return"`
	WinRate     float64 `json:"win_rate"`
	TotalTrades int     `json:"total_trades"`
	AvgWin      float64 `json:"avg_win"`
	AvgLoss     float64 `json:"avg_loss"`
	Expectancy  float64 `json:"expectancy"`
	MaxDrawdown float64 `json:"max_drawdown"`
	ExposurePct float64 `json:"exposure_pct"` // fraction of trades that hit TP or SL (not timed out)
}

// BreakoutEntry represents a pre-processed breakout signal with entry data.
type BreakoutEntry struct {
	Symbol     string
	EntryDate  string
	EntryPrice float64
	Highs      []float64
	Lows       []float64
	Closes     []float64
}

// Optimize runs a parallel grid search over TP/SL combinations.
func Optimize(entries []BreakoutEntry, tpMin, tpMax, tpStep, slMin, slMax, slStep, minRewardRisk, capital float64) []TradeResult {
	var combos []struct{ tp, sl float64 }

	for tp := tpMin; tp <= tpMax+0.001; tp += tpStep {
		for sl := slMin; sl <= slMax+0.001; sl += slStep {
			if tp >= minRewardRisk*sl {
				combos = append(combos, struct{ tp, sl float64 }{tp, sl})
			}
		}
	}

	results := make([]TradeResult, len(combos))
	var wg sync.WaitGroup

	for idx, combo := range combos {
		wg.Add(1)
		go func(i int, tp, sl float64) {
			defer wg.Done()
			results[i] = evaluateCombo(entries, tp, sl, capital)
		}(idx, combo.tp, combo.sl)
	}

	wg.Wait()
	return results
}

func evaluateCombo(entries []BreakoutEntry, tp, sl, capital float64) TradeResult {
	totalProfit := 0.0
	numTrades := len(entries)

	for _, entry := range entries {
		tpPrice := entry.EntryPrice * (1 + tp)
		slPrice := entry.EntryPrice * (1 - sl)
		exitPrice := 0.0

		for j := 0; j < len(entry.Highs); j++ {
			if entry.Highs[j] >= tpPrice {
				exitPrice = tpPrice
				break
			}
			if entry.Lows[j] <= slPrice {
				exitPrice = slPrice
				break
			}
		}

		if exitPrice == 0 && len(entry.Closes) > 0 {
			exitPrice = entry.Closes[len(entry.Closes)-1]
		}

		if exitPrice > 0 {
			shares := capital / entry.EntryPrice
			totalProfit += (exitPrice - entry.EntryPrice) * shares
		}
	}

	totalInit := float64(numTrades) * capital
	return TradeResult{
		TP:          tp,
		SL:          sl,
		TotalInit:   totalInit,
		FinalAmount: totalInit + totalProfit,
	}
}

// EvaluateStrategy computes comprehensive performance metrics for a TP/SL combo.
func EvaluateStrategy(entries []BreakoutEntry, tp, sl float64) StrategyMetrics {
	var returns []float64
	var wins []float64
	var losses []float64
	hitCount := 0 // trades that hit TP or SL (not timed out)

	for _, entry := range entries {
		tpPrice := entry.EntryPrice * (1 + tp)
		slPrice := entry.EntryPrice * (1 - sl)
		ret := 0.0

		hit := false
		for j := 0; j < len(entry.Highs); j++ {
			if entry.Highs[j] >= tpPrice {
				ret = tp
				hit = true
				break
			}
			if entry.Lows[j] <= slPrice {
				ret = -sl
				hit = true
				break
			}
		}

		if !hit && len(entry.Closes) > 0 {
			ret = entry.Closes[len(entry.Closes)-1]/entry.EntryPrice - 1
		}

		if hit {
			hitCount++
		}

		returns = append(returns, ret)
		if ret > 0 {
			wins = append(wins, ret)
		} else if ret < 0 {
			losses = append(losses, ret)
		}
	}

	avgReturn := indicators.Mean(returns)
	std := indicators.Std(returns)
	sharpe := 0.0
	if std > 0 {
		sharpe = avgReturn / std
	}

	winRate := 0.0
	if len(returns) > 0 {
		winRate = float64(len(wins)) / float64(len(returns))
	}

	avgWin := indicators.Mean(wins)
	avgLoss := 0.0
	if len(losses) > 0 {
		avgLoss = indicators.Mean(losses)
	}

	// Expectancy = (WinRate * AvgWin) + ((1 - WinRate) * AvgLoss)
	// AvgLoss is negative, so this naturally subtracts
	expectancy := 0.0
	if len(returns) > 0 {
		expectancy = winRate*avgWin + (1-winRate)*avgLoss
	}

	// Max drawdown from cumulative returns
	maxDrawdown := 0.0
	if len(returns) > 0 {
		cumulative := 1.0
		peak := 1.0
		for _, r := range returns {
			cumulative *= (1 + r)
			if cumulative > peak {
				peak = cumulative
			}
			dd := (peak - cumulative) / peak
			if dd > maxDrawdown {
				maxDrawdown = dd
			}
		}
	}

	// Exposure: fraction of trades that hit TP or SL
	exposurePct := 0.0
	if len(returns) > 0 {
		exposurePct = float64(hitCount) / float64(len(returns))
	}

	if math.IsNaN(sharpe) {
		sharpe = 0
	}
	if math.IsNaN(avgReturn) {
		avgReturn = 0
	}
	if math.IsNaN(avgWin) {
		avgWin = 0
	}
	if math.IsNaN(avgLoss) {
		avgLoss = 0
	}
	if math.IsNaN(expectancy) {
		expectancy = 0
	}
	if math.IsNaN(maxDrawdown) {
		maxDrawdown = 0
	}

	return StrategyMetrics{
		TP:          tp,
		SL:          sl,
		Sharpe:      sharpe,
		AvgReturn:   avgReturn,
		WinRate:     winRate,
		TotalTrades: len(returns),
		AvgWin:      avgWin,
		AvgLoss:     avgLoss,
		Expectancy:  expectancy,
		MaxDrawdown: maxDrawdown,
		ExposurePct: exposurePct,
	}
}

