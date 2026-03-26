package cmd

import (
	"crypto/rand"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/rohitsharma04/stockctl/internal/config"
	"github.com/rohitsharma04/stockctl/internal/marketdata"
	"github.com/spf13/cobra"
)

var (
	cfgFile      string
	outputFmt    string
	marketID     string
	verbose      bool
	appConfig    *config.Config
	activeMarket marketdata.Market
	runDir       string // unique per-run output directory
)

// defaultConfigPath returns the default config path:
// 1. $STOCKCTL_CONFIG env var (if set)
// 2. ~/.config/stockctl/config.toml
func defaultConfigPath() string {
	if envPath := os.Getenv("STOCKCTL_CONFIG"); envPath != "" {
		return envPath
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "config.toml"
	}
	return filepath.Join(home, ".config", "stockctl", "config.toml")
}

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

var rootCmd = &cobra.Command{
	Use:   "stockctl",
	Short: "Stock analysis CLI — screeners, pairs trading, and backtesting",
	Long: `stockctl is a command-line tool for stock analysis.

It provides:
  • Stock screeners (breakout-caution, high-performance, stellar-breakout, descending-breakout)
  • Pairs trading simulation (correlative hedging with z-score signals)
  • Backtesting engine (TP/SL optimization with Sharpe ratio analysis)

Configuration: ~/.config/stockctl/config.toml (override: STOCKCTL_CONFIG env var or --config)
Output files:  /tmp/stockctl/run_<timestamp>_<id>/ (unique per run, never overwrites)`,
	PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
		// Skip config loading for the markets command
		if cmd.Name() == "markets" {
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
	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}

var marketsCmd = &cobra.Command{
	Use:   "markets",
	Short: "List all supported stock markets",
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Println("Supported markets:")
		fmt.Println()
		for _, id := range marketdata.ListMarkets() {
			m := marketdata.Markets[id]
			fmt.Printf("  %-14s %-25s suffix: %-5s  benchmark: %-12s  currency: %s\n",
				m.ID, m.Name, m.Suffix, m.Benchmark, m.Currency)
		}
	},
}

func init() {
	rootCmd.PersistentFlags().StringVar(&cfgFile, "config", defaultConfigPath(), "config file path")
	rootCmd.PersistentFlags().StringVarP(&outputFmt, "output", "o", "table", "output format: table, json, csv")
	rootCmd.PersistentFlags().StringVarP(&marketID, "market", "m", "", "stock market: us, india, japan, uk, etc. (use 'stockctl markets' to list)")
	rootCmd.PersistentFlags().BoolVarP(&verbose, "verbose", "v", false, "verbose output")
	rootCmd.AddCommand(marketsCmd)
}
