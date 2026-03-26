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
// TTL defaults to 24 hours for daily data.
type DiskCachedProvider struct {
	inner    Provider
	cacheDir string
	ttl      time.Duration
}

// diskCacheEntry stores OHLCV data with a fetch timestamp.
type diskCacheEntry struct {
	FetchedAt time.Time
	Data      []OHLCV
}

// NewDiskCachedProvider creates a caching wrapper that persists data to disk.
func NewDiskCachedProvider(inner Provider, ttl time.Duration) *DiskCachedProvider {
	cacheDir := filepath.Join(config.StockctlDir(), "cache")
	os.MkdirAll(cacheDir, 0755)
	return &DiskCachedProvider{
		inner:    inner,
		cacheDir: cacheDir,
		ttl:      ttl,
	}
}

func diskCacheFilename(symbol, period, interval string) string {
	// Sanitize symbol for filesystem (replace . ^ / with _)
	safe := strings.NewReplacer(".", "_", "^", "_", "/", "_", ":", "_").Replace(symbol)
	return fmt.Sprintf("%s_%s_%s.gob", safe, period, interval)
}

// GetHistory returns cached data if fresh, otherwise fetches and caches.
func (d *DiskCachedProvider) GetHistory(ctx context.Context, symbol, period, interval string) ([]OHLCV, error) {
	path := filepath.Join(d.cacheDir, diskCacheFilename(symbol, period, interval))

	// Try reading from cache
	if entry, err := d.readCache(path); err == nil {
		if time.Since(entry.FetchedAt) < d.ttl {
			return entry.Data, nil
		}
	}

	// Cache miss or stale — fetch from upstream
	data, err := d.inner.GetHistory(ctx, symbol, period, interval)
	if err != nil {
		return nil, err
	}

	// Write to cache (best effort, don't fail on write errors)
	d.writeCache(path, data)

	return data, nil
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

func (d *DiskCachedProvider) writeCache(path string, data []OHLCV) {
	f, err := os.Create(path)
	if err != nil {
		return
	}
	defer f.Close()

	entry := diskCacheEntry{
		FetchedAt: time.Now(),
		Data:      data,
	}
	gob.NewEncoder(f).Encode(entry)
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
