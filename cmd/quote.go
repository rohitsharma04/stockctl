package cmd

import (
	"fmt"
	"os"
	"time"

	"github.com/rohitsharma04/stockctl/internal/marketdata"
	"github.com/rohitsharma04/stockctl/internal/output"
	"github.com/spf13/cobra"
)

var quoteCmd = &cobra.Command{
	Use:   "quote [tickers...]",
	Short: "Fetch real-time quotes for one or more tickers",
	Long: `Fetch the latest price and volume for one or more stock tickers.

Examples:
  stockctl quote AAPL MSFT GOOGL --output json
  stockctl quote RELIANCE TCS -m india --output json`,
	Args: cobra.MinimumNArgs(1),
	RunE: runQuote,
}

func init() {
	rootCmd.AddCommand(quoteCmd)
}

type quoteResult struct {
	Ticker string  `json:"ticker"`
	Price  float64 `json:"price"`
	Volume float64 `json:"volume"`
}

func runQuote(cmd *cobra.Command, args []string) error {
	ctx := rootCtx
	startTime := time.Now()
	provider := marketdata.BuildProvider(noCache)

	var results []quoteResult
	var errors []output.ErrorInfo

	for _, ticker := range args {
		fullTicker := activeMarket.ApplySuffix(ticker)
		logf("📊 Fetching quote for %s...\n", fullTicker)

		q, err := provider.GetQuote(ctx, fullTicker)
		if err != nil {
			if verbose {
				logf("  ⚠ %s: %v\n", fullTicker, err)
			}
			errors = append(errors, output.ErrorInfo{Ticker: fullTicker, Error: err.Error()})
			continue
		}

		results = append(results, quoteResult{
			Ticker: fullTicker,
			Price:  q.Price,
			Volume: q.Volume,
		})
	}

	switch output.Format(appConfig.General.Output) {
	case output.FormatJSON:
		meta := output.NewMeta("quote")
		meta.Market = activeMarket.ID
		meta.DurationMs = time.Since(startTime).Milliseconds()
		env := output.Envelope{
			Meta:    meta,
			Results: results,
			Errors:  errors,
		}
		return output.WriteEnvelope(os.Stdout, env)

	default:
		if len(results) == 0 {
			logf("No quotes fetched.\n")
			return nil
		}
		cs := activeMarket.CurrencySymbol
		tw := output.NewTableWriter(os.Stdout)
		tw.SetHeaders("Ticker", "Price", "Volume")
		for _, r := range results {
			tw.AddRow(r.Ticker, fmt.Sprintf("%s%.2f", cs, r.Price), fmt.Sprintf("%.0f", r.Volume))
		}
		tw.Render()
	}

	return nil
}
