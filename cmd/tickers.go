package cmd

import (
	"fmt"

	"github.com/rohitsharma04/stockctl/internal/marketdata"
	"github.com/spf13/cobra"
)

var tickersCmd = &cobra.Command{
	Use:   "tickers",
	Short: "List the built-in ticker universe for a market",
	Long: `Shows the embedded index constituents for the active market.

Supported markets with built-in universes:
  us      S&P 500 (503 tickers)
  india   Nifty 500 (500 tickers)

For other markets, use --tickers flag with the scan command.`,
	RunE: runTickers,
}

func init() {
	rootCmd.AddCommand(tickersCmd)
}

func runTickers(cmd *cobra.Command, args []string) error {
	mktID := activeMarket.ID

	tickers, err := marketdata.GetUniverse(mktID)
	if err != nil {
		// Show available universes
		fmt.Printf("No built-in universe for %q\n\nAvailable:\n", mktID)
		for _, u := range marketdata.ListAvailableUniverses() {
			t, _ := marketdata.GetUniverse(u.MarketID)
			fmt.Printf("  %-10s %s (%d tickers)\n", u.MarketID, u.Name, len(t))
		}
		return nil
	}

	fmt.Printf("📊 %s (%s): %d tickers\n", activeMarket.Name, mktID, len(tickers))

	limit := 20
	if len(tickers) < limit {
		limit = len(tickers)
	}
	fmt.Printf("   Sample: %v\n", tickers[:limit])

	return nil
}
