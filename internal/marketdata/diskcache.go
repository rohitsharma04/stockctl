package marketdata

import (
	"context"
	"encoding/gob"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/rohitsharma04/stockctl/internal/config"
)

// DiskCachedProvider wraps a Provider with file-based caching.
// Each (symbol, period, interval) is stored as a gob file in ~/.stockctl/cache/.
//
// On every call, the provider:
//  1. Reads existing cache to find the last bar date
//  2. Fetches only the delta (with 3-day overlap for corrections/splits)
//  3. Merges new bars into the cache (overwrite overlap, append new)
//  4. Returns the merged data
//
// If the upstream fetch fails, stale cached data is returned instead of an error.
type DiskCachedProvider struct {
	inner    Provider
	cacheDir string
}

// diskCacheEntry stores OHLCV data with metadata for delta computation.
type diskCacheEntry struct {
	FetchedAt   time.Time
	LastBarDate time.Time // Date of the most recent bar in cache
	OrigPeriod  string    // Original period requested (e.g., "5y")
	Data        []OHLCV
}

const tailFetchPeriod = "5d"

// NewDiskCachedProvider creates a caching wrapper that persists data to disk.
func NewDiskCachedProvider(inner Provider) *DiskCachedProvider {
	cacheDir := filepath.Join(config.StockctlDir(), "cache")
	os.MkdirAll(cacheDir, 0755)
	return &DiskCachedProvider{
		inner:    inner,
		cacheDir: cacheDir,
	}
}

func diskCacheFilename(symbol, period, interval string) string {
	// Sanitize symbol for filesystem (replace . ^ / with _)
	safe := strings.NewReplacer(".", "_", "^", "_", "/", "_", ":", "_").Replace(symbol)
	return fmt.Sprintf("%s_%s_%s.gob", safe, period, interval)
}

// deltaPeriod returns the fixed overlap/tail range used after an initial
// full fetch. The 5-day window catches corrections, splits, and late bars
// without refetching the full requested history.
func deltaPeriod(lastBarDate time.Time, origPeriod string) string {
	return deltaPeriodAsOf(lastBarDate, time.Now(), origPeriod)
}

func deltaPeriodAsOf(lastBarDate, asOf time.Time, origPeriod string) string {
	return tailFetchPeriod
}

// mergeOHLCV merges new bars into existing cached bars.
// Bars with the same date are overwritten (overlap correction).
// The result is sorted by date ascending.
func mergeOHLCV(cached, fresh []OHLCV) []OHLCV {
	// Build a map keyed by date (truncated to day)
	byDate := make(map[string]OHLCV, len(cached)+len(fresh))
	order := make([]string, 0, len(cached)+len(fresh))

	for _, bar := range cached {
		key := bar.Date.Format("2006-01-02")
		if _, exists := byDate[key]; !exists {
			order = append(order, key)
		}
		byDate[key] = bar
	}

	for _, bar := range fresh {
		key := bar.Date.Format("2006-01-02")
		if _, exists := byDate[key]; !exists {
			order = append(order, key)
		}
		// Overwrite — fresh data takes priority
		byDate[key] = bar
	}

	// Sort dates
	sortStrings(order)

	merged := make([]OHLCV, 0, len(order))
	for _, key := range order {
		merged = append(merged, byDate[key])
	}
	return merged
}

// sortStrings sorts a string slice in place (date strings sort correctly).
func sortStrings(s []string) {
	// Simple insertion sort — fine for mostly-sorted date lists
	for i := 1; i < len(s); i++ {
		key := s[i]
		j := i - 1
		for j >= 0 && s[j] > key {
			s[j+1] = s[j]
			j--
		}
		s[j+1] = key
	}
}

// GetHistory preserves the legacy Provider contract. If a cached value exists
// and upstream refresh fails, stale cached data is returned with a nil error.
func (d *DiskCachedProvider) GetHistory(ctx context.Context, symbol, period, interval string) ([]OHLCV, error) {
	result, err := d.GetHistoryWithProvenance(ctx, HistoryRequest{
		Symbol:   symbol,
		Period:   period,
		Interval: interval,
	})
	if err != nil {
		return nil, err
	}
	return result.Data, nil
}

// GetHistoryWithProvenance returns historical bars with cache/upstream
// provenance while keeping cache refresh behavior internal to the provider.
func (d *DiskCachedProvider) GetHistoryWithProvenance(ctx context.Context, req HistoryRequest) (*HistoryResult, error) {
	symbol, period, interval := req.Symbol, req.Period, req.Interval
	path := filepath.Join(d.cacheDir, diskCacheFilename(symbol, period, interval))

	// Try reading existing cache
	entry, cacheErr := d.readCache(path)

	if cacheErr != nil || len(entry.Data) == 0 {
		// No cache — full fetch
		data, err := d.inner.GetHistory(ctx, symbol, period, interval)
		if err != nil {
			return nil, err
		}
		fetchedAt, lastBar := d.writeCache(path, data, period)
		return &HistoryResult{
			Data: data,
			Provenance: HistoryProvenance{
				Source:      HistorySourceUpstream,
				FetchedAt:   fetchedAt,
				LastBarDate: lastBar,
			},
		}, nil
	}

	if cacheCoversRequest(entry, req) {
		return &HistoryResult{
			Data: entry.Data,
			Provenance: HistoryProvenance{
				Source:      HistorySourceCache,
				FetchedAt:   entry.FetchedAt,
				LastBarDate: entry.LastBarDate,
			},
		}, nil
	}

	// Cache exists — fetch delta
	dp := deltaPeriodAsOf(entry.LastBarDate, req.AsOf, period)
	fresh, err := d.inner.GetHistory(ctx, symbol, dp, interval)
	if err != nil {
		// Stale-while-revalidate: return cached data on upstream failure
		return &HistoryResult{
			Data: entry.Data,
			Provenance: HistoryProvenance{
				Source:        HistorySourceCache,
				FetchedAt:     entry.FetchedAt,
				LastBarDate:   entry.LastBarDate,
				Stale:         true,
				UpstreamError: err.Error(),
			},
		}, nil
	}

	// Merge and save
	merged := mergeOHLCV(entry.Data, fresh)
	fetchedAt, lastBar := d.writeCache(path, merged, period)
	return &HistoryResult{
		Data: merged,
		Provenance: HistoryProvenance{
			Source:      HistorySourceCacheAndUpstream,
			FetchedAt:   fetchedAt,
			LastBarDate: lastBar,
		},
	}, nil
}

// GetQuote delegates to the inner provider (no caching for real-time quotes).
func (d *DiskCachedProvider) GetQuote(ctx context.Context, symbol string) (*Quote, error) {
	return d.inner.GetQuote(ctx, symbol)
}

func (d *DiskCachedProvider) readCache(path string) (*diskCacheEntry, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var entry diskCacheEntry
	if err := gob.NewDecoder(f).Decode(&entry); err != nil {
		return nil, err
	}
	return &entry, nil
}

func cacheCoversRequest(entry *diskCacheEntry, req HistoryRequest) bool {
	if req.AsOf.IsZero() {
		return sameHistoryDay(entry.LastBarDate, time.Now())
	}
	return !entry.LastBarDate.Before(truncateHistoryDay(req.AsOf))
}

func truncateHistoryDay(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, t.Location())
}

func sameHistoryDay(a, b time.Time) bool {
	return truncateHistoryDay(a).Equal(truncateHistoryDay(b))
}

func (d *DiskCachedProvider) writeCache(path string, data []OHLCV, origPeriod string) (time.Time, time.Time) {
	lastBar := time.Time{}
	if len(data) > 0 {
		lastBar = data[len(data)-1].Date
	}
	fetchedAt := time.Now()
	entry := diskCacheEntry{
		FetchedAt:   fetchedAt,
		LastBarDate: lastBar,
		OrigPeriod:  origPeriod,
		Data:        data,
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return fetchedAt, lastBar
	}
	unlock := acquireCacheWriteLock(path)
	defer unlock()

	tmp, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".*.tmp")
	if err != nil {
		return fetchedAt, lastBar
	}
	tmpName := tmp.Name()
	rename := false
	if err := gob.NewEncoder(tmp).Encode(entry); err == nil {
		if err := tmp.Sync(); err == nil {
			rename = true
		}
	}
	if err := tmp.Close(); err != nil {
		rename = false
	}
	if rename {
		if err := os.Rename(tmpName, path); err == nil {
			return fetchedAt, lastBar
		}
	}
	_ = os.Remove(tmpName)
	return fetchedAt, lastBar
}

func acquireCacheWriteLock(path string) func() {
	lockDir := path + ".lock"
	for {
		if err := os.Mkdir(lockDir, 0755); err == nil {
			return func() { _ = os.Remove(lockDir) }
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// CacheStats holds statistics about the disk cache.
type CacheStats struct {
	CacheDir   string `json:"cache_dir"`
	TotalFiles int    `json:"total_files"`
	TotalBytes int64  `json:"total_bytes"`
	OldestFile string `json:"oldest_file,omitempty"`
	NewestFile string `json:"newest_file,omitempty"`
}

// GetCacheStats returns statistics about the disk cache.
func GetCacheStats() CacheStats {
	cacheDir := filepath.Join(config.StockctlDir(), "cache")
	stats := CacheStats{CacheDir: cacheDir}

	entries, err := os.ReadDir(cacheDir)
	if err != nil {
		return stats
	}

	var oldestTime, newestTime time.Time
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".gob") {
			continue
		}
		stats.TotalFiles++
		info, err := e.Info()
		if err != nil {
			continue
		}
		stats.TotalBytes += info.Size()

		if oldestTime.IsZero() || info.ModTime().Before(oldestTime) {
			oldestTime = info.ModTime()
			stats.OldestFile = info.ModTime().Format(time.RFC3339)
		}
		if info.ModTime().After(newestTime) {
			newestTime = info.ModTime()
			stats.NewestFile = info.ModTime().Format(time.RFC3339)
		}
	}

	return stats
}

// ClearCache removes all cached files. If market is non-empty, only clears
// files matching that market's suffix pattern.
func ClearCache(market string) (int, error) {
	cacheDir := filepath.Join(config.StockctlDir(), "cache")
	entries, err := os.ReadDir(cacheDir)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, err
	}

	removed := 0
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".gob") {
			continue
		}

		if market != "" {
			// Only clear files matching market suffix
			mkt, ok := Markets[market]
			if !ok {
				return 0, fmt.Errorf("unknown market: %s", market)
			}
			suffix := strings.ReplaceAll(mkt.Suffix, ".", "_")
			if suffix != "" && !strings.Contains(e.Name(), suffix) {
				continue
			}
		}

		if err := os.Remove(filepath.Join(cacheDir, e.Name())); err == nil {
			removed++
		}
	}

	return removed, nil
}
