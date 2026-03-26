package cmd

import (
	"encoding/csv"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"

	"github.com/rohitsharma04/stockctl/internal/backtest"
	"github.com/rohitsharma04/stockctl/internal/output"
	"github.com/spf13/cobra"
)

var (
	btInput   string
	btTPRange string
	btSLRange string
	btCapital float64
)

var backtestCmd = &cobra.Command{
	Use:   "backtest",
	Short: "Backtest breakout strategies with TP/SL optimization",
	Long: `Run a grid search over take-profit and stop-loss combinations
to find the optimal strategy parameters.

Reads pre-processed breakout entries from a CSV file with columns:
  symbol, entry_date, entry_price, highs, lows, closes

Examples:
  stockctl backtest --input trading_results.csv
  stockctl backtest --input results.csv --tp-range 0.05:0.50 --sl-range 0.01:0.10
  stockctl backtest --input results.csv --capital 200000`,
	RunE: runBacktest,
}

func init() {
	backtestCmd.Flags().StringVar(&btInput, "input", "trading_results.csv", "CSV file with breakout entries")
	backtestCmd.Flags().StringVar(&btTPRange, "tp-range", "", "take-profit range min:max (default from config)")
	backtestCmd.Flags().StringVar(&btSLRange, "sl-range", "", "stop-loss range min:max (default from config)")
	backtestCmd.Flags().Float64Var(&btCapital, "capital", 0, "capital per trade (default from config)")
	rootCmd.AddCommand(backtestCmd)
}

func runBacktest(cmd *cobra.Command, args []string) error {
	cfg := appConfig.Backtest

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

	// Load entries
	entries, err := loadBreakoutEntries(btInput)
	if err != nil {
		return fmt.Errorf("loading entries: %w", err)
	}
	fmt.Printf("📊 Loaded %d breakout entries from %s\n", len(entries), btInput)

	if len(entries) == 0 {
		return fmt.Errorf("no entries found in %s", btInput)
	}

	// Run optimization
	fmt.Printf("⚙️  Optimizing TP: %.0f%%–%.0f%%, SL: %.0f%%–%.0f%% (TP ≥ %.0fx SL)...\n",
		tpMin*100, tpMax*100, slMin*100, slMax*100, cfg.MinRewardRisk)

	results := backtest.Optimize(entries, tpMin, tpMax, tpStep, slMin, slMax, slStep, cfg.MinRewardRisk, capital)

	// Sort by final amount descending
	sort.Slice(results, func(i, j int) bool {
		return results[i].FinalAmount > results[j].FinalAmount
	})

	// Summary
	totalInit := 0.0
	totalFinal := 0.0
	for _, r := range results {
		totalInit += r.TotalInit
		totalFinal += r.FinalAmount
	}

	fmt.Printf("\n📈 Performance Summary:\n")
	fmt.Printf("   Total Initial Investment: ₹%.2f\n", totalInit)
	fmt.Printf("   Total Final Amount:       ₹%.2f\n", totalFinal)
	fmt.Printf("   Net Profit:               ₹%.2f\n", totalFinal-totalInit)

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
			fmt.Sprintf("₹%.0f", r.TotalInit),
			fmt.Sprintf("₹%.0f", r.FinalAmount),
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
