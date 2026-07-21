package marketdata

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// CircuitState represents the current state of the circuit breaker.
type CircuitState int

const (
	CircuitClosed   CircuitState = iota // Normal operation
	CircuitOpen                         // Failing, reject requests
	CircuitHalfOpen                     // Probing with a single request
)

func (s CircuitState) String() string {
	switch s {
	case CircuitClosed:
		return "closed"
	case CircuitOpen:
		return "open"
	case CircuitHalfOpen:
		return "half-open"
	default:
		return "unknown"
	}
}

// CircuitBreakerConfig holds the tuning parameters for the circuit breaker.
type CircuitBreakerConfig struct {
	MaxFailures    int
	CooldownPeriod time.Duration
}

func DefaultCircuitBreakerConfig() CircuitBreakerConfig {
	return CircuitBreakerConfig{MaxFailures: 5, CooldownPeriod: 30 * time.Second}
}

// CircuitBreakerProvider wraps a Provider with circuit breaker logic.
type CircuitBreakerProvider struct {
	inner  Provider
	config CircuitBreakerConfig

	mu               sync.Mutex
	state            CircuitState
	failures         int
	lastFailure      time.Time
	halfOpenInFlight bool
}

func NewCircuitBreakerProvider(inner Provider, cfg CircuitBreakerConfig) *CircuitBreakerProvider {
	if cfg.MaxFailures <= 0 {
		cfg.MaxFailures = 5
	}
	if cfg.CooldownPeriod <= 0 {
		cfg.CooldownPeriod = 30 * time.Second
	}
	return &CircuitBreakerProvider{inner: inner, config: cfg, state: CircuitClosed}
}

var ErrCircuitOpen = fmt.Errorf("circuit breaker is open: too many consecutive failures")

func (cb *CircuitBreakerProvider) State() CircuitState {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	return cb.state
}

func (cb *CircuitBreakerProvider) Failures() int {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	return cb.failures
}

// allowRequest atomically reserves the sole half-open probe. The returned bool
// identifies that reservation so completion cannot be confused with an older
// closed-state request completing concurrently.
func (cb *CircuitBreakerProvider) allowRequest() (allowed, probe bool) {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	switch cb.state {
	case CircuitClosed:
		return true, false
	case CircuitOpen:
		if time.Since(cb.lastFailure) < cb.config.CooldownPeriod {
			return false, false
		}
		cb.state = CircuitHalfOpen
		cb.halfOpenInFlight = true
		return true, true
	case CircuitHalfOpen:
		return false, false
	default:
		return false, false
	}
}

func (cb *CircuitBreakerProvider) recordSuccess(probe bool) {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	if probe {
		// Only the reserved probe may close a half-open circuit.
		if cb.state != CircuitHalfOpen || !cb.halfOpenInFlight {
			return
		}
		cb.halfOpenInFlight = false
		cb.state = CircuitClosed
		cb.failures = 0
		return
	}
	// Do not let an older normal request interfere with a live probe.
	if cb.state == CircuitHalfOpen {
		return
	}
	cb.state = CircuitClosed
	cb.failures = 0
}

func (cb *CircuitBreakerProvider) recordFailure(probe bool) {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	if probe {
		if cb.state != CircuitHalfOpen || !cb.halfOpenInFlight {
			return
		}
		cb.halfOpenInFlight = false
		cb.state = CircuitOpen
		cb.lastFailure = time.Now()
		return
	}
	// A request started while closed must not alter a half-open probe.
	if cb.state == CircuitHalfOpen {
		return
	}
	cb.failures++
	cb.lastFailure = time.Now()
	if cb.failures >= cb.config.MaxFailures {
		cb.state = CircuitOpen
	}
}

func (cb *CircuitBreakerProvider) circuitOpenError() error {
	cb.mu.Lock()
	remaining := cb.config.CooldownPeriod - time.Since(cb.lastFailure)
	cb.mu.Unlock()
	return fmt.Errorf("%w (cooldown %s remaining)", ErrCircuitOpen, remaining.Truncate(time.Second))
}

func (cb *CircuitBreakerProvider) GetHistory(ctx context.Context, symbol, period, interval string) ([]OHLCV, error) {
	allowed, probe := cb.allowRequest()
	if !allowed {
		return nil, cb.circuitOpenError()
	}
	data, err := cb.inner.GetHistory(ctx, symbol, period, interval)
	if err != nil {
		cb.recordFailure(probe)
		return nil, err
	}
	cb.recordSuccess(probe)
	return data, nil
}

func (cb *CircuitBreakerProvider) GetQuote(ctx context.Context, symbol string) (*Quote, error) {
	allowed, probe := cb.allowRequest()
	if !allowed {
		return nil, cb.circuitOpenError()
	}
	quote, err := cb.inner.GetQuote(ctx, symbol)
	if err != nil {
		cb.recordFailure(probe)
		return nil, err
	}
	cb.recordSuccess(probe)
	return quote, nil
}
