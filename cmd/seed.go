package cmd

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/rand"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"

	"github.com/rohitsharma04/stockctl/internal/config"
	"github.com/rohitsharma04/stockctl/internal/marketdata"
	"github.com/rohitsharma04/stockctl/internal/output"
	"github.com/spf13/cobra"
)

const seedStateVersion = 3
const legacySeedHistoryPeriod = "5y"
const defaultSeedHistoryPeriod = "max"

// seedHistoryCoverage identifies the semantic promise of a completed seed,
// rather than merely the Yahoo period spelling. Bump it when that promise
// changes so old successes are never reused for a broader request.
const seedHistoryCoverage = "yahoo-daily-all-available-v1"

type seedTickerState struct {
	Status    string    `json:"status"`
	Attempts  int       `json:"attempts"`
	NextRetry time.Time `json:"next_retry,omitempty"`
	LastError string    `json:"last_error,omitempty"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type seedCheckpoint struct {
	Version  int                         `json:"version"`
	Period   string                      `json:"period"`
	Coverage string                      `json:"coverage"`
	Markets  []string                    `json:"markets"`
	Tickers  map[string]*seedTickerState `json:"tickers"`
	Updated  time.Time                   `json:"updated_at"`
}

type seedSummary struct {
	RunID     string    `json:"run_id"`
	Started   time.Time `json:"started"`
	Finished  time.Time `json:"finished"`
	Markets   []string  `json:"markets"`
	Period    string    `json:"requested_period"`
	Coverage  string    `json:"coverage_intent"`
	Total     int       `json:"total"`
	Attempted int       `json:"attempted"`
	Succeeded int       `json:"succeeded"`
	Failed    int       `json:"failed"`
	Pending   int       `json:"pending"`
	Retries   int       `json:"retries"`
	CacheHits int       `json:"cache_hits,omitempty"`
	Upstream  int       `json:"upstream,omitempty"`
	Stale     int       `json:"stale_cache,omitempty"`
}

type seedFailure struct {
	Ticker    string    `json:"ticker"`
	Status    string    `json:"status"`
	Attempts  int       `json:"attempts"`
	LastError string    `json:"last_error,omitempty"`
	NextRetry time.Time `json:"next_retry,omitempty"`
}

type seedHistoryResult struct {
	Summary  seedSummary   `json:"summary"`
	Failures []seedFailure `json:"failures"`
}

// seedHistoryIncompleteError marks a JSON envelope that seed history already
// wrote. The root must preserve that command-owned envelope verbatim.
type seedHistoryIncompleteError struct {
	err    error
	result seedHistoryResult
}

func (e *seedHistoryIncompleteError) Error() string            { return e.err.Error() }
func (e *seedHistoryIncompleteError) Unwrap() error            { return e.err }
func (e *seedHistoryIncompleteError) JSONResults() interface{} { return e.result }
func (e *seedHistoryIncompleteError) OutputWritten() bool      { return true }

var ErrSeedAlreadyRunning = errors.New("seed already running")

func acquireSeedRunLease(stateFile string) (func(), error) {
	lockPath := stateFile + ".lock"
	if err := os.MkdirAll(filepath.Dir(lockPath), 0755); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		_ = f.Close()
		if err == syscall.EWOULDBLOCK || err == syscall.EAGAIN {
			return nil, fmt.Errorf("%w: %s", ErrSeedAlreadyRunning, lockPath)
		}
		return nil, err
	}
	return func() { _ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN); _ = f.Close() }, nil
}

// seedProviderFactory is a seam for command tests. Production code always uses
// the rate-aware provider builder.
var seedProviderFactory = func(noCache bool, rps int) marketdata.Provider {
	return marketdata.BuildProviderWithRPS(noCache, rps)
}

// seedCacheRead is a test seam around the cache proof required before a
// checkpointed success can be trusted on a later resume.
var seedCacheRead = func(ticker, period string) ([]marketdata.OHLCV, error) {
	data, _, err := marketdata.ReadCacheEntry(filepath.Join(marketdata.CacheDir(), marketdata.CacheFilename(ticker, period, "1d")))
	return data, err
}

func newSeedCmd() *cobra.Command {
	seedCmd := &cobra.Command{
		Use:   "seed",
		Short: "Populate local market-data cache",
		Long: `Populate and verify durable daily-history cache data.

stockctl owns checkpointing, retries, rate limiting, resume behavior, and
cache writes. Hermes only schedules, launches, and supervises delivery.

Canonical workflow:
  stockctl seed history --market us --period max --output json --quiet
  stockctl seed status --output json --quiet
  stockctl seed verify --market us --period max --output json --quiet
  stockctl cache stats --verify --output json --quiet
  stockctl cache clear --yes`,
	}
	seedCmd.AddCommand(newSeedHistoryCmd(), newSeedStatusCmd(), newSeedVerifyCmd())
	return seedCmd
}

func newSeedStatusCmd() *cobra.Command {
	var stateFile string
	cmd := &cobra.Command{Use: "status", Short: "Show seed checkpoint status", Long: "Show the stockctl-owned durable seed checkpoint. Use after `seed history` to inspect resumable work:\n  stockctl seed status --output json --quiet", SilenceUsage: true, SilenceErrors: true, RunE: func(cmd *cobra.Command, args []string) error {
		if stateFile == "" {
			stateFile = filepath.Join(config.StockctlDir(), "seed-history-state.json")
		}
		if _, err := os.Stat(stateFile); os.IsNotExist(err) {
			return writeSeedResult(cmd, "seed-status", map[string]interface{}{"status": "not_found", "state_file": stateFile})
		} else if err != nil {
			return err
		}
		state, err := loadSeedCheckpoint(stateFile)
		if err != nil {
			return err
		}
		counts := map[string]int{}
		due := 0
		var oldest time.Time
		samples := make([]seedFailure, 0, 20)
		now := time.Now()
		tickers := make([]string, 0, len(state.Tickers))
		for ticker := range state.Tickers {
			tickers = append(tickers, ticker)
		}
		sort.Strings(tickers)
		for _, ticker := range tickers {
			st := state.Tickers[ticker]
			counts[st.Status]++
			if st.Status != "success" && (st.NextRetry.IsZero() || !st.NextRetry.After(now)) {
				due++
			}
			if st.Status != "success" && (oldest.IsZero() || st.CreatedAt.Before(oldest)) {
				oldest = st.CreatedAt
			}
			if st.Status != "success" && len(samples) < 20 {
				samples = append(samples, seedFailure{Ticker: ticker, Status: st.Status, Attempts: st.Attempts, LastError: st.LastError, NextRetry: st.NextRetry})
			}
		}
		return writeSeedResult(cmd, "seed-status", map[string]interface{}{"status": "ok", "state_file": stateFile, "version": state.Version, "period": state.Period, "coverage": state.Coverage, "markets": state.Markets, "updated_at": state.Updated, "counts": counts, "due_retry": due, "oldest_pending": oldest, "failures": samples})
	}}
	cmd.Flags().StringVar(&stateFile, "state-file", "", "checkpoint JSON file")
	return cmd
}

func newSeedVerifyCmd() *cobra.Command {
	var markets []string
	var stateFile, period string
	cmd := &cobra.Command{Use: "verify", Short: "Verify seeded cache entries without Yahoo calls", Long: "Verify stockctl-managed seed cache entries locally, without Yahoo calls. Run after seed history:\n  stockctl seed verify --market us --period max --output json --quiet", SilenceUsage: true, SilenceErrors: true, RunE: func(cmd *cobra.Command, args []string) error {
		if len(markets) == 0 {
			return errors.New("at least one --market (india or us) is required")
		}
		markets = uniqueStrings(markets)
		sort.Strings(markets)
		if period == "" {
			period = defaultSeedHistoryPeriod
		}
		if !isSupportedSeedPeriod(period) {
			return fmt.Errorf("unsupported --period %q", period)
		}
		for _, market := range markets {
			if market != "india" && market != "us" {
				return fmt.Errorf("unsupported seed market %q (allowed: india, us)", market)
			}
		}
		valid, missing, corrupt := 0, 0, 0
		tickers, err := seedTickers(markets)
		if err != nil {
			return err
		}
		details := make([]seedFailure, 0)
		for _, ticker := range tickers {
			data, _, err := marketdata.ReadCacheEntry(filepath.Join(marketdata.CacheDir(), marketdata.CacheFilename(ticker, period, "1d")))
			if os.IsNotExist(err) {
				missing++
				details = append(details, seedFailure{Ticker: ticker, Status: "missing"})
				continue
			}
			if err != nil || validateSeedHistory(data, period) != nil {
				corrupt++
				msg := "invalid cache"
				if err != nil {
					msg = err.Error()
				}
				details = append(details, seedFailure{Ticker: ticker, Status: "corrupt", LastError: msg})
				continue
			}
			valid++
		}
		return writeSeedResult(cmd, "seed-verify", map[string]interface{}{"state_file": stateFile, "markets": markets, "period": period, "valid": valid, "missing": missing, "corrupt": corrupt, "failures": details})
	}}
	cmd.Flags().StringSliceVar(&markets, "market", nil, "market to verify (repeatable: india, us; required)")
	cmd.Flags().StringVar(&stateFile, "state-file", "", "checkpoint JSON file (identity only)")
	cmd.Flags().StringVar(&period, "period", defaultSeedHistoryPeriod, "cached history period")
	return cmd
}

func writeSeedResult(cmd *cobra.Command, command string, result interface{}) error {
	if selectedOutputFormat() == output.FormatJSON {
		return output.WriteEnvelope(cmd.OutOrStdout(), output.Envelope{Meta: output.NewMeta(command), Results: result})
	}
	if selectedOutputFormat() == output.FormatCSV {
		return fmt.Errorf("%s does not support --output csv", command)
	}
	values, ok := result.(map[string]interface{})
	if !ok {
		return fmt.Errorf("cannot render %s as a table", command)
	}
	switch command {
	case "seed-status":
		fmt.Fprintln(cmd.OutOrStdout(), "Seed status")
		fmt.Fprintf(cmd.OutOrStdout(), "Status: %v\nState file: %v\nPeriod: %v\nCoverage: %v\n", values["status"], values["state_file"], values["period"], values["coverage"])
		fmt.Fprintln(cmd.OutOrStdout(), "\nStatus counts:")
		tw := output.NewTableWriter(cmd.OutOrStdout())
		tw.SetHeaders("Status", "Count")
		counts, _ := values["counts"].(map[string]int)
		statuses := make([]string, 0, len(counts))
		for status := range counts {
			statuses = append(statuses, status)
		}
		sort.Strings(statuses)
		for _, status := range statuses {
			tw.AddRow(status, fmt.Sprintf("%d", counts[status]))
		}
		tw.Render()
		fmt.Fprintf(cmd.OutOrStdout(), "Due retry: %v\n", values["due_retry"])
	case "seed-verify":
		fmt.Fprintln(cmd.OutOrStdout(), "Seed cache verification")
		tw := output.NewTableWriter(cmd.OutOrStdout())
		tw.SetHeaders("Valid", "Missing", "Corrupt")
		tw.AddRow(fmt.Sprint(values["valid"]), fmt.Sprint(values["missing"]), fmt.Sprint(values["corrupt"]))
		tw.Render()
	default:
		return fmt.Errorf("%s does not support table output", command)
	}
	return nil
}

func newSeedHistoryCmd() *cobra.Command {
	var markets []string
	var stateFile, deadline, period string
	var rate, workers, maxAttempts int
	cmd := &cobra.Command{
		Use:   "history",
		Short: "Seed daily history into the local cache",
		Long: `Seed durable daily history into the local cache.

stockctl owns the checkpoint, retry, rate-limit, cache, and resume logic. A
later invocation resumes from the same checkpoint; Hermes only schedules,
launches, and supervises delivery. Follow with:
  stockctl seed status --output json --quiet
  stockctl seed verify --market us --period max --output json --quiet`,
		SilenceUsage:  true,
		SilenceErrors: true,
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
			if !isSupportedSeedPeriod(period) {
				return fmt.Errorf("unsupported --period %q (allowed: 5y, 10y, max)", period)
			}
			for _, market := range markets {
				if market != "india" && market != "us" {
					return fmt.Errorf("unsupported seed market %q (allowed: india, us)", market)
				}
			}
			markets = uniqueStrings(markets)
			sort.Strings(markets)
			if noCache {
				return errors.New("seed history cannot use --no-cache because it would not populate the disk cache")
			}
			if stateFile == "" {
				stateFile = filepath.Join(config.StockctlDir(), "seed-history-state.json")
			}
			release, err := acquireSeedRunLease(stateFile)
			if err != nil {
				return err
			}
			defer release()
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
			now := time.Now().UTC()
			reconcileSeedCheckpointCoverage(checkpoint, period, seedCoverageIntent(period), now)
			checkpoint.Markets = markets
			tickers, err := seedTickers(markets)
			if err != nil {
				return err
			}
			for _, ticker := range tickers {
				if checkpoint.Tickers[ticker] == nil {
					checkpoint.Tickers[ticker] = &seedTickerState{Status: "pending", CreatedAt: now, UpdatedAt: now}
				}
			}
			requeueUnverifiedSeedSuccesses(checkpoint, tickers, period, now)
			if err := saveSeedCheckpoint(stateFile, checkpoint); err != nil {
				return err
			}
			provider := seedProviderFactory(noCache, rate)
			summary := seedSummary{RunID: fmt.Sprintf("seed-%d", now.UnixNano()), Started: now, Markets: markets, Period: period, Coverage: seedCoverageIntent(period), Total: len(tickers)}
			interrupted := false
			for _, ticker := range tickers {
				if interrupted {
					break
				}
				st := checkpoint.Tickers[ticker]
				if st.Status == "success" || st.Status == "failed" {
					continue
				}
				if !st.NextRetry.IsZero() && time.Now().Before(st.NextRetry) {
					continue // durable resume: a later invocation owns this retry
				}
				for {
					if err := ctx.Err(); err != nil {
						markSeedPending(st, err)
						if err := saveSeedCheckpoint(stateFile, checkpoint); err != nil {
							return err
						}
						interrupted = true
						break
					}
					st.Attempts++
					summary.Attempted++
					st.UpdatedAt = time.Now().UTC()
					err := seedGetHistory(ctx, provider, ticker, period, &summary)
					if err == nil {
						st.Status, st.LastError, st.NextRetry = "success", "", time.Time{}
						st.UpdatedAt = time.Now().UTC()
						if err := saveSeedCheckpoint(stateFile, checkpoint); err != nil {
							return err
						}
						break
					}
					if ctx.Err() != nil || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
						markSeedPending(st, err)
						if err := saveSeedCheckpoint(stateFile, checkpoint); err != nil {
							return err
						}
						interrupted = true
						break
					}
					if !isTransientSeedError(err) || st.Attempts >= maxAttempts {
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
						markSeedPending(st, ctx.Err())
						if err := saveSeedCheckpoint(stateFile, checkpoint); err != nil {
							return err
						}
						interrupted = true
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
			failures := seedFailures(tickers, checkpoint)
			meta := output.NewMeta("seed-history")
			meta.DurationMs = summary.Finished.Sub(summary.Started).Milliseconds()
			result := seedHistoryResult{Summary: summary, Failures: failures}
			incomplete := summary.Failed > 0 || summary.Pending > 0
			incompleteErr := fmt.Errorf("seed incomplete: %d failed, %d pending", summary.Failed, summary.Pending)
			if selectedOutputFormat() == output.FormatJSON {
				env := output.Envelope{Meta: meta, Results: result}
				if incomplete {
					env.Errors = []output.ErrorInfo{{Error: incompleteErr.Error()}}
				}
				if err := output.WriteEnvelope(cmd.OutOrStdout(), env); err != nil {
					return err
				}
				if incomplete {
					return &seedHistoryIncompleteError{err: incompleteErr, result: result}
				}
				return nil
			}
			writeSeedHistoryText(cmd.OutOrStdout(), result)
			if incomplete {
				return incompleteErr
			}
			return nil
		},
	}
	cmd.Flags().StringSliceVar(&markets, "market", nil, "market to seed (repeatable: india, us; required)")
	cmd.Flags().StringVar(&stateFile, "state-file", "", "checkpoint JSON file")
	cmd.Flags().StringVar(&deadline, "deadline", "", "overall seed deadline (for example 30m)")
	cmd.Flags().StringVar(&period, "period", defaultSeedHistoryPeriod, "Yahoo daily history period to seed (default max: all available daily history)")
	cmd.Flags().IntVar(&rate, "rate", 5, "maximum upstream requests per second")
	cmd.Flags().IntVar(&workers, "workers", 1, "workers (only 1 is currently supported)")
	cmd.Flags().IntVar(&maxAttempts, "max-attempts", 3, "maximum attempts per ticker")
	return cmd
}

func writeSeedHistoryText(w io.Writer, result seedHistoryResult) {
	s := result.Summary
	fmt.Fprintln(w, "Seed history summary")
	fmt.Fprintf(w, "Markets: %s\nPeriod: %s\n", strings.Join(s.Markets, ", "), s.Period)
	fmt.Fprintf(w, "Total: %d  Attempted: %d  Succeeded: %d  Failed: %d  Pending: %d  Retries: %d\n", s.Total, s.Attempted, s.Succeeded, s.Failed, s.Pending, s.Retries)
	if len(result.Failures) == 0 {
		return
	}
	fmt.Fprintln(w, "Incomplete entries:")
	for _, failure := range result.Failures {
		if failure.LastError == "" {
			fmt.Fprintf(w, "  %s: %s (attempts: %d)\n", failure.Ticker, failure.Status, failure.Attempts)
			continue
		}
		fmt.Fprintf(w, "  %s: %s (attempts: %d; %s)\n", failure.Ticker, failure.Status, failure.Attempts, failure.LastError)
	}
}

func seedGetHistory(ctx context.Context, provider marketdata.Provider, ticker, period string, summary *seedSummary) error {
	if hp, ok := provider.(marketdata.HistoryProvider); ok {
		result, err := hp.GetHistoryWithProvenance(ctx, marketdata.HistoryRequest{Symbol: ticker, Period: period, Interval: "1d", RequireCompletePeriod: true})
		if err == nil {
			switch result.Provenance.Source {
			case marketdata.HistorySourceCache:
				summary.CacheHits++
			case marketdata.HistorySourceUpstream, marketdata.HistorySourceCacheAndUpstream:
				summary.Upstream++
			}
			if result.Provenance.Stale {
				summary.Stale++
				if result.Provenance.UpstreamError != "" {
					return fmt.Errorf("stale cache fallback: %s", result.Provenance.UpstreamError)
				}
				return errors.New("stale cache fallback")
			}
			if err = validateSeedHistory(result.Data, period); err != nil {
				return err
			}
		}
		return err
	}
	data, err := provider.GetHistory(ctx, ticker, period, "1d")
	if err != nil {
		return err
	}
	return validateSeedHistory(data, period)
}

func validateSeedHistory(data []marketdata.OHLCV, period string) error {
	if len(data) == 0 {
		return errors.New("seed history is empty")
	}
	var previous time.Time
	for i, bar := range data {
		if bar.Date.IsZero() {
			return fmt.Errorf("seed history has invalid date at index %d", i)
		}
		day := time.Date(bar.Date.Year(), bar.Date.Month(), bar.Date.Day(), 0, 0, 0, 0, bar.Date.Location())
		if i > 0 && !day.After(previous) {
			return fmt.Errorf("seed history dates are not strictly ascending")
		}
		previous = day
	}
	if period == "5y" || period == "10y" {
		start := time.Now().AddDate(-map[string]int{"5y": 5, "10y": 10}[period], 0, 0)
		if data[0].Date.After(start) {
			return fmt.Errorf("seed history does not cover requested %s period", period)
		}
	}
	return nil
}

func seedFailures(tickers []string, checkpoint *seedCheckpoint) []seedFailure {
	failures := make([]seedFailure, 0)
	for _, ticker := range tickers {
		st := checkpoint.Tickers[ticker]
		if st != nil && st.Status != "success" {
			failures = append(failures, seedFailure{Ticker: ticker, Status: st.Status, Attempts: st.Attempts, LastError: st.LastError, NextRetry: st.NextRetry})
		}
	}
	if len(failures) > 100 {
		return failures[:100]
	}
	return failures
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

// requeueUnverifiedSeedSuccesses makes checkpoint state subordinate to the
// durable cache. A success without a readable, period-valid cache entry may
// have been written by an older buggy seed and must be fetched again.
func requeueUnverifiedSeedSuccesses(checkpoint *seedCheckpoint, tickers []string, period string, now time.Time) int {
	requeued := 0
	for _, ticker := range tickers {
		st := checkpoint.Tickers[ticker]
		if st == nil || st.Status != "success" {
			continue
		}
		data, err := seedCacheRead(ticker, period)
		if err == nil {
			err = validateSeedHistory(data, period)
		}
		if err == nil {
			continue
		}
		st.Status = "pending"
		st.Attempts = 0
		st.NextRetry = time.Time{}
		st.LastError = fmt.Sprintf("checkpoint success cache validation failed: %v", err)
		st.UpdatedAt = now
		requeued++
	}
	return requeued
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
	switch state.Version {
	case 1:
		state.Version = seedStateVersion
		state.Period = legacySeedHistoryPeriod
	case 2:
		// Version 2 recorded a period but no coverage promise. Treat it as
		// incompatible so a newly stronger seed can never skip old successes.
		state.Version = seedStateVersion
	case seedStateVersion:
	default:
		return nil, fmt.Errorf("unsupported seed state version %d", state.Version)
	}
	return state, nil
}

func seedCoverageIntent(period string) string {
	if period == "max" {
		return seedHistoryCoverage
	}
	return "yahoo-daily-period-" + period + "-v1"
}

func reconcileSeedCheckpointCoverage(state *seedCheckpoint, period, coverage string, now time.Time) {
	if state.Period == period && state.Coverage == coverage {
		return
	}
	state.Period = period
	state.Coverage = coverage
	state.Tickers = map[string]*seedTickerState{}
	state.Updated = now
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
func markSeedPending(st *seedTickerState, err error) {
	st.Status, st.LastError, st.NextRetry, st.UpdatedAt = "pending", err.Error(), time.Time{}, time.Now().UTC()
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
func isSupportedSeedPeriod(period string) bool {
	switch period {
	case "5y", "10y", "max":
		return true
	default:
		return false
	}
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
