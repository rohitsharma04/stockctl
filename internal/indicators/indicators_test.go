package indicators

import (
	"math"
	"testing"
)

func almostEqual(a, b, tol float64) bool {
	if math.IsNaN(a) && math.IsNaN(b) {
		return true
	}
	return math.Abs(a-b) < tol
}

func TestSMA(t *testing.T) {
	// Known dataset: SMA(3) of [1, 2, 3, 4, 5] = [NaN, NaN, 2, 3, 4]
	data := []float64{1, 2, 3, 4, 5}
	result := SMA(data, 3)

	if len(result) != 5 {
		t.Fatalf("expected 5 values, got %d", len(result))
	}
	if !math.IsNaN(result[0]) || !math.IsNaN(result[1]) {
		t.Errorf("first two values should be NaN, got %v, %v", result[0], result[1])
	}
	expected := []float64{2.0, 3.0, 4.0}
	for i, exp := range expected {
		if !almostEqual(result[i+2], exp, 0.001) {
			t.Errorf("SMA[%d] = %f, want %f", i+2, result[i+2], exp)
		}
	}
}

func TestSMAEmpty(t *testing.T) {
	result := SMA(nil, 5)
	if len(result) != 0 {
		t.Errorf("expected empty result for nil input, got %d", len(result))
	}
}

func TestBollingerBands(t *testing.T) {
	// 30 data points, period 20
	data := make([]float64, 30)
	for i := range data {
		data[i] = 100 + float64(i)
	}

	upper, middle, lower := BollingerBands(data, 20, 2.0)

	if len(upper) != 30 || len(middle) != 30 || len(lower) != 30 {
		t.Fatalf("expected 30 values each, got %d/%d/%d", len(upper), len(middle), len(lower))
	}

	// After the idle period (19), values should be valid
	for i := 19; i < 30; i++ {
		if math.IsNaN(upper[i]) {
			t.Errorf("upper[%d] should not be NaN", i)
		}
		if math.IsNaN(lower[i]) {
			t.Errorf("lower[%d] should not be NaN", i)
		}
		if upper[i] <= middle[i] {
			t.Errorf("upper[%d]=%f should be > middle=%f", i, upper[i], middle[i])
		}
		if lower[i] >= middle[i] {
			t.Errorf("lower[%d]=%f should be < middle=%f", i, lower[i], middle[i])
		}
	}
}

func TestATR(t *testing.T) {
	n := 20
	highs := make([]float64, n)
	lows := make([]float64, n)
	closes := make([]float64, n)
	for i := 0; i < n; i++ {
		closes[i] = 100 + float64(i)
		highs[i] = closes[i] + 2
		lows[i] = closes[i] - 2
	}

	result := ATR(highs, lows, closes, 5)
	if len(result) != n {
		t.Fatalf("expected %d values, got %d", n, len(result))
	}

	// After idle period, ATR should be roughly 4 (high-low range)
	for i := 6; i < n; i++ {
		if math.IsNaN(result[i]) {
			t.Errorf("ATR[%d] should not be NaN", i)
		}
		if result[i] < 3.5 || result[i] > 4.5 {
			t.Errorf("ATR[%d]=%f, expected ~4.0", i, result[i])
		}
	}
}

func TestHeikinAshi(t *testing.T) {
	opens := []float64{10, 11, 12}
	highs := []float64{12, 13, 14}
	lows := []float64{9, 10, 11}
	closes := []float64{11, 12, 13}

	haOpen, haClose := HeikinAshi(opens, highs, lows, closes)

	if len(haOpen) != 3 || len(haClose) != 3 {
		t.Fatalf("expected 3 values each")
	}

	// First HA close = (10+12+9+11)/4 = 10.5
	if !almostEqual(haClose[0], 10.5, 0.001) {
		t.Errorf("haClose[0] = %f, want 10.5", haClose[0])
	}

	// Second HA open = (11+12)/2 = 11.5  (prev open + prev close / 2)
	if !almostEqual(haOpen[1], 10.5, 0.001) {
		t.Errorf("haOpen[1] = %f, want 10.5", haOpen[1])
	}
}

func TestPctChange(t *testing.T) {
	data := []float64{100, 110, 99}
	result := PctChange(data)

	if !math.IsNaN(result[0]) {
		t.Error("first value should be NaN")
	}
	if !almostEqual(result[1], 0.10, 0.001) {
		t.Errorf("result[1] = %f, want 0.10", result[1])
	}
	if !almostEqual(result[2], -0.1, 0.001) {
		t.Errorf("result[2] = %f, want -0.1", result[2])
	}
}

func TestMax(t *testing.T) {
	if Max([]float64{3, 1, 4, 1, 5}) != 5 {
		t.Error("Max should be 5")
	}
}

func TestMin(t *testing.T) {
	if Min([]float64{3, 1, 4, 1, 5}) != 1 {
		t.Error("Min should be 1")
	}
}

func TestMean(t *testing.T) {
	if !almostEqual(Mean([]float64{2, 4, 6}), 4.0, 0.001) {
		t.Error("Mean should be 4.0")
	}
}

func TestStd(t *testing.T) {
	// Std of [2, 4, 6] = sqrt(8/3) ≈ 1.633
	if !almostEqual(Std([]float64{2, 4, 6}), 1.633, 0.01) {
		t.Errorf("Std = %f", Std([]float64{2, 4, 6}))
	}
}

func TestIsMonotonicallyIncreasing(t *testing.T) {
	if !IsMonotonicallyIncreasing([]float64{1, 2, 3, 4}) {
		t.Error("should be monotonically increasing")
	}
	if IsMonotonicallyIncreasing([]float64{1, 3, 2, 4}) {
		t.Error("should not be monotonically increasing")
	}
}

func TestTail(t *testing.T) {
	data := []float64{1, 2, 3, 4, 5}
	tail := Tail(data, 3)
	if len(tail) != 3 || tail[0] != 3 || tail[1] != 4 || tail[2] != 5 {
		t.Errorf("Tail(5, 3) = %v", tail)
	}
}
