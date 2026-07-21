package marketdata

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	yfa "github.com/oscarli916/yahoo-finance-api"
	"golang.org/x/time/rate"
)

func TestYahooProvider_GetHistoryRetriesRetryableYahooError(t *testing.T) {
	var calls int
	var slept []time.Duration
	provider := NewYahooProvider(1)
	provider.limiter = rate.NewLimiter(rate.Inf, 0)
	provider.history = func(_ string, _ yfa.HistoryQuery) (map[string]yfa.PriceData, error) {
		calls++
		if calls == 1 {
			return nil, fmt.Errorf("yahoo returned status 429: too many requests")
		}
		return map[string]yfa.PriceData{
			"2024-01-02": {Open: 100, High: 110, Low: 90, Close: 105, Volume: 1000},
		}, nil
	}
	provider.retry = yahooRetryConfig{
		MaxAttempts: 3,
		BaseDelay:   100 * time.Millisecond,
		MaxDelay:    time.Second,
		Rand:        func(max time.Duration) time.Duration { return max / 2 },
		Sleep: func(_ context.Context, d time.Duration) error {
			slept = append(slept, d)
			return nil
		},
	}

	bars, err := provider.GetHistory(context.Background(), "AAPL", "1mo", "1d")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if calls != 2 {
		t.Fatalf("expected 2 history attempts, got %d", calls)
	}
	if len(slept) != 1 || slept[0] != 50*time.Millisecond {
		t.Fatalf("expected one full-jitter sleep at half base delay, got %v", slept)
	}
	if len(bars) != 1 || bars[0].Close != 105 {
		t.Fatalf("unexpected bars: %#v", bars)
	}
}

func TestYahooProvider_GetHistoryBoundsRetriesWithExponentialFullJitter(t *testing.T) {
	var calls int
	var jitterInputs []time.Duration
	var slept []time.Duration
	provider := NewYahooProvider(1)
	provider.limiter = rate.NewLimiter(rate.Inf, 0)
	provider.history = func(_ string, _ yfa.HistoryQuery) (map[string]yfa.PriceData, error) {
		calls++
		return nil, fmt.Errorf("server returned status 503")
	}
	provider.retry = yahooRetryConfig{
		MaxAttempts: 4,
		BaseDelay:   100 * time.Millisecond,
		MaxDelay:    250 * time.Millisecond,
		Rand: func(max time.Duration) time.Duration {
			jitterInputs = append(jitterInputs, max)
			return max
		},
		Sleep: func(_ context.Context, d time.Duration) error {
			slept = append(slept, d)
			return nil
		},
	}

	_, err := provider.GetHistory(context.Background(), "AAPL", "1mo", "1d")
	if err == nil {
		t.Fatal("expected final yahoo error")
	}
	if calls != 4 {
		t.Fatalf("expected exactly 4 bounded attempts, got %d", calls)
	}
	want := []time.Duration{100 * time.Millisecond, 200 * time.Millisecond, 250 * time.Millisecond}
	if !sameDurations(jitterInputs, want) {
		t.Fatalf("expected jitter upper bounds %v, got %v", want, jitterInputs)
	}
	if !sameDurations(slept, want) {
		t.Fatalf("expected sleeps %v, got %v", want, slept)
	}
}

func TestYahooProvider_GetHistoryDoesNotRetryContextCancellation(t *testing.T) {
	var calls int
	provider := NewYahooProvider(1)
	provider.limiter = rate.NewLimiter(rate.Inf, 0)
	provider.history = func(_ string, _ yfa.HistoryQuery) (map[string]yfa.PriceData, error) {
		calls++
		return nil, fmt.Errorf("wrapped cancel: %w", context.Canceled)
	}
	provider.retry = yahooRetryConfig{
		MaxAttempts: 3,
		BaseDelay:   time.Millisecond,
		MaxDelay:    time.Millisecond,
		Sleep: func(context.Context, time.Duration) error {
			t.Fatal("context cancellation must not sleep or retry")
			return nil
		},
		Rand: func(time.Duration) time.Duration { return 0 },
	}

	_, err := provider.GetHistory(context.Background(), "AAPL", "1mo", "1d")
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected wrapped context canceled error, got %v", err)
	}
	if calls != 1 {
		t.Fatalf("expected one attempt, got %d", calls)
	}
}

func TestYahooProvider_GetQuoteDoesNotRetryCircuitOpen(t *testing.T) {
	var calls int
	provider := NewYahooProvider(1)
	provider.limiter = rate.NewLimiter(rate.Inf, 0)
	provider.quote = func(_ string) (yfa.PriceData, error) {
		calls++
		return yfa.PriceData{}, ErrCircuitOpen
	}
	provider.retry = yahooRetryConfig{
		MaxAttempts: 3,
		BaseDelay:   time.Millisecond,
		MaxDelay:    time.Millisecond,
		Sleep: func(context.Context, time.Duration) error {
			t.Fatal("circuit-open errors must not sleep or retry")
			return nil
		},
		Rand: func(time.Duration) time.Duration { return 0 },
	}

	_, err := provider.GetQuote(context.Background(), "AAPL")
	if !errors.Is(err, ErrCircuitOpen) {
		t.Fatalf("expected circuit-open error, got %v", err)
	}
	if calls != 1 {
		t.Fatalf("expected one attempt, got %d", calls)
	}
}

func TestYahooProvider_GetQuoteRetriesTemporaryNetworkError(t *testing.T) {
	var calls int
	provider := NewYahooProvider(1)
	provider.limiter = rate.NewLimiter(rate.Inf, 0)
	provider.quote = func(_ string) (yfa.PriceData, error) {
		calls++
		if calls == 1 {
			return yfa.PriceData{}, temporaryNetError{msg: "temporary lookup failure"}
		}
		return yfa.PriceData{Close: 101, Volume: 2000}, nil
	}
	provider.retry = yahooRetryConfig{
		MaxAttempts: 2,
		BaseDelay:   time.Millisecond,
		MaxDelay:    time.Millisecond,
		Sleep:       func(context.Context, time.Duration) error { return nil },
		Rand:        func(time.Duration) time.Duration { return 0 },
	}

	quote, err := provider.GetQuote(context.Background(), "AAPL")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if calls != 2 {
		t.Fatalf("expected 2 quote attempts, got %d", calls)
	}
	if quote.Price != 101 || quote.Volume != 2000 {
		t.Fatalf("unexpected quote: %#v", quote)
	}
}

type temporaryNetError struct {
	msg string
}

func (e temporaryNetError) Error() string   { return e.msg }
func (e temporaryNetError) Timeout() bool   { return false }
func (e temporaryNetError) Temporary() bool { return true }

func sameDurations(got, want []time.Duration) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}
