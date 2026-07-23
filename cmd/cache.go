package cmd

import (
	"fmt"

	"github.com/rohitsharma04/stockctl/internal/marketdata"
	"github.com/rohitsharma04/stockctl/internal/output"
	"github.com/spf13/cobra"
)

var cacheCmd = &cobra.Command{
	Use:   "cache",
	Short: "Manage the local data cache",
	Long: `View and manage the local disk cache in ~/.stockctl/cache/.

Cached data avoids redundant Yahoo Finance API calls across runs.
Cache TTL defaults to 24 hours for daily data.

Safe operator workflow:
  stockctl cache stats --verify --output json --quiet
  stockctl cache clear --yes

Use --market with cache clear to limit removal to one market. Cache clear
requires --yes; run cache stats --verify before clearing when auditing cache
health. CSV output is not supported by cache commands.`,
}

var cacheStatsCmd = &cobra.Command{
	Use:           "stats",
	Short:         "Show cache statistics",
	Long:          "Show local cache statistics. For a safe health check, use:\n  stockctl cache stats --verify --output json --quiet\n\nCSV output is not supported.",
	SilenceUsage:  true,
	SilenceErrors: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := rejectCacheCSV("cache stats"); err != nil {
			return err
		}
		stats := getCacheStats(cacheStatsVerify)

		if selectedOutputFormat() == output.FormatJSON {
			env := output.Envelope{
				Meta:    output.NewMeta("cache-stats"),
				Results: stats,
			}
			return output.WriteEnvelope(cmd.OutOrStdout(), env)
		}

		fmt.Fprintf(cmd.OutOrStdout(), "📦 Cache: %s\n", stats.CacheDir)
		fmt.Fprintf(cmd.OutOrStdout(), "   Files:  %d\n", stats.TotalFiles)
		fmt.Fprintf(cmd.OutOrStdout(), "   Size:   %.1f MB\n", float64(stats.TotalBytes)/(1024*1024))
		if stats.OldestFile != "" {
			fmt.Fprintf(cmd.OutOrStdout(), "   Oldest: %s\n", stats.OldestFile)
			fmt.Fprintf(cmd.OutOrStdout(), "   Newest: %s\n", stats.NewestFile)
		}
		return nil
	},
}

var cacheClearMarket string
var cacheClearYes, cacheClearDryRun, cacheStatsVerify bool

var cacheClearWithOptions = marketdata.ClearCacheWithOptions
var getCacheStats = marketdata.GetCacheStats

func rejectCacheCSV(command string) error {
	if selectedOutputFormat() == output.FormatCSV {
		return fmt.Errorf("%s does not support --output csv", command)
	}
	return nil
}

type cacheClearProgressError struct {
	err     error
	matched int
	removed int
	market  string
	dryRun  bool
}

func (e *cacheClearProgressError) Error() string { return e.err.Error() }
func (e *cacheClearProgressError) Unwrap() error { return e.err }
func (e *cacheClearProgressError) JSONResults() interface{} {
	return map[string]interface{}{"matched": e.matched, "removed": e.removed, "market": e.market, "dry_run": e.dryRun}
}

var cacheClearCmd = &cobra.Command{
	Use:           "clear",
	Short:         "Clear cached data",
	Long:          "Remove local cache entries only after explicit confirmation. Safe workflow:\n  stockctl cache stats --verify --output json --quiet\n  stockctl cache clear --yes\n\nAdd --market to limit removal to one market. CSV output is not supported.",
	SilenceUsage:  true,
	SilenceErrors: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := rejectCacheCSV("cache clear"); err != nil {
			return err
		}
		if !cacheClearYes {
			return fmt.Errorf("cache clear requires --yes")
		}
		matched, removed, err := cacheClearWithOptions(cacheClearMarket, cacheClearDryRun)
		if err != nil {
			return &cacheClearProgressError{err: err, matched: matched, removed: removed, market: cacheClearMarket, dryRun: cacheClearDryRun}
		}

		if selectedOutputFormat() == output.FormatJSON {
			return output.WriteEnvelope(cmd.OutOrStdout(), output.Envelope{Meta: output.NewMeta("cache-clear"), Results: map[string]interface{}{"matched": matched, "removed": removed, "market": cacheClearMarket, "dry_run": cacheClearDryRun}})
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
	cacheClearCmd.Flags().BoolVar(&cacheClearYes, "yes", false, "confirm cache removal")
	cacheClearCmd.Flags().BoolVar(&cacheClearDryRun, "dry-run", false, "show files that would be removed")
	cacheStatsCmd.Flags().BoolVar(&cacheStatsVerify, "verify", false, "decode cache files to report corrupt entries")
	cacheCmd.AddCommand(cacheStatsCmd)
	cacheCmd.AddCommand(cacheClearCmd)
	rootCmd.AddCommand(cacheCmd)
}
