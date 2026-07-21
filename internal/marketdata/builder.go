package marketdata

// BuildProvider constructs the standard provider stack:
//
//	Yahoo (rate-limited) → CircuitBreaker → DiskCache
//
// When noCache is true, only the Yahoo provider is returned (no disk cache).
func BuildProvider(noCache bool) Provider {
	return BuildProviderWithRPS(noCache, 5)
}

// BuildProviderWithRPS constructs the standard provider stack with an
// explicitly configured Yahoo request rate. This is useful for batch jobs
// which must control their upstream footprint without bypassing the cache.
func BuildProviderWithRPS(noCache bool, rps int) Provider {
	yahoo := NewYahooProvider(float64(rps))

	if noCache {
		return yahoo
	}

	cb := NewCircuitBreakerProvider(yahoo, DefaultCircuitBreakerConfig())
	return NewDiskCachedProvider(cb)
}
