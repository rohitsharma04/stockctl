package cmd

import (
	"fmt"

	"github.com/rohitsharma04/stockctl/internal/config"
	"github.com/rohitsharma04/stockctl/internal/marketdata"
	"github.com/spf13/cobra"
)

var (
	tickersForce bool
)

var tickersCmd = &cobra.Command{
	Use:   "tickers",
	Short: "Fetch or refresh the ticker universe for a market",
	Long: `Downloads index constituents for the active market and caches them locally.
Supported auto-download markets: us (S&P 500), india (Nifty 500), japan (Nikkei 225), uk (FTSE 100), germany (DAX 40).
For other markets, use --tickers flag with the scan command.`,
	RunE: runTickers,
}

func init() {
	tickersCmd.Flags().BoolVar(&tickersForce, "force", false, "Force re-download even if cache is fresh")
	rootCmd.AddCommand(tickersCmd)
}

func runTickers(cmd *cobra.Command, args []string) error {
	appConfig, err := config.Load(cfgFile)
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}

	mktID := appConfig.General.Market

	// Handle "all" markets
	if mktID == "all" {
		sources := marketdata.DefaultUniverseSources()
		for id := range sources {
			fmt.Printf("\n── %s ──\n", id)
			tickers, err := marketdata.FetchUniverse(id, tickersForce)
			if err != nil {
				fmt.Printf("⚠ %s: %v\n", id, err)
				continue
			}
			fmt.Printf("📊 %s: %d tickers\n", id, len(tickers))
		}
		return nil
	}

	activeMarket, ok := marketdata.Markets[mktID]
	if !ok {
		return fmt.Errorf("unknown market: %s", mktID)
	}

	tickers, err := marketdata.FetchUniverse(mktID, tickersForce)
	if err != nil {
		return err
	}

	fmt.Printf("\n📊 %s (%s): %d tickers loaded\n", activeMarket.Name, mktID, len(tickers))

	// Show first 10 as a sample
	limit := 10
	if len(tickers) < limit {
		limit = len(tickers)
	}
	fmt.Printf("   Sample: %v\n", tickers[:limit])

	return nil
}
