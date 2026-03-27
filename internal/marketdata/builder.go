package marketdata

// BuildProvider constructs the standard provider stack:
//
//	Yahoo (rate-limited) → CircuitBreaker → DiskCache
//
// When noCache is true, only the Yahoo provider is returned (no disk cache).
func BuildProvider(noCache bool) Provider {
	yahoo := NewYahooProvider(5)

	if noCache {
		return yahoo
	}

	cb := NewCircuitBreakerProvider(yahoo, DefaultCircuitBreakerConfig())
	return NewDiskCachedProvider(cb)
}
