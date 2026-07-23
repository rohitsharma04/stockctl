package cmd

import (
	"fmt"
	"os"

	"github.com/rohitsharma04/stockctl/internal/marketdata"
	"github.com/rohitsharma04/stockctl/internal/output"
	"github.com/spf13/cobra"
)

var tickersCmd = &cobra.Command{
	Use:          "tickers",
	Short:        "List the built-in ticker universe for a market",
	SilenceUsage: true,
	Long: `Shows the embedded index constituents for the active market.

Every market has a built-in universe. Use 'stockctl markets' to see all.`,
	RunE: runTickers,
}

func init() {
	rootCmd.AddCommand(tickersCmd)
}

type tickersResult struct {
	Market  string   `json:"market"`
	Name    string   `json:"name"`
	Count   int      `json:"count"`
	Tickers []string `json:"tickers"`
}

func runTickers(cmd *cobra.Command, args []string) error {
	if selectedOutputFormat() == output.FormatCSV {
		return fmt.Errorf("tickers does not support --output csv")
	}
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

	switch selectedOutputFormat() {
	case output.FormatJSON:
		env := output.Envelope{
			Meta: output.NewMeta("tickers"),
			Results: tickersResult{
				Market:  mktID,
				Name:    activeMarket.Name,
				Count:   len(tickers),
				Tickers: tickers,
			},
		}
		return output.WriteEnvelope(os.Stdout, env)

	default:
		fmt.Printf("📊 %s (%s): %d tickers\n\n", activeMarket.Name, mktID, len(tickers))
		for i, t := range tickers {
			if i > 0 && i%10 == 0 {
				fmt.Println()
			}
			fmt.Printf("  %-12s", t)
		}
		fmt.Println()
	}

	return nil
}
