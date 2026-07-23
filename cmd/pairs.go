package cmd

import (
	"encoding/csv"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/rohitsharma04/stockctl/internal/indicators"
	"github.com/rohitsharma04/stockctl/internal/marketdata"
	"github.com/rohitsharma04/stockctl/internal/output"
	"github.com/rohitsharma04/stockctl/internal/pairs"
	"github.com/spf13/cobra"
)

var (
	pairsStocks        string
	pairsThreshold     float64
	pairsCapital       float64
	pairsWindow        int
	pairsZThresh       float64
	pairsExportSignals bool
)

var pairsProviderFactory = func(noCache bool) marketdata.Provider { return marketdata.BuildProvider(noCache) }

type pairResult struct {
	pairs.SimulationResult
	Correlation float64                                 `json:"correlation"`
	AlignedBars int                                     `json:"aligned_bars"`
	DataAsOf    string                                  `json:"data_as_of,omitempty"`
	Provenance  map[string]marketdata.HistoryProvenance `json:"provenance"`
}

type pairSymbolResult struct {
	Symbol     string                       `json:"symbol"`
	DataAsOf   string                       `json:"data_as_of,omitempty"`
	Provenance marketdata.HistoryProvenance `json:"provenance"`
}

// pairsResultSet retains the successful-input audit even when correlation
// analysis cannot produce a tradable pair.
type pairsResultSet struct {
	Pairs   []pairResult       `json:"pairs"`
	Symbols []pairSymbolResult `json:"symbols"`
}

type stockEntry struct {
	symbol     string
	data       []marketdata.OHLCV
	provenance marketdata.HistoryProvenance
}

// pairsSnapshotError preserves successful/failed fetch diagnostics when there
// are too few usable symbols to continue correlation analysis.
type pairsSnapshotError struct {
	err         error
	results     pairsResultSet
	fetchErrors []output.ErrorInfo
}

func (e *pairsSnapshotError) Error() string { return e.err.Error() }
func (e *pairsSnapshotError) Unwrap() error { return e.err }
func (e *pairsSnapshotError) JSONResults() interface{} {
	return e.results
}
func (e *pairsSnapshotError) JSONErrors() []output.ErrorInfo {
	return append([]output.ErrorInfo(nil), e.fetchErrors...)
}

var pairsCmd = &cobra.Command{
	Use:          "pairs",
	Short:        "Run pairs trading analysis",
	SilenceUsage: true,
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
	pairsCmd.Flags().BoolVar(&pairsExportSignals, "export-signals", false, "export trade signals as backtest-compatible CSV")
	rootCmd.AddCommand(pairsCmd)
}

func runPairs(cmd *cobra.Command, args []string) error {
	if selectedOutputFormat() == output.FormatCSV {
		return fmt.Errorf("pairs does not support --output csv; use --export-signals to write a signals CSV")
	}
	ctx := rootCtx
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
		return fmt.Errorf("need at least 2 stocks for pairs trading\nUse --stocks \"AAPL,MSFT,GOOGL\" or set stocks in [pairs] config")
	}

	// Apply market suffix
	for i := range stocks {
		stocks[i] = activeMarket.ApplySuffix(stocks[i])
	}

	provider := pairsProviderFactory(noCache)

	// Download data for all stocks
	logf("🌍 Market: %s (%s)\n", activeMarket.Name, activeMarket.Currency)
	logf("📊 Downloading data for %d stocks...\n", len(stocks))
	var entries []stockEntry
	var fetchErrors []output.ErrorInfo

	for _, sym := range stocks {
		history, err := getHistoryResult(ctx, provider, marketdata.HistoryRequest{Symbol: sym, Period: "5y", Interval: "1d"})
		if err != nil {
			logf("  ⚠ Skipping %s: %v\n", sym, err)
			fetchErrors = append(fetchErrors, output.ErrorInfo{Ticker: sym, Error: err.Error()})
			continue
		}
		logf("  ✓ %s (%d bars)\n", sym, len(history.Data))
		entries = append(entries, stockEntry{symbol: sym, data: history.Data, provenance: history.Provenance})
	}

	if len(entries) < 2 {
		return &pairsSnapshotError{
			err:         fmt.Errorf("need at least 2 valid stocks, got %d", len(entries)),
			results:     pairsResults(nil, entries),
			fetchErrors: fetchErrors,
		}
	}

	// Calculate returns
	logf("📈 Calculating correlations...\n")
	// Find correlated pairs from returns joined on the same trading dates.
	// Position-based return slices can otherwise correlate different sessions.
	var corrPairs []pairs.CorrelatedPair
	for i := range entries {
		for j := i + 1; j < len(entries); j++ {
			leftReturns, rightReturns, _ := alignedPairReturns(entries[i].data, entries[j].data)
			found := pairs.FindCorrelatedPairs([]string{entries[i].symbol, entries[j].symbol}, [][]float64{leftReturns, rightReturns}, threshold)
			corrPairs = append(corrPairs, found...)
		}
	}
	logf("🔗 Found %d correlated pairs (threshold: %.2f)\n", len(corrPairs), threshold)

	if len(corrPairs) == 0 {
		logf("No correlated pairs found. Try lowering --threshold.\n")
		if selectedOutputFormat() == output.FormatJSON {
			return output.WriteEnvelope(cmd.OutOrStdout(), pairsEnvelope(nil, entries, fetchErrors, time.Since(startTime)))
		}
		return nil
	}

	// Simulate trading for top 5 pairs
	limit := 5
	if len(corrPairs) < limit {
		limit = len(corrPairs)
	}

	var results []pairResult
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

		aligned1, aligned2 := alignPairHistory(data1, data2)
		if len(aligned1) < 2 {
			continue
		}

		prices1 := marketdata.Closes(aligned1)
		prices2 := marketdata.Closes(aligned2)

		// Build time slice
		dates := make([]time.Time, len(aligned1))
		for i, bar := range aligned1 {
			dates[i] = bar.Date
		}

		result := pairs.Simulate(
			pair.Stock1, pair.Stock2,
			prices1, prices2, dates,
			window, zThresh, cfg.ZExitLow, cfg.ZExitHigh, capital,
		)
		_, _, asOf := alignedPairReturns(aligned1, aligned2)
		provenance := make(map[string]marketdata.HistoryProvenance, 2)
		for _, entry := range entries {
			if entry.symbol == pair.Stock1 || entry.symbol == pair.Stock2 {
				provenance[entry.symbol] = provenanceWithLastBar(entry.provenance, entry.data)
			}
		}
		results = append(results, pairResult{SimulationResult: result, Correlation: pair.Correlation, AlignedBars: len(aligned1), DataAsOf: asOf.Format("2006-01-02"), Provenance: provenance})

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

	// Export signals if requested
	if pairsExportSignals {
		if err := exportPairsSignals(results); err != nil {
			return err
		}
	}

	// Output
	switch selectedOutputFormat() {
	case output.FormatJSON:
		return output.WriteEnvelope(cmd.OutOrStdout(), pairsEnvelope(results, entries, fetchErrors, time.Since(startTime)))

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

func pairsEnvelope(results []pairResult, entries []stockEntry, fetchErrors []output.ErrorInfo, duration time.Duration) output.Envelope {
	meta := output.NewMeta("pairs")
	meta.Market = activeMarket.ID
	meta.DurationMs = duration.Milliseconds()
	return output.Envelope{Meta: meta, Results: pairsResults(results, entries), Errors: fetchErrors}
}

func pairsResults(results []pairResult, entries []stockEntry) pairsResultSet {
	symbols := make([]pairSymbolResult, 0, len(entries))
	for _, entry := range entries {
		provenance := provenanceWithLastBar(entry.provenance, entry.data)
		asOf := ""
		if !provenance.LastBarDate.IsZero() {
			asOf = provenance.LastBarDate.Format("2006-01-02")
		}
		symbols = append(symbols, pairSymbolResult{Symbol: entry.symbol, DataAsOf: asOf, Provenance: provenance})
	}
	if results == nil {
		results = []pairResult{}
	}
	return pairsResultSet{Pairs: results, Symbols: symbols}
}

// alignPairHistory joins bars by trading date; tail alignment can pair prices
// from different sessions when one symbol has missing dates.
func alignPairHistory(a, b []marketdata.OHLCV) ([]marketdata.OHLCV, []marketdata.OHLCV) {
	byDay := make(map[string]marketdata.OHLCV, len(b))
	for _, bar := range b {
		byDay[bar.Date.Format("2006-01-02")] = bar
	}
	left, right := make([]marketdata.OHLCV, 0), make([]marketdata.OHLCV, 0)
	for _, bar := range a {
		if other, ok := byDay[bar.Date.Format("2006-01-02")]; ok {
			left = append(left, bar)
			right = append(right, other)
		}
	}
	return left, right
}

// alignedPairReturns calculates close-to-close returns only after joining the
// price histories by date. It also returns the newest common trading date.
func alignedPairReturns(a, b []marketdata.OHLCV) ([]float64, []float64, time.Time) {
	left, right := alignPairHistory(a, b)
	if len(left) < 2 {
		return nil, nil, time.Time{}
	}
	leftReturns := indicators.PctChange(marketdata.Closes(left))
	rightReturns := indicators.PctChange(marketdata.Closes(right))
	return leftReturns[1:], rightReturns[1:], left[len(left)-1].Date
}

// exportPairsSignals writes trade signals from pairs simulation as a
// backtest-compatible CSV file that can be fed to `stockctl backtest --input`.
func exportPairsSignals(results []pairResult) error {
	if len(results) == 0 {
		logf("No trades to export.\n")
		return nil
	}

	filename := filepath.Join(runDir, fmt.Sprintf("pairs_signals_%s.csv", time.Now().Format("2006-01-02_150405")))
	f, err := os.Create(filename)
	if err != nil {
		return fmt.Errorf("creating signals CSV: %w", err)
	}
	defer f.Close()

	w := csv.NewWriter(f)
	w.Write([]string{"Symbol", "Entry_Date", "Exit_Date", "Long_Stock", "Short_Stock", "PnL", "Position"})

	for _, r := range results {
		pairName := fmt.Sprintf("%s/%s", r.Stock1, r.Stock2)
		for _, t := range r.Trades {
			dir := "long_spread"
			if t.Position == -1 {
				dir = "short_spread"
			}
			w.Write([]string{
				pairName,
				t.EntryDate.Format("2006-01-02"),
				t.ExitDate.Format("2006-01-02"),
				t.LongStock,
				t.ShortStock,
				fmt.Sprintf("%.2f", t.Profit),
				dir,
			})
		}
	}
	w.Flush()
	logf("📁 Signals exported to %s\n", filename)
	return nil
}
