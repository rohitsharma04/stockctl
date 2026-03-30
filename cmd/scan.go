package cmd

import (
	"bufio"
	"context"
	"encoding/csv"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
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
	minScore    float64
	sortBy      string
	scanDetail  bool
	scanDryRun  bool
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
  rsi-bounce           RSI oversold bounce with volume confirmation
  macd-crossover       MACD bullish crossover with trend confirmation
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
	scanCmd.Flags().Float64Var(&minScore, "min-score", 1.0, "minimum score to include (0.0-1.0, default 1.0 = only full passes)")
	scanCmd.Flags().StringVar(&sortBy, "sort", "score", "sort results by: score, ticker, filters")
	scanCmd.Flags().BoolVar(&scanDetail, "detail", false, "include per-filter breakdown in results")
	scanCmd.Flags().BoolVar(&scanDryRun, "dry-run", false, "show scan plan without fetching data")
	rootCmd.AddCommand(scanCmd)
}

func runScan(cmd *cobra.Command, args []string) error {
	strategy := args[0]
	ctx := rootCtx
	startTime := time.Now()

	// Parse --date for historical analysis
	var asOfDate time.Time
	if scanDate != "" {
		var err error
		asOfDate, err = time.Parse("2006-01-02", scanDate)
		if err != nil {
			return fmt.Errorf("invalid --date format (expected YYYY-MM-DD): %w", err)
		}
		logf("📅 Historical analysis as of: %s\n", asOfDate.Format("2006-01-02"))
	}

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
		// Auto-resolve from embedded universe
		rawTickers, err = marketdata.GetUniverse(appConfig.General.Market)
		if err != nil {
			return fmt.Errorf("no built-in universe for %s: %w\nUse --tickers to specify a CSV file, or run 'stockctl tickers' to see available markets", appConfig.General.Market, err)
		}
		logf("📋 Loaded %d tickers from built-in %s universe\n", len(rawTickers), appConfig.General.Market)
	}

	// Apply market suffix to each ticker
	tickers := make([]string, len(rawTickers))
	for i, t := range rawTickers {
		tickers[i] = activeMarket.ApplySuffix(t)
	}

	// Create provider with disk cache + circuit breaker (unless --no-cache)
	provider := marketdata.BuildProvider(noCache)

	logf("🌍 Market: %s (%s)\n", activeMarket.Name, activeMarket.Currency)

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

	// Dry-run mode: output plan and exit
	if scanDryRun {
		type scanPlan struct {
			Market          string   `json:"market"`
			Tickers         int      `json:"tickers"`
			Strategy        string   `json:"strategy"`
			StrategiesCount int      `json:"strategies_count"`
			StrategyNames   []string `json:"strategy_names"`
			Workers         int      `json:"workers"`
			EstDurationSec  int      `json:"estimated_duration_s"`
		}
		names := make([]string, len(screeners))
		for i, s := range screeners {
			names[i] = s.Name()
		}
		// Rough estimate: ~0.5s per ticker per strategy with cache
		estSec := len(tickers) * len(screeners) / w
		if estSec < 5 {
			estSec = 5
		}
		plan := scanPlan{
			Market:          activeMarket.ID,
			Tickers:         len(tickers),
			Strategy:        strategy,
			StrategiesCount: len(screeners),
			StrategyNames:   names,
			Workers:         w,
			EstDurationSec:  estSec,
		}
		if appConfig.General.Output == "json" {
			meta := output.NewMeta("scan-plan")
			meta.Market = activeMarket.ID
			meta.Strategy = strategy
			env := output.Envelope{
				Meta:    meta,
				Results: plan,
			}
			return output.WriteEnvelope(os.Stdout, env)
		}
		logf("📋 Dry-run plan:\n")
		logf("   Market:     %s (%s)\n", activeMarket.Name, activeMarket.ID)
		logf("   Tickers:    %d\n", len(tickers))
		logf("   Strategy:   %s (%d screeners)\n", strategy, len(screeners))
		logf("   Workers:    %d\n", w)
		logf("   Est. time:  ~%ds\n", estSec)
		return nil
	}

	// Fetch benchmark data for screeners that need it
	logf("📊 Fetching benchmark data (%s)...\n", activeMarket.Benchmark)
	benchmarkData, err := provider.GetHistory(ctx, activeMarket.Benchmark, "5y", "1d")
	if err != nil {
		logf("⚠️  Could not fetch benchmark: %v (relative strength checks will be skipped)\n", err)
	}

	// Truncate benchmark data to the as-of date
	if !asOfDate.IsZero() && benchmarkData != nil {
		benchmarkData, err = marketdata.TruncateAt(benchmarkData, asOfDate)
		if err != nil {
			logf("⚠️  Could not truncate benchmark to %s: %v\n", asOfDate.Format("2006-01-02"), err)
			benchmarkData = nil
		}
	}

	// Run screening
	if appConfig.General.Output == "csv" {
		logf("📂 Output directory: %s\n", runDir)
	}

	var allResults []scanResult
	var allErrors []output.ErrorInfo
	totalScanned := len(tickers)
	totalFailed := 0

	for _, scr := range screeners {
		logf("\n🔍 Running %s screener on %d tickers (workers: %d)...\n", scr.Name(), len(tickers), w)
		results, errors := runScreener(ctx, scr, tickers, provider, benchmarkData, w, asOfDate)

		totalFailed += len(errors)
		allErrors = append(allErrors, errors...)

		// Output results
		logf("✅ %s: %d stocks passed\n", scr.Name(), len(results))
		allResults = append(allResults, results...)

		if len(results) > 0 && appConfig.General.Output != "json" {
			renderResults(scr.Name(), results, appConfig.General.Output)
		}
	}

	// Sort results deterministically
	sort.Slice(allResults, func(i, j int) bool {
		switch sortBy {
		case "ticker":
			return allResults[i].Ticker < allResults[j].Ticker
		case "filters":
			if allResults[i].FiltersPassed != allResults[j].FiltersPassed {
				return allResults[i].FiltersPassed > allResults[j].FiltersPassed
			}
			return allResults[i].Ticker < allResults[j].Ticker
		default: // "score"
			if allResults[i].Score != allResults[j].Score {
				return allResults[i].Score > allResults[j].Score
			}
			return allResults[i].Ticker < allResults[j].Ticker
		}
	})

	// JSON output with envelope
	if appConfig.General.Output == "json" {
		meta := output.NewMeta("scan")
		meta.Market = activeMarket.ID
		meta.Strategy = strategy
		if !asOfDate.IsZero() {
			meta.AsOfDate = asOfDate.Format("2006-01-02")
		}
		meta.TickersScanned = totalScanned
		meta.TickersFailed = totalFailed
		meta.DurationMs = time.Since(startTime).Milliseconds()
		env := output.Envelope{
			Meta:    meta,
			Results: allResults,
			Errors:  allErrors,
		}
		return output.WriteEnvelope(os.Stdout, env)
	}

	return nil
}

type scanResult struct {
	Ticker        string                  `json:"ticker"`
	Strategy      string                  `json:"strategy"`
	Score         float64                 `json:"score"`
	FiltersPassed int                     `json:"filters_passed"`
	TotalFilters  int                     `json:"total_filters"`
	ClosePrice    float64                 `json:"close_price,omitempty"`
	Volume        float64                 `json:"volume,omitempty"`
	ChangePct     float64                 `json:"change_pct,omitempty"`
	Filters       []screener.FilterResult `json:"filters,omitempty"`
}

func runScreener(ctx context.Context, scr screener.Screener, tickers []string,
	provider marketdata.Provider, benchmark []marketdata.OHLCV, workers int, asOfDate time.Time) ([]scanResult, []output.ErrorInfo) {

	screenerStart := time.Now()
	type job struct {
		ticker string
	}

	jobs := make(chan job, len(tickers))
	var results []scanResult
	var errors []output.ErrorInfo
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
						logf("  ⚠ %s: %v\n", j.ticker, err)
					}
					mu.Lock()
					errors = append(errors, output.ErrorInfo{Ticker: j.ticker, Error: err.Error()})
					mu.Unlock()
					atomic.AddInt64(&processed, 1)
					continue
				}

				// Truncate data to as-of date for historical analysis
				if !asOfDate.IsZero() {
					data, err = marketdata.TruncateAt(data, asOfDate)
					if err != nil {
						if verbose {
							logf("  ⚠ %s: %v\n", j.ticker, err)
						}
						mu.Lock()
						errors = append(errors, output.ErrorInfo{Ticker: j.ticker, Error: err.Error()})
						mu.Unlock()
						atomic.AddInt64(&processed, 1)
						continue
					}
				}

				result, err := scr.Screen(ctx, data, benchmark)
				if err != nil {
					if verbose {
						logf("  ⚠ %s: %v\n", j.ticker, err)
					}
					mu.Lock()
					errors = append(errors, output.ErrorInfo{Ticker: j.ticker, Error: err.Error()})
					mu.Unlock()
					atomic.AddInt64(&processed, 1)
					continue
				}

				if result.Score >= minScore {
					passed := 0
					for _, f := range result.Filters {
						if f.Pass {
							passed++
						}
					}

					// Extract price data from already-fetched data
					dn := len(data)
					closePrice := data[dn-1].Close
					volume := data[dn-1].Volume
					changePct := 0.0
					if dn >= 2 && data[dn-2].Close > 0 {
						changePct = (data[dn-1].Close - data[dn-2].Close) / data[dn-2].Close
					}

					sr := scanResult{
						Ticker:        j.ticker,
						Strategy:      scr.Name(),
						Score:         result.Score,
						FiltersPassed: passed,
						TotalFilters:  len(result.Filters),
						ClosePrice:    closePrice,
						Volume:        volume,
						ChangePct:     changePct,
					}
					if scanDetail {
						sr.Filters = result.Filters
					}
					mu.Lock()
					results = append(results, sr)
					mu.Unlock()
				}

				count := atomic.AddInt64(&processed, 1)
				if count%50 == 0 || count == total {
					reportProgress(int(count), int(total), time.Since(screenerStart).Milliseconds())
				}
			}
		}()
	}

	for _, t := range tickers {
		jobs <- job{ticker: t}
	}
	close(jobs)
	wg.Wait()

	return results, errors
}

func renderResults(strategyName string, results []scanResult, format string) {
	switch output.Format(format) {
	case output.FormatJSON:
		// Handled by envelope in runScan
		return

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
			logf("📁 Results saved to %s\n", filename)
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

