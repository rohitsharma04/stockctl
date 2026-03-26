package cmd

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/rohitsharma04/stockctl/internal/indicators"
	"github.com/rohitsharma04/stockctl/internal/marketdata"
	"github.com/rohitsharma04/stockctl/internal/output"
	"github.com/rohitsharma04/stockctl/internal/pairs"
	"github.com/spf13/cobra"
)

var (
	pairsStocks    string
	pairsThreshold float64
	pairsCapital   float64
	pairsWindow    int
	pairsZThresh   float64
)

var pairsCmd = &cobra.Command{
	Use:   "pairs",
	Short: "Run pairs trading analysis",
	Long: `Find correlated stock pairs and simulate mean-reversion pairs trading.

Uses z-score based entry/exit signals on the price spread between
correlated stocks. Calculates hedge ratios via OLS regression.

Examples:
  stockctl pairs
  stockctl pairs --stocks "RELIANCE.NS,TCS.NS,HDFCBANK.NS,INFY.NS"
  stockctl pairs --threshold 0.8 --capital 200000`,
	RunE: runPairs,
}

func init() {
	pairsCmd.Flags().StringVar(&pairsStocks, "stocks", "", "comma-separated stock symbols (default from config)")
	pairsCmd.Flags().Float64Var(&pairsThreshold, "threshold", 0, "correlation threshold (default from config)")
	pairsCmd.Flags().Float64Var(&pairsCapital, "capital", 0, "initial capital per pair (default from config)")
	pairsCmd.Flags().IntVar(&pairsWindow, "window", 0, "rolling window for z-score (default from config)")
	pairsCmd.Flags().Float64Var(&pairsZThresh, "z-threshold", 0, "z-score entry threshold (default from config)")
	rootCmd.AddCommand(pairsCmd)
}

func runPairs(cmd *cobra.Command, args []string) error {
	ctx := context.Background()
	startTime := time.Now()
	cfg := appConfig.Pairs

	// Resolve overrides
	stocks := cfg.Stocks
	if pairsStocks != "" {
		stocks = strings.Split(pairsStocks, ",")
		for i := range stocks {
			stocks[i] = strings.TrimSpace(stocks[i])
		}
	}

	threshold := cfg.CorrelationThreshold
	if pairsThreshold > 0 {
		threshold = pairsThreshold
	}
	capital := cfg.Capital
	if pairsCapital > 0 {
		capital = pairsCapital
	}
	window := cfg.Window
	if pairsWindow > 0 {
		window = pairsWindow
	}
	zThresh := cfg.ZThreshold
	if pairsZThresh > 0 {
		zThresh = pairsZThresh
	}

	if len(stocks) < 2 {
		return fmt.Errorf("need at least 2 stocks for pairs trading")
	}

	// Apply market suffix
	for i := range stocks {
		stocks[i] = activeMarket.ApplySuffix(stocks[i])
	}

	yahoo := marketdata.NewYahooProvider(5)
	var provider marketdata.Provider = yahoo
	if !noCache {
		provider = marketdata.NewDiskCachedProvider(yahoo, 24*time.Hour)
	}

	// Download data for all stocks
	logf("🌍 Market: %s (%s)\n", activeMarket.Name, activeMarket.Currency)
	logf("📊 Downloading data for %d stocks...\n", len(stocks))
	type stockEntry struct {
		symbol string
		data   []marketdata.OHLCV
	}
	var entries []stockEntry

	for _, sym := range stocks {
		data, err := provider.GetHistory(ctx, sym, "5y", "1d")
		if err != nil {
			logf("  ⚠ Skipping %s: %v\n", sym, err)
			continue
		}
		logf("  ✓ %s (%d bars)\n", sym, len(data))
		entries = append(entries, stockEntry{symbol: sym, data: data})
	}

	if len(entries) < 2 {
		return fmt.Errorf("need at least 2 valid stocks, got %d", len(entries))
	}

	// Calculate returns
	logf("📈 Calculating correlations...\n")
	validSymbols := make([]string, len(entries))
	allReturns := make([][]float64, len(entries))
	for i, e := range entries {
		validSymbols[i] = e.symbol
		closes := marketdata.Closes(e.data)
		allReturns[i] = indicators.PctChange(closes)
	}

	// Find correlated pairs
	corrPairs := pairs.FindCorrelatedPairs(validSymbols, allReturns, threshold)
	logf("🔗 Found %d correlated pairs (threshold: %.2f)\n", len(corrPairs), threshold)

	if len(corrPairs) == 0 {
		logf("No correlated pairs found. Try lowering --threshold.\n")
		return nil
	}

	// Simulate trading for top 5 pairs
	limit := 5
	if len(corrPairs) < limit {
		limit = len(corrPairs)
	}

	var results []pairs.SimulationResult
	for _, pair := range corrPairs[:limit] {
		// Find data for each stock
		var data1, data2 []marketdata.OHLCV
		for _, e := range entries {
			if e.symbol == pair.Stock1 {
				data1 = e.data
			}
			if e.symbol == pair.Stock2 {
				data2 = e.data
			}
		}
		if data1 == nil || data2 == nil {
			continue
		}

		// Align data to same length
		minLen := len(data1)
		if len(data2) < minLen {
			minLen = len(data2)
		}
		aligned1 := data1[len(data1)-minLen:]
		aligned2 := data2[len(data2)-minLen:]

		prices1 := marketdata.Closes(aligned1)
		prices2 := marketdata.Closes(aligned2)

		// Build time slice
		dates := make([]time.Time, minLen)
		for i, bar := range aligned1 {
			dates[i] = bar.Date
		}

		result := pairs.Simulate(
			pair.Stock1, pair.Stock2,
			prices1, prices2, dates,
			window, zThresh, cfg.ZExitLow, cfg.ZExitHigh, capital,
		)
		results = append(results, result)

		logf("\n🔗 %s — %s (corr: %.4f)\n", pair.Stock1, pair.Stock2, pair.Correlation)
		logf("   Hedge Ratio: %.4f\n", result.HedgeRatio)
		logf("   Trades: %d\n", len(result.Trades))
		cs := activeMarket.CurrencySymbol
		logf("   Final Capital: %s%.2f\n", cs, result.FinalCapital)
		logf("   Total Profit: %s%.2f\n", cs, result.TotalProfit)
		if len(result.Trades) > 0 {
			logf("   Win Rate: %.1f%%\n", result.WinRate*100)
		}
	}

	// Output
	switch output.Format(appConfig.General.Output) {
	case output.FormatJSON:
		meta := output.NewMeta("pairs")
		meta.Market = activeMarket.ID
		meta.DurationMs = time.Since(startTime).Milliseconds()
		env := output.Envelope{
			Meta:    meta,
			Results: results,
		}
		return output.WriteEnvelope(os.Stdout, env)

	default:
		if len(results) > 0 {
			cs := activeMarket.CurrencySymbol
			fmt.Println("\n📊 Summary:")
			tw := output.NewTableWriter(os.Stdout)
			tw.SetHeaders("Pair", "Hedge Ratio", "Trades", "Profit", "Win Rate")
			for _, r := range results {
				tw.AddRow(
					fmt.Sprintf("%s/%s", r.Stock1, r.Stock2),
					fmt.Sprintf("%.4f", r.HedgeRatio),
					fmt.Sprintf("%d", len(r.Trades)),
					fmt.Sprintf("%s%.2f", cs, r.TotalProfit),
					fmt.Sprintf("%.1f%%", r.WinRate*100),
				)
			}
			tw.Render()
		}
	}

	return nil
}
