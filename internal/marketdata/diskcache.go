package marketdata

import (
	"context"
	"encoding/gob"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
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

// These seams keep filesystem failures observable in focused contract tests.
// Production always uses the standard cache location and os.Remove.
var (
	cacheDirPath = func() string { return filepath.Join(config.StockctlDir(), "cache") }
	cacheRemove  = os.Remove
)

// NewDiskCachedProvider creates a caching wrapper that persists data to disk.
func NewDiskCachedProvider(inner Provider) *DiskCachedProvider {
	cacheDir := cacheDirPath()
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

// CacheFilename returns the on-disk cache filename for an entry.
func CacheFilename(symbol, period, interval string) string {
	return diskCacheFilename(symbol, period, interval)
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

	unlock, err := acquireCacheFileLock(ctx, path)
	if err != nil {
		return nil, err
	}
	defer unlock()

	// Try reading existing cache
	entry, cacheErr := d.readCache(path)

	if cacheErr != nil || len(entry.Data) == 0 {
		// No cache — full fetch
		fetchPeriod := initialFetchPeriod(period, interval)
		if req.RequireCompletePeriod {
			fetchPeriod = period
		}
		data, err := d.inner.GetHistory(ctx, symbol, fetchPeriod, interval)
		if err != nil {
			return nil, err
		}
		fetchedAt, _, err := d.writeCache(path, data, period)
		if err != nil {
			return nil, err
		}
		resultData := filterBarsAsOf(data, req.AsOf)
		resultLastBar := lastBarDate(resultData)
		return &HistoryResult{
			Data: resultData,
			Provenance: HistoryProvenance{
				Source:      HistorySourceUpstream,
				FetchedAt:   fetchedAt,
				LastBarDate: resultLastBar,
			},
		}, nil
	}

	if !req.RequireCompletePeriod && cacheCoversRequest(entry, req) {
		data := filterBarsAsOf(entry.Data, req.AsOf)
		return &HistoryResult{
			Data: data,
			Provenance: HistoryProvenance{
				Source:      HistorySourceCache,
				FetchedAt:   entry.FetchedAt,
				LastBarDate: lastBarDate(data),
			},
		}, nil
	}

	// Cache exists — fetch missing history or refresh the tail.
	fetchPeriod := deltaPeriodAsOf(entry.LastBarDate, req.AsOf, period)
	if req.RequireCompletePeriod {
		fetchPeriod = period
	} else if missingHistoricalStartCoverage(entry, req) {
		fetchPeriod = initialFetchPeriod(period, interval)
	}
	fresh, err := d.inner.GetHistory(ctx, symbol, fetchPeriod, interval)
	if err != nil {
		// Stale-while-revalidate: return cached data on upstream failure
		data := filterBarsAsOf(entry.Data, req.AsOf)
		return &HistoryResult{
			Data: data,
			Provenance: HistoryProvenance{
				Source:        HistorySourceCache,
				FetchedAt:     entry.FetchedAt,
				LastBarDate:   lastBarDate(data),
				Stale:         true,
				UpstreamError: err.Error(),
			},
		}, nil
	}

	// Merge and save
	merged := mergeOHLCV(entry.Data, fresh)
	fetchedAt, _, err := d.writeCache(path, merged, period)
	if err != nil {
		return nil, err
	}
	resultData := filterBarsAsOf(merged, req.AsOf)
	return &HistoryResult{
		Data: resultData,
		Provenance: HistoryProvenance{
			Source:      HistorySourceCacheAndUpstream,
			FetchedAt:   fetchedAt,
			LastBarDate: lastBarDate(resultData),
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
	earliest, latest, ok := cacheDateRange(entry.Data)
	if !ok {
		return false
	}
	if req.AsOf.IsZero() {
		return sameHistoryDay(latest, time.Now())
	}
	start, ok := requestedPeriodStart(req.AsOf, req.Period)
	if !ok {
		return false
	}
	return coversHistoricalStart(earliest, start, req.Period) && !latest.Before(truncateHistoryDay(req.AsOf))
}

func missingHistoricalStartCoverage(entry *diskCacheEntry, req HistoryRequest) bool {
	if req.AsOf.IsZero() {
		return false
	}
	start, ok := requestedPeriodStart(req.AsOf, req.Period)
	if !ok {
		return false
	}
	earliest, _, ok := cacheDateRange(entry.Data)
	if !ok {
		return true
	}
	return !coversHistoricalStart(earliest, start, req.Period)
}

func coversHistoricalStart(earliest, start time.Time, period string) bool {
	if period == "max" {
		return false
	}
	return !earliest.After(start)
}

func cacheDateRange(data []OHLCV) (time.Time, time.Time, bool) {
	if len(data) == 0 {
		return time.Time{}, time.Time{}, false
	}
	earliest := truncateHistoryDay(data[0].Date)
	latest := earliest
	for _, bar := range data[1:] {
		day := truncateHistoryDay(bar.Date)
		if day.Before(earliest) {
			earliest = day
		}
		if day.After(latest) {
			latest = day
		}
	}
	return earliest, latest, true
}

func truncateHistoryDay(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, t.Location())
}

func sameHistoryDay(a, b time.Time) bool {
	return truncateHistoryDay(a).Equal(truncateHistoryDay(b))
}

func initialFetchPeriod(period, interval string) string {
	// A full-history seed must not be silently narrowed. Normal daily cache
	// population remains bounded to five years so interactive cold-cache scans do
	// not turn into unexpectedly expensive all-history requests.
	if period == "max" {
		return "max"
	}
	if interval == "1d" {
		return "5y"
	}
	return period
}

func requestedPeriodStart(asOf time.Time, period string) (time.Time, bool) {
	asOfDay := truncateHistoryDay(asOf)
	switch period {
	case "ytd":
		return time.Date(asOf.Year(), 1, 1, 0, 0, 0, 0, asOf.Location()), true
	case "max":
		return time.Time{}, true
	}

	if strings.HasSuffix(period, "d") {
		n, err := strconv.Atoi(strings.TrimSuffix(period, "d"))
		if err != nil {
			return time.Time{}, false
		}
		return asOfDay.AddDate(0, 0, -n), true
	}
	if strings.HasSuffix(period, "mo") {
		n, err := strconv.Atoi(strings.TrimSuffix(period, "mo"))
		if err != nil {
			return time.Time{}, false
		}
		return asOfDay.AddDate(0, -n, 0), true
	}
	if strings.HasSuffix(period, "y") {
		n, err := strconv.Atoi(strings.TrimSuffix(period, "y"))
		if err != nil {
			return time.Time{}, false
		}
		return asOfDay.AddDate(-n, 0, 0), true
	}
	return time.Time{}, false
}

func filterBarsAsOf(data []OHLCV, asOf time.Time) []OHLCV {
	if asOf.IsZero() {
		return data
	}
	asOfDay := truncateHistoryDay(asOf)
	filtered := make([]OHLCV, 0, len(data))
	for _, bar := range data {
		if !truncateHistoryDay(bar.Date).After(asOfDay) {
			filtered = append(filtered, bar)
		}
	}
	return filtered
}

func lastBarDate(data []OHLCV) time.Time {
	if len(data) == 0 {
		return time.Time{}
	}
	latest := data[0].Date
	for _, bar := range data[1:] {
		if bar.Date.After(latest) {
			latest = bar.Date
		}
	}
	return latest
}

func (d *DiskCachedProvider) writeCache(path string, data []OHLCV, origPeriod string) (time.Time, time.Time, error) {
	lastBar := lastBarDate(data)
	fetchedAt := time.Now()
	entry := diskCacheEntry{
		FetchedAt:   fetchedAt,
		LastBarDate: lastBar,
		OrigPeriod:  origPeriod,
		Data:        data,
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return fetchedAt, lastBar, err
	}

	tmp, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".*.tmp")
	if err != nil {
		return fetchedAt, lastBar, err
	}
	tmpName := tmp.Name()
	removeTmp := true
	defer func() {
		if removeTmp {
			_ = os.Remove(tmpName)
		}
	}()

	if err := gob.NewEncoder(tmp).Encode(entry); err != nil {
		_ = tmp.Close()
		return fetchedAt, lastBar, err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return fetchedAt, lastBar, err
	}
	if err := tmp.Close(); err != nil {
		return fetchedAt, lastBar, err
	}
	if err := os.Rename(tmpName, path); err != nil {
		return fetchedAt, lastBar, err
	}
	removeTmp = false
	return fetchedAt, lastBar, nil
}

func acquireCacheFileLock(ctx context.Context, path string) (func(), error) {
	lockPath := path + ".lock"
	if err := os.MkdirAll(filepath.Dir(lockPath), 0755); err != nil {
		return nil, err
	}

	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}

		f, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0644)
		if err != nil {
			return nil, err
		}
		if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err == nil {
			return func() {
				_ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
				_ = f.Close()
			}, nil
		} else if err != syscall.EWOULDBLOCK && err != syscall.EAGAIN {
			_ = f.Close()
			return nil, err
		}
		_ = f.Close()

		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(10 * time.Millisecond):
		}
	}
}

func acquireCacheWriteLock(path string) func() {
	unlock, err := acquireCacheFileLock(context.Background(), path)
	if err != nil {
		return func() {}
	}
	return unlock
}

// CacheStats holds statistics about the disk cache.
type CacheStats struct {
	CacheDir   string `json:"cache_dir"`
	TotalFiles int    `json:"total_files"`
	TotalBytes int64  `json:"total_bytes"`
	OldestFile string `json:"oldest_file,omitempty"`
	NewestFile string `json:"newest_file,omitempty"`
	Decodable  int    `json:"decodable,omitempty"`
	Corrupt    int    `json:"corrupt,omitempty"`
	LockFiles  int    `json:"lock_files,omitempty"`
}

// GetCacheStats returns statistics about the disk cache.
func GetCacheStats(verify ...bool) CacheStats {
	cacheDir := cacheDirPath()
	stats := CacheStats{CacheDir: cacheDir}

	entries, err := os.ReadDir(cacheDir)
	if err != nil {
		return stats
	}

	var oldestTime, newestTime time.Time
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		if strings.HasSuffix(e.Name(), ".lock") {
			stats.LockFiles++
			continue
		}
		if !strings.HasSuffix(e.Name(), ".gob") {
			continue
		}
		stats.TotalFiles++
		info, err := e.Info()
		if err != nil {
			continue
		}
		stats.TotalBytes += info.Size()
		if len(verify) > 0 && verify[0] {
			if _, _, err := ReadCacheEntry(filepath.Join(cacheDir, e.Name())); err != nil {
				stats.Corrupt++
			} else {
				stats.Decodable++
			}
		}

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

// ReadCacheEntry decodes a cache entry without refreshing or modifying it.
func ReadCacheEntry(path string) ([]OHLCV, string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, "", err
	}
	defer f.Close()
	var entry diskCacheEntry
	if err := gob.NewDecoder(f).Decode(&entry); err != nil {
		return nil, "", err
	}
	if entry.OrigPeriod == "" {
		return nil, "", fmt.Errorf("invalid cache entry: missing original period")
	}
	if len(entry.Data) == 0 {
		return nil, "", fmt.Errorf("invalid cache entry: no bars")
	}
	for _, bar := range entry.Data {
		if bar.Date.IsZero() {
			return nil, "", fmt.Errorf("invalid cache entry: bar has no date")
		}
	}
	return entry.Data, entry.OrigPeriod, nil
}

func CacheDir() string { return cacheDirPath() }

// ClearCache removes all cached files. If market is non-empty, only clears
// files matching that market's suffix pattern.
func ClearCache(market string) (int, error) {
	_, removed, err := ClearCacheWithOptions(market, false)
	return removed, err
}

// ClearCacheWithOptions reports matching and removed cache files. It never
// considers advisory lock files for removal.
func ClearCacheWithOptions(market string, dryRun bool) (int, int, error) {
	var suffix string
	if market != "" {
		mkt, ok := Markets[market]
		if !ok {
			return 0, 0, fmt.Errorf("unknown market: %s", market)
		}
		suffix = strings.ReplaceAll(mkt.Suffix, ".", "_")
	}
	cacheDir := cacheDirPath()
	entries, err := os.ReadDir(cacheDir)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, 0, nil
		}
		return 0, 0, err
	}

	matched, removed := 0, 0
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".gob") {
			continue
		}

		if market != "" {
			// Only clear files matching market suffix
			if suffix != "" && !strings.Contains(e.Name(), suffix) {
				continue
			}
		}
		matched++

		if !dryRun {
			if err := cacheRemove(filepath.Join(cacheDir, e.Name())); err != nil {
				return matched, removed, fmt.Errorf("removing cache entry %s: %w", e.Name(), err)
			}
			removed++
		}
	}

	return matched, removed, nil
}
