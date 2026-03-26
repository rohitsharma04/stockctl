package cmd

import (
	"fmt"
	"os"
	"runtime"

	"github.com/rohitsharma04/stockctl/internal/config"
	"github.com/rohitsharma04/stockctl/internal/marketdata"
	"github.com/rohitsharma04/stockctl/internal/output"
	"github.com/rohitsharma04/stockctl/internal/screener"
	"github.com/spf13/cobra"
)

// Version is set via -ldflags at build time.
var Version = "dev"

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Print version and capabilities",
	RunE: func(cmd *cobra.Command, args []string) error {
		outFmt := "table"
		if cmd.Flags().Changed("output") {
			outFmt = outputFmt
		}

		type versionInfo struct {
			Version      string   `json:"version"`
			GoVersion    string   `json:"go_version"`
			Strategies   []string `json:"strategies"`
			Markets      []string `json:"markets"`
			TotalTickers int      `json:"total_tickers"`
		}

		strategies := []string{
			"breakout-caution", "high-performance",
			"stellar-breakout", "descending-breakout",
			"rsi-bounce", "macd-crossover", "all",
		}

		markets := marketdata.ListMarkets()

		totalTickers := 0
		for _, u := range marketdata.ListAvailableUniverses() {
			t, _ := marketdata.GetUniverse(u.MarketID)
			totalTickers += len(t)
		}

		if output.Format(outFmt) == output.FormatJSON {
			env := output.Envelope{
				Meta: output.NewMeta("version"),
				Results: versionInfo{
					Version:      Version,
					GoVersion:    runtime.Version(),
					Strategies:   strategies,
					Markets:      markets,
					TotalTickers: totalTickers,
				},
			}
			return output.WriteEnvelope(os.Stdout, env)
		}

		fmt.Printf("stockctl %s (%s)\n", Version, runtime.Version())
		fmt.Printf("Strategies: %v\n", strategies)
		fmt.Printf("Markets:    %d supported (%d total tickers)\n", len(markets), totalTickers)

		// Also list strategy descriptions
		cfg, _ := config.Load(cfgFile)
		if cfg != nil {
			registry := screener.Registry(cfg)
			fmt.Println("\nStrategy details:")
			for _, name := range strategies[:len(strategies)-1] { // skip "all"
				if s, ok := registry[name]; ok {
					fmt.Printf("  %-22s %s\n", name, s.Description())
				}
			}
		}

		return nil
	},
}

func init() {
	rootCmd.AddCommand(versionCmd)
}
