package indicators

import "math"

// SMA calculates the Simple Moving Average for the given period.
// Returns a slice of the same length as input; values before the window are NaN.
func SMA(values []float64, period int) []float64 {
	n := len(values)
	result := make([]float64, n)
	if period <= 0 || n == 0 {
		return result
	}

	for i := 0; i < n; i++ {
		if i < period-1 {
			result[i] = math.NaN()
			continue
		}
		sum := 0.0
		for j := i - period + 1; j <= i; j++ {
			sum += values[j]
		}
		result[i] = sum / float64(period)
	}
	return result
}

// RollingStd calculates the rolling standard deviation for the given period.
func RollingStd(values []float64, period int) []float64 {
	n := len(values)
	result := make([]float64, n)
	sma := SMA(values, period)

	for i := 0; i < n; i++ {
		if i < period-1 {
			result[i] = math.NaN()
			continue
		}
		sumSq := 0.0
		for j := i - period + 1; j <= i; j++ {
			diff := values[j] - sma[i]
			sumSq += diff * diff
		}
		result[i] = math.Sqrt(sumSq / float64(period))
	}
	return result
}

// RollingMax returns the rolling maximum over the given window.
func RollingMax(values []float64, period int) []float64 {
	n := len(values)
	result := make([]float64, n)
	for i := 0; i < n; i++ {
		if i < period-1 {
			result[i] = math.NaN()
			continue
		}
		mx := math.Inf(-1)
		for j := i - period + 1; j <= i; j++ {
			if values[j] > mx {
				mx = values[j]
			}
		}
		result[i] = mx
	}
	return result
}

// RollingMin returns the rolling minimum over the given window.
func RollingMin(values []float64, period int) []float64 {
	n := len(values)
	result := make([]float64, n)
	for i := 0; i < n; i++ {
		if i < period-1 {
			result[i] = math.NaN()
			continue
		}
		mn := math.Inf(1)
		for j := i - period + 1; j <= i; j++ {
			if values[j] < mn {
				mn = values[j]
			}
		}
		result[i] = mn
	}
	return result
}

// RollingMean returns the rolling mean over the given window (alias for SMA).
func RollingMean(values []float64, period int) []float64 {
	return SMA(values, period)
}

// PctChange calculates the percentage change between consecutive values.
// Returns a slice of len(values); first element is NaN.
func PctChange(values []float64) []float64 {
	n := len(values)
	result := make([]float64, n)
	if n == 0 {
		return result
	}
	result[0] = math.NaN()
	for i := 1; i < n; i++ {
		if values[i-1] == 0 {
			result[i] = math.NaN()
		} else {
			result[i] = (values[i] - values[i-1]) / values[i-1]
		}
	}
	return result
}

// Max returns the max value in a slice.
func Max(values []float64) float64 {
	if len(values) == 0 {
		return math.NaN()
	}
	mx := values[0]
	for _, v := range values[1:] {
		if v > mx {
			mx = v
		}
	}
	return mx
}

// Min returns the min value in a slice.
func Min(values []float64) float64 {
	if len(values) == 0 {
		return math.NaN()
	}
	mn := values[0]
	for _, v := range values[1:] {
		if v < mn {
			mn = v
		}
	}
	return mn
}

// Mean returns the arithmetic mean of a slice.
func Mean(values []float64) float64 {
	if len(values) == 0 {
		return math.NaN()
	}
	sum := 0.0
	for _, v := range values {
		sum += v
	}
	return sum / float64(len(values))
}

// Std returns the population standard deviation of a slice.
func Std(values []float64) float64 {
	if len(values) == 0 {
		return math.NaN()
	}
	m := Mean(values)
	sumSq := 0.0
	for _, v := range values {
		diff := v - m
		sumSq += diff * diff
	}
	return math.Sqrt(sumSq / float64(len(values)))
}

// IsMonotonicallyIncreasing checks if all consecutive values increase.
func IsMonotonicallyIncreasing(values []float64) bool {
	for i := 1; i < len(values); i++ {
		if values[i] < values[i-1] {
			return false
		}
	}
	return true
}

// Tail returns the last n elements of a slice.
func Tail(values []float64, n int) []float64 {
	if n >= len(values) {
		return values
	}
	return values[len(values)-n:]
}
