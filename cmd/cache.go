package cmd

import (
	"fmt"
	"os"

	"github.com/rohitsharma04/stockctl/internal/marketdata"
	"github.com/rohitsharma04/stockctl/internal/output"
	"github.com/spf13/cobra"
)

var cacheCmd = &cobra.Command{
	Use:   "cache",
	Short: "Manage the local data cache",
	Long: `View and manage the local disk cache in ~/.stockctl/cache/.

Cached data avoids redundant Yahoo Finance API calls across runs.
Cache TTL defaults to 24 hours for daily data.`,
}

var cacheStatsCmd = &cobra.Command{
	Use:   "stats",
	Short: "Show cache statistics",
	RunE: func(cmd *cobra.Command, args []string) error {
		stats := marketdata.GetCacheStats()

		outFmt := "table"
		if cmd.Flags().Changed("output") {
			outFmt = outputFmt
		}

		if output.Format(outFmt) == output.FormatJSON {
			env := output.Envelope{
				Meta:    output.NewMeta("cache-stats"),
				Results: stats,
			}
			return output.WriteEnvelope(os.Stdout, env)
		}

		fmt.Printf("📦 Cache: %s\n", stats.CacheDir)
		fmt.Printf("   Files:  %d\n", stats.TotalFiles)
		fmt.Printf("   Size:   %.1f MB\n", float64(stats.TotalBytes)/(1024*1024))
		if stats.OldestFile != "" {
			fmt.Printf("   Oldest: %s\n", stats.OldestFile)
			fmt.Printf("   Newest: %s\n", stats.NewestFile)
		}
		return nil
	},
}

var cacheClearMarket string

var cacheClearCmd = &cobra.Command{
	Use:   "clear",
	Short: "Clear cached data",
	RunE: func(cmd *cobra.Command, args []string) error {
		removed, err := marketdata.ClearCache(cacheClearMarket)
		if err != nil {
			return err
		}

		if cacheClearMarket != "" {
			logf("🗑️  Cleared %d cached files for market %q\n", removed, cacheClearMarket)
		} else {
			logf("🗑️  Cleared %d cached files\n", removed)
		}
		return nil
	},
}

func init() {
	cacheClearCmd.Flags().StringVarP(&cacheClearMarket, "market", "m", "", "only clear data for a specific market")
	cacheCmd.AddCommand(cacheStatsCmd)
	cacheCmd.AddCommand(cacheClearCmd)
	rootCmd.AddCommand(cacheCmd)
}
