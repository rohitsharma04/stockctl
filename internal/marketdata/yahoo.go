package marketdata

import (
	"context"
	"errors"
	"fmt"
	"math/rand"
	"net"
	"sort"
	"strings"
	"time"

	yfa "github.com/oscarli916/yahoo-finance-api"
	"golang.org/x/time/rate"
)

// yahooRetryConfig is deliberately internal: it makes retry timing testable
// without making provider policy part of the public API.
type yahooRetryConfig struct {
	MaxAttempts int
	BaseDelay   time.Duration
	MaxDelay    time.Duration
	Rand        func(max time.Duration) time.Duration // full jitter in [0, max]
	Sleep       func(context.Context, time.Duration) error
}

func defaultYahooRetryConfig() yahooRetryConfig {
	return yahooRetryConfig{
		MaxAttempts: 3,
		BaseDelay:   250 * time.Millisecond,
		MaxDelay:    2 * time.Second,
		Rand: func(max time.Duration) time.Duration {
			if max <= 0 {
				return 0
			}
			return time.Duration(rand.Int63n(int64(max) + 1))
		},
		Sleep: sleepWithContext,
	}
}

func sleepWithContext(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

// YahooProvider implements Provider using the Yahoo Finance unofficial API.
type YahooProvider struct {
	limiter *rate.Limiter
	retry   yahooRetryConfig

	// Boundaries are injectable for deterministic tests. The upstream Yahoo
	// library does not offer a context-aware History call.
	history func(string, yfa.HistoryQuery) (map[string]yfa.PriceData, error)
	quote   func(string) (yfa.PriceData, error)
}

// NewYahooProvider creates a new Yahoo Finance data provider.
func NewYahooProvider(rps float64) *YahooProvider {
	if rps <= 0 {
		rps = 5
	}
	return &YahooProvider{
		limiter: rate.NewLimiter(rate.Limit(rps), 1),
		retry:   defaultYahooRetryConfig(),
		history: func(symbol string, query yfa.HistoryQuery) (map[string]yfa.PriceData, error) {
			return yfa.NewTicker(symbol).History(query)
		},
		quote: func(symbol string) (yfa.PriceData, error) {
			return yfa.NewTicker(symbol).Quote()
		},
	}
}

func (y *YahooProvider) retryCall(ctx context.Context, call func() error) error {
	cfg := y.retry.withDefaults()
	for attempt := 0; attempt < cfg.MaxAttempts; attempt++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := y.limiter.Wait(ctx); err != nil {
			return fmt.Errorf("rate limit: %w", err)
		}
		err := call()
		if err == nil {
			return nil
		}
		if !isRetryableYahooError(err) || attempt == cfg.MaxAttempts-1 {
			return err
		}
		cap := cfg.BaseDelay << attempt
		if cap < 0 || cap > cfg.MaxDelay {
			cap = cfg.MaxDelay
		}
		if cap < 0 {
			cap = 0
		}
		delay := cfg.Rand(cap)
		if delay < 0 || delay > cap {
			delay = cap
		}
		if err := cfg.Sleep(ctx, delay); err != nil {
			return err
		}
	}
	return nil
}

func (cfg yahooRetryConfig) withDefaults() yahooRetryConfig {
	defaults := defaultYahooRetryConfig()
	if cfg.MaxAttempts <= 0 {
		cfg.MaxAttempts = defaults.MaxAttempts
	}
	if cfg.BaseDelay <= 0 {
		cfg.BaseDelay = defaults.BaseDelay
	}
	if cfg.MaxDelay <= 0 {
		cfg.MaxDelay = defaults.MaxDelay
	}
	if cfg.Rand == nil {
		cfg.Rand = defaults.Rand
	}
	if cfg.Sleep == nil {
		cfg.Sleep = defaults.Sleep
	}
	return cfg
}

func isRetryableYahooError(err error) bool {
	if err == nil || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) || errors.Is(err, ErrCircuitOpen) {
		return false
	}
	var netErr net.Error
	if errors.As(err, &netErr) && (netErr.Timeout() || netErr.Temporary()) {
		return true
	}
	text := strings.ToLower(err.Error())
	if strings.Contains(text, "429") || strings.Contains(text, "too many requests") ||
		strings.Contains(text, "rate limit") || strings.Contains(text, "timed out") ||
		strings.Contains(text, "timeout") || strings.Contains(text, "temporary") {
		return true
	}
	for _, status := range []string{"500", "501", "502", "503", "504", "505", "506", "507", "508", "509", "510", "511"} {
		if strings.Contains(text, status) {
			return true
		}
	}
	return false
}

// GetHistory fetches historical OHLCV data for a symbol. Returns bars sorted by date ascending.
func (y *YahooProvider) GetHistory(ctx context.Context, symbol, period, interval string) ([]OHLCV, error) {
	var histMap map[string]yfa.PriceData
	err := y.retryCall(ctx, func() error {
		var err error
		histMap, err = y.history(symbol, yfa.HistoryQuery{Range: period, Interval: interval})
		return err
	})
	if err != nil {
		return nil, fmt.Errorf("yahoo history for %s: %w", symbol, err)
	}
	if len(histMap) == 0 {
		return nil, fmt.Errorf("no data returned for %s", symbol)
	}

	dates := make([]string, 0, len(histMap))
	for d := range histMap {
		dates = append(dates, d)
	}
	sort.Strings(dates)
	bars := make([]OHLCV, 0, len(dates))
	for _, dateStr := range dates {
		pd := histMap[dateStr]
		parsedDate, err := time.Parse("2006-01-02", dateStr)
		if err != nil {
			parsedDate, err = time.Parse("2006-01-02 15:04:05", dateStr)
			if err != nil {
				parsedDate, _ = time.Parse(time.RFC3339, dateStr)
			}
		}
		bars = append(bars, OHLCV{Date: parsedDate, Open: pd.Open, High: pd.High, Low: pd.Low, Close: pd.Close, Volume: float64(pd.Volume)})
	}
	return bars, nil
}

// GetQuote fetches the latest quote for a symbol.
func (y *YahooProvider) GetQuote(ctx context.Context, symbol string) (*Quote, error) {
	var pd yfa.PriceData
	err := y.retryCall(ctx, func() error {
		var err error
		pd, err = y.quote(symbol)
		return err
	})
	if err != nil {
		return nil, fmt.Errorf("yahoo quote for %s: %w", symbol, err)
	}
	return &Quote{Symbol: symbol, Price: pd.Close, Volume: float64(pd.Volume)}, nil
}
