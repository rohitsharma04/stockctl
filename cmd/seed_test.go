package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/rohitsharma04/stockctl/internal/marketdata"
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
	return nil, p.err
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
	var summary seedSummary
	dec := json.NewDecoder(bytes.NewReader(stdout))
	if decodeErr := dec.Decode(&summary); decodeErr != nil {
		t.Fatalf("stdout must contain a parseable JSON summary, got %q: %v", string(stdout), decodeErr)
	}
	var extra json.RawMessage
	if decodeErr := dec.Decode(&extra); !errors.Is(decodeErr, io.EOF) {
		t.Fatalf("stdout must contain exactly one JSON value, got %q", string(stdout))
	}
	return summary
}

func executeSeedHistoryForTest(t *testing.T, provider marketdata.Provider, args ...string) (seedSummary, string, error) {
	t.Helper()
	old := seedProviderFactory
	seedProviderFactory = func(bool, int) marketdata.Provider { return provider }
	defer func() { seedProviderFactory = old }()

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
