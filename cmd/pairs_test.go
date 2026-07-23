package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/rohitsharma04/stockctl/internal/config"
	"github.com/rohitsharma04/stockctl/internal/marketdata"
	"github.com/rohitsharma04/stockctl/internal/output"
	"github.com/rohitsharma04/stockctl/internal/pairs"
	"github.com/spf13/cobra"
)

type pairsFixtureProvider struct {
	history map[string][]marketdata.OHLCV
	errors  map[string]error
}

func (p pairsFixtureProvider) GetHistory(_ context.Context, symbol, _, _ string) ([]marketdata.OHLCV, error) {
	if err := p.errors[symbol]; err != nil {
		return nil, err
	}
	return p.history[symbol], nil
}

func (p pairsFixtureProvider) GetQuote(context.Context, string) (*marketdata.Quote, error) {
	return nil, errors.New("not implemented")
}

func (p pairsFixtureProvider) GetHistoryWithProvenance(ctx context.Context, req marketdata.HistoryRequest) (*marketdata.HistoryResult, error) {
	data, err := p.GetHistory(ctx, req.Symbol, req.Period, req.Interval)
	if err != nil {
		return nil, err
	}
	return &marketdata.HistoryResult{Data: data, Provenance: marketdata.HistoryProvenance{Source: marketdata.HistorySourceCache}}, nil
}

func TestPairsCommandNoCorrelationsWritesEnvelope(t *testing.T) {
	day := func(n int) time.Time { return time.Date(2025, 1, n, 0, 0, 0, 0, time.UTC) }
	provider := pairsFixtureProvider{history: map[string][]marketdata.OHLCV{
		"AAA": {{Date: day(1), Close: 100}, {Date: day(2), Close: 110}, {Date: day(3), Close: 132}, {Date: day(4), Close: 145.2}, {Date: day(5), Close: 174.24}},
		"BBB": {{Date: day(1), Close: 100}, {Date: day(2), Close: 90}, {Date: day(3), Close: 72}, {Date: day(4), Close: 64.8}, {Date: day(5), Close: 51.84}},
	}}
	stdout := runPairsFixture(t, provider, "AAA,BBB", 0.99)
	var env struct {
		Meta    output.Meta `json:"meta"`
		Results struct {
			Pairs   []json.RawMessage `json:"pairs"`
			Symbols []struct {
				Symbol     string                       `json:"symbol"`
				DataAsOf   string                       `json:"data_as_of"`
				Provenance marketdata.HistoryProvenance `json:"provenance"`
			} `json:"symbols"`
		} `json:"results"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &env); err != nil {
		t.Fatalf("pairs output is not an envelope: %q: %v", stdout.String(), err)
	}
	if env.Meta.Command != "pairs" || len(env.Results.Pairs) != 0 || len(env.Results.Symbols) != 2 || env.Results.Symbols[0].DataAsOf != "2025-01-05" || env.Results.Symbols[0].Provenance.Source != marketdata.HistorySourceCache {
		t.Fatalf("no-correlation envelope = %#v", env)
	}
}

func TestPairsCommandPartialFetchErrorKeepsResultDiagnosticsAndProvenance(t *testing.T) {
	day := func(n int) time.Time { return time.Date(2025, 1, n, 0, 0, 0, 0, time.UTC) }
	provider := pairsFixtureProvider{history: map[string][]marketdata.OHLCV{
		"AAA": {{Date: day(1), Close: 100}, {Date: day(2), Close: 110}, {Date: day(3), Close: 132}, {Date: day(4), Close: 145.2}, {Date: day(5), Close: 174.24}},
		"BBB": {{Date: day(1), Close: 50}, {Date: day(2), Close: 55}, {Date: day(3), Close: 66}, {Date: day(4), Close: 72.6}, {Date: day(5), Close: 87.12}},
	}, errors: map[string]error{"BAD": errors.New("timeout")}}
	stdout := runPairsFixture(t, provider, "AAA,BBB,BAD", 0.9)
	var env struct {
		Results struct {
			Pairs []pairResult `json:"pairs"`
		} `json:"results"`
		Errors []output.ErrorInfo `json:"errors"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &env); err != nil {
		t.Fatalf("pairs output is not an envelope: %q: %v", stdout.String(), err)
	}
	if len(env.Errors) != 1 || env.Errors[0].Ticker != "BAD" || len(env.Results.Pairs) != 1 {
		t.Fatalf("partial-fetch envelope = %#v", env)
	}
	result := env.Results.Pairs[0]
	if result.DataAsOf != "2025-01-05" || result.Provenance["AAA"].Source != marketdata.HistorySourceCache {
		t.Fatalf("result diagnostics/provenance = %#v", result)
	}
}

func TestPairsCommandInsufficientValidStocksKeepsFetchDiagnosticsEnvelope(t *testing.T) {
	day := func(n int) time.Time { return time.Date(2025, 1, n, 0, 0, 0, 0, time.UTC) }
	provider := pairsFixtureProvider{
		history: map[string][]marketdata.OHLCV{"AAA": {{Date: day(1), Close: 100}, {Date: day(2), Close: 110}}},
		errors:  map[string]error{"BAD": errors.New("timeout")},
	}
	oldFactory, oldOutput, oldQuiet := pairsProviderFactory, outputFmt, quiet
	t.Cleanup(func() { pairsProviderFactory, outputFmt, quiet = oldFactory, oldOutput, oldQuiet })
	pairsProviderFactory = func(bool) marketdata.Provider { return provider }
	var stdout, stderr bytes.Buffer
	rootCmd.SetOut(&stdout)
	rootCmd.SetErr(&stderr)
	rootCmd.SetArgs([]string{"--quiet", "--output", "json", "pairs", "--stocks", "AAA,BAD"})
	if err := executeRoot(rootCmd, &stdout, &stderr); err == nil {
		t.Fatal("insufficient-valid-stocks failure unexpectedly succeeded")
	}
	var env struct {
		Results struct {
			Pairs   []pairResult `json:"pairs"`
			Symbols []struct {
				Symbol   string `json:"symbol"`
				DataAsOf string `json:"data_as_of"`
			} `json:"symbols"`
		} `json:"results"`
		Errors []output.ErrorInfo `json:"errors"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &env); err != nil {
		t.Fatalf("pairs output is not an envelope: %q: %v", stdout.String(), err)
	}
	if env.Results.Pairs == nil || len(env.Results.Symbols) != 1 || env.Results.Symbols[0].Symbol != "AAA" || env.Results.Symbols[0].DataAsOf != "2025-01-02" || len(env.Errors) < 2 || env.Errors[0].Ticker != "BAD" {
		t.Fatalf("insufficient-valid-stocks envelope = %#v", env)
	}
}

func runPairsFixture(t *testing.T, provider marketdata.Provider, stocks string, threshold float64) *bytes.Buffer {
	t.Helper()
	oldFactory, oldConfig, oldMarket, oldCtx, oldOutput, oldStocks, oldThreshold, oldQuiet, oldResolved := pairsProviderFactory, appConfig, activeMarket, rootCtx, outputFmt, pairsStocks, pairsThreshold, quiet, outputResolved
	t.Cleanup(func() {
		pairsProviderFactory, appConfig, activeMarket, rootCtx, outputFmt, pairsStocks, pairsThreshold, quiet, outputResolved = oldFactory, oldConfig, oldMarket, oldCtx, oldOutput, oldStocks, oldThreshold, oldQuiet, oldResolved
	})
	pairsProviderFactory = func(bool) marketdata.Provider { return provider }
	appConfig = &config.Config{General: config.GeneralConfig{Output: "json"}, Pairs: config.PairsConfig{Stocks: []string{"ignored"}, Window: 2, Capital: 1000, ZThreshold: 2, ZExitLow: -0.5, ZExitHigh: 0.5}}
	activeMarket = marketdata.Markets["us"]
	rootCtx = context.Background()
	outputFmt, pairsStocks, pairsThreshold, quiet, outputResolved = "json", stocks, threshold, true, false
	var stdout bytes.Buffer
	command := &cobra.Command{}
	command.SetOut(&stdout)
	if err := runPairs(command, nil); err != nil {
		t.Fatal(err)
	}
	return &stdout
}

func TestPairsEnvelopePreservesPartialFetchErrorsAndProvenance(t *testing.T) {
	result := pairResult{SimulationResult: pairs.SimulationResult{Stock1: "AAA", Stock2: "BBB"}, AlignedBars: 4, DataAsOf: "2025-01-04", Provenance: map[string]marketdata.HistoryProvenance{"AAA": {Source: marketdata.HistorySourceCache}}}
	env := pairsEnvelope([]pairResult{result}, nil, []output.ErrorInfo{{Ticker: "FAILED", Error: "timeout"}}, time.Millisecond)
	if len(env.Errors) != 1 || env.Errors[0].Ticker != "FAILED" {
		t.Fatalf("partial fetch errors = %#v", env.Errors)
	}
	results, ok := env.Results.(pairsResultSet)
	if !ok || results.Pairs[0].DataAsOf != "2025-01-04" || results.Pairs[0].Provenance["AAA"].Source != marketdata.HistorySourceCache {
		t.Fatalf("pairs provenance results = %#v", env.Results)
	}
}

func TestAlignedPairReturnsJoinDatesBeforeComputingCorrelation(t *testing.T) {
	day := func(n int) time.Time { return time.Date(2025, 1, n, 0, 0, 0, 0, time.UTC) }
	left := []marketdata.OHLCV{{Date: day(1), Close: 100}, {Date: day(2), Close: 110}, {Date: day(3), Close: 99}}
	right := []marketdata.OHLCV{{Date: day(1), Close: 200}, {Date: day(3), Close: 180}, {Date: day(4), Close: 198}}

	leftReturns, rightReturns, asOf := alignedPairReturns(left, right)
	if len(leftReturns) != 1 || len(rightReturns) != 1 {
		t.Fatalf("aligned returns lengths = %d/%d, want 1/1", len(leftReturns), len(rightReturns))
	}
	if asOf.Format("2006-01-02") != "2025-01-03" {
		t.Fatalf("data as-of = %s, want joined last date", asOf.Format("2006-01-02"))
	}
}

func TestPairsExplicitlyRejectsCSVOutput(t *testing.T) {
	oldOutput, oldResolved := outputFmt, outputResolved
	outputFmt, outputResolved = "csv", false
	t.Cleanup(func() { outputFmt, outputResolved = oldOutput, oldResolved })
	err := runPairs(&cobra.Command{}, nil)
	if err == nil || err.Error() != "pairs does not support --output csv; use --export-signals to write a signals CSV" {
		t.Fatalf("CSV error = %v", err)
	}
}

func TestAlignedPairReturnsRejectsInsufficientDateOverlap(t *testing.T) {
	day := func(n int) time.Time { return time.Date(2025, 1, n, 0, 0, 0, 0, time.UTC) }
	leftReturns, rightReturns, _ := alignedPairReturns([]marketdata.OHLCV{{Date: day(1), Close: 10}}, []marketdata.OHLCV{{Date: day(2), Close: 20}})
	if len(leftReturns) != 0 || len(rightReturns) != 0 {
		t.Fatalf("non-overlapping histories returned %d/%d returns", len(leftReturns), len(rightReturns))
	}
}
