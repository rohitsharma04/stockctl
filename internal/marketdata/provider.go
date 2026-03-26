package marketdata

import "context"

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
