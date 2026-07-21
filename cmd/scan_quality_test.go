package cmd

import (
	"context"
	"errors"
	"math"
	"sync"
	"testing"
	"time"

	"github.com/rohitsharma04/stockctl/internal/marketdata"
)

func TestEffectiveMinPrice(t *testing.T) {
	tests := []struct {
		name                         string
		override, configured, market float64
		want                         float64
	}{
		{"market default", 0, 0, 5, 5},
		{"configured value", 0, 8, 5, 8},
		{"positive override", 12.5, 8, 5, 12.5},
		{"nonpositive override uses configured", -1, 8, 5, 8},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := effectiveMinPrice(tt.override, tt.configured, tt.market); got != tt.want {
				t.Fatalf("effectiveMinPrice(%v, %v, %v) = %v, want %v", tt.override, tt.configured, tt.market, got, tt.want)
			}
		})
	}
}

func TestMeetsMinPrice(t *testing.T) {
	tests := []struct {
		name  string
		data  []marketdata.OHLCV
		floor float64
		want  bool
	}{
		{"latest valid close meets floor", []marketdata.OHLCV{{Close: 8}, {Close: 10}}, 10, true},
		{"below floor excluded", []marketdata.OHLCV{{Close: 8}, {Close: 9.99}}, 10, false},
		{"missing data excluded", nil, 10, false},
		{"nonpositive data excluded", []marketdata.OHLCV{{Close: 0}, {Close: -2}}, 10, false},
		{"nan data excluded", []marketdata.OHLCV{{Close: math.NaN()}}, 10, false},
		{"trailing invalid close uses latest valid close", []marketdata.OHLCV{{Close: 12}, {Close: 0}}, 10, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := meetsMinPrice(tt.data, tt.floor); got != tt.want {
				t.Fatalf("meetsMinPrice() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestFetchScanSnapshotRecordsPerTickerProvenanceQuality(t *testing.T) {
	provider := &provenanceScanProvider{
		data: map[string][]marketdata.OHLCV{
			"FRESH": {{Close: 10}}, "STALE": {{Close: 10}}, "CACHE": {{Close: 10}},
		},
		errs: map[string]error{"FAIL": errors.New("unavailable")},
		provenance: map[string]marketdata.HistoryProvenance{
			"FRESH": {Source: marketdata.HistorySourceUpstream},
			"STALE": {Source: marketdata.HistorySourceCache, Stale: true, UpstreamError: "timeout"},
			"CACHE": {Source: marketdata.HistorySourceCache},
		},
		counts: make(map[string]int),
	}

	snapshot, _ := fetchScanSnapshot(context.Background(), []string{"FRESH", "STALE", "CACHE", "FAIL"}, provider, "", 2, time.Time{})
	quality := snapshot.dataQualitySummary()
	if quality.TickersComplete != 2 || quality.TickersPartial != 1 || quality.TickersFailed != 1 {
		t.Fatalf("quality = %#v, want complete=2 partial=1 failed=1", quality)
	}
	if quality.TickersComplete+quality.TickersPartial+quality.TickersFailed != 4 {
		t.Fatalf("quality counts do not total ticker universe: %#v", quality)
	}
	if quality.TickersStaleFallback != 1 {
		t.Fatalf("stale fallback count = %d, want 1", quality.TickersStaleFallback)
	}
	if quality.SourceCounts[string(marketdata.HistorySourceUpstream)] != 1 || quality.SourceCounts[string(marketdata.HistorySourceCache)] != 2 {
		t.Fatalf("source counts = %#v", quality.SourceCounts)
	}
	for _, ticker := range []string{"FRESH", "STALE", "CACHE", "FAIL"} {
		if got := provider.Count(ticker); got != 1 {
			t.Fatalf("%s fetches = %d, want 1", ticker, got)
		}
	}
}

type provenanceScanProvider struct {
	mu         sync.Mutex
	data       map[string][]marketdata.OHLCV
	errs       map[string]error
	provenance map[string]marketdata.HistoryProvenance
	counts     map[string]int
}

func (p *provenanceScanProvider) Count(symbol string) int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.counts[symbol]
}
func (p *provenanceScanProvider) GetQuote(_ context.Context, symbol string) (*marketdata.Quote, error) {
	return &marketdata.Quote{Symbol: symbol}, nil
}
func (p *provenanceScanProvider) GetHistory(ctx context.Context, symbol, period, interval string) ([]marketdata.OHLCV, error) {
	r, err := p.GetHistoryWithProvenance(ctx, marketdata.HistoryRequest{Symbol: symbol, Period: period, Interval: interval})
	if err != nil {
		return nil, err
	}
	return r.Data, nil
}
func (p *provenanceScanProvider) GetHistoryWithProvenance(_ context.Context, req marketdata.HistoryRequest) (*marketdata.HistoryResult, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.counts[req.Symbol]++
	if err := p.errs[req.Symbol]; err != nil {
		return nil, err
	}
	return &marketdata.HistoryResult{Data: append([]marketdata.OHLCV(nil), p.data[req.Symbol]...), Provenance: p.provenance[req.Symbol]}, nil
}
