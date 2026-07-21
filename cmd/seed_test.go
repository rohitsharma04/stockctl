package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/rohitsharma04/stockctl/internal/marketdata"
)

type seedTestProvider struct {
	mu    sync.Mutex
	calls map[string]int
	err   error
}

func (p *seedTestProvider) GetHistory(ctx context.Context, symbol, period, interval string) ([]marketdata.OHLCV, error) {
	p.mu.Lock()
	p.calls[symbol]++
	p.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return nil, p.err
}
func (p *seedTestProvider) GetQuote(context.Context, string) (*marketdata.Quote, error) {
	return nil, nil
}

func TestSeedHistoryInitialUniverseRunWritesAtomicCheckpoint(t *testing.T) {
	stateFile := filepath.Join(t.TempDir(), "nested", "state.json")
	provider := &seedTestProvider{calls: map[string]int{}}
	old := seedProviderFactory
	seedProviderFactory = func(bool, int) marketdata.Provider { return provider }
	defer func() { seedProviderFactory = old }()
	command := newSeedHistoryCmd()
	var stdout bytes.Buffer
	command.SetOut(&stdout)
	command.SetErr(&stdout)
	command.SetArgs([]string{"--market", "us", "--state-file", stateFile, "--rate", "1"})
	if err := command.Execute(); err != nil {
		t.Fatal(err)
	}
	state, err := loadSeedCheckpoint(stateFile)
	if err != nil {
		t.Fatal(err)
	}
	tickers, _ := seedTickers([]string{"us"})
	if len(state.Tickers) != len(tickers) || provider.calls["^GSPC"] != 1 {
		t.Fatalf("state/calls mismatch: got %d tickers benchmark=%d", len(state.Tickers), provider.calls["^GSPC"])
	}
	var summary seedSummary
	if err := json.Unmarshal(stdout.Bytes(), &summary); err != nil || summary.Succeeded != len(tickers) {
		t.Fatalf("invalid summary: %#v, %v", summary, err)
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
	state := &seedCheckpoint{Version: seedStateVersion, Markets: []string{"india"}, Tickers: map[string]*seedTickerState{}}
	for _, ticker := range tickers {
		state.Tickers[ticker] = &seedTickerState{Status: "success", Attempts: 1, CreatedAt: now, UpdatedAt: now}
	}
	state.Tickers[tickers[0]] = &seedTickerState{Status: "retry", Attempts: 1, NextRetry: now.Add(-time.Second), CreatedAt: now, UpdatedAt: now}
	if err := saveSeedCheckpoint(stateFile, state); err != nil {
		t.Fatal(err)
	}
	provider := &seedTestProvider{calls: map[string]int{}}
	old := seedProviderFactory
	seedProviderFactory = func(bool, int) marketdata.Provider { return provider }
	defer func() { seedProviderFactory = old }()
	command := newSeedHistoryCmd()
	command.SetOut(&bytes.Buffer{})
	command.SetArgs([]string{"--market", "india", "--state-file", stateFile})
	if err := command.Execute(); err != nil {
		t.Fatal(err)
	}
	if len(provider.calls) != 1 || provider.calls[tickers[0]] != 1 {
		t.Fatalf("resume refetched successes: %#v", provider.calls)
	}
}

func TestSeedHistoryRetryExhaustionAndDeadline(t *testing.T) {
	if isTransientSeedError(context.Canceled) || isTransientSeedError(context.DeadlineExceeded) || !isTransientSeedError(errors.New("HTTP 503 temporary network failure")) {
		t.Fatal("transient classification incorrect")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Nanosecond)
	defer cancel()
	<-ctx.Done()
	provider := &seedTestProvider{calls: map[string]int{}}
	if err := seedGetHistory(ctx, provider, "X", &seedSummary{}); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("context error = %v", err)
	}
}
