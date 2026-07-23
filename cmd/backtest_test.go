package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/rohitsharma04/stockctl/internal/marketdata"
	"github.com/rohitsharma04/stockctl/internal/output"
)

type failingBacktestProvider struct{}

func (failingBacktestProvider) GetHistory(context.Context, string, string, string) ([]marketdata.OHLCV, error) {
	return nil, errors.New("ticker fixture unavailable")
}

type benchmarkFailingBacktestProvider struct{}

func (benchmarkFailingBacktestProvider) GetHistory(_ context.Context, symbol, _, _ string) ([]marketdata.OHLCV, error) {
	if symbol == marketdata.Markets["us"].Benchmark {
		return nil, errors.New("benchmark fixture unavailable")
	}
	return []marketdata.OHLCV{{Date: time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC), Close: 100}}, nil
}

func (benchmarkFailingBacktestProvider) GetQuote(context.Context, string) (*marketdata.Quote, error) {
	return nil, errors.New("fixture unavailable")
}

func TestStrategyBacktestBenchmarkFailureIsIncludedInFetchErrors(t *testing.T) {
	oldFactory, oldOutput, oldQuiet, oldStrategy := backtestProviderFactory, outputFmt, quiet, btStrategy
	t.Cleanup(func() {
		backtestProviderFactory, outputFmt, quiet, btStrategy = oldFactory, oldOutput, oldQuiet, oldStrategy
	})
	backtestProviderFactory = func(bool) marketdata.Provider { return benchmarkFailingBacktestProvider{} }
	var stdout, stderr bytes.Buffer
	rootCmd.SetOut(&stdout)
	rootCmd.SetErr(&stderr)
	rootCmd.SetArgs([]string{"--quiet", "--output", "json", "backtest", "--strategy", "breakout-caution"})
	if err := executeRoot(rootCmd, &stdout, &stderr); err == nil {
		t.Fatal("benchmark failure unexpectedly succeeded")
	}
	var env struct {
		Results struct {
			FetchErrors []output.ErrorInfo `json:"fetch_errors"`
		} `json:"results"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &env); err != nil {
		t.Fatal(err)
	}
	for _, fetchErr := range env.Results.FetchErrors {
		if fetchErr.Ticker == marketdata.Markets["us"].Benchmark && strings.Contains(fetchErr.Error, "benchmark fixture unavailable") {
			return
		}
	}
	t.Fatalf("benchmark failure missing from fetch_errors: %#v", env.Results.FetchErrors)
}

func (failingBacktestProvider) GetQuote(context.Context, string) (*marketdata.Quote, error) {
	return nil, errors.New("fixture unavailable")
}

func TestStrategyBacktestQualityFailureWritesSnapshotDiagnosticsEnvelope(t *testing.T) {
	oldFactory, oldOutput, oldQuiet, oldStrategy := backtestProviderFactory, outputFmt, quiet, btStrategy
	t.Cleanup(func() {
		backtestProviderFactory, outputFmt, quiet, btStrategy = oldFactory, oldOutput, oldQuiet, oldStrategy
	})
	backtestProviderFactory = func(bool) marketdata.Provider { return failingBacktestProvider{} }
	var stdout, stderr bytes.Buffer
	rootCmd.SetOut(&stdout)
	rootCmd.SetErr(&stderr)
	rootCmd.SetArgs([]string{"--quiet", "--output", "json", "backtest", "--strategy", "breakout-caution"})
	if err := executeRoot(rootCmd, &stdout, &stderr); err == nil {
		t.Fatal("snapshot-quality failure unexpectedly succeeded")
	}
	if stderr.Len() != 0 {
		t.Fatalf("quiet JSON failure wrote stderr: %q", stderr.String())
	}
	var env struct {
		Results struct {
			DataQuality       output.DataQualitySummary `json:"data_quality"`
			FetchErrors       []output.ErrorInfo        `json:"fetch_errors"`
			EntriesConsidered int                       `json:"entries_considered"`
			EntriesUsed       int                       `json:"entries_used"`
		} `json:"results"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &env); err != nil {
		t.Fatalf("backtest output is not an envelope: %q: %v", stdout.String(), err)
	}
	if env.Results.DataQuality.BenchmarkAvailable || len(env.Results.FetchErrors) == 0 || env.Results.EntriesConsidered != 0 || env.Results.EntriesUsed != 0 {
		t.Fatalf("snapshot-quality diagnostics = %#v", env.Results)
	}
}

type benchmarkOnlyBacktestProvider struct{}

func (benchmarkOnlyBacktestProvider) GetHistory(_ context.Context, symbol, _, _ string) ([]marketdata.OHLCV, error) {
	if symbol == marketdata.Markets["us"].Benchmark {
		return []marketdata.OHLCV{{Date: time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC), Close: 100}}, nil
	}
	return nil, errors.New("fixture unavailable")
}

func (benchmarkOnlyBacktestProvider) GetQuote(context.Context, string) (*marketdata.Quote, error) {
	return nil, errors.New("fixture unavailable")
}

func TestStrategyBacktestExcessiveFetchFailuresWriteSnapshotDiagnosticsEnvelope(t *testing.T) {
	oldFactory, oldOutput, oldQuiet, oldStrategy := backtestProviderFactory, outputFmt, quiet, btStrategy
	t.Cleanup(func() {
		backtestProviderFactory, outputFmt, quiet, btStrategy = oldFactory, oldOutput, oldQuiet, oldStrategy
	})
	backtestProviderFactory = func(bool) marketdata.Provider { return benchmarkOnlyBacktestProvider{} }
	var stdout, stderr bytes.Buffer
	rootCmd.SetOut(&stdout)
	rootCmd.SetErr(&stderr)
	rootCmd.SetArgs([]string{"--quiet", "--output", "json", "backtest", "--strategy", "breakout-caution"})
	err := executeRoot(rootCmd, &stdout, &stderr)
	if err == nil || !strings.Contains(err.Error(), "data quality insufficient") {
		t.Fatalf("error = %v, want excessive-failure diagnostic", err)
	}
	var env struct {
		Results struct {
			DataQuality output.DataQualitySummary `json:"data_quality"`
			FetchErrors []output.ErrorInfo        `json:"fetch_errors"`
		} `json:"results"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &env); err != nil {
		t.Fatalf("backtest output is not an envelope: %q: %v", stdout.String(), err)
	}
	if !env.Results.DataQuality.BenchmarkAvailable || len(env.Results.FetchErrors) == 0 {
		t.Fatalf("excessive-failure diagnostics = %#v", env.Results)
	}
}

func TestParseBacktestRange(t *testing.T) {
	tests := []struct {
		input      string
		defaultMin float64
		defaultMax float64
		wantMin    float64
		wantMax    float64
		wantErr    bool
	}{
		{"", 0.05, 0.50, 0.05, 0.50, false},
		{"0.05:0.50", 0, 0, 0.05, 0.50, false},
		{"0.05", 0, 0, 0, 0, true},
		{"abc:0.50", 0, 0, 0, 0, true},
		{"0.50:0.05", 0, 0, 0, 0, true},
		{"-0.05:0.50", 0, 0, 0, 0, true},
		{"NaN:0.50", 0, 0, 0, 0, true},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			min, max, err := parseBacktestRange("--tp-range", tt.input, tt.defaultMin, tt.defaultMax)
			if (err != nil) != tt.wantErr {
				t.Fatalf("parseBacktestRange(%q) error = %v, wantErr %v", tt.input, err, tt.wantErr)
			}
			if !tt.wantErr && (min != tt.wantMin || max != tt.wantMax) {
				t.Fatalf("parseBacktestRange(%q) = %v:%v, want %v:%v", tt.input, min, max, tt.wantMin, tt.wantMax)
			}
		})
	}
}

func TestStrategyBacktestSnapshotIncludesQualityErrorsAndEntryCounts(t *testing.T) {
	snapshot := scanSnapshot{
		tickerData:       map[string][]marketdata.OHLCV{"GOOD": {{Close: 10}}},
		tickerProvenance: map[string]marketdata.HistoryProvenance{"GOOD": {Source: marketdata.HistorySourceCache}},
		errors:           []output.ErrorInfo{{Ticker: "BAD", Error: errors.New("unavailable").Error()}},
	}
	got := strategyBacktestSnapshot(snapshot, 3, 1)
	if got.EntriesConsidered != 3 || got.EntriesUsed != 1 {
		t.Fatalf("entry counts = %d/%d, want 3/1", got.EntriesConsidered, got.EntriesUsed)
	}
	if got.DataQuality.TickersFailed != 1 || len(got.FetchErrors) != 1 || got.FetchErrors[0].Ticker != "BAD" {
		t.Fatalf("snapshot diagnostics = %#v", got)
	}
}

func TestValidateBacktestParameters(t *testing.T) {
	valid := backtestParameters{tpMin: 0.05, tpMax: 0.5, tpStep: 0.05, slMin: 0.01, slMax: 0.1, slStep: 0.01, minRewardRisk: 3, capital: 100000}
	if err := validateBacktestParameters(valid); err != nil {
		t.Fatalf("valid parameters rejected: %v", err)
	}
	for _, mutate := range []func(*backtestParameters){
		func(p *backtestParameters) { p.tpStep = 0 },
		func(p *backtestParameters) { p.slStep = -0.01 },
		func(p *backtestParameters) { p.tpMin = math.NaN() },
		func(p *backtestParameters) { p.slMax = math.Inf(1) },
		func(p *backtestParameters) { p.capital = 0 },
		func(p *backtestParameters) { p.minRewardRisk = -1 },
	} {
		params := valid
		mutate(&params)
		if err := validateBacktestParameters(params); err == nil {
			t.Fatal("invalid parameters accepted")
		}
	}
}
