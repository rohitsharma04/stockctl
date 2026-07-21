package cmd

import (
	"context"
	"crypto/rand"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/rohitsharma04/stockctl/internal/config"
	"github.com/rohitsharma04/stockctl/internal/marketdata"
	"github.com/rohitsharma04/stockctl/internal/output"
	"github.com/spf13/cobra"
)

var (
	cfgFile      string
	outputFmt    string
	marketID     string
	verbose      bool
	quiet        bool
	noCache      bool
	timeoutStr   string
	progressMode string
	appConfig    *config.Config
	activeMarket marketdata.Market
	runDir       string          // unique per-run output directory
	rootCtx      context.Context // root context, supports --timeout
)

// createRunDir creates a unique output directory under /tmp/stockctl/
// Format: /tmp/stockctl/run_20260326_201500_a3f8/
func createRunDir() (string, error) {
	b := make([]byte, 4)
	rand.Read(b)
	shortID := fmt.Sprintf("%x", b)
	ts := time.Now().Format("20060102_150405")
	dir := filepath.Join(os.TempDir(), "stockctl", fmt.Sprintf("run_%s_%s", ts, shortID))
	if err := os.MkdirAll(dir, 0755); err != nil {
		return "", err
	}
	return dir, nil
}

// logf prints to stderr unless --quiet is set.
func logf(format string, a ...interface{}) {
	if !quiet {
		fmt.Fprintf(os.Stderr, format, a...)
	}
}

// reportProgress emits a structured progress event.
// In "json" mode, emits NDJSON to stderr. In "text" mode, emits human-readable text.
// In "none" mode (or when --quiet is set), does nothing.
func reportProgress(current, total int, elapsedMs int64) {
	effectiveMode := progressMode
	if quiet && effectiveMode == "" {
		effectiveMode = "none"
	}
	if effectiveMode == "" {
		effectiveMode = "text"
	}

	switch effectiveMode {
	case "json":
		fmt.Fprintf(os.Stderr, `{"type":"progress","current":%d,"total":%d,"elapsed_ms":%d}`+"\n", current, total, elapsedMs)
	case "text":
		if !quiet {
			fmt.Fprintf(os.Stderr, "  \U0001F4C8 Progress: %d/%d\n", current, total)
		}
	case "none":
		// silent
	}
}

var rootCmd = &cobra.Command{
	Use:   "stockctl",
	Short: "Stock analysis CLI — screeners, pairs trading, and backtesting",
	Long: `stockctl is a command-line tool for stock analysis.

It provides:
  • Stock screeners (breakout-caution, high-performance, stellar-breakout,
    descending-breakout, rsi-bounce, macd-crossover)
  • Pairs trading simulation (correlative hedging with z-score signals)
  • Backtesting engine (TP/SL optimization with Sharpe ratio analysis)
  • Real-time quotes for quick price checks

Configuration: ~/.stockctl/config.toml (override: STOCKCTL_CONFIG env var or --config)
Output files:  /tmp/stockctl/run_<timestamp>_<id>/ (unique per run, never overwrites)`,
	PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
		// Initialize root context (always, even for skipped commands)
		rootCtx = context.Background()
		if timeoutStr != "" {
			dur, err := time.ParseDuration(timeoutStr)
			if err != nil {
				return fmt.Errorf("invalid --timeout %q: %w", timeoutStr, err)
			}
			var cancel context.CancelFunc
			rootCtx, cancel = context.WithTimeout(rootCtx, dur)
			_ = cancel // cleaned up on process exit
		}

		// Skip config loading for the markets command
		if cmd.Name() == "markets" || cmd.Name() == "version" || cmd.Name() == "stats" || cmd.Name() == "clear" {
			return nil
		}

		var err error
		appConfig, err = config.Load(cfgFile)
		if err != nil {
			return fmt.Errorf("loading config %s: %w", cfgFile, err)
		}

		// Override output format from flag if set
		if cmd.Flags().Changed("output") {
			appConfig.General.Output = outputFmt
		}

		// seed history owns a repeatable local --market flag rather than the
		// single global market selection used by interactive commands.
		if cmd.Name() == "history" && cmd.Parent() != nil && cmd.Parent().Name() == "seed" {
			return nil
		}

		// Resolve market
		if cmd.Flags().Changed("market") {
			appConfig.General.Market = marketID
		}
		mkt, ok := marketdata.Markets[appConfig.General.Market]
		if !ok {
			return fmt.Errorf("unknown market: %s (use 'stockctl markets' to list)", appConfig.General.Market)
		}
		activeMarket = mkt

		// Create unique run directory for output files
		runDir, err = createRunDir()
		if err != nil {
			return fmt.Errorf("creating output directory: %w", err)
		}

		return nil
	},
}

func Execute() {
	err := rootCmd.Execute()
	if err != nil {
		// When JSON output was requested, emit structured error envelope on stdout
		if outputFmt == "json" {
			env := output.Envelope{
				Meta:   output.NewMeta("error"),
				Errors: []output.ErrorInfo{{Error: err.Error()}},
			}
			output.WriteEnvelope(os.Stdout, env)
		}
		os.Exit(1)
	}
}

var marketsCmd = &cobra.Command{
	Use:   "markets",
	Short: "List all supported stock markets",
	RunE: func(cmd *cobra.Command, args []string) error {
		// Check output format (need to read flag directly since PersistentPreRunE skips for "markets")
		outFmt := "table"
		if cmd.Flags().Changed("output") {
			outFmt = outputFmt
		}

		type marketInfo struct {
			ID             string  `json:"id"`
			Name           string  `json:"name"`
			Suffix         string  `json:"suffix"`
			Benchmark      string  `json:"benchmark"`
			Currency       string  `json:"currency"`
			CurrencySymbol string  `json:"currency_symbol"`
			MinPrice       float64 `json:"min_price"`
			Timezone       string  `json:"timezone"`
			SessionOpen    string  `json:"session_open"`
			SessionClose   string  `json:"session_close"`
		}

		if output.Format(outFmt) == output.FormatJSON {
			var markets []marketInfo
			for _, id := range marketdata.ListMarkets() {
				m := marketdata.Markets[id]
				markets = append(markets, marketInfo{
					ID: m.ID, Name: m.Name, Suffix: m.Suffix,
					Benchmark: m.Benchmark, Currency: m.Currency,
					CurrencySymbol: m.CurrencySymbol, MinPrice: m.MinPrice,
					Timezone: m.Timezone, SessionOpen: m.SessionOpen, SessionClose: m.SessionClose,
				})
			}
			env := output.Envelope{
				Meta:    output.NewMeta("markets"),
				Results: markets,
			}
			return output.WriteEnvelope(os.Stdout, env)
		}

		fmt.Println("Supported markets:")
		fmt.Println()
		for _, id := range marketdata.ListMarkets() {
			m := marketdata.Markets[id]
			fmt.Printf("  %-14s %-25s suffix: %-5s  benchmark: %-12s  currency: %s\n",
				m.ID, m.Name, m.Suffix, m.Benchmark, m.Currency)
		}
		return nil
	},
}

func init() {
	rootCmd.PersistentFlags().StringVar(&cfgFile, "config", config.DefaultConfigPath(), "config file path")
	rootCmd.PersistentFlags().StringVarP(&outputFmt, "output", "o", "table", "output format: table, json, csv")
	rootCmd.PersistentFlags().StringVarP(&marketID, "market", "m", "", "stock market: us, india, japan, uk, etc. (use 'stockctl markets' to list)")
	rootCmd.PersistentFlags().BoolVarP(&verbose, "verbose", "v", false, "verbose output")
	rootCmd.PersistentFlags().BoolVarP(&quiet, "quiet", "q", false, "suppress all progress output (agent mode)")
	rootCmd.PersistentFlags().BoolVar(&noCache, "no-cache", false, "bypass disk cache")
	rootCmd.PersistentFlags().StringVar(&timeoutStr, "timeout", "", "command timeout (e.g., 5m, 30s)")
	rootCmd.PersistentFlags().StringVar(&progressMode, "progress", "", "progress output mode: text, json, none (default: text, or none with --quiet)")
	rootCmd.AddCommand(marketsCmd)
}
