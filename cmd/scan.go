package cmd

import (
	"bufio"
	"context"
	"encoding/csv"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/rohitsharma04/stockctl/internal/marketdata"
	"github.com/rohitsharma04/stockctl/internal/output"
	"github.com/rohitsharma04/stockctl/internal/screener"
	"github.com/spf13/cobra"
)

var (
	scanDate    string
	tickersFile string
	minPrice    float64
	workers     int
	months      int
)

var scanCmd = &cobra.Command{
	Use:   "scan [strategy]",
	Short: "Screen stocks using a strategy",
	Long: `Run a stock screening strategy against a universe of tickers.

Available strategies:
  breakout-caution     Bollinger Band breakout + volume + relative strength
  high-performance     Sustained uptrend with consistent new highs
  stellar-breakout     Volume explosion + Heikin-Ashi confirmation
  descending-breakout  Descending triangle breakout with volume
  all                  Run all screeners

Examples:
  stockctl scan breakout-caution
  stockctl scan high-performance --tickers us_stocks.csv
  stockctl scan all --market india --output json`,
	Args: cobra.ExactArgs(1),
	RunE: runScan,
}

func init() {
	scanCmd.Flags().StringVar(&scanDate, "date", "", "backtest date (YYYY-MM-DD), default: today")
	scanCmd.Flags().StringVar(&tickersFile, "tickers", "", "CSV file with tickers (default from config)")
	scanCmd.Flags().Float64Var(&minPrice, "min-price", 0, "minimum stock price (default from config)")
	scanCmd.Flags().IntVar(&workers, "workers", 0, "number of concurrent workers (default from config)")
	scanCmd.Flags().IntVar(&months, "months", 0, "months for descending-breakout (default from config)")
	rootCmd.AddCommand(scanCmd)
}

func runScan(cmd *cobra.Command, args []string) error {
	strategy := args[0]
	ctx := context.Background()

	// Resolve config overrides
	tf := appConfig.General.TickersFile
	if tickersFile != "" {
		tf = tickersFile
	}

	w := appConfig.General.Workers
	if workers > 0 {
		w = workers
	}
	if w <= 0 {
		w = runtime.NumCPU()
	}

	// Load tickers
	var rawTickers []string
	var err error
	if tf != "" {
		rawTickers, err = loadTickers(tf)
		if err != nil {
			return fmt.Errorf("loading tickers: %w", err)
		}
	} else {
		// Auto-resolve from universe
		rawTickers, err = marketdata.FetchUniverse(appConfig.General.Market, false)
		if err != nil {
			return fmt.Errorf("auto-resolving tickers for %s: %w\nUse --tickers to specify a CSV file", appConfig.General.Market, err)
		}
		fmt.Printf("📋 Auto-loaded %d tickers from %s universe\n", len(rawTickers), appConfig.General.Market)
	}

	// Apply market suffix to each ticker
	tickers := make([]string, len(rawTickers))
	for i, t := range rawTickers {
		tickers[i] = activeMarket.ApplySuffix(t)
	}

	// Create provider (cached for "all" to avoid redundant API calls)
	yahoo := marketdata.NewYahooProvider(5)
	var provider marketdata.Provider = yahoo
	if strategy == "all" {
		provider = marketdata.NewCachedProvider(yahoo)
	}

	fmt.Printf("🌍 Market: %s (%s)\n", activeMarket.Name, activeMarket.Currency)

	// Get registry
	registry := screener.Registry(appConfig)

	// Determine which screeners to run
	var screeners []screener.Screener
	if strategy == "all" {
		for _, s := range registry {
			screeners = append(screeners, s)
		}
	} else {
		s, ok := registry[strategy]
		if !ok {
			return fmt.Errorf("unknown strategy: %s\nAvailable: breakout-caution, high-performance, stellar-breakout, descending-breakout, all", strategy)
		}
		screeners = append(screeners, s)
	}

	// Fetch benchmark data for screeners that need it
	fmt.Printf("📊 Fetching benchmark data (%s)...\n", activeMarket.Benchmark)
	benchmarkData, err := provider.GetHistory(ctx, activeMarket.Benchmark, "5y", "1d")
	if err != nil {
		fmt.Fprintf(os.Stderr, "⚠️  Could not fetch benchmark: %v (relative strength checks will be skipped)\n", err)
	}

	// Run screening
	if appConfig.General.Output == "csv" {
		fmt.Printf("📂 Output directory: %s\n", runDir)
	}
	for _, scr := range screeners {
		fmt.Printf("\n🔍 Running %s screener on %d tickers (workers: %d)...\n", scr.Name(), len(tickers), w)
		results := runScreener(ctx, scr, tickers, provider, benchmarkData, w)

		// Output results
		fmt.Printf("✅ %s: %d stocks passed\n", scr.Name(), len(results))
		if len(results) > 0 {
			renderResults(scr.Name(), results, appConfig.General.Output)
		}
	}

	return nil
}

type scanResult struct {
	Ticker        string  `json:"ticker"`
	Strategy      string  `json:"strategy"`
	Score         float64 `json:"score"`
	FiltersPassed int     `json:"filters_passed"`
	TotalFilters  int     `json:"total_filters"`
}

func runScreener(ctx context.Context, scr screener.Screener, tickers []string,
	provider marketdata.Provider, benchmark []marketdata.OHLCV, workers int) []scanResult {

	type job struct {
		ticker string
	}

	jobs := make(chan job, len(tickers))
	var results []scanResult
	var mu sync.Mutex
	var processed int64
	total := int64(len(tickers))

	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := range jobs {
				data, err := provider.GetHistory(ctx, j.ticker, "5y", "1d")
				if err != nil {
					if verbose {
						fmt.Fprintf(os.Stderr, "  ⚠ %s: %v\n", j.ticker, err)
					}
					atomic.AddInt64(&processed, 1)
					continue
				}

				result, err := scr.Screen(ctx, data, benchmark)
				if err != nil {
					if verbose {
						fmt.Fprintf(os.Stderr, "  ⚠ %s: %v\n", j.ticker, err)
					}
					atomic.AddInt64(&processed, 1)
					continue
				}

				if result.Pass {
					passed := 0
					for _, f := range result.Filters {
						if f.Pass {
							passed++
						}
					}
					mu.Lock()
					results = append(results, scanResult{
						Ticker:        j.ticker,
						Strategy:      scr.Name(),
						Score:         result.Score,
						FiltersPassed: passed,
						TotalFilters:  len(result.Filters),
					})
					mu.Unlock()
				}

				count := atomic.AddInt64(&processed, 1)
				if count%50 == 0 || count == total {
					fmt.Printf("  📈 Progress: %d/%d\n", count, total)
				}
			}
		}()
	}

	for _, t := range tickers {
		jobs <- job{ticker: t}
	}
	close(jobs)
	wg.Wait()

	return results
}

func renderResults(strategyName string, results []scanResult, format string) {
	switch output.Format(format) {
	case output.FormatJSON:
		output.WriteJSON(os.Stdout, results)

	case output.FormatCSV:
		filename := filepath.Join(runDir, fmt.Sprintf("%s_%s.csv", strategyName, time.Now().Format("2006-01-02_150405")))
		headers := []string{"Ticker", "Strategy", "Score", "Filters Passed", "Total Filters"}
		var rows [][]string
		for _, r := range results {
			rows = append(rows, []string{r.Ticker, r.Strategy, fmt.Sprintf("%.2f", r.Score), fmt.Sprintf("%d", r.FiltersPassed), fmt.Sprintf("%d", r.TotalFilters)})
		}
		if err := output.WriteCSV(filename, headers, rows); err != nil {
			fmt.Fprintf(os.Stderr, "Error writing CSV: %v\n", err)
		} else {
			fmt.Printf("📁 Results saved to %s\n", filename)
		}

	default: // table
		tw := output.NewTableWriter(os.Stdout)
		tw.SetHeaders("Ticker", "Strategy", "Score", "Filters")
		for _, r := range results {
			tw.AddRow(r.Ticker, r.Strategy, fmt.Sprintf("%.0f%%", r.Score*100), fmt.Sprintf("%d/%d", r.FiltersPassed, r.TotalFilters))
		}
		tw.Render()
	}
}

func loadTickers(path string) ([]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	// Try CSV first
	if strings.HasSuffix(path, ".csv") {
		reader := csv.NewReader(f)
		records, err := reader.ReadAll()
		if err != nil {
			return nil, err
		}

		// Find Symbol column
		if len(records) == 0 {
			return nil, fmt.Errorf("empty CSV file")
		}

		symIdx := -1
		for i, h := range records[0] {
			if strings.EqualFold(strings.TrimSpace(h), "symbol") || strings.EqualFold(strings.TrimSpace(h), "ticker") {
				symIdx = i
				break
			}
		}
		if symIdx == -1 {
			symIdx = 0 // assume first column
		}

		var tickers []string
		for _, row := range records[1:] {
			if symIdx < len(row) {
				ticker := strings.TrimSpace(row[symIdx])
				if ticker != "" {
					// Replace / with - (same as Python scripts)
					ticker = strings.ReplaceAll(ticker, "/", "-")
					tickers = append(tickers, ticker)
				}
			}
		}
		return tickers, nil
	}

	// Plain text, one ticker per line
	var tickers []string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		t := strings.TrimSpace(scanner.Text())
		if t != "" {
			tickers = append(tickers, t)
		}
	}
	return tickers, scanner.Err()
}
