package marketdata

import (
	"context"
	"fmt"
	"testing"
	"time"
)

// mockProvider is a test helper that returns success or failure on demand.
type mockProvider struct {
	shouldFail bool
	callCount  int
	data       []OHLCV
}

func (m *mockProvider) GetHistory(_ context.Context, symbol, period, interval string) ([]OHLCV, error) {
	m.callCount++
	if m.shouldFail {
		return nil, fmt.Errorf("mock API failure for %s", symbol)
	}
	return m.data, nil
}

func (m *mockProvider) GetQuote(_ context.Context, symbol string) (*Quote, error) {
	m.callCount++
	if m.shouldFail {
		return nil, fmt.Errorf("mock API failure for %s", symbol)
	}
	return &Quote{Symbol: symbol, Price: 100.0}, nil
}

func TestCircuitBreaker_ClosedState(t *testing.T) {
	mock := &mockProvider{data: []OHLCV{{Close: 100}}}
	cb := NewCircuitBreakerProvider(mock, DefaultCircuitBreakerConfig())

	data, err := cb.GetHistory(context.Background(), "AAPL", "1y", "1d")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(data) != 1 {
		t.Errorf("expected 1 bar, got %d", len(data))
	}
	if cb.State() != CircuitClosed {
		t.Errorf("expected closed state, got %s", cb.State())
	}
}

func TestCircuitBreaker_OpensAfterMaxFailures(t *testing.T) {
	mock := &mockProvider{shouldFail: true}
	cfg := CircuitBreakerConfig{MaxFailures: 3, CooldownPeriod: 1 * time.Second}
	cb := NewCircuitBreakerProvider(mock, cfg)

	// First 3 failures should go through, then circuit opens
	for i := 0; i < 3; i++ {
		_, err := cb.GetHistory(context.Background(), "AAPL", "1y", "1d")
		if err == nil {
			t.Fatalf("expected error on call %d", i+1)
		}
	}

	if cb.State() != CircuitOpen {
		t.Fatalf("expected open state after %d failures, got %s", cfg.MaxFailures, cb.State())
	}

	// Next call should be rejected immediately without hitting the API
	callsBefore := mock.callCount
	_, err := cb.GetHistory(context.Background(), "AAPL", "1y", "1d")
	if err == nil {
		t.Fatal("expected circuit open error")
	}
	if mock.callCount != callsBefore {
		t.Error("circuit open should not have called the inner provider")
	}
}

func TestCircuitBreaker_HalfOpenRecovery(t *testing.T) {
	mock := &mockProvider{shouldFail: true}
	cfg := CircuitBreakerConfig{MaxFailures: 2, CooldownPeriod: 50 * time.Millisecond}
	cb := NewCircuitBreakerProvider(mock, cfg)

	// Trip the circuit
	for i := 0; i < 2; i++ {
		cb.GetHistory(context.Background(), "AAPL", "1y", "1d")
	}
	if cb.State() != CircuitOpen {
		t.Fatal("expected open state")
	}

	// Wait for cooldown
	time.Sleep(60 * time.Millisecond)

	// Now fix the API
	mock.shouldFail = false
	mock.data = []OHLCV{{Close: 200}}

	// Probe request should succeed and close the circuit
	data, err := cb.GetHistory(context.Background(), "AAPL", "1y", "1d")
	if err != nil {
		t.Fatalf("probe should succeed: %v", err)
	}
	if len(data) != 1 || data[0].Close != 200 {
		t.Errorf("unexpected data: %v", data)
	}
	if cb.State() != CircuitClosed {
		t.Errorf("expected closed after successful probe, got %s", cb.State())
	}
}

func TestCircuitBreaker_HalfOpenFailureReopens(t *testing.T) {
	mock := &mockProvider{shouldFail: true}
	cfg := CircuitBreakerConfig{MaxFailures: 2, CooldownPeriod: 50 * time.Millisecond}
	cb := NewCircuitBreakerProvider(mock, cfg)

	// Trip the circuit
	for i := 0; i < 2; i++ {
		cb.GetHistory(context.Background(), "AAPL", "1y", "1d")
	}

	// Wait for cooldown
	time.Sleep(60 * time.Millisecond)

	// Probe fails — should reopen
	_, err := cb.GetHistory(context.Background(), "AAPL", "1y", "1d")
	if err == nil {
		t.Fatal("probe should fail")
	}
	if cb.State() != CircuitOpen {
		t.Errorf("expected open after failed probe, got %s", cb.State())
	}
}

func TestCircuitBreaker_SuccessResetsFailures(t *testing.T) {
	mock := &mockProvider{shouldFail: true}
	cfg := CircuitBreakerConfig{MaxFailures: 3, CooldownPeriod: 1 * time.Second}
	cb := NewCircuitBreakerProvider(mock, cfg)

	// 2 failures
	cb.GetHistory(context.Background(), "A", "1y", "1d")
	cb.GetHistory(context.Background(), "B", "1y", "1d")
	if cb.Failures() != 2 {
		t.Fatalf("expected 2 failures, got %d", cb.Failures())
	}

	// 1 success should reset
	mock.shouldFail = false
	mock.data = []OHLCV{{Close: 50}}
	cb.GetHistory(context.Background(), "C", "1y", "1d")
	if cb.Failures() != 0 {
		t.Errorf("expected 0 failures after success, got %d", cb.Failures())
	}
	if cb.State() != CircuitClosed {
		t.Errorf("expected closed, got %s", cb.State())
	}
}
