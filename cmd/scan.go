package cmd

import (
	"bufio"
	"context"
	"encoding/csv"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/rohitsharma04/stockctl/internal/config"
	"github.com/rohitsharma04/stockctl/internal/indicators"
	"github.com/rohitsharma04/stockctl/internal/marketdata"
	"github.com/rohitsharma04/stockctl/internal/output"
	"github.com/rohitsharma04/stockctl/internal/screener"
	"github.com/spf13/cobra"
)

var (
	scanDate       string
	tickersFile    string
	minPrice       float64
	workers        int
	months         int
	minScore       float64
	sortBy         string
	scanDetail     bool
	scanDryRun     bool
	minTradedValue float64
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
	scanCmd.Flags().Float64Var(&minTradedValue, "min-traded-value", 0, "minimum 20-day avg traded value (price × volume)")
	rootCmd.AddCommand(scanCmd)
}

// scanResult is the per-ticker output in scan results.
type scanResult struct {
	Ticker             string  `json:"ticker"`
	Strategy           string  `json:"strategy"`
	Score              float64 `json:"score"`
	WeightedScore      float64 `json:"weighted_score"`
	DataConfidence     float64 `json:"data_confidence"`
	ActionabilityScore float64 `json:"actionability_score"`
	FiltersPassed      int     `json:"filters_passed"`
	TotalFilters       int     `json:"total_filters"`
	ClosePrice         float64 `json:"close_price,omitempty"`
	Volume             float64 `json:"volume,omitempty"`
	ChangePct          float64 `json:"change_pct,omitempty"`
	// Actionable watchlist fields
	Status            string  `json:"status"`        // confirmed_breakout, early_breakout, watch, avoid
	StatusReason      string  `json:"status_reason"` // volume_confirmation_missing, etc.
	TriggerPrice      float64 `json:"trigger_price,omitempty"`
	TriggerType       string  `json:"trigger_type,omitempty"`
	InvalidationPrice float64 `json:"invalidation_price,omitempty"`
	DistToTriggerPct  float64 `json:"dist_to_trigger_pct,omitempty"`
	VolumeRatio       float64 `json:"volume_ratio,omitempty"`
	RequiredVolRatio  float64 `json:"required_volume_ratio,omitempty"`
	ATRStop           float64 `json:"atr_stop,omitempty"`
	// Data health
	DataHealth     string   `json:"data_health"` // complete, partial, degraded
	ResultWarnings []string `json:"warnings,omitempty"`
	// Sector enrichment
	Sector         string  `json:"sector,omitempty"`
	Industry       string  `json:"industry,omitempty"`
	CapTier        string  `json:"cap_tier,omitempty"`
	AvgTradedValue float64 `json:"avg_traded_value,omitempty"`
	// Timeframe alignment
	TimeframeAlignment string `json:"timeframe_alignment,omitempty"`
	// Filters
	Filters []screener.FilterResult `json:"filters,omitempty"`
}

// scanEnvelope wraps scan results with market summary for JSON output.
type scanEnvelope struct {
	MarketSummary *screener.MarketSummary `json:"market_summary,omitempty"`
	Results       []scanResult            `json:"results"`
}

func runScan(cmd *cobra.Command, args []string) error {
	strategy := args[0]
	ctx := rootCtx
	startTime := time.Now()
	if err := validateScanOptions(minScore, minPrice, minTradedValue, sortBy); err != nil {
		return err
	}
	if err := applyScanOverrides(appConfig, months); err != nil {
		return err
	}

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
	effectivePriceFloor := effectiveMinPrice(minPrice, appConfig.General.MinPrice, activeMarket.MinPrice)
	// Keep strategy-level min-price filters aligned with the effective policy:
	// CLI override, then an explicit config value, then the active-market default.
	appConfig.General.MinPrice = effectivePriceFloor
	logf("📏 Min price: %s%.2f\n", activeMarket.CurrencySymbol, effectivePriceFloor)

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

	snapshot, scanWarnings := fetchScanSnapshot(ctx, tickers, provider, activeMarket.Benchmark, w, asOfDate)
	snapshot.applyPriceFloor(effectivePriceFloor)

	// Run screening
	if appConfig.General.Output == "csv" {
		logf("📂 Output directory: %s\n", runDir)
	}

	var allResults []scanResult
	allErrors := append([]output.ErrorInfo(nil), snapshot.errors...)
	breadthData := append([]screener.TickerBreadthData(nil), snapshot.breadthData...)
	totalScanned := len(tickers)

	for _, scr := range screeners {
		logf("\n🔍 Running %s screener on %d tickers (workers: %d)...\n", scr.Name(), len(tickers), w)
		results, errors, scoreUpdates := runScreenerFromSnapshot(ctx, scr, tickers, snapshot, w)

		allErrors = append(allErrors, errors...)
		breadthData = applyBreadthScoreUpdates(breadthData, scoreUpdates)

		// Output results
		logf("✅ %s: %d stocks passed\n", scr.Name(), len(results))
		allResults = append(allResults, results...)

		if len(results) > 0 && appConfig.General.Output != "json" {
			renderResults(scr.Name(), results, appConfig.General.Output)
		}
	}

	// Compute market summary
	var mktSummary *screener.MarketSummary
	if len(breadthData) > 0 {
		breadth := screener.ComputeBreadth(breadthData)
		var benchStatus screener.BenchmarkStatus
		if snapshot.benchmarkData != nil {
			benchStatus = screener.ComputeBenchmarkStatus(activeMarket.Benchmark, snapshot.benchmarkData)
		} else {
			benchStatus = screener.BenchmarkStatus{Symbol: activeMarket.Benchmark, TrendLabel: "unavailable"}
		}

		displayDate := time.Now().Format("2006-01-02")
		if !asOfDate.IsZero() {
			displayDate = asOfDate.Format("2006-01-02")
		}

		sectorBreadth := screener.ComputeSectorBreadth(breadthData)

		mktSummary = &screener.MarketSummary{
			MarketID:      activeMarket.ID,
			MarketName:    activeMarket.Name,
			AsOfDate:      displayDate,
			Benchmark:     benchStatus,
			Breadth:       breadth,
			SectorBreadth: sectorBreadth,
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
		meta.Currency = activeMarket.Currency
		meta.CurrencySymbol = activeMarket.CurrencySymbol
		if !asOfDate.IsZero() {
			meta.AsOfDate = asOfDate.Format("2006-01-02")
		}
		quality := snapshot.dataQualitySummary()
		quality.BenchmarkAvailable = snapshot.benchmarkAvailable
		quality.BenchmarkSymbol = activeMarket.Benchmark
		quality.BenchmarkBars = len(snapshot.benchmarkData)
		meta.TickersScanned = totalScanned
		meta.TickersFailed = quality.TickersFailed
		meta.DurationMs = time.Since(startTime).Milliseconds()
		meta.DataQuality = &quality
		meta.EffectivePolicy = &output.EffectivePolicy{
			MinPrice:       effectivePriceFloor,
			MinTradedValue: minTradedValue,
		}

		envResults := scanEnvelope{
			MarketSummary: mktSummary,
			Results:       allResults,
		}

		env := output.Envelope{
			Meta:     meta,
			Results:  envResults,
			Errors:   allErrors,
			Warnings: scanWarnings,
		}
		return output.WriteEnvelope(os.Stdout, env)
	}

	return nil
}

// applyScanOverrides applies CLI-only screener configuration without mutating
// unrelated strategies. A descending triangle needs at least two monthly bars:
// the implementation compares the final close with the prior month high.
func applyScanOverrides(cfg *config.Config, months int) error {
	if months == 0 {
		return nil
	}
	if months < 2 {
		return fmt.Errorf("--months must be at least 2")
	}
	if cfg == nil {
		return errors.New("scan configuration is not initialized")
	}
	if cfg.Screeners == nil {
		cfg.Screeners = make(map[string]config.ScreenerConfig)
	}
	descending := cfg.Screeners["descending_breakout"]
	descending.Months = months
	cfg.Screeners["descending_breakout"] = descending
	return nil
}

func validateScanOptions(minScore, minPrice, minTradedValue float64, sortBy string) error {
	if math.IsNaN(minScore) || math.IsInf(minScore, 0) || minScore < 0 || minScore > 1 {
		return fmt.Errorf("--min-score must be between 0 and 1")
	}
	if math.IsNaN(minPrice) || math.IsInf(minPrice, 0) || minPrice < 0 {
		return fmt.Errorf("--min-price must be a non-negative finite number")
	}
	if math.IsNaN(minTradedValue) || math.IsInf(minTradedValue, 0) || minTradedValue < 0 {
		return fmt.Errorf("--min-traded-value must be a non-negative finite number")
	}
	switch sortBy {
	case "score", "ticker", "filters":
		return nil
	default:
		return fmt.Errorf("unsupported --sort %q (allowed: score, ticker, filters)", sortBy)
	}
}

// tickerScanData holds per-ticker metadata collected during screening.
type tickerScanData struct {
	data        []marketdata.OHLCV
	aboveSMA50  bool
	aboveSMA200 bool
	newHigh     bool
	newLow      bool
}

type scanSnapshot struct {
	benchmarkData       []marketdata.OHLCV
	benchmarkAvailable  bool
	benchmarkProvenance marketdata.HistoryProvenance
	tickerData          map[string][]marketdata.OHLCV
	tickerProvenance    map[string]marketdata.HistoryProvenance
	excludedByPolicy    map[string]string
	breadthData         []screener.TickerBreadthData
	errors              []output.ErrorInfo
	upstreamFailures    int
}

type fetchedTickerData struct {
	index      int
	ticker     string
	data       []marketdata.OHLCV
	breadth    screener.TickerBreadthData
	provenance marketdata.HistoryProvenance
	err        error
}

type tickerScoreUpdate struct {
	ticker   string
	score    float64
	fullPass bool
}

func effectiveMinPrice(override, configured, marketDefault float64) float64 {
	if override > 0 {
		return override
	}
	if configured > 0 {
		return configured
	}
	return marketDefault
}

func meetsMinPrice(data []marketdata.OHLCV, floor float64) bool {
	close, ok := latestValidClose(data)
	return ok && close >= floor
}

// latestValidClose returns the most recent finite, positive close. It is kept
// pure so policy behavior is deterministic for missing and malformed bars.
func latestValidClose(data []marketdata.OHLCV) (float64, bool) {
	for i := len(data) - 1; i >= 0; i-- {
		close := data[i].Close
		if close > 0 && !math.IsNaN(close) && !math.IsInf(close, 0) {
			return close, true
		}
	}
	return 0, false
}

func getHistoryResult(ctx context.Context, provider marketdata.Provider, req marketdata.HistoryRequest) (*marketdata.HistoryResult, error) {
	if hp, ok := provider.(marketdata.HistoryProvider); ok {
		return hp.GetHistoryWithProvenance(ctx, req)
	}
	data, err := provider.GetHistory(ctx, req.Symbol, req.Period, req.Interval)
	if err != nil {
		return nil, err
	}
	provenance := marketdata.HistoryProvenance{Source: marketdata.HistorySourceUpstream}
	if len(data) > 0 {
		provenance.LastBarDate = data[len(data)-1].Date
	}
	return &marketdata.HistoryResult{Data: data, Provenance: provenance}, nil
}

func provenanceWithLastBar(provenance marketdata.HistoryProvenance, data []marketdata.OHLCV) marketdata.HistoryProvenance {
	if len(data) > 0 {
		provenance.LastBarDate = data[len(data)-1].Date
	}
	return provenance
}

func (s *scanSnapshot) applyPriceFloor(floor float64) {
	if s.excludedByPolicy == nil {
		s.excludedByPolicy = make(map[string]string)
	}
	for ticker, data := range s.tickerData {
		if !meetsMinPrice(data, floor) {
			s.excludedByPolicy[ticker] = "below_min_price"
		}
	}
}

func (s scanSnapshot) dataQualitySummary() output.DataQualitySummary {
	quality := output.DataQualitySummary{
		BenchmarkAvailable: s.benchmarkAvailable,
		BenchmarkBars:      len(s.benchmarkData),
		TickersFailed:      len(s.errors),
		UpstreamFailures:   s.upstreamFailures,
		SourceCounts:       make(map[string]int),
		AgeDistribution:    make(map[string]int),
	}
	var latestBar time.Time
	var fetchedAt time.Time
	now := time.Now()
	for ticker, data := range s.tickerData {
		provenance := s.tickerProvenance[ticker]
		source := string(provenance.Source)
		if source == "" {
			source = string(marketdata.HistorySourceUpstream)
		}
		quality.SourceCounts[source]++
		if provenance.Source == marketdata.HistorySourceCache {
			quality.CacheOnlyTickers++
		}
		if provenance.Source == marketdata.HistorySourceCacheAndUpstream {
			quality.DeltaRefreshedTickers++
		}
		_, usable := latestValidClose(data)
		if provenance.Stale || !usable {
			quality.TickersPartial++
			if provenance.Stale {
				quality.TickersStaleFallback++
			}
		} else {
			quality.TickersComplete++
		}
		if provenance.LastBarDate.After(latestBar) {
			latestBar = provenance.LastBarDate
		}
		if provenance.FetchedAt.After(fetchedAt) {
			fetchedAt = provenance.FetchedAt
		}
		lastBar := provenance.LastBarDate
		if lastBar.IsZero() && len(data) > 0 {
			lastBar = data[len(data)-1].Date
		}
		if !lastBar.IsZero() {
			ageDays := int(now.Sub(lastBar).Hours() / 24)
			switch {
			case ageDays <= 1:
				quality.AgeDistribution["0-1d"]++
			case ageDays <= 3:
				quality.AgeDistribution["2-3d"]++
			case ageDays <= 7:
				quality.AgeDistribution["4-7d"]++
			default:
				quality.AgeDistribution["gt_7d"]++
			}
		}
	}
	if !latestBar.IsZero() {
		quality.DataAsOf = latestBar.Format("2006-01-02")
	}
	if !fetchedAt.IsZero() {
		quality.ProviderFetchedAt = fetchedAt.UTC().Format(time.RFC3339)
	}
	if len(quality.SourceCounts) == 0 {
		quality.SourceCounts = nil
	}
	if len(quality.AgeDistribution) == 0 {
		quality.AgeDistribution = nil
	}
	return quality
}

func fetchScanSnapshot(ctx context.Context, tickers []string, provider marketdata.Provider, benchmarkSymbol string, workers int, asOfDate time.Time) (scanSnapshot, []output.Warning) {
	snapshot := scanSnapshot{
		tickerData:       make(map[string][]marketdata.OHLCV, len(tickers)),
		tickerProvenance: make(map[string]marketdata.HistoryProvenance, len(tickers)),
		excludedByPolicy: make(map[string]string),
	}
	var warnings []output.Warning

	if benchmarkSymbol != "" {
		logf("📊 Fetching benchmark data (%s)...\n", benchmarkSymbol)
		benchmarkResult, err := getHistoryResult(ctx, provider, marketdata.HistoryRequest{
			Symbol: benchmarkSymbol, Period: "5y", Interval: "1d", AsOf: asOfDate,
		})
		if err != nil {
			logf("⚠️  Could not fetch benchmark: %v (relative strength checks will be skipped)\n", err)
			warnings = append(warnings, output.Warning{
				Code:    "benchmark_missing",
				Message: fmt.Sprintf("Could not fetch benchmark %s: %v", benchmarkSymbol, err),
			})
			snapshot.errors = append(snapshot.errors, output.ErrorInfo{Ticker: benchmarkSymbol, Error: err.Error()})
		} else if !asOfDate.IsZero() {
			benchmarkResult.Data, err = marketdata.TruncateAt(benchmarkResult.Data, asOfDate)
			if err != nil {
				logf("⚠️  Could not truncate benchmark to %s: %v\n", asOfDate.Format("2006-01-02"), err)
				warnings = append(warnings, output.Warning{
					Code:    "benchmark_truncation_failed",
					Message: fmt.Sprintf("Benchmark truncation to %s failed: %v", asOfDate.Format("2006-01-02"), err),
				})
				benchmarkResult = nil
			}
		}
		if benchmarkResult != nil {
			benchmarkResult.Provenance = provenanceWithLastBar(benchmarkResult.Provenance, benchmarkResult.Data)
			snapshot.benchmarkData = cloneOHLCV(benchmarkResult.Data)
			snapshot.benchmarkProvenance = benchmarkResult.Provenance
			snapshot.benchmarkAvailable = true
		}
	}

	tickerData, provenance, errors, breadthData, upstreamFailures := fetchTickerSnapshotData(ctx, tickers, provider, workers, asOfDate)
	snapshot.tickerData = tickerData
	snapshot.tickerProvenance = provenance
	snapshot.errors = append(snapshot.errors, errors...)
	snapshot.breadthData = breadthData
	snapshot.upstreamFailures = upstreamFailures
	return snapshot, warnings
}

func fetchTickerSnapshotData(ctx context.Context, tickers []string, provider marketdata.Provider, workers int, asOfDate time.Time) (map[string][]marketdata.OHLCV, map[string]marketdata.HistoryProvenance, []output.ErrorInfo, []screener.TickerBreadthData, int) {
	if workers <= 0 {
		workers = 1
	}
	jobs := make(chan fetchedTickerData, len(tickers))
	results := make(chan fetchedTickerData, len(tickers))
	total := int64(len(tickers))
	var processed int64
	start := time.Now()

	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := range jobs {
				result, err := getHistoryResult(ctx, provider, marketdata.HistoryRequest{
					Symbol: j.ticker, Period: "5y", Interval: "1d", AsOf: asOfDate,
				})
				if err == nil && !asOfDate.IsZero() {
					result.Data, err = marketdata.TruncateAt(result.Data, asOfDate)
				}
				if err != nil {
					if verbose {
						logf("  ⚠ %s: %v\n", j.ticker, err)
					}
					results <- fetchedTickerData{index: j.index, ticker: j.ticker, err: err}
				} else {
					result.Provenance = provenanceWithLastBar(result.Provenance, result.Data)
					immutableData := cloneOHLCV(result.Data)
					results <- fetchedTickerData{
						index:      j.index,
						ticker:     j.ticker,
						data:       immutableData,
						provenance: result.Provenance,
						breadth:    enrichTickerBreadth(j.ticker, computeTickerBreadth(j.ticker, immutableData)),
					}
				}

				count := atomic.AddInt64(&processed, 1)
				if count%50 == 0 || count == total {
					reportProgress(int(count), int(total), time.Since(start).Milliseconds())
				}
			}
		}()
	}

	for i, t := range tickers {
		jobs <- fetchedTickerData{index: i, ticker: t}
	}
	close(jobs)
	wg.Wait()
	close(results)

	ordered := make([]fetchedTickerData, len(tickers))
	seen := make([]bool, len(tickers))
	for r := range results {
		ordered[r.index] = r
		seen[r.index] = true
	}

	tickerData := make(map[string][]marketdata.OHLCV, len(tickers))
	provenance := make(map[string]marketdata.HistoryProvenance, len(tickers))
	var errors []output.ErrorInfo
	var breadthData []screener.TickerBreadthData
	upstreamFailures := 0
	for i := range ordered {
		if !seen[i] {
			continue
		}
		r := ordered[i]
		if r.err != nil {
			errors = append(errors, output.ErrorInfo{Ticker: r.ticker, Error: r.err.Error()})
			upstreamFailures++
			continue
		}
		tickerData[r.ticker] = r.data
		provenance[r.ticker] = r.provenance
		if r.provenance.UpstreamError != "" {
			upstreamFailures++
		}
		breadthData = append(breadthData, r.breadth)
	}
	return tickerData, provenance, errors, breadthData, upstreamFailures
}

func evaluateScanSnapshot(ctx context.Context, snapshot scanSnapshot, screeners []screener.Screener, tickers []string, workers int) ([]scanResult, []output.ErrorInfo, []screener.TickerBreadthData) {
	allErrors := append([]output.ErrorInfo(nil), snapshot.errors...)
	allBreadth := append([]screener.TickerBreadthData(nil), snapshot.breadthData...)
	var allResults []scanResult
	for _, scr := range screeners {
		results, errors, scoreUpdates := runScreenerFromSnapshot(ctx, scr, tickers, snapshot, workers)
		allResults = append(allResults, results...)
		allErrors = append(allErrors, errors...)
		allBreadth = applyBreadthScoreUpdates(allBreadth, scoreUpdates)
	}
	return allResults, allErrors, allBreadth
}

func runScreenerV2(ctx context.Context, scr screener.Screener, tickers []string,
	provider marketdata.Provider, benchmark []marketdata.OHLCV, workers int, asOfDate time.Time,
	benchmarkAvailable bool) ([]scanResult, []output.ErrorInfo, []screener.TickerBreadthData) {

	tickerData, provenance, errors, breadthData, upstreamFailures := fetchTickerSnapshotData(ctx, tickers, provider, workers, asOfDate)
	snapshot := scanSnapshot{
		benchmarkData:      cloneOHLCV(benchmark),
		benchmarkAvailable: benchmarkAvailable,
		tickerData:         tickerData,
		tickerProvenance:   provenance,
		excludedByPolicy:   make(map[string]string),
		breadthData:        breadthData,
		errors:             errors,
		upstreamFailures:   upstreamFailures,
	}
	results, screenErrors, scoreUpdates := runScreenerFromSnapshot(ctx, scr, tickers, snapshot, workers)
	errors = append(errors, screenErrors...)
	breadthData = applyBreadthScoreUpdates(breadthData, scoreUpdates)
	return results, errors, breadthData
}

func runScreenerFromSnapshot(ctx context.Context, scr screener.Screener, tickers []string,
	snapshot scanSnapshot, workers int) ([]scanResult, []output.ErrorInfo, []tickerScoreUpdate) {

	screenerStart := time.Now()
	type job struct {
		ticker string
	}

	jobs := make(chan job, len(tickers))
	var results []scanResult
	var errors []output.ErrorInfo
	var scoreUpdates []tickerScoreUpdate
	var mu sync.Mutex
	var processed int64
	total := int64(0)
	for t := range snapshot.tickerData {
		if _, excluded := snapshot.excludedByPolicy[t]; !excluded {
			total++
		}
	}
	if total == 0 {
		return nil, nil, nil
	}
	if workers <= 0 {
		workers = 1
	}

	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := range jobs {
				data := cloneOHLCV(snapshot.tickerData[j.ticker])
				result, err := scr.Screen(ctx, data, cloneOHLCV(snapshot.benchmarkData))
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
					if sr, ok := buildScanResult(j.ticker, scr.Name(), data, result, snapshot.benchmarkAvailable); ok {
						mu.Lock()
						results = append(results, sr)
						mu.Unlock()
					}
				}
				mu.Lock()
				scoreUpdates = append(scoreUpdates, tickerScoreUpdate{
					ticker:   j.ticker,
					score:    result.Score,
					fullPass: result.Pass,
				})
				mu.Unlock()

				count := atomic.AddInt64(&processed, 1)
				if count%50 == 0 || count == total {
					reportProgress(int(count), int(total), time.Since(screenerStart).Milliseconds())
				}
			}
		}()
	}

	for _, t := range tickers {
		if _, ok := snapshot.tickerData[t]; ok {
			if _, excluded := snapshot.excludedByPolicy[t]; !excluded {
				jobs <- job{ticker: t}
			}
		}
	}
	close(jobs)
	wg.Wait()

	return results, errors, scoreUpdates
}

func applyBreadthScoreUpdates(breadthData []screener.TickerBreadthData, updates []tickerScoreUpdate) []screener.TickerBreadthData {
	indexByTicker := make(map[string]int, len(breadthData))
	for i := range breadthData {
		indexByTicker[breadthData[i].Ticker] = i
	}
	for _, update := range updates {
		i, ok := indexByTicker[update.ticker]
		if !ok {
			continue
		}
		if update.score > breadthData[i].Score {
			breadthData[i].Score = update.score
		}
		if update.fullPass {
			breadthData[i].FullPass = true
		}
	}
	return breadthData
}

func buildScanResult(ticker, strategyName string, data []marketdata.OHLCV, result *screener.ScreenResult, benchmarkAvailable bool) (scanResult, bool) {
	if len(data) == 0 {
		return scanResult{}, false
	}

	passed := 0
	for _, f := range result.Filters {
		if f.Pass {
			passed++
		}
	}

	dn := len(data)
	closePrice := data[dn-1].Close
	volume := data[dn-1].Volume
	changePct := 0.0
	if dn >= 2 && data[dn-2].Close > 0 {
		changePct = (data[dn-1].Close - data[dn-2].Close) / data[dn-2].Close
	}

	status, statusReason := deriveStatus(result)
	triggerPrice, triggerType, invalidationPrice, atrStop, distToTrigger := computeActionableFields(data, result)
	volRatio, reqVolRatio := extractVolumeRatio(result)
	dataHealth, warnings := assessDataHealth(result, benchmarkAvailable)
	avgTradedValue := computeAvgTradedValue(data)
	if minTradedValue > 0 && avgTradedValue < minTradedValue {
		return scanResult{}, false
	}

	sr := scanResult{
		Ticker:             ticker,
		Strategy:           strategyName,
		Score:              result.Score,
		WeightedScore:      result.WeightedScore,
		DataConfidence:     result.DataConfidence,
		ActionabilityScore: result.ActionabilityScore,
		FiltersPassed:      passed,
		TotalFilters:       len(result.Filters),
		ClosePrice:         closePrice,
		Volume:             volume,
		ChangePct:          changePct,
		Status:             status,
		StatusReason:       statusReason,
		TriggerPrice:       triggerPrice,
		TriggerType:        triggerType,
		InvalidationPrice:  invalidationPrice,
		DistToTriggerPct:   distToTrigger,
		VolumeRatio:        volRatio,
		RequiredVolRatio:   reqVolRatio,
		ATRStop:            atrStop,
		DataHealth:         dataHealth,
		ResultWarnings:     warnings,
		TimeframeAlignment: computeTimeframeAlignment(data),
		AvgTradedValue:     avgTradedValue,
	}

	if sectorInfo, hasSector := marketdata.GetSectorInfo(ticker); hasSector {
		sr.Sector = sectorInfo.Sector
		sr.Industry = sectorInfo.Industry
		sr.CapTier = sectorInfo.CapTier
	}
	if scanDetail {
		sr.Filters = result.Filters
	}
	return sr, true
}

func enrichTickerBreadth(ticker string, tbd screener.TickerBreadthData) screener.TickerBreadthData {
	if sectorInfo, hasSector := marketdata.GetSectorInfo(ticker); hasSector {
		tbd.Sector = sectorInfo.Sector
	}
	return tbd
}

func cloneOHLCV(data []marketdata.OHLCV) []marketdata.OHLCV {
	return append([]marketdata.OHLCV(nil), data...)
}

// computeTickerBreadth computes SMA50/200 and 52-week high/low for breadth.
func computeTickerBreadth(ticker string, data []marketdata.OHLCV) screener.TickerBreadthData {
	tbd := screener.TickerBreadthData{Ticker: ticker}
	if len(data) < 201 {
		return tbd
	}
	closes := marketdata.Closes(data)
	n := len(closes)

	sma50 := indicators.SMA(closes, 50)
	sma200 := indicators.SMA(closes, 200)

	tbd.AboveSMA50 = !math.IsNaN(sma50[n-1]) && closes[n-1] > sma50[n-1]
	tbd.AboveSMA200 = !math.IsNaN(sma200[n-1]) && closes[n-1] > sma200[n-1]

	// 52-week high/low check
	tail252 := indicators.Tail(closes, 252)
	high52w := indicators.Max(tail252)
	low52w := indicators.Min(tail252)
	tbd.NewHigh = closes[n-1] >= high52w*0.98 // within 2% of 52w high
	tbd.NewLow = closes[n-1] <= low52w*1.02   // within 2% of 52w low

	return tbd
}

// deriveStatus determines the watchlist status and reason from screening result.
func deriveStatus(result *screener.ScreenResult) (string, string) {
	if result.Pass && result.DataConfidence >= 0.9 {
		// Check if volume is confirmed
		for _, f := range result.Filters {
			if (f.Name == "volume_spike" || f.Name == "volume_confirmation" || f.Name == "volume_explosion") && !f.Pass {
				return "early_breakout", "volume_confirmation_missing"
			}
		}
		return "confirmed_breakout", ""
	}

	// Near miss
	if result.Score >= 0.67 {
		// Find the most important failing filter
		reason := findFailingReason(result)
		if result.DataConfidence < 0.8 {
			return "watch", "insufficient_data_confidence"
		}
		return "watch", reason
	}

	if result.Score >= 0.5 {
		reason := findFailingReason(result)
		return "watch", reason
	}

	return "avoid", findFailingReason(result)
}

// findFailingReason returns the status_reason string from the most important failing filter.
func findFailingReason(result *screener.ScreenResult) string {
	// Priority: critical unknown > critical fail > major unknown > major fail
	for _, imp := range []string{screener.ImportanceCritical, screener.ImportanceMajor, screener.ImportanceMinor} {
		for _, f := range result.Filters {
			if f.Importance == imp && f.Status == screener.StatusUnknown {
				return f.Name + "_unknown"
			}
		}
		for _, f := range result.Filters {
			if f.Importance == imp && f.Status == screener.StatusFail {
				return f.Name + "_failed"
			}
		}
	}
	return "multiple_filters_failed"
}

// computeActionableFields derives trigger, invalidation, and ATR stop from filter data.
func computeActionableFields(data []marketdata.OHLCV, result *screener.ScreenResult) (triggerPrice float64, triggerType string, invalidationPrice, atrStop, distToTrigger float64) {
	if len(data) < 20 {
		return
	}
	n := len(data)
	closes := marketdata.Closes(data)
	highs := marketdata.Highs(data)
	lows := marketdata.Lows(data)

	// Trigger from bollinger upper band
	for _, f := range result.Filters {
		if f.Name == "bollinger_breakout" && f.Threshold > 0 {
			triggerPrice = f.Threshold
			triggerType = "bollinger_breakout"
			if closes[n-1] > 0 {
				distToTrigger = (triggerPrice - closes[n-1]) / closes[n-1]
			}
		}
		if f.Name == "trendline_breakout" && f.Threshold > 0 && triggerPrice == 0 {
			triggerPrice = f.Threshold
			triggerType = "trendline_breakout"
			if closes[n-1] > 0 {
				distToTrigger = (triggerPrice - closes[n-1]) / closes[n-1]
			}
		}
	}

	// ATR-based stop and invalidation
	atrValues := indicators.ATR(highs, lows, closes, 14)
	if len(atrValues) > 0 && !math.IsNaN(atrValues[n-1]) {
		atrStop = closes[n-1] - 1.5*atrValues[n-1]
		if invalidationPrice == 0 {
			sma := indicators.SMA(closes, 10)
			if !math.IsNaN(sma[n-1]) {
				invalidationPrice = sma[n-1] - 1.5*atrValues[n-1]
			} else {
				invalidationPrice = atrStop
			}
		}
	}

	return
}

// extractVolumeRatio finds volume ratio from filter results.
func extractVolumeRatio(result *screener.ScreenResult) (volRatio, reqVolRatio float64) {
	for _, f := range result.Filters {
		if f.Name == "volume_spike" || f.Name == "volume_confirmation" || f.Name == "volume_explosion" {
			return f.Value, f.Threshold
		}
	}
	return 0, 0
}

// assessDataHealth derives overall data health and per-ticker warnings.
func assessDataHealth(result *screener.ScreenResult, benchmarkAvailable bool) (string, []string) {
	var warnings []string
	unknowns := 0
	for _, f := range result.Filters {
		if f.Status == screener.StatusUnknown {
			unknowns++
			warnings = append(warnings, fmt.Sprintf("%s: %s", f.Name, f.Detail))
		}
	}

	if !benchmarkAvailable {
		warnings = append(warnings, "benchmark data not available for relative strength")
	}

	if unknowns == 0 {
		return "complete", warnings
	}
	if unknowns <= 1 {
		return "partial", warnings
	}
	return "degraded", warnings
}

// computeAvgTradedValue calculates 20-day average of price × volume.
func computeAvgTradedValue(data []marketdata.OHLCV) float64 {
	n := len(data)
	window := 20
	if n < window {
		window = n
	}
	total := 0.0
	for i := n - window; i < n; i++ {
		total += data[i].Close * data[i].Volume
	}
	if window == 0 {
		return 0
	}
	return total / float64(window)
}

// computeTimeframeAlignment checks weekly/monthly SMA alignment.
func computeTimeframeAlignment(data []marketdata.OHLCV) string {
	if len(data) < 60 {
		return "insufficient_data"
	}

	// Weekly alignment
	weekly := marketdata.ToWeekly(data)
	wn := len(weekly)
	weeklyAligned := false
	if wn >= 12 {
		wCloses := make([]float64, wn)
		for i, w := range weekly {
			wCloses[i] = w.Close
		}
		wSMA10 := indicators.SMA(wCloses, 10)
		if !math.IsNaN(wSMA10[wn-1]) && wCloses[wn-1] > wSMA10[wn-1] {
			weeklyAligned = true
		}
	}

	// Monthly alignment
	monthly := marketdata.ToMonthly(data)
	mn := len(monthly)
	monthlyAligned := false
	if mn >= 12 {
		mCloses := make([]float64, mn)
		for i, m := range monthly {
			mCloses[i] = m.Close
		}
		mSMA10 := indicators.SMA(mCloses, 10)
		if !math.IsNaN(mSMA10[mn-1]) && mCloses[mn-1] > mSMA10[mn-1] {
			monthlyAligned = true
		}
	}

	switch {
	case weeklyAligned && monthlyAligned:
		return "daily+weekly+monthly"
	case weeklyAligned:
		return "daily+weekly"
	case monthlyAligned:
		return "counter_trend"
	default:
		return "daily_only"
	}
}

func renderResults(strategyName string, results []scanResult, format string) {
	switch output.Format(format) {
	case output.FormatJSON:
		// Handled by envelope in runScan
		return

	case output.FormatCSV:
		filename := filepath.Join(runDir, fmt.Sprintf("%s_%s.csv", strategyName, time.Now().Format("2006-01-02_150405")))
		headers := []string{"Ticker", "Strategy", "Score", "Status", "StatusReason", "Filters Passed", "Total Filters"}
		var rows [][]string
		for _, r := range results {
			rows = append(rows, []string{r.Ticker, r.Strategy, fmt.Sprintf("%.2f", r.Score), r.Status, r.StatusReason, fmt.Sprintf("%d", r.FiltersPassed), fmt.Sprintf("%d", r.TotalFilters)})
		}
		if err := output.WriteCSV(filename, headers, rows); err != nil {
			fmt.Fprintf(os.Stderr, "Error writing CSV: %v\n", err)
		} else {
			logf("📁 Results saved to %s\n", filename)
		}

	default: // table
		tw := output.NewTableWriter(os.Stdout)
		tw.SetHeaders("Ticker", "Strategy", "Score", "Status", "Filters")
		for _, r := range results {
			tw.AddRow(r.Ticker, r.Strategy, fmt.Sprintf("%.0f%%", r.Score*100), r.Status, fmt.Sprintf("%d/%d", r.FiltersPassed, r.TotalFilters))
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
