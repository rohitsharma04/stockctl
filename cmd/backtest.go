package cmd

import (
	"encoding/csv"
	"fmt"
	"math"
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

var backtestProviderFactory = func(noCache bool) marketdata.Provider { return marketdata.BuildProvider(noCache) }

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
	tpMin, tpMax, err := parseBacktestRange("--tp-range", btTPRange, cfg.TPMin, cfg.TPMax)
	if err != nil {
		return err
	}
	slMin, slMax, err := parseBacktestRange("--sl-range", btSLRange, cfg.SLMin, cfg.SLMax)
	if err != nil {
		return err
	}
	capital := cfg.Capital
	if btCapital != 0 {
		capital = btCapital
	}
	tpStep, slStep := cfg.TPStep, cfg.SLStep
	params := backtestParameters{tpMin: tpMin, tpMax: tpMax, tpStep: tpStep, slMin: slMin, slMax: slMax, slStep: slStep, minRewardRisk: cfg.MinRewardRisk, capital: capital}
	if err := validateBacktestParameters(params); err != nil {
		return err
	}

	// Load entries — either from strategy scan or CSV file
	var entries []backtest.BreakoutEntry
	var strategySnapshot *strategyBacktestDiagnostics
	if btStrategy != "" {
		// Strategy mode: run scan internally and construct entries
		var err error
		entries, strategySnapshot, err = buildEntriesFromScanWithSnapshot(btStrategy)
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
			Optimized         []backtest.TradeResult     `json:"optimized"`
			Metrics           []backtest.StrategyMetrics `json:"metrics"`
			DataQuality       *output.DataQualitySummary `json:"data_quality,omitempty"`
			FetchErrors       []output.ErrorInfo         `json:"fetch_errors,omitempty"`
			EntriesConsidered int                        `json:"entries_considered,omitempty"`
			EntriesUsed       int                        `json:"entries_used,omitempty"`
		}
		var metrics []backtest.StrategyMetrics
		for tp := tpMin; tp <= tpMax+0.001; tp += tpStep {
			metrics = append(metrics, backtest.EvaluateStrategy(entries, tp, 0.05))
		}
		meta := output.NewMeta("backtest")
		meta.Market = activeMarket.ID
		if btStrategy != "" {
			meta.Strategy = btStrategy
		}
		meta.DurationMs = time.Since(startTime).Milliseconds()
		env := output.Envelope{
			Meta: meta,
			Results: func() btResult {
				result := btResult{Optimized: results, Metrics: metrics}
				if strategySnapshot != nil {
					result.DataQuality = &strategySnapshot.DataQuality
					result.FetchErrors = strategySnapshot.FetchErrors
					result.EntriesConsidered = strategySnapshot.EntriesConsidered
					result.EntriesUsed = strategySnapshot.EntriesUsed
				}
				return result
			}(),
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
	tw2.SetHeaders("TP%", "Sharpe", "Avg Return", "Win Rate", "Avg Win", "Avg Loss", "Expectancy", "MaxDD", "Exposure")
	for tp := tpMin; tp <= tpMax+0.001; tp += tpStep {
		m := backtest.EvaluateStrategy(entries, tp, 0.05)
		tw2.AddRow(
			fmt.Sprintf("%.1f%%", m.TP*100),
			fmt.Sprintf("%.2f", m.Sharpe),
			fmt.Sprintf("%.2f%%", m.AvgReturn*100),
			fmt.Sprintf("%.1f%%", m.WinRate*100),
			fmt.Sprintf("%.2f%%", m.AvgWin*100),
			fmt.Sprintf("%.2f%%", m.AvgLoss*100),
			fmt.Sprintf("%.2f%%", m.Expectancy*100),
			fmt.Sprintf("%.1f%%", m.MaxDrawdown*100),
			fmt.Sprintf("%.0f%%", m.ExposurePct*100),
		)
	}
	tw2.Render()

	fmt.Println("\n✅ Backtest completed.")
	return nil
}

type backtestParameters struct {
	tpMin, tpMax, tpStep float64
	slMin, slMax, slStep float64
	minRewardRisk        float64
	capital              float64
}

func parseBacktestRange(flag, value string, defaultMin, defaultMax float64) (float64, float64, error) {
	if value == "" {
		return defaultMin, defaultMax, nil
	}
	parts := strings.Split(value, ":")
	if len(parts) != 2 {
		return 0, 0, fmt.Errorf("%s must be min:max", flag)
	}
	min, err := strconv.ParseFloat(strings.TrimSpace(parts[0]), 64)
	if err != nil || !isPositiveFinite(min) {
		return 0, 0, fmt.Errorf("%s minimum must be a positive finite number", flag)
	}
	max, err := strconv.ParseFloat(strings.TrimSpace(parts[1]), 64)
	if err != nil || !isPositiveFinite(max) {
		return 0, 0, fmt.Errorf("%s maximum must be a positive finite number", flag)
	}
	if min > max {
		return 0, 0, fmt.Errorf("%s minimum must not exceed maximum", flag)
	}
	return min, max, nil
}

func validateBacktestParameters(p backtestParameters) error {
	for _, field := range []struct {
		name  string
		value float64
	}{
		{"take-profit minimum", p.tpMin}, {"take-profit maximum", p.tpMax}, {"take-profit step", p.tpStep},
		{"stop-loss minimum", p.slMin}, {"stop-loss maximum", p.slMax}, {"stop-loss step", p.slStep},
		{"capital", p.capital},
	} {
		if !isPositiveFinite(field.value) {
			return fmt.Errorf("backtest %s must be a positive finite number", field.name)
		}
	}
	if p.tpMin > p.tpMax || p.slMin > p.slMax {
		return fmt.Errorf("backtest range minimum must not exceed maximum")
	}
	if math.IsNaN(p.minRewardRisk) || math.IsInf(p.minRewardRisk, 0) || p.minRewardRisk < 0 {
		return fmt.Errorf("backtest minimum reward/risk must be a non-negative finite number")
	}
	return nil
}

func isPositiveFinite(v float64) bool {
	return v > 0 && !math.IsNaN(v) && !math.IsInf(v, 0)
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
type strategyBacktestDiagnostics struct {
	DataQuality       output.DataQualitySummary `json:"data_quality"`
	FetchErrors       []output.ErrorInfo        `json:"fetch_errors"`
	EntriesConsidered int                       `json:"entries_considered"`
	EntriesUsed       int                       `json:"entries_used"`
}

// strategyBacktestSnapshotError keeps the failed scan's diagnostics available
// to the root JSON error envelope without changing the command's exit status.
type strategyBacktestSnapshotError struct {
	err         error
	diagnostics strategyBacktestDiagnostics
}

func (e *strategyBacktestSnapshotError) Error() string { return e.err.Error() }
func (e *strategyBacktestSnapshotError) Unwrap() error { return e.err }
func (e *strategyBacktestSnapshotError) JSONResults() interface{} {
	return e.diagnostics
}

func strategyBacktestSnapshot(snapshot scanSnapshot, considered, used int) strategyBacktestDiagnostics {
	quality := snapshot.dataQualitySummary()
	quality.BenchmarkAvailable = snapshot.benchmarkAvailable
	quality.BenchmarkBars = len(snapshot.benchmarkData)
	return strategyBacktestDiagnostics{
		DataQuality: quality, FetchErrors: append([]output.ErrorInfo(nil), snapshot.errors...),
		EntriesConsidered: considered, EntriesUsed: used,
	}
}

// buildEntriesFromScan preserves the internal helper used by callers that do
// not need the scan diagnostics.
func buildEntriesFromScan(strategy string) ([]backtest.BreakoutEntry, error) {
	entries, _, err := buildEntriesFromScanWithSnapshot(strategy)
	return entries, err
}

func buildEntriesFromScanWithSnapshot(strategy string) ([]backtest.BreakoutEntry, *strategyBacktestDiagnostics, error) {
	ctx := rootCtx

	// Load tickers
	rawTickers, err := marketdata.GetUniverse(appConfig.General.Market)
	if err != nil {
		return nil, nil, fmt.Errorf("no built-in universe for %s: %w", appConfig.General.Market, err)
	}

	tickers := make([]string, len(rawTickers))
	for i, t := range rawTickers {
		tickers[i] = activeMarket.ApplySuffix(t)
	}

	// Create provider (with disk cache + circuit breaker)
	provider := backtestProviderFactory(noCache)

	// Get screeners
	registry := screener.Registry(appConfig)
	var screeners []screener.Screener
	if strategy == "all" {
		for _, s := range registry {
			screeners = append(screeners, s)
		}
	} else {
		s, ok := registry[strategy]
		if !ok {
			return nil, nil, fmt.Errorf("unknown strategy: %s", strategy)
		}
		screeners = append(screeners, s)
	}

	// Fetch the universe once and share its immutable snapshot among strategies.
	logf("📊 Scanning %d tickers with %s for backtest...\n", len(tickers), strategy)
	w := appConfig.General.Workers
	if w <= 0 {
		w = 8
	}
	snapshot, _ := fetchScanSnapshot(ctx, tickers, provider, activeMarket.Benchmark, w, time.Time{})
	diagnostics := strategyBacktestSnapshot(snapshot, 0, 0)
	diagnostics.DataQuality.BenchmarkSymbol = activeMarket.Benchmark
	if !snapshot.benchmarkAvailable {
		return nil, &diagnostics, &strategyBacktestSnapshotError{
			err:         fmt.Errorf("backtest strategy requires benchmark data for %s", activeMarket.Benchmark),
			diagnostics: diagnostics,
		}
	}
	if len(snapshot.errors) > len(tickers)/2 {
		return nil, &diagnostics, &strategyBacktestSnapshotError{
			err:         fmt.Errorf("backtest strategy data quality insufficient: %d of %d ticker fetches failed", len(snapshot.errors), len(tickers)),
			diagnostics: diagnostics,
		}
	}

	// Run scan and build entries
	var entries []backtest.BreakoutEntry
	considered := 0

	for _, scr := range screeners {
		results, _, _ := runScreenerFromSnapshot(ctx, scr, tickers, snapshot, w)
		for _, r := range results {
			considered++
			if r.Score < 1.0 {
				continue // Only fully passing stocks for backtest
			}
			data := snapshot.tickerData[r.Ticker]
			if len(data) < 60 {
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

	diagnostics = strategyBacktestSnapshot(snapshot, considered, len(entries))
	diagnostics.DataQuality.BenchmarkSymbol = activeMarket.Benchmark
	return entries, &diagnostics, nil
}
