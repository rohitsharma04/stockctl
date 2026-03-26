package marketdata

import (
	"context"
	"fmt"
	"sync"
)

// CachedProvider wraps a YahooProvider with an in-memory cache.
// Cache entries live for the duration of the process — ideal for
// `scan all` where each ticker's data is fetched once and reused
// across 4 screeners.
type CachedProvider struct {
	inner *YahooProvider
	mu    sync.RWMutex
	cache map[string][]OHLCV
}

// NewCachedProvider creates a caching wrapper around a YahooProvider.
func NewCachedProvider(inner *YahooProvider) *CachedProvider {
	return &CachedProvider{
		inner: inner,
		cache: make(map[string][]OHLCV),
	}
}

func cacheKey(symbol, period, interval string) string {
	return fmt.Sprintf("%s:%s:%s", symbol, period, interval)
}

// GetHistory returns cached data if available, otherwise fetches and caches.
func (c *CachedProvider) GetHistory(ctx context.Context, symbol, period, interval string) ([]OHLCV, error) {
	key := cacheKey(symbol, period, interval)

	c.mu.RLock()
	if data, ok := c.cache[key]; ok {
		c.mu.RUnlock()
		return data, nil
	}
	c.mu.RUnlock()

	data, err := c.inner.GetHistory(ctx, symbol, period, interval)
	if err != nil {
		return nil, err
	}

	c.mu.Lock()
	c.cache[key] = data
	c.mu.Unlock()

	return data, nil
}

// GetQuote delegates to the inner provider (no caching for real-time quotes).
func (c *CachedProvider) GetQuote(ctx context.Context, symbol string) (*Quote, error) {
	return c.inner.GetQuote(ctx, symbol)
}

// Stats returns the number of cached entries.
func (c *CachedProvider) Stats() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return len(c.cache)
}
