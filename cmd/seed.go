package cmd

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/rohitsharma04/stockctl/internal/config"
	"github.com/rohitsharma04/stockctl/internal/marketdata"
	"github.com/spf13/cobra"
)

const seedStateVersion = 1

type seedTickerState struct {
	Status    string    `json:"status"`
	Attempts  int       `json:"attempts"`
	NextRetry time.Time `json:"next_retry,omitempty"`
	LastError string    `json:"last_error,omitempty"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type seedCheckpoint struct {
	Version int                         `json:"version"`
	Markets []string                    `json:"markets"`
	Tickers map[string]*seedTickerState `json:"tickers"`
	Updated time.Time                   `json:"updated_at"`
}

type seedSummary struct {
	Started   time.Time `json:"started"`
	Finished  time.Time `json:"finished"`
	Markets   []string  `json:"markets"`
	Total     int       `json:"total"`
	Succeeded int       `json:"succeeded"`
	Failed    int       `json:"failed"`
	Pending   int       `json:"pending"`
	Retries   int       `json:"retries"`
	CacheHits int       `json:"cache_hits,omitempty"`
	Upstream  int       `json:"upstream,omitempty"`
	Stale     int       `json:"stale_cache,omitempty"`
}

// seedProviderFactory is a seam for command tests. Production code always uses
// the rate-aware provider builder.
var seedProviderFactory = func(noCache bool, rps int) marketdata.Provider {
	return marketdata.BuildProviderWithRPS(noCache, rps)
}

func newSeedCmd() *cobra.Command {
	seedCmd := &cobra.Command{Use: "seed", Short: "Populate local market-data cache"}
	seedCmd.AddCommand(newSeedHistoryCmd())
	return seedCmd
}

func newSeedHistoryCmd() *cobra.Command {
	var markets []string
	var stateFile, deadline string
	var rate, workers, maxAttempts int
	cmd := &cobra.Command{
		Use:   "history",
		Short: "Seed five years of daily history into the local cache",
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(markets) == 0 {
				return errors.New("at least one --market (india or us) is required")
			}
			if workers != 1 {
				return errors.New("--workers currently supports only 1 (sequential seed is intentional)")
			}
			if rate <= 0 || maxAttempts < 1 {
				return errors.New("--rate and --max-attempts must be positive")
			}
			for _, market := range markets {
				if market != "india" && market != "us" {
					return fmt.Errorf("unsupported seed market %q (allowed: india, us)", market)
				}
			}
			markets = uniqueStrings(markets)
			sort.Strings(markets)
			if stateFile == "" {
				stateFile = filepath.Join(config.StockctlDir(), "seed-history-state.json")
			}
			ctx := cmd.Context()
			if ctx == nil {
				ctx = context.Background()
			}
			var cancel context.CancelFunc
			if deadline != "" {
				d, err := time.ParseDuration(deadline)
				if err != nil {
					return fmt.Errorf("invalid --deadline: %w", err)
				}
				ctx, cancel = context.WithTimeout(ctx, d)
				defer cancel()
			}
			checkpoint, err := loadSeedCheckpoint(stateFile)
			if err != nil {
				return err
			}
			checkpoint.Markets = markets
			tickers, err := seedTickers(markets)
			if err != nil {
				return err
			}
			now := time.Now().UTC()
			for _, ticker := range tickers {
				if checkpoint.Tickers[ticker] == nil {
					checkpoint.Tickers[ticker] = &seedTickerState{Status: "pending", CreatedAt: now, UpdatedAt: now}
				}
			}
			if err := saveSeedCheckpoint(stateFile, checkpoint); err != nil {
				return err
			}
			provider := seedProviderFactory(noCache, rate)
			summary := seedSummary{Started: now, Markets: markets, Total: len(tickers)}
			for _, ticker := range tickers {
				st := checkpoint.Tickers[ticker]
				if st.Status == "success" || st.Status == "failed" {
					continue
				}
				if !st.NextRetry.IsZero() && time.Now().Before(st.NextRetry) {
					continue // durable resume: a later invocation owns this retry
				}
				for {
					if err := ctx.Err(); err != nil {
						markSeedFailure(st, err)
						if err := saveSeedCheckpoint(stateFile, checkpoint); err != nil {
							return err
						}
						break
					}
					st.Attempts++
					st.UpdatedAt = time.Now().UTC()
					err := seedGetHistory(ctx, provider, ticker, &summary)
					if err == nil {
						st.Status, st.LastError, st.NextRetry = "success", "", time.Time{}
						st.UpdatedAt = time.Now().UTC()
						if err := saveSeedCheckpoint(stateFile, checkpoint); err != nil {
							return err
						}
						break
					}
					if ctx.Err() != nil || !isTransientSeedError(err) || st.Attempts >= maxAttempts {
						markSeedFailure(st, err)
						if err := saveSeedCheckpoint(stateFile, checkpoint); err != nil {
							return err
						}
						break
					}
					st.Status, st.LastError = "retry", err.Error()
					delay := seedJitter(st.Attempts)
					st.NextRetry, st.UpdatedAt = time.Now().Add(delay).UTC(), time.Now().UTC()
					summary.Retries++
					if err := saveSeedCheckpoint(stateFile, checkpoint); err != nil {
						return err
					}
					select {
					case <-ctx.Done():
						markSeedFailure(st, ctx.Err())
						if err := saveSeedCheckpoint(stateFile, checkpoint); err != nil {
							return err
						}
					case <-time.After(delay):
						continue
					}
					break
				}
			}
			summary.Finished = time.Now().UTC()
			for _, ticker := range tickers {
				switch checkpoint.Tickers[ticker].Status {
				case "success":
					summary.Succeeded++
				case "failed":
					summary.Failed++
				default:
					summary.Pending++
				}
			}
			if err := json.NewEncoder(cmd.OutOrStdout()).Encode(summary); err != nil {
				return err
			}
			if summary.Failed > 0 || summary.Pending > 0 {
				return fmt.Errorf("seed incomplete: %d failed, %d pending", summary.Failed, summary.Pending)
			}
			return nil
		},
	}
	cmd.Flags().StringSliceVar(&markets, "market", nil, "market to seed (repeatable: india, us; required)")
	cmd.Flags().StringVar(&stateFile, "state-file", "", "checkpoint JSON file")
	cmd.Flags().StringVar(&deadline, "deadline", "", "overall seed deadline (for example 30m)")
	cmd.Flags().IntVar(&rate, "rate", 5, "maximum upstream requests per second")
	cmd.Flags().IntVar(&workers, "workers", 1, "workers (only 1 is currently supported)")
	cmd.Flags().IntVar(&maxAttempts, "max-attempts", 3, "maximum attempts per ticker")
	return cmd
}

func seedGetHistory(ctx context.Context, provider marketdata.Provider, ticker string, summary *seedSummary) error {
	if hp, ok := provider.(marketdata.HistoryProvider); ok {
		result, err := hp.GetHistoryWithProvenance(ctx, marketdata.HistoryRequest{Symbol: ticker, Period: "5y", Interval: "1d"})
		if err == nil {
			switch result.Provenance.Source {
			case marketdata.HistorySourceCache:
				summary.CacheHits++
			case marketdata.HistorySourceUpstream, marketdata.HistorySourceCacheAndUpstream:
				summary.Upstream++
			}
			if result.Provenance.Stale {
				summary.Stale++
			}
		}
		return err
	}
	_, err := provider.GetHistory(ctx, ticker, "5y", "1d")
	return err
}

func seedTickers(markets []string) ([]string, error) {
	seen := map[string]bool{}
	var result []string
	for _, id := range markets {
		m := marketdata.Markets[id]
		universe, err := marketdata.GetUniverse(id)
		if err != nil {
			return nil, err
		}
		for _, ticker := range universe {
			ticker = m.ApplySuffix(ticker)
			if !seen[ticker] {
				seen[ticker] = true
				result = append(result, ticker)
			}
		}
		if !seen[m.Benchmark] {
			seen[m.Benchmark] = true
			result = append(result, m.Benchmark)
		}
	}
	return result, nil
}

func loadSeedCheckpoint(path string) (*seedCheckpoint, error) {
	state := &seedCheckpoint{Version: seedStateVersion, Tickers: map[string]*seedTickerState{}}
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return state, nil
	}
	if err != nil {
		return nil, fmt.Errorf("reading seed state: %w", err)
	}
	if err := json.Unmarshal(data, state); err != nil {
		return nil, fmt.Errorf("parsing seed state %s: %w", path, err)
	}
	if state.Tickers == nil {
		state.Tickers = map[string]*seedTickerState{}
	}
	if state.Version != seedStateVersion {
		return nil, fmt.Errorf("unsupported seed state version %d", state.Version)
	}
	return state, nil
}

func saveSeedCheckpoint(path string, state *seedCheckpoint) error {
	state.Updated = time.Now().UTC()
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".seed-history-*.tmp")
	if err != nil {
		return err
	}
	name := tmp.Name()
	defer os.Remove(name)
	if _, err = tmp.Write(append(data, '\n')); err == nil {
		err = tmp.Chmod(0600)
	}
	if closeErr := tmp.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	return os.Rename(name, path)
}

func markSeedFailure(st *seedTickerState, err error) {
	st.Status, st.LastError, st.NextRetry, st.UpdatedAt = "failed", err.Error(), time.Time{}, time.Now().UTC()
}
func seedJitter(attempt int) time.Duration {
	cap := 2 * time.Second
	max := 100 * time.Millisecond * time.Duration(1<<uint(attempt-1))
	if max > cap {
		max = cap
	}
	return time.Duration(rand.Int63n(int64(max) + 1))
}
func isTransientSeedError(err error) bool {
	if err == nil || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return false
	}
	s := strings.ToLower(err.Error())
	for _, token := range []string{"429", "timeout", "temporary", "connection", "network", "5xx", "500", "502", "503", "504", "rate limit"} {
		if strings.Contains(s, token) {
			return true
		}
	}
	type temporary interface{ Temporary() bool }
	var t temporary
	return errors.As(err, &t) && t.Temporary()
}
func uniqueStrings(in []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, s := range in {
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	return out
}

func init() { rootCmd.AddCommand(newSeedCmd()) }
