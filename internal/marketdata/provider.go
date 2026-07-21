package marketdata

import (
	"context"
	"time"
)

// Provider is the interface for fetching market data.
// Implementations can wrap Yahoo Finance, Angel One, or any other source.
type Provider interface {
	// GetHistory fetches historical OHLCV data.
	// period: "1d","5d","1mo","3mo","6mo","1y","2y","5y","10y","ytd","max"
	// interval: "1m","5m","15m","30m","1h","1d","1wk","1mo"
	GetHistory(ctx context.Context, symbol, period, interval string) ([]OHLCV, error)

	// GetQuote fetches the latest quote for a symbol.
	GetQuote(ctx context.Context, symbol string) (*Quote, error)
}

// HistoryProvider is an optional extension for callers that need cache
// provenance without changing the legacy Provider contract.
type HistoryProvider interface {
	Provider
	GetHistoryWithProvenance(ctx context.Context, req HistoryRequest) (*HistoryResult, error)
}

// HistoryRequest describes a historical data request for provenance-aware
// providers. AsOf is optional; when set, a cache that covers that date can be
// served without refreshing upstream.
type HistoryRequest struct {
	Symbol   string
	Period   string
	Interval string
	AsOf     time.Time
}

type HistorySource string

const (
	HistorySourceUpstream         HistorySource = "upstream"
	HistorySourceCache            HistorySource = "cache"
	HistorySourceCacheAndUpstream HistorySource = "cache+upstream"
)

// HistoryProvenance reports where data came from and whether the cache was
// used as a stale fallback after an upstream failure.
type HistoryProvenance struct {
	Source        HistorySource
	FetchedAt     time.Time
	LastBarDate   time.Time
	Stale         bool
	UpstreamError string
}

type HistoryResult struct {
	Data       []OHLCV
	Provenance HistoryProvenance
}
