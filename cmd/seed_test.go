package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/rohitsharma04/stockctl/internal/marketdata"
	"github.com/rohitsharma04/stockctl/internal/output"
	"github.com/spf13/cobra"
)

type seedTestProvider struct {
	mu          sync.Mutex
	calls       map[string]int
	periods     map[string][]string
	order       []string
	err         error
	blockOnCall int
}

type staleSeedProvider struct{ *seedTestProvider }

func (p *staleSeedProvider) GetHistoryWithProvenance(_ context.Context, req marketdata.HistoryRequest) (*marketdata.HistoryResult, error) {
	p.mu.Lock()
	p.calls[req.Symbol]++
	p.mu.Unlock()
	return &marketdata.HistoryResult{Data: []marketdata.OHLCV{{Close: 100}}, Provenance: marketdata.HistoryProvenance{
		Source: marketdata.HistorySourceCache, Stale: true, UpstreamError: "HTTP 503 temporary network failure",
	}}, nil
}

func TestSeedHistoryRejectsNoCache(t *testing.T) {
	oldNoCache := noCache
	noCache = true
	t.Cleanup(func() { noCache = oldNoCache })

	command := newSeedHistoryCmd()
	command.SetArgs([]string{"--market", "us", "--state-file", filepath.Join(t.TempDir(), "state.json")})
	err := command.Execute()
	if err == nil || err.Error() != "seed history cannot use --no-cache because it would not populate the disk cache" {
		t.Fatalf("--no-cache error = %v", err)
	}
}

func TestSeedGetHistoryTreatsStaleFallbackAsFailure(t *testing.T) {
	provider := &staleSeedProvider{seedTestProvider: &seedTestProvider{calls: map[string]int{}}}
	summary := &seedSummary{}
	err := seedGetHistory(context.Background(), provider, "STALE", "max", summary)
	if err == nil || err.Error() != "stale cache fallback: HTTP 503 temporary network failure" {
		t.Fatalf("stale fallback error = %v", err)
	}
	if summary.Stale != 1 || summary.CacheHits != 1 {
		t.Fatalf("summary = %#v, want one stale cache hit", summary)
	}
}

func (p *seedTestProvider) GetHistory(ctx context.Context, symbol, period, interval string) ([]marketdata.OHLCV, error) {
	p.mu.Lock()
	p.calls[symbol]++
	if p.periods != nil {
		p.periods[symbol] = append(p.periods[symbol], period)
	}
	p.order = append(p.order, symbol)
	call := len(p.order)
	p.mu.Unlock()
	if p.blockOnCall == call {
		<-ctx.Done()
		return nil, ctx.Err()
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if p.err != nil {
		return nil, p.err
	}
	start := time.Now().UTC().AddDate(-1, 0, 0)
	if period == "5y" || period == "10y" {
		start = time.Now().UTC().AddDate(-11, 0, 0)
	}
	return []marketdata.OHLCV{{Date: start, Close: 100}, {Date: time.Now().UTC(), Close: 101}}, nil
}

func TestSeedHistoryDefaultPeriodMaxPassesThroughToHistoryProvider(t *testing.T) {
	stateFile := filepath.Join(t.TempDir(), "state.json")
	provider := &seedTestProvider{calls: map[string]int{}, periods: map[string][]string{}}
	summary, stderr, err := executeSeedHistoryForTest(t, provider, "--market", "us", "--state-file", stateFile)
	if err != nil {
		t.Fatal(err)
	}
	if stderr != "" {
		t.Fatalf("stderr = %q, want quiet command output", stderr)
	}
	if summary.Period != "max" || summary.Coverage != seedHistoryCoverage {
		t.Fatalf("summary identity = %q/%q, want max/%q", summary.Period, summary.Coverage, seedHistoryCoverage)
	}
	tickers, _ := seedTickers([]string{"us"})
	if summary.Succeeded != len(tickers) {
		t.Fatalf("summary = %#v, want all tickers successful", summary)
	}
	state, err := loadSeedCheckpoint(stateFile)
	if err != nil {
		t.Fatal(err)
	}
	if state.Version != seedStateVersion || state.Period != "max" || state.Coverage != seedHistoryCoverage {
		t.Fatalf("checkpoint identity = %d/%q/%q, want %d/max/%q", state.Version, state.Period, state.Coverage, seedStateVersion, seedHistoryCoverage)
	}
	for _, ticker := range tickers {
		if got := provider.periods[ticker]; len(got) != 1 || got[0] != "max" {
			t.Fatalf("periods[%s] = %#v, want [max]", ticker, got)
		}
	}
}

func TestSeedHistoryExplicitPeriodPassesThroughToProvider(t *testing.T) {
	stateFile := filepath.Join(t.TempDir(), "state.json")
	provider := &seedTestProvider{calls: map[string]int{}, periods: map[string][]string{}}
	_, _, err := executeSeedHistoryForTest(t, provider, "--market", "us", "--state-file", stateFile, "--period", "10y")
	if err != nil {
		t.Fatal(err)
	}
	for ticker, periods := range provider.periods {
		if len(periods) != 1 || periods[0] != "10y" {
			t.Fatalf("periods[%s] = %#v, want [10y]", ticker, periods)
		}
	}
}

func TestSeedHistoryPeriodMaxUpgradesLegacyCheckpointAndRefetchesOldSuccesses(t *testing.T) {
	stateFile := filepath.Join(t.TempDir(), "state.json")
	tickers, _ := seedTickers([]string{"india"})
	now := time.Now().UTC()
	legacy := &seedCheckpoint{Version: 1, Markets: []string{"india"}, Tickers: map[string]*seedTickerState{}}
	for _, ticker := range tickers {
		legacy.Tickers[ticker] = &seedTickerState{Status: "success", Attempts: 1, CreatedAt: now, UpdatedAt: now}
	}
	if err := saveSeedCheckpoint(stateFile, legacy); err != nil {
		t.Fatal(err)
	}

	provider := &seedTestProvider{calls: map[string]int{}, periods: map[string][]string{}}
	summary, stderr, err := executeSeedHistoryForTest(t, provider, "--market", "india", "--state-file", stateFile, "--period", "max")
	if err != nil {
		t.Fatal(err)
	}
	if stderr != "" {
		t.Fatalf("stderr = %q, want quiet command output", stderr)
	}
	if summary.Succeeded != len(tickers) || summary.Pending != 0 || summary.Failed != 0 {
		t.Fatalf("summary = %#v, want completed max seed", summary)
	}
	if len(provider.calls) != len(tickers) {
		t.Fatalf("legacy successes were skipped: got %d calls, want %d", len(provider.calls), len(tickers))
	}
	for _, ticker := range tickers {
		if provider.calls[ticker] != 1 {
			t.Fatalf("calls[%s] = %d, want legacy success refetched for max", ticker, provider.calls[ticker])
		}
		if got := provider.periods[ticker]; len(got) != 1 || got[0] != "max" {
			t.Fatalf("periods[%s] = %#v, want [max]", ticker, got)
		}
	}
	state, err := loadSeedCheckpoint(stateFile)
	if err != nil {
		t.Fatal(err)
	}
	if state.Version != seedStateVersion || state.Period != "max" || state.Coverage != seedHistoryCoverage {
		t.Fatalf("checkpoint identity = %d/%q/%q, want %d/max/%q", state.Version, state.Period, state.Coverage, seedStateVersion, seedHistoryCoverage)
	}
}
func (p *seedTestProvider) GetQuote(context.Context, string) (*marketdata.Quote, error) {
	return nil, nil
}

func decodeSingleSeedSummary(t *testing.T, stdout []byte) seedSummary {
	t.Helper()
	var envelope struct {
		Meta    output.Meta `json:"meta"`
		Results struct {
			Summary seedSummary `json:"summary"`
		} `json:"results"`
	}
	dec := json.NewDecoder(bytes.NewReader(stdout))
	if decodeErr := dec.Decode(&envelope); decodeErr != nil {
		t.Fatalf("stdout must contain a parseable JSON envelope, got %q: %v", string(stdout), decodeErr)
	}
	if envelope.Meta.Command != "seed-history" {
		t.Fatalf("command = %q", envelope.Meta.Command)
	}
	var extra json.RawMessage
	if decodeErr := dec.Decode(&extra); !errors.Is(decodeErr, io.EOF) {
		t.Fatalf("stdout must contain exactly one JSON value, got %q", string(stdout))
	}
	return envelope.Results.Summary
}

func TestValidateSeedHistory(t *testing.T) {
	now := time.Now().UTC()
	cases := []struct {
		name, period string
		data         []marketdata.OHLCV
		wantErr      bool
	}{
		{"empty", "max", nil, true},
		{"duplicate", "max", []marketdata.OHLCV{{Date: now}, {Date: now}}, true},
		{"unordered", "max", []marketdata.OHLCV{{Date: now}, {Date: now.AddDate(0, 0, -1)}}, true},
		{"undercovered", "5y", []marketdata.OHLCV{{Date: now.AddDate(-4, 0, 0)}, {Date: now}}, true},
		{"sparse-valid", "5y", []marketdata.OHLCV{{Date: now.AddDate(-5, 0, -1)}, {Date: now}}, false},
		{"max-valid", "max", []marketdata.OHLCV{{Date: now.AddDate(-1, 0, 0)}, {Date: now}}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := validateSeedHistory(tc.data, tc.period); (err != nil) != tc.wantErr {
				t.Fatalf("err = %v", err)
			}
		})
	}
}

func TestSeedRunLeaseRejectsSecondOwner(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	release, err := acquireSeedRunLease(path)
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	_, err = acquireSeedRunLease(path)
	if !errors.Is(err, ErrSeedAlreadyRunning) {
		t.Fatalf("err = %v", err)
	}
}

func TestSeedStatusMissingStateIsSuccessful(t *testing.T) {
	command := newSeedStatusCmd()
	var stdout bytes.Buffer
	command.SetOut(&stdout)
	command.SetArgs([]string{"--state-file", filepath.Join(t.TempDir(), "missing.json")})
	if err := command.Execute(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stdout.String(), "not_found") {
		t.Fatalf("status output = %s", stdout.String())
	}
}

func TestSeedStatusHonorsTableAndJSONOutputContractsThroughRoot(t *testing.T) {
	stateFile := filepath.Join(t.TempDir(), "state.json")
	now := time.Now().UTC()
	if err := saveSeedCheckpoint(stateFile, &seedCheckpoint{Version: seedStateVersion, Period: "max", Coverage: seedHistoryCoverage, Tickers: map[string]*seedTickerState{
		"GOOD": {Status: "success", CreatedAt: now, UpdatedAt: now},
		"BAD":  {Status: "failed", Attempts: 1, LastError: "unavailable", CreatedAt: now, UpdatedAt: now},
	}}); err != nil {
		t.Fatal(err)
	}

	oldOutput := outputFmt
	t.Cleanup(func() { outputFmt = oldOutput })
	for _, format := range []string{"table", "json"} {
		t.Run(format, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			rootCmd.SetOut(&stdout)
			rootCmd.SetErr(&stderr)
			rootCmd.SetArgs([]string{"--output", format, "seed", "status", "--state-file", stateFile})
			if err := rootCmd.Execute(); err != nil {
				t.Fatal(err)
			}
			if stderr.Len() != 0 {
				t.Fatalf("stderr = %q", stderr.String())
			}
			if format == "table" {
				if strings.HasPrefix(strings.TrimSpace(stdout.String()), "{") || !strings.Contains(stdout.String(), "Seed status") || !strings.Contains(stdout.String(), "failed") {
					t.Fatalf("table output = %q", stdout.String())
				}
				return
			}
			var env output.Envelope
			if err := json.Unmarshal(stdout.Bytes(), &env); err != nil {
				t.Fatalf("JSON output is not an envelope: %v", err)
			}
			if env.Meta.Command != "seed-status" || env.Results == nil {
				t.Fatalf("envelope = %#v", env)
			}
		})
	}
}

func executeSeedHistoryForTest(t *testing.T, provider marketdata.Provider, args ...string) (seedSummary, string, error) {
	t.Helper()
	old, oldOutput, oldResolved := seedProviderFactory, outputFmt, outputResolved
	seedProviderFactory = func(bool, int) marketdata.Provider { return provider }
	outputFmt, outputResolved = "json", false
	defer func() { seedProviderFactory, outputFmt, outputResolved = old, oldOutput, oldResolved }()

	command := newSeedHistoryCmd()
	var stdout, stderr bytes.Buffer
	command.SetOut(&stdout)
	command.SetErr(&stderr)
	command.SetArgs(args)
	err := command.Execute()

	summary := decodeSingleSeedSummary(t, stdout.Bytes())
	return summary, stderr.String(), err
}

func TestSeedHistoryInitialUniverseRunWritesAtomicCheckpoint(t *testing.T) {
	stateFile := filepath.Join(t.TempDir(), "nested", "state.json")
	provider := &seedTestProvider{calls: map[string]int{}}
	summary, stderr, err := executeSeedHistoryForTest(t, provider, "--market", "us", "--state-file", stateFile, "--rate", "1")
	if err != nil {
		t.Fatal(err)
	}
	if stderr != "" {
		t.Fatalf("stderr = %q, want quiet command output", stderr)
	}
	state, err := loadSeedCheckpoint(stateFile)
	if err != nil {
		t.Fatal(err)
	}
	tickers, _ := seedTickers([]string{"us"})
	if len(state.Tickers) != len(tickers) || provider.calls["^GSPC"] != 1 {
		t.Fatalf("state/calls mismatch: got %d tickers benchmark=%d", len(state.Tickers), provider.calls["^GSPC"])
	}
	if summary.Succeeded != len(tickers) {
		t.Fatalf("invalid summary: %#v", summary)
	}
	data, _ := os.ReadFile(stateFile)
	var parsed seedCheckpoint
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("checkpoint must always be parseable: %v", err)
	}
}

func TestRequeueUnverifiedSeedSuccesses(t *testing.T) {
	now := time.Now().UTC()
	state := &seedCheckpoint{Tickers: map[string]*seedTickerState{
		"PRESENT": {Status: "success", Attempts: 1, CreatedAt: now, UpdatedAt: now},
		"MISSING": {Status: "success", Attempts: 1, CreatedAt: now, UpdatedAt: now},
	}}
	oldRead := seedCacheRead
	seedCacheRead = func(ticker, period string) ([]marketdata.OHLCV, error) {
		if ticker == "MISSING" {
			return nil, os.ErrNotExist
		}
		return []marketdata.OHLCV{{Date: now.AddDate(-1, 0, 0), Close: 100}, {Date: now, Close: 101}}, nil
	}
	t.Cleanup(func() { seedCacheRead = oldRead })

	requeued := requeueUnverifiedSeedSuccesses(state, []string{"PRESENT", "MISSING"}, "max", now)

	if requeued != 1 {
		t.Fatalf("requeued = %d, want 1", requeued)
	}
	if state.Tickers["PRESENT"].Status != "success" {
		t.Fatalf("present cache status = %q, want success", state.Tickers["PRESENT"].Status)
	}
	missing := state.Tickers["MISSING"]
	if missing.Status != "pending" || missing.Attempts != 0 || missing.LastError == "" || !missing.NextRetry.IsZero() {
		t.Fatalf("missing cache state = %#v, want pending reset for refetch", missing)
	}
}

func TestSeedHistoryResumeSkipsSuccesses(t *testing.T) {
	stateFile := filepath.Join(t.TempDir(), "state.json")
	tickers, _ := seedTickers([]string{"india"})
	now := time.Now().UTC()
	state := &seedCheckpoint{Version: seedStateVersion, Period: "max", Coverage: seedHistoryCoverage, Markets: []string{"india"}, Tickers: map[string]*seedTickerState{}}
	for _, ticker := range tickers {
		state.Tickers[ticker] = &seedTickerState{Status: "success", Attempts: 1, CreatedAt: now, UpdatedAt: now}
	}
	state.Tickers[tickers[0]] = &seedTickerState{Status: "retry", Attempts: 1, NextRetry: now.Add(-time.Second), CreatedAt: now, UpdatedAt: now}
	if err := saveSeedCheckpoint(stateFile, state); err != nil {
		t.Fatal(err)
	}
	oldRead := seedCacheRead
	seedCacheRead = func(string, string) ([]marketdata.OHLCV, error) {
		return []marketdata.OHLCV{{Date: now.AddDate(-1, 0, 0), Close: 100}, {Date: now, Close: 101}}, nil
	}
	t.Cleanup(func() { seedCacheRead = oldRead })
	provider := &seedTestProvider{calls: map[string]int{}}
	if _, _, err := executeSeedHistoryForTest(t, provider, "--market", "india", "--state-file", stateFile); err != nil {
		t.Fatal(err)
	}
	if len(provider.calls) != 1 || provider.calls[tickers[0]] != 1 {
		t.Fatalf("resume refetched successes: %#v", provider.calls)
	}
}

func TestSeedHistoryRetryExhaustionReturnsNonzeroSummaryWithoutUsage(t *testing.T) {
	stateFile := filepath.Join(t.TempDir(), "state.json")
	provider := &seedTestProvider{calls: map[string]int{}, err: errors.New("HTTP 503 temporary network failure")}
	summary, stderr, err := executeSeedHistoryForTest(t, provider, "--market", "us", "--state-file", stateFile, "--max-attempts", "1")
	if err == nil {
		t.Fatal("expected nonzero command error")
	}
	if stderr != "" {
		t.Fatalf("stderr = %q, want no Cobra usage/error contamination", stderr)
	}
	tickers, _ := seedTickers([]string{"us"})
	if summary.Total != len(tickers) || summary.Failed != len(tickers) || summary.Pending != 0 || summary.Succeeded != 0 {
		t.Fatalf("summary = %#v, want all tickers terminal failed", summary)
	}
	state, err := loadSeedCheckpoint(stateFile)
	if err != nil {
		t.Fatal(err)
	}
	for _, ticker := range tickers {
		if state.Tickers[ticker].Status != "failed" {
			t.Fatalf("%s status = %q, want failed", ticker, state.Tickers[ticker].Status)
		}
	}
}

func TestSeedHistoryQuietJSONIncompleteEmitsSingleSummary(t *testing.T) {
	stateFile := filepath.Join(t.TempDir(), "state.json")
	provider := &seedTestProvider{calls: map[string]int{}, err: errors.New("HTTP 503 temporary network failure")}
	old := seedProviderFactory
	seedProviderFactory = func(bool, int) marketdata.Provider { return provider }
	defer func() { seedProviderFactory = old }()

	var quietFlag bool
	var outputFlag string
	root := &cobra.Command{Use: "stockctl", SilenceUsage: true, SilenceErrors: true}
	root.PersistentFlags().BoolVar(&quietFlag, "quiet", false, "quiet")
	root.PersistentFlags().StringVar(&outputFlag, "output", "table", "output")
	root.AddCommand(newSeedCmd())
	var stdout, stderr bytes.Buffer
	root.SetOut(&stdout)
	root.SetErr(&stderr)
	root.SetArgs([]string{"--quiet", "--output", "json", "seed", "history", "--market", "us", "--state-file", stateFile, "--max-attempts", "1"})

	err := root.Execute()
	if err == nil {
		t.Fatal("expected incomplete seed to return nonzero")
	}
	if !quietFlag || outputFlag != "json" {
		t.Fatalf("persistent flags not parsed: quiet=%v output=%q", quietFlag, outputFlag)
	}
	if stderr.String() != "" {
		t.Fatalf("stderr = %q, want no Cobra usage/error contamination", stderr.String())
	}
	summary := decodeSingleSeedSummary(t, stdout.Bytes())
	tickers, _ := seedTickers([]string{"us"})
	if summary.Failed != len(tickers) || summary.Pending != 0 {
		t.Fatalf("summary = %#v, want failed incomplete summary", summary)
	}
}

func TestExecuteRootSeedHistoryQuietJSONIncompleteWritesOneErrorEnvelopeWithSummary(t *testing.T) {
	stateFile := filepath.Join(t.TempDir(), "state.json")
	provider := &seedTestProvider{calls: map[string]int{}, err: errors.New("fixture unavailable")}
	oldFactory, oldOutput, oldQuiet := seedProviderFactory, outputFmt, quiet
	t.Cleanup(func() { seedProviderFactory, outputFmt, quiet = oldFactory, oldOutput, oldQuiet })
	seedProviderFactory = func(bool, int) marketdata.Provider { return provider }
	var stdout, stderr bytes.Buffer
	rootCmd.SetOut(&stdout)
	rootCmd.SetErr(&stderr)
	rootCmd.SetArgs([]string{"--quiet", "--output", "json", "seed", "history", "--market", "us", "--state-file", stateFile, "--max-attempts", "1"})
	if err := executeRoot(rootCmd, &stdout, &stderr); err == nil {
		t.Fatal("expected incomplete seed error")
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q", stderr.String())
	}
	var env struct {
		Meta    output.Meta `json:"meta"`
		Results struct {
			Summary  seedSummary   `json:"summary"`
			Failures []seedFailure `json:"failures"`
		} `json:"results"`
		Errors []output.ErrorInfo `json:"errors"`
	}
	dec := json.NewDecoder(bytes.NewReader(stdout.Bytes()))
	if err := dec.Decode(&env); err != nil {
		t.Fatalf("JSON envelope = %q: %v", stdout.String(), err)
	}
	var extra json.RawMessage
	if err := dec.Decode(&extra); !errors.Is(err, io.EOF) {
		t.Fatalf("expected exactly one JSON envelope, got %q", stdout.String())
	}
	if env.Meta.Command != "seed-history" || env.Results.Summary.Failed == 0 || len(env.Results.Failures) == 0 || len(env.Errors) != 1 {
		t.Fatalf("incomplete seed error envelope = %#v", env)
	}
}

func TestExecuteRootSeedHistoryQuietTableIncompleteWritesTextAndOneDiagnostic(t *testing.T) {
	stateFile := filepath.Join(t.TempDir(), "state.json")
	provider := &seedTestProvider{calls: map[string]int{}, err: errors.New("fixture unavailable")}
	oldFactory, oldOutput, oldQuiet, oldResolved := seedProviderFactory, outputFmt, quiet, outputResolved
	t.Cleanup(func() {
		seedProviderFactory, outputFmt, quiet, outputResolved = oldFactory, oldOutput, oldQuiet, oldResolved
	})
	seedProviderFactory = func(bool, int) marketdata.Provider { return provider }
	var stdout, stderr bytes.Buffer
	rootCmd.SetOut(&stdout)
	rootCmd.SetErr(&stderr)
	rootCmd.SetArgs([]string{"--quiet", "--output", "table", "seed", "history", "--market", "us", "--state-file", stateFile, "--max-attempts", "1"})
	if err := executeRoot(rootCmd, &stdout, &stderr); err == nil {
		t.Fatal("expected incomplete seed error")
	}
	if strings.HasPrefix(strings.TrimSpace(stdout.String()), "{") || !strings.Contains(stdout.String(), "Seed history summary") || !strings.Contains(stdout.String(), "Failed: 504") || !strings.Contains(stdout.String(), "MMM: failed") {
		t.Fatalf("table summary = %q", stdout.String())
	}
	if stderr.String() != "Error: seed incomplete: 504 failed, 0 pending\n" {
		t.Fatalf("stderr = %q", stderr.String())
	}
}

func TestSeedHistoryDeadlineInterruptionLeavesPendingAndResumes(t *testing.T) {
	stateFile := filepath.Join(t.TempDir(), "state.json")
	tickers, _ := seedTickers([]string{"us"})

	firstProvider := &seedTestProvider{calls: map[string]int{}, blockOnCall: 2}
	summary, stderr, err := executeSeedHistoryForTest(t, firstProvider, "--market", "us", "--state-file", stateFile, "--deadline", "250ms")
	if err == nil {
		t.Fatal("expected nonzero command error")
	}
	if stderr != "" {
		t.Fatalf("stderr = %q, want no Cobra usage/error contamination", stderr)
	}
	if summary.Succeeded != 1 || summary.Failed != 0 || summary.Pending != len(tickers)-1 {
		t.Fatalf("interrupted summary = %#v, want one success and remaining pending", summary)
	}
	if len(firstProvider.order) != 2 {
		t.Fatalf("deadline run called %d tickers, want immediate stop after current ticker", len(firstProvider.order))
	}
	state, err := loadSeedCheckpoint(stateFile)
	if err != nil {
		t.Fatal(err)
	}
	if state.Tickers[tickers[0]].Status != "success" {
		t.Fatalf("prior success status = %q", state.Tickers[tickers[0]].Status)
	}
	if state.Tickers[tickers[1]].Status != "pending" {
		t.Fatalf("interrupted ticker status = %q, want pending", state.Tickers[tickers[1]].Status)
	}
	for _, ticker := range tickers[2:] {
		if state.Tickers[ticker].Status != "pending" || state.Tickers[ticker].Attempts != 0 {
			t.Fatalf("unstarted ticker %s = %#v, want untouched pending", ticker, state.Tickers[ticker])
		}
	}

	resumeProvider := &seedTestProvider{calls: map[string]int{}}
	oldRead := seedCacheRead
	seedCacheRead = func(ticker, period string) ([]marketdata.OHLCV, error) {
		if ticker == tickers[0] {
			now := time.Now().UTC()
			return []marketdata.OHLCV{{Date: now.AddDate(-1, 0, 0), Close: 100}, {Date: now, Close: 101}}, nil
		}
		return nil, os.ErrNotExist
	}
	t.Cleanup(func() { seedCacheRead = oldRead })
	resumeSummary, _, err := executeSeedHistoryForTest(t, resumeProvider, "--market", "us", "--state-file", stateFile)
	if err != nil {
		t.Fatal(err)
	}
	if resumeProvider.calls[tickers[0]] != 0 {
		t.Fatalf("resume refetched prior success %s", tickers[0])
	}
	if resumeSummary.Succeeded != len(tickers) || resumeSummary.Pending != 0 || resumeSummary.Failed != 0 {
		t.Fatalf("resume summary = %#v, want complete success", resumeSummary)
	}
	for _, ticker := range tickers[1:] {
		if resumeProvider.calls[ticker] != 1 {
			t.Fatalf("resume calls[%s] = %d, want unfinished ticker eligible", ticker, resumeProvider.calls[ticker])
		}
	}
}

func TestSeedHistoryFutureRetryStaysPendingWithoutUpstreamCall(t *testing.T) {
	stateFile := filepath.Join(t.TempDir(), "state.json")
	tickers, _ := seedTickers([]string{"india"})
	now := time.Now().UTC()
	state := &seedCheckpoint{Version: seedStateVersion, Period: "max", Coverage: seedHistoryCoverage, Markets: []string{"india"}, Tickers: map[string]*seedTickerState{}}
	for _, ticker := range tickers {
		state.Tickers[ticker] = &seedTickerState{Status: "success", Attempts: 1, CreatedAt: now, UpdatedAt: now}
	}
	state.Tickers[tickers[0]] = &seedTickerState{Status: "retry", Attempts: 1, NextRetry: now.Add(time.Hour), CreatedAt: now, UpdatedAt: now}
	if err := saveSeedCheckpoint(stateFile, state); err != nil {
		t.Fatal(err)
	}
	oldRead := seedCacheRead
	seedCacheRead = func(string, string) ([]marketdata.OHLCV, error) {
		return []marketdata.OHLCV{{Date: now.AddDate(-1, 0, 0), Close: 100}, {Date: now, Close: 101}}, nil
	}
	t.Cleanup(func() { seedCacheRead = oldRead })
	provider := &seedTestProvider{calls: map[string]int{}}
	summary, stderr, err := executeSeedHistoryForTest(t, provider, "--market", "india", "--state-file", stateFile)
	if err == nil {
		t.Fatal("expected pending future retry to return nonzero")
	}
	if stderr != "" {
		t.Fatalf("stderr = %q, want no Cobra usage/error contamination", stderr)
	}
	if len(provider.calls) != 0 {
		t.Fatalf("future retry made upstream calls: %#v", provider.calls)
	}
	if summary.Pending != 1 || summary.Failed != 0 || summary.Succeeded != len(tickers)-1 {
		t.Fatalf("summary = %#v, want durable retry pending", summary)
	}
}

func TestSeedHistoryTransientClassificationAndContextErrors(t *testing.T) {
	if isTransientSeedError(context.Canceled) || isTransientSeedError(context.DeadlineExceeded) || !isTransientSeedError(errors.New("HTTP 503 temporary network failure")) {
		t.Fatal("transient classification incorrect")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Nanosecond)
	defer cancel()
	<-ctx.Done()
	provider := &seedTestProvider{calls: map[string]int{}}
	if err := seedGetHistory(ctx, provider, "X", "5y", &seedSummary{}); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("context error = %v", err)
	}
}
