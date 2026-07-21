package marketdata

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestDiskCache_DeltaFetchMerge(t *testing.T) {
	// Setup: temp dir, mock provider
	tmpDir := t.TempDir()
	mock := &mockProvider{
		data: []OHLCV{
			{Date: time.Date(2025, 3, 25, 0, 0, 0, 0, time.UTC), Close: 100, High: 102, Low: 98, Volume: 1000},
			{Date: time.Date(2025, 3, 26, 0, 0, 0, 0, time.UTC), Close: 105, High: 107, Low: 103, Volume: 1200},
			{Date: time.Date(2025, 3, 27, 0, 0, 0, 0, time.UTC), Close: 110, High: 112, Low: 108, Volume: 1500},
		},
	}

	dc := &DiskCachedProvider{inner: mock, cacheDir: tmpDir}

	// First call — no cache, full fetch
	data, err := dc.GetHistory(context.Background(), "TEST", "5y", "1d")
	if err != nil {
		t.Fatalf("first call failed: %v", err)
	}
	if len(data) != 3 {
		t.Fatalf("expected 3 bars, got %d", len(data))
	}
	if mock.callCount != 1 {
		t.Errorf("expected 1 API call, got %d", mock.callCount)
	}

	// Second call — cache exists, should do delta fetch
	// Simulate "new" data for delta
	mock.data = []OHLCV{
		{Date: time.Date(2025, 3, 26, 0, 0, 0, 0, time.UTC), Close: 106, High: 108, Low: 104, Volume: 1300}, // overlap — should overwrite
		{Date: time.Date(2025, 3, 27, 0, 0, 0, 0, time.UTC), Close: 111, High: 113, Low: 109, Volume: 1600}, // overlap — should overwrite
		{Date: time.Date(2025, 3, 28, 0, 0, 0, 0, time.UTC), Close: 115, High: 117, Low: 113, Volume: 1800}, // new bar
	}

	data, err = dc.GetHistory(context.Background(), "TEST", "5y", "1d")
	if err != nil {
		t.Fatalf("second call failed: %v", err)
	}
	if mock.callCount != 2 {
		t.Errorf("expected 2 API calls, got %d", mock.callCount)
	}

	// Should have 4 unique bars: Mar 25 (original), Mar 26+27 (overwritten), Mar 28 (new)
	if len(data) != 4 {
		t.Fatalf("expected 4 merged bars, got %d", len(data))
	}

	// Verify Mar 26 was overwritten with fresh data
	if data[1].Close != 106 {
		t.Errorf("Mar 26 close should be 106 (fresh), got %.0f", data[1].Close)
	}

	// Verify Mar 28 was appended
	if data[3].Close != 115 {
		t.Errorf("Mar 28 close should be 115, got %.0f", data[3].Close)
	}
}

func TestDiskCache_StaleWhileRevalidate(t *testing.T) {
	tmpDir := t.TempDir()
	mock := &mockProvider{
		data: []OHLCV{
			{Date: time.Date(2025, 3, 25, 0, 0, 0, 0, time.UTC), Close: 100},
			{Date: time.Date(2025, 3, 26, 0, 0, 0, 0, time.UTC), Close: 105},
		},
	}

	dc := &DiskCachedProvider{inner: mock, cacheDir: tmpDir}

	// Populate cache
	_, err := dc.GetHistory(context.Background(), "STALE", "5y", "1d")
	if err != nil {
		t.Fatalf("initial fetch failed: %v", err)
	}

	// Now make API fail
	mock.shouldFail = true

	// Should return stale cached data without error
	data, err := dc.GetHistory(context.Background(), "STALE", "5y", "1d")
	if err != nil {
		t.Fatalf("stale-while-revalidate should not return error: %v", err)
	}
	if len(data) != 2 {
		t.Errorf("expected 2 stale bars, got %d", len(data))
	}
}

func TestDiskCache_NoCacheAndApiFails(t *testing.T) {
	tmpDir := t.TempDir()
	mock := &mockProvider{shouldFail: true}
	dc := &DiskCachedProvider{inner: mock, cacheDir: tmpDir}

	// No cache and API fails — should return error
	_, err := dc.GetHistory(context.Background(), "FAIL", "5y", "1d")
	if err == nil {
		t.Fatal("expected error when no cache and API fails")
	}
}

func TestMergeOHLCV(t *testing.T) {
	cached := []OHLCV{
		{Date: time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC), Close: 10},
		{Date: time.Date(2025, 1, 2, 0, 0, 0, 0, time.UTC), Close: 20},
		{Date: time.Date(2025, 1, 3, 0, 0, 0, 0, time.UTC), Close: 30},
	}
	fresh := []OHLCV{
		{Date: time.Date(2025, 1, 2, 0, 0, 0, 0, time.UTC), Close: 25}, // overlap
		{Date: time.Date(2025, 1, 3, 0, 0, 0, 0, time.UTC), Close: 35}, // overlap
		{Date: time.Date(2025, 1, 4, 0, 0, 0, 0, time.UTC), Close: 40}, // new
	}

	merged := mergeOHLCV(cached, fresh)
	if len(merged) != 4 {
		t.Fatalf("expected 4, got %d", len(merged))
	}
	if merged[0].Close != 10 {
		t.Errorf("Jan 1 should stay 10, got %.0f", merged[0].Close)
	}
	if merged[1].Close != 25 {
		t.Errorf("Jan 2 should be overwritten to 25, got %.0f", merged[1].Close)
	}
	if merged[2].Close != 35 {
		t.Errorf("Jan 3 should be overwritten to 35, got %.0f", merged[2].Close)
	}
	if merged[3].Close != 40 {
		t.Errorf("Jan 4 should be 40, got %.0f", merged[3].Close)
	}
}

func TestDeltaPeriod(t *testing.T) {
	cases := []struct {
		daysAgo  int
		expected string
	}{
		{1, "5d"},
		{3, "5d"},
		{10, "5d"},
		{50, "5d"},
		{120, "5d"},
		{300, "5d"},
		{600, "5d"},
		{1000, "5d"},
	}

	for _, tc := range cases {
		lastBar := time.Now().AddDate(0, 0, -tc.daysAgo)
		got := deltaPeriod(lastBar, "5y")
		if got != tc.expected {
			t.Errorf("deltaPeriod(%d days ago) = %s, want %s", tc.daysAgo, got, tc.expected)
		}
	}
}

func TestDiskCacheFilename(t *testing.T) {
	// Verify symbol sanitization
	name := diskCacheFilename("RELIANCE.NS", "5y", "1d")
	if name != "RELIANCE_NS_5y_1d.gob" {
		t.Errorf("unexpected filename: %s", name)
	}

	name = diskCacheFilename("^GSPC", "1y", "1d")
	if name != "_GSPC_1y_1d.gob" {
		t.Errorf("unexpected filename: %s", name)
	}
}

func TestDiskCache_CacheFileCreated(t *testing.T) {
	tmpDir := t.TempDir()
	mock := &mockProvider{
		data: []OHLCV{
			{Date: time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC), Close: 100},
		},
	}

	dc := &DiskCachedProvider{inner: mock, cacheDir: tmpDir}
	dc.GetHistory(context.Background(), "AAPL", "5y", "1d")

	// Verify file exists
	path := filepath.Join(tmpDir, "AAPL_5y_1d.gob")
	if _, err := os.Stat(path); os.IsNotExist(err) {
		t.Error("cache file should exist after fetch")
	}
}

func TestDiskCache_GetHistoryWithProvenanceCacheHitSkipsUpstream(t *testing.T) {
	tmpDir := t.TempDir()
	mock := &mockProvider{
		data: []OHLCV{
			{Date: time.Date(2025, 3, 25, 0, 0, 0, 0, time.UTC), Close: 100},
			{Date: time.Date(2025, 3, 26, 0, 0, 0, 0, time.UTC), Close: 105},
			{Date: time.Date(2025, 3, 27, 0, 0, 0, 0, time.UTC), Close: 110},
		},
	}
	dc := &DiskCachedProvider{inner: mock, cacheDir: tmpDir}

	_, err := dc.GetHistoryWithProvenance(context.Background(), HistoryRequest{
		Symbol: "AAPL", Period: "5y", Interval: "1d",
	})
	if err != nil {
		t.Fatalf("initial fetch failed: %v", err)
	}

	result, err := dc.GetHistoryWithProvenance(context.Background(), HistoryRequest{
		Symbol: "AAPL", Period: "5y", Interval: "1d", AsOf: time.Date(2025, 3, 27, 12, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("cache hit failed: %v", err)
	}
	if mock.callCount != 1 {
		t.Fatalf("cache hit should not call upstream, got %d calls", mock.callCount)
	}
	if result.Provenance.Source != HistorySourceCache {
		t.Fatalf("source = %q, want %q", result.Provenance.Source, HistorySourceCache)
	}
	if result.Provenance.FetchedAt.IsZero() {
		t.Fatal("expected fetched time in provenance")
	}
	if !sameDay(result.Provenance.LastBarDate, time.Date(2025, 3, 27, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("last bar date = %s, want 2025-03-27", result.Provenance.LastBarDate.Format("2006-01-02"))
	}
	if result.Provenance.Stale {
		t.Fatal("cache hit should not be marked stale")
	}
}

func TestDiskCache_GetHistoryWithProvenanceTailFetchUsesFiveDayOverlapAndMerges(t *testing.T) {
	tmpDir := t.TempDir()
	mock := &recordingProvider{
		data: []OHLCV{
			{Date: time.Date(2025, 3, 25, 0, 0, 0, 0, time.UTC), Close: 100},
			{Date: time.Date(2025, 3, 26, 0, 0, 0, 0, time.UTC), Close: 105},
			{Date: time.Date(2025, 3, 27, 0, 0, 0, 0, time.UTC), Close: 110},
		},
	}
	dc := &DiskCachedProvider{inner: mock, cacheDir: tmpDir}

	_, err := dc.GetHistoryWithProvenance(context.Background(), HistoryRequest{
		Symbol: "MSFT", Period: "5y", Interval: "1d",
	})
	if err != nil {
		t.Fatalf("initial fetch failed: %v", err)
	}

	mock.data = []OHLCV{
		{Date: time.Date(2025, 3, 26, 0, 0, 0, 0, time.UTC), Close: 106},
		{Date: time.Date(2025, 3, 27, 0, 0, 0, 0, time.UTC), Close: 111},
		{Date: time.Date(2025, 3, 28, 0, 0, 0, 0, time.UTC), Close: 115},
	}
	result, err := dc.GetHistoryWithProvenance(context.Background(), HistoryRequest{
		Symbol: "MSFT", Period: "5y", Interval: "1d", AsOf: time.Date(2025, 3, 28, 12, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("tail fetch failed: %v", err)
	}
	if len(mock.calls) != 2 {
		t.Fatalf("expected 2 upstream calls, got %d", len(mock.calls))
	}
	if mock.calls[0].period != "5y" {
		t.Fatalf("initial fetch period = %q, want 5y", mock.calls[0].period)
	}
	if mock.calls[1].period != "5d" {
		t.Fatalf("tail fetch period = %q, want 5d", mock.calls[1].period)
	}
	if result.Provenance.Source != HistorySourceCacheAndUpstream {
		t.Fatalf("source = %q, want %q", result.Provenance.Source, HistorySourceCacheAndUpstream)
	}
	if len(result.Data) != 4 {
		t.Fatalf("expected 4 merged bars, got %d", len(result.Data))
	}
	if result.Data[1].Close != 106 || result.Data[2].Close != 111 || result.Data[3].Close != 115 {
		t.Fatalf("unexpected merged closes: %.0f %.0f %.0f", result.Data[1].Close, result.Data[2].Close, result.Data[3].Close)
	}
}

func TestDiskCache_StaleFallbackProvenanceAndNoStderr(t *testing.T) {
	tmpDir := t.TempDir()
	mock := &mockProvider{
		data: []OHLCV{
			{Date: time.Date(2025, 3, 25, 0, 0, 0, 0, time.UTC), Close: 100},
			{Date: time.Date(2025, 3, 26, 0, 0, 0, 0, time.UTC), Close: 105},
		},
	}
	dc := &DiskCachedProvider{inner: mock, cacheDir: tmpDir}

	_, err := dc.GetHistory(context.Background(), "STALE2", "5y", "1d")
	if err != nil {
		t.Fatalf("initial fetch failed: %v", err)
	}

	mock.shouldFail = true
	stderr := captureStderr(t, func() {
		data, err := dc.GetHistory(context.Background(), "STALE2", "5y", "1d")
		if err != nil {
			t.Fatalf("legacy stale fallback should not return error: %v", err)
		}
		if len(data) != 2 {
			t.Fatalf("expected 2 stale bars, got %d", len(data))
		}
	})
	if stderr != "" {
		t.Fatalf("cache provider wrote to stderr: %q", stderr)
	}

	result, err := dc.GetHistoryWithProvenance(context.Background(), HistoryRequest{
		Symbol: "STALE2", Period: "5y", Interval: "1d", AsOf: time.Date(2025, 3, 27, 12, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("provenance stale fallback should not return error: %v", err)
	}
	if !result.Provenance.Stale {
		t.Fatal("expected stale provenance")
	}
	if result.Provenance.UpstreamError == "" {
		t.Fatal("expected upstream error in provenance")
	}
	if result.Provenance.Source != HistorySourceCache {
		t.Fatalf("source = %q, want %q", result.Provenance.Source, HistorySourceCache)
	}
}

func TestDiskCache_WriteCacheAtomicNoTempFiles(t *testing.T) {
	tmpDir := t.TempDir()
	mock := &mockProvider{
		data: []OHLCV{{Date: time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC), Close: 100}},
	}
	dc := &DiskCachedProvider{inner: mock, cacheDir: tmpDir}

	if _, err := dc.GetHistory(context.Background(), "ATOMIC", "5y", "1d"); err != nil {
		t.Fatalf("fetch failed: %v", err)
	}

	matches, err := filepath.Glob(filepath.Join(tmpDir, "*.tmp"))
	if err != nil {
		t.Fatalf("glob failed: %v", err)
	}
	if len(matches) != 0 {
		t.Fatalf("expected no temp files after atomic write, got %v", matches)
	}
}

func TestDiskCache_WriteLockSerializesWriters(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "LOCKED_5y_1d.gob")

	unlockFirst := acquireCacheWriteLock(path)
	lockDir := path + ".lock"
	if _, err := os.Stat(lockDir); err != nil {
		unlockFirst()
		t.Fatalf("expected lock directory to exist: %v", err)
	}

	acquired := make(chan func(), 1)
	go func() {
		acquired <- acquireCacheWriteLock(path)
	}()

	select {
	case unlockSecond := <-acquired:
		unlockSecond()
		unlockFirst()
		t.Fatal("second writer acquired lock before first writer released it")
	case <-time.After(50 * time.Millisecond):
	}

	unlockFirst()
	select {
	case unlockSecond := <-acquired:
		unlockSecond()
	case <-time.After(2 * time.Second):
		t.Fatal("second writer did not acquire lock after first writer released it")
	}
}

func TestDiskCache_WriteLockDoesNotProceedWithoutOwningLock(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "LONG_LOCK_5y_1d.gob")

	unlockFirst := acquireCacheWriteLock(path)
	defer unlockFirst()

	acquired := make(chan func(), 1)
	go func() { acquired <- acquireCacheWriteLock(path) }()

	select {
	case unlockSecond := <-acquired:
		unlockSecond()
		t.Fatal("writer proceeded without acquiring a lock held for two seconds")
	case <-time.After(2 * time.Second):
	}
}

type historyCall struct {
	symbol   string
	period   string
	interval string
}

type recordingProvider struct {
	data       []OHLCV
	shouldFail bool
	calls      []historyCall
}

func (r *recordingProvider) GetHistory(_ context.Context, symbol, period, interval string) ([]OHLCV, error) {
	r.calls = append(r.calls, historyCall{symbol: symbol, period: period, interval: interval})
	if r.shouldFail {
		return nil, errors.New("recording provider failure")
	}
	return r.data, nil
}

func (r *recordingProvider) GetQuote(_ context.Context, symbol string) (*Quote, error) {
	return &Quote{Symbol: symbol, Price: 100}, nil
}

func sameDay(a, b time.Time) bool {
	return a.Format("2006-01-02") == b.Format("2006-01-02")
}

func captureStderr(t *testing.T, fn func()) string {
	t.Helper()
	old := os.Stderr
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe stderr: %v", err)
	}
	os.Stderr = w

	fn()

	if err := w.Close(); err != nil {
		t.Fatalf("close stderr writer: %v", err)
	}
	os.Stderr = old
	out, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("read stderr: %v", err)
	}
	return string(out)
}
