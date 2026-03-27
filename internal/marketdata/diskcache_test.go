package marketdata

import (
	"context"
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
		{1, "5d"},   // 1+3=4 days total → 5d
		{3, "1mo"},  // 3+3=6 days total → 1mo (overlap pushes past 5d)
		{10, "1mo"}, // 10+3=13 → 1mo
		{50, "3mo"}, // 50+3=53 → 3mo
		{120, "6mo"},
		{300, "1y"},
		{600, "2y"},
		{1000, "5y"}, // falls back to orig
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
