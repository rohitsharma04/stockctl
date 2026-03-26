package marketdata

import (
	"context"
	"fmt"
	"sort"
	"time"

	yfa "github.com/oscarli916/yahoo-finance-api"
	"golang.org/x/time/rate"
)

// YahooProvider implements Provider using the Yahoo Finance unofficial API.
type YahooProvider struct {
	limiter *rate.Limiter
}

// NewYahooProvider creates a new Yahoo Finance data provider.
// rps controls requests-per-second rate limiting.
func NewYahooProvider(rps float64) *YahooProvider {
	if rps <= 0 {
		rps = 5
	}
	return &YahooProvider{
		limiter: rate.NewLimiter(rate.Limit(rps), 1),
	}
}

// GetHistory fetches historical OHLCV data for a symbol.
// Returns bars sorted by date ascending.
func (y *YahooProvider) GetHistory(ctx context.Context, symbol, period, interval string) ([]OHLCV, error) {
	if err := y.limiter.Wait(ctx); err != nil {
		return nil, fmt.Errorf("rate limit: %w", err)
	}

	t := yfa.NewTicker(symbol)
	// Returns map[string]PriceData where key is date string like "2024-01-15"
	histMap, err := t.History(yfa.HistoryQuery{Range: period, Interval: interval})
	if err != nil {
		return nil, fmt.Errorf("yahoo history for %s: %w", symbol, err)
	}

	if len(histMap) == 0 {
		return nil, fmt.Errorf("no data returned for %s", symbol)
	}

	// Sort dates
	dates := make([]string, 0, len(histMap))
	for d := range histMap {
		dates = append(dates, d)
	}
	sort.Strings(dates)

	// Convert to OHLCV bars
	bars := make([]OHLCV, 0, len(dates))
	for _, dateStr := range dates {
		pd := histMap[dateStr]
		parsedDate, err := time.Parse("2006-01-02", dateStr)
		if err != nil {
			// Try other formats
			parsedDate, err = time.Parse("2006-01-02 15:04:05", dateStr)
			if err != nil {
				parsedDate, _ = time.Parse(time.RFC3339, dateStr)
			}
		}
		bars = append(bars, OHLCV{
			Date:   parsedDate,
			Open:   pd.Open,
			High:   pd.High,
			Low:    pd.Low,
			Close:  pd.Close,
			Volume: float64(pd.Volume),
		})
	}

	return bars, nil
}

// GetQuote fetches the latest quote for a symbol.
func (y *YahooProvider) GetQuote(ctx context.Context, symbol string) (*Quote, error) {
	if err := y.limiter.Wait(ctx); err != nil {
		return nil, fmt.Errorf("rate limit: %w", err)
	}

	t := yfa.NewTicker(symbol)
	pd, err := t.Quote()
	if err != nil {
		return nil, fmt.Errorf("yahoo quote for %s: %w", symbol, err)
	}

	return &Quote{
		Symbol: symbol,
		Price:  pd.Close,
		Volume: float64(pd.Volume),
	}, nil
}
