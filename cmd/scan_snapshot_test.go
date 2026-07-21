package cmd

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/rohitsharma04/stockctl/internal/marketdata"
	"github.com/rohitsharma04/stockctl/internal/screener"
)

func TestFetchAndEvaluateScanSnapshotFetchesOnceAndSharesImmutableData(t *testing.T) {
	asOfDate := time.Date(2024, 1, 10, 0, 0, 0, 0, time.UTC)
	provider := newCountingScanProvider(map[string][]marketdata.OHLCV{
		"BENCH": barsWithExtraFuture(210, asOfDate),
		"AAA":   barsWithExtraFuture(210, asOfDate),
		"BBB":   barsWithExtraFuture(210, asOfDate),
	}, map[string]error{
		"ERR": errors.New("upstream unavailable"),
	})

	snapshot, warnings := fetchScanSnapshot(context.Background(), []string{"AAA", "BBB", "ERR"}, provider, "BENCH", 3, asOfDate)
	if len(warnings) != 0 {
		t.Fatalf("expected no benchmark warnings, got %#v", warnings)
	}
	if got := provider.Count("BENCH"); got != 1 {
		t.Fatalf("benchmark requests = %d, want 1", got)
	}
	for _, ticker := range []string{"AAA", "BBB", "ERR"} {
		if got := provider.Count(ticker); got != 1 {
			t.Fatalf("%s requests = %d, want 1", ticker, got)
		}
	}
	if len(snapshot.errors) != 1 || snapshot.errors[0].Ticker != "ERR" {
		t.Fatalf("expected exactly one fetch error for ERR, got %#v", snapshot.errors)
	}
	if len(snapshot.breadthData) != 2 {
		t.Fatalf("breadth rows = %d, want one per successful ticker", len(snapshot.breadthData))
	}
	if got := len(snapshot.tickerData["AAA"]); got != 210 {
		t.Fatalf("AAA bars after --date truncation = %d, want 210", got)
	}

	screeners := []screener.Screener{
		&fakeScanScreener{name: "mutating", mutateFirstClose: 999},
		&fakeScanScreener{name: "observer", wantFirstClose: 1},
	}
	oldMinScore := minScore
	t.Cleanup(func() { minScore = oldMinScore })
	minScore = 0

	results, errors, breadth := evaluateScanSnapshot(context.Background(), snapshot, screeners, []string{"AAA", "BBB", "ERR"}, 2)
	if len(errors) != 1 || errors[0].Ticker != "ERR" {
		t.Fatalf("expected exactly one reported ticker error, got %#v", errors)
	}
	if len(breadth) != 2 {
		t.Fatalf("evaluation breadth rows = %d, want one per successful ticker", len(breadth))
	}
	for _, b := range breadth {
		if !b.FullPass || b.Score != 1 {
			t.Fatalf("breadth score for %s = score %.2f fullPass %v, want score 1 fullPass true", b.Ticker, b.Score, b.FullPass)
		}
	}
	if len(results) != 4 {
		t.Fatalf("results = %d, want two successful tickers across two screeners", len(results))
	}
	for _, scr := range screeners {
		fake := scr.(*fakeScanScreener)
		if got := fake.Calls(); got != 2 {
			t.Fatalf("%s calls = %d, want one per successful ticker", fake.name, got)
		}
	}
	for _, ticker := range []string{"AAA", "BBB", "ERR"} {
		if got := provider.Count(ticker); got != 1 {
			t.Fatalf("%s requests after evaluation = %d, want still 1", ticker, got)
		}
	}
	if got := provider.Count("BENCH"); got != 1 {
		t.Fatalf("benchmark requests after evaluation = %d, want still 1", got)
	}
}

type countingScanProvider struct {
	mu     sync.Mutex
	data   map[string][]marketdata.OHLCV
	errs   map[string]error
	counts map[string]int
}

func newCountingScanProvider(data map[string][]marketdata.OHLCV, errs map[string]error) *countingScanProvider {
	return &countingScanProvider{data: data, errs: errs, counts: make(map[string]int)}
}

func (p *countingScanProvider) GetHistory(_ context.Context, symbol, _, _ string) ([]marketdata.OHLCV, error) {
	p.mu.Lock()
	p.counts[symbol]++
	err := p.errs[symbol]
	data := append([]marketdata.OHLCV(nil), p.data[symbol]...)
	p.mu.Unlock()
	if err != nil {
		return nil, err
	}
	return data, nil
}

func (p *countingScanProvider) GetQuote(_ context.Context, symbol string) (*marketdata.Quote, error) {
	return &marketdata.Quote{Symbol: symbol}, nil
}

func (p *countingScanProvider) Count(symbol string) int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.counts[symbol]
}

type fakeScanScreener struct {
	name             string
	mutateFirstClose float64
	wantFirstClose   float64
	mu               sync.Mutex
	calls            int
}

func (s *fakeScanScreener) Name() string        { return s.name }
func (s *fakeScanScreener) Description() string { return s.name }

func (s *fakeScanScreener) Screen(_ context.Context, data []marketdata.OHLCV, benchmark []marketdata.OHLCV) (*screener.ScreenResult, error) {
	if len(benchmark) != 210 {
		return nil, errors.New("benchmark was not truncated to as-of date")
	}
	if s.wantFirstClose != 0 && data[0].Close != s.wantFirstClose {
		return nil, errors.New("screener observed mutated snapshot data")
	}
	if s.mutateFirstClose != 0 {
		data[0].Close = s.mutateFirstClose
	}
	s.mu.Lock()
	s.calls++
	s.mu.Unlock()
	return screener.NewScreenResult([]screener.FilterResult{
		screener.MakeFilter("fake", true, 1, 1, screener.ImportanceCritical, ""),
	}), nil
}

func (s *fakeScanScreener) Calls() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.calls
}

func barsWithExtraFuture(count int, asOfDate time.Time) []marketdata.OHLCV {
	start := asOfDate.AddDate(0, 0, -count+1)
	bars := make([]marketdata.OHLCV, 0, count+1)
	for i := 0; i < count; i++ {
		close := float64(i + 1)
		bars = append(bars, marketdata.OHLCV{
			Date:   start.AddDate(0, 0, i),
			Open:   close,
			High:   close + 1,
			Low:    close - 1,
			Close:  close,
			Volume: 1000,
		})
	}
	bars = append(bars, marketdata.OHLCV{
		Date:   asOfDate.AddDate(0, 0, 1),
		Open:   500,
		High:   501,
		Low:    499,
		Close:  500,
		Volume: 1000,
	})
	return bars
}
