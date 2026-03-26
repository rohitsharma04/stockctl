package pairs

import (
	"time"

	"github.com/rohitsharma04/stockctl/internal/indicators"
)

// Trade represents a single pairs trade.
type Trade struct {
	EntryDate  time.Time
	ExitDate   time.Time
	Profit     float64
	Position   int    // 1 = long spread, -1 = short spread
	LongStock  string
	ShortStock string
	Amount     float64
	HedgeRatio float64
}

// SimulationResult holds results from a pairs trading simulation.
type SimulationResult struct {
	Stock1       string
	Stock2       string
	HedgeRatio   float64
	Trades       []Trade
	FinalCapital float64
	TotalProfit  float64
	WinRate      float64
}

// Simulate runs a pairs trading simulation for two price series.
// It uses z-score based entry/exit signals on the spread.
func Simulate(stock1, stock2 string, prices1, prices2 []float64, dates []time.Time,
	window int, zThreshold, zExitLow, zExitHigh, initialCapital float64) SimulationResult {

	n := len(prices1)
	if n == 0 || len(prices2) != n || len(dates) != n {
		return SimulationResult{Stock1: stock1, Stock2: stock2}
	}

	// Calculate hedge ratio
	beta := HedgeRatio(prices1, prices2)

	// Calculate spread: Stock1 - beta * Stock2
	spread := make([]float64, n)
	for i := 0; i < n; i++ {
		spread[i] = prices1[i] - beta*prices2[i]
	}

	// Calculate z-score of spread
	ma := indicators.SMA(spread, window)
	std := indicators.RollingStd(spread, window)
	zScore := make([]float64, n)
	for i := 0; i < n; i++ {
		if std[i] != 0 {
			zScore[i] = (spread[i] - ma[i]) / std[i]
		}
	}

	// Trading simulation
	var trades []Trade
	capital := initialCapital
	position := 0
	var entryPrice1, entryPrice2 float64
	var longStock, shortStock string

	cutoffDate := time.Now().AddDate(-1, 0, 0)

	for i := 1; i < n; i++ {
		if position == 0 {
			// Look for entry
			if zScore[i] < -zThreshold {
				position = 1 // long spread
				entryPrice1 = prices1[i]
				entryPrice2 = prices2[i]
				longStock = stock1
				shortStock = stock2
			} else if zScore[i] > zThreshold {
				position = -1 // short spread
				entryPrice1 = prices1[i]
				entryPrice2 = prices2[i]
				longStock = stock2
				shortStock = stock1
			}
		} else if position != 0 && zScore[i] > zExitLow && zScore[i] < zExitHigh {
			// Exit
			exitPrice1 := prices1[i]
			exitPrice2 := prices2[i]

			var profit float64
			if position == 1 {
				profit = (exitPrice1 - entryPrice1) - beta*(exitPrice2-entryPrice2)
			} else {
				profit = (entryPrice1 - exitPrice1) - beta*(entryPrice2-exitPrice2)
			}

			// Only record trades from the last year
			if dates[i-1].After(cutoffDate) {
				trades = append(trades, Trade{
					EntryDate:  dates[i-1],
					ExitDate:   dates[i],
					Profit:     profit,
					Position:   position,
					LongStock:  longStock,
					ShortStock: shortStock,
					Amount:     initialCapital / 2,
					HedgeRatio: beta,
				})
			}

			capital += profit
			position = 0
		}
	}

	// Calculate win rate
	winRate := 0.0
	if len(trades) > 0 {
		profitable := 0
		for _, t := range trades {
			if t.Profit > 0 {
				profitable++
			}
		}
		winRate = float64(profitable) / float64(len(trades))
	}

	return SimulationResult{
		Stock1:       stock1,
		Stock2:       stock2,
		HedgeRatio:   beta,
		Trades:       trades,
		FinalCapital: capital,
		TotalProfit:  capital - initialCapital,
		WinRate:      winRate,
	}
}
