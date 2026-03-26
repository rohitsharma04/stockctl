package cmd

import (
	"context"
	"encoding/csv"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/rohitsharma04/stockctl/internal/backtest"
	"github.com/rohitsharma04/stockctl/internal/marketdata"
	"github.com/rohitsharma04/stockctl/internal/output"
	"github.com/rohitsharma04/stockctl/internal/screener"
	"github.com/spf13/cobra"
)

var (
	btInput    string
	btTPRange  string
	btSLRange  string
	btCapital  float64
	btStrategy string
)

var backtestCmd = &cobra.Command{
	Use:   "backtest",
	Short: "Backtest breakout strategies with TP/SL optimization",
	Long: `Run a grid search over take-profit and stop-loss combinations
to find the optimal strategy parameters.

Can either read pre-processed entries from a CSV, or run a scan internally:

  CSV mode:      stockctl backtest --input trading_results.csv
  Strategy mode: stockctl backtest --strategy breakout-caution -m india

Examples:
  stockctl backtest --input trading_results.csv
  stockctl backtest --strategy breakout-caution -m us --output json
  stockctl backtest --input results.csv --tp-range 0.05:0.50 --sl-range 0.01:0.10
  stockctl backtest --input results.csv --capital 200000`,
	RunE: runBacktest,
}

func init() {
	backtestCmd.Flags().StringVar(&btInput, "input", "", "CSV file with breakout entries")
	backtestCmd.Flags().StringVar(&btStrategy, "strategy", "", "run scan and backtest results (bypasses --input)")
	backtestCmd.Flags().StringVar(&btTPRange, "tp-range", "", "take-profit range min:max (default from config)")
	backtestCmd.Flags().StringVar(&btSLRange, "sl-range", "", "stop-loss range min:max (default from config)")
	backtestCmd.Flags().Float64Var(&btCapital, "capital", 0, "capital per trade (default from config)")
	rootCmd.AddCommand(backtestCmd)
}

func runBacktest(cmd *cobra.Command, args []string) error {
	cfg := appConfig.Backtest
	startTime := time.Now()

	// Parse range overrides
	tpMin, tpMax, tpStep := cfg.TPMin, cfg.TPMax, cfg.TPStep
	if btTPRange != "" {
		parts := strings.Split(btTPRange, ":")
		if len(parts) == 2 {
			tpMin, _ = strconv.ParseFloat(parts[0], 64)
			tpMax, _ = strconv.ParseFloat(parts[1], 64)
		}
	}

	slMin, slMax, slStep := cfg.SLMin, cfg.SLMax, cfg.SLStep
	if btSLRange != "" {
		parts := strings.Split(btSLRange, ":")
		if len(parts) == 2 {
			slMin, _ = strconv.ParseFloat(parts[0], 64)
			slMax, _ = strconv.ParseFloat(parts[1], 64)
		}
	}

	capital := cfg.Capital
	if btCapital > 0 {
		capital = btCapital
	}

	// Load entries — either from strategy scan or CSV file
	var entries []backtest.BreakoutEntry
	if btStrategy != "" {
		// Strategy mode: run scan internally and construct entries
		var err error
		entries, err = buildEntriesFromScan(btStrategy)
		if err != nil {
			return err
		}
		logf("📊 Scan produced %d breakout entries for backtesting\n", len(entries))
	} else {
		// CSV mode
		if btInput == "" {
			return fmt.Errorf("provide --input CSV or --strategy to run scan")
		}
		var err error
		entries, err = loadBreakoutEntries(btInput)
		if err != nil {
			return fmt.Errorf("loading entries: %w", err)
		}
		logf("📊 Loaded %d breakout entries from %s\n", len(entries), btInput)
	}

	if len(entries) == 0 {
		if btStrategy != "" {
			return fmt.Errorf("no breakout signals found with strategy %q — nothing to backtest", btStrategy)
		}
		return fmt.Errorf("no entries found in %s", btInput)
	}

	// Run optimization
	logf("⚙️  Optimizing TP: %.0f%%–%.0f%%, SL: %.0f%%–%.0f%% (TP ≥ %.0fx SL)...\n",
		tpMin*100, tpMax*100, slMin*100, slMax*100, cfg.MinRewardRisk)

	results := backtest.Optimize(entries, tpMin, tpMax, tpStep, slMin, slMax, slStep, cfg.MinRewardRisk, capital)

	// Sort by final amount descending
	sort.Slice(results, func(i, j int) bool {
		return results[i].FinalAmount > results[j].FinalAmount
	})

	cs := activeMarket.CurrencySymbol

	// JSON output with envelope
	if appConfig.General.Output == "json" {
		type btResult struct {
			Optimized []backtest.TradeResult   `json:"optimized"`
			Metrics   []backtest.StrategyMetrics `json:"metrics"`
		}
		var metrics []backtest.StrategyMetrics
		for tp := tpMin; tp <= tpMax+0.001; tp += tpStep {
			metrics = append(metrics, backtest.EvaluateStrategy(entries, tp, 0.05))
		}
		meta := output.NewMeta("backtest")
		meta.Market = activeMarket.ID
		meta.DurationMs = time.Since(startTime).Milliseconds()
		env := output.Envelope{
			Meta:    meta,
			Results: btResult{Optimized: results, Metrics: metrics},
		}
		return output.WriteEnvelope(os.Stdout, env)
	}

	// Summary
	totalInit := 0.0
	totalFinal := 0.0
	for _, r := range results {
		totalInit += r.TotalInit
		totalFinal += r.FinalAmount
	}

	fmt.Printf("\n📈 Performance Summary:\n")
	fmt.Printf("   Total Initial Investment: %s%.2f\n", cs, totalInit)
	fmt.Printf("   Total Final Amount:       %s%.2f\n", cs, totalFinal)
	fmt.Printf("   Net Profit:               %s%.2f\n", cs, totalFinal-totalInit)

	// Top 10 performers
	limit := 10
	if len(results) < limit {
		limit = len(results)
	}

	fmt.Printf("\n🏆 Top %d Performers:\n", limit)
	tw := output.NewTableWriter(os.Stdout)
	tw.SetHeaders("TP%", "SL%", "Initial", "Final", "Return%")
	for _, r := range results[:limit] {
		ret := 0.0
		if r.TotalInit > 0 {
			ret = (r.FinalAmount/r.TotalInit - 1) * 100
		}
		tw.AddRow(
			fmt.Sprintf("%.1f%%", r.TP*100),
			fmt.Sprintf("%.1f%%", r.SL*100),
			fmt.Sprintf("%s%.0f", cs, r.TotalInit),
			fmt.Sprintf("%s%.0f", cs, r.FinalAmount),
			fmt.Sprintf("%.2f%%", ret),
		)
	}
	tw.Render()

	// Strategy metrics for top TP levels
	fmt.Println("\n🔍 Strategy Metrics (fixed SL=5%):")
	tw2 := output.NewTableWriter(os.Stdout)
	tw2.SetHeaders("TP%", "Sharpe", "Avg Return", "Win Rate")
	for tp := tpMin; tp <= tpMax+0.001; tp += tpStep {
		m := backtest.EvaluateStrategy(entries, tp, 0.05)
		tw2.AddRow(
			fmt.Sprintf("%.1f%%", m.TP*100),
			fmt.Sprintf("%.2f", m.Sharpe),
			fmt.Sprintf("%.2f%%", m.AvgReturn*100),
			fmt.Sprintf("%.1f%%", m.WinRate*100),
		)
	}
	tw2.Render()

	fmt.Println("\n✅ Backtest completed.")
	return nil
}

func loadBreakoutEntries(path string) ([]backtest.BreakoutEntry, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	reader := csv.NewReader(f)
	records, err := reader.ReadAll()
	if err != nil {
		return nil, err
	}

	if len(records) < 2 {
		return nil, fmt.Errorf("CSV has no data rows")
	}

	// Find column indices
	header := records[0]
	colIdx := make(map[string]int)
	for i, h := range header {
		colIdx[strings.TrimSpace(strings.ToLower(h))] = i
	}

	var entries []backtest.BreakoutEntry
	for _, row := range records[1:] {
		entry := backtest.BreakoutEntry{}

		if idx, ok := colIdx["symbol"]; ok && idx < len(row) {
			entry.Symbol = row[idx]
		}
		if idx, ok := colIdx["entry_date"]; ok && idx < len(row) {
			entry.EntryDate = row[idx]
		}
		if idx, ok := colIdx["entry_price"]; ok && idx < len(row) {
			entry.EntryPrice, _ = strconv.ParseFloat(row[idx], 64)
		}
		if idx, ok := colIdx["highs"]; ok && idx < len(row) {
			entry.Highs = parseFloatList(row[idx])
		}
		if idx, ok := colIdx["lows"]; ok && idx < len(row) {
			entry.Lows = parseFloatList(row[idx])
		}
		if idx, ok := colIdx["closes"]; ok && idx < len(row) {
			entry.Closes = parseFloatList(row[idx])
		}

		if entry.EntryPrice > 0 {
			entries = append(entries, entry)
		}
	}

	return entries, nil
}

// parseFloatList parses a Python-style list string like "[1.0, 2.0, 3.0]"
func parseFloatList(s string) []float64 {
	s = strings.TrimSpace(s)
	s = strings.Trim(s, "[]")
	if s == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	var result []float64
	for _, p := range parts {
		v, err := strconv.ParseFloat(strings.TrimSpace(p), 64)
		if err == nil {
			result = append(result, v)
		}
	}
	return result
}

// buildEntriesFromScan runs a scan with the given strategy and builds
// BreakoutEntry objects from the results.
func buildEntriesFromScan(strategy string) ([]backtest.BreakoutEntry, error) {
	ctx := context.Background()

	// Load tickers
	rawTickers, err := marketdata.GetUniverse(appConfig.General.Market)
	if err != nil {
		return nil, fmt.Errorf("no built-in universe for %s: %w", appConfig.General.Market, err)
	}

	tickers := make([]string, len(rawTickers))
	for i, t := range rawTickers {
		tickers[i] = activeMarket.ApplySuffix(t)
	}

	// Create provider (with disk cache)
	yahoo := marketdata.NewYahooProvider(5)
	var provider marketdata.Provider = yahoo
	if !noCache {
		provider = marketdata.NewDiskCachedProvider(yahoo, 24*time.Hour)
	}

	// Get screeners
	registry := screener.Registry(appConfig)
	var screeners []screener.Screener
	if strategy == "all" {
		for _, s := range registry {
			screeners = append(screeners, s)
		}
		// Layer in-memory cache for scan-all
		if !noCache {
			provider = marketdata.NewCachedProviderFrom(provider)
		} else {
			provider = marketdata.NewCachedProvider(yahoo)
		}
	} else {
		s, ok := registry[strategy]
		if !ok {
			return nil, fmt.Errorf("unknown strategy: %s", strategy)
		}
		screeners = append(screeners, s)
	}

	// Fetch benchmark
	logf("📊 Scanning %d tickers with %s for backtest...\n", len(tickers), strategy)
	benchmark, _ := provider.GetHistory(ctx, activeMarket.Benchmark, "5y", "1d")

	// Run scan and build entries
	var entries []backtest.BreakoutEntry
	w := appConfig.General.Workers
	if w <= 0 {
		w = 8
	}

	for _, scr := range screeners {
		results, _ := runScreener(ctx, scr, tickers, provider, benchmark, w)
		for _, r := range results {
			if r.Score < 1.0 {
				continue // Only fully passing stocks for backtest
			}
			// Re-fetch the data (should hit cache) to build the entry
			data, err := provider.GetHistory(ctx, r.Ticker, "5y", "1d")
			if err != nil || len(data) < 60 {
				continue
			}

			n := len(data)
			// Use last 60 trading days as the backtest window
			window := 60
			if n < window {
				window = n
			}
			tail := data[n-window:]

			highs := marketdata.Highs(tail)
			lows := marketdata.Lows(tail)
			closes := marketdata.Closes(tail)

			entries = append(entries, backtest.BreakoutEntry{
				Symbol:     r.Ticker,
				EntryDate:  tail[0].Date.Format("2006-01-02"),
				EntryPrice: closes[0],
				Highs:      highs,
				Lows:       lows,
				Closes:     closes,
			})
		}
	}

	return entries, nil
}
