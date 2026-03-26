// Package indicators provides slice-based wrappers around cinar/indicator v2.
// This preserves the calling convention used by screeners while delegating
// all indicator math to the battle-tested cinar library.
package indicators

import (
	"math"

	"github.com/cinar/indicator/v2/helper"
	"github.com/cinar/indicator/v2/momentum"
	"github.com/cinar/indicator/v2/trend"
	"github.com/cinar/indicator/v2/volatility"
)

// SMA calculates the Simple Moving Average for the given period.
// Returns a slice of the same length as input; values before the window are NaN.
func SMA(values []float64, period int) []float64 {
	n := len(values)
	if n == 0 || period <= 0 {
		return make([]float64, n)
	}

	sma := trend.NewSmaWithPeriod[float64](period)
	result := helper.ChanToSlice(sma.Compute(helper.SliceToChan(values)))

	// Pad front with NaN to match input length (cinar skips idle period)
	idle := sma.IdlePeriod()
	out := make([]float64, n)
	for i := 0; i < idle && i < n; i++ {
		out[i] = math.NaN()
	}
	for i, v := range result {
		if i+idle < n {
			out[i+idle] = v
		}
	}
	return out
}

// RollingStd calculates the rolling standard deviation for the given period.
func RollingStd(values []float64, period int) []float64 {
	n := len(values)
	if n == 0 || period <= 0 {
		return make([]float64, n)
	}

	std := volatility.NewMovingStdWithPeriod[float64](period)
	result := helper.ChanToSlice(std.Compute(helper.SliceToChan(values)))

	idle := std.IdlePeriod()
	out := make([]float64, n)
	for i := 0; i < idle && i < n; i++ {
		out[i] = math.NaN()
	}
	for i, v := range result {
		if i+idle < n {
			out[i+idle] = v
		}
	}
	return out
}

// BollingerBands calculates Bollinger Bands with a configurable standard deviation multiplier.
// Returns upper, middle (SMA), and lower bands.
func BollingerBands(closes []float64, period int, stdDev float64) (upper, middle, lower []float64) {
	n := len(closes)
	upper = make([]float64, n)
	lower = make([]float64, n)
	middle = SMA(closes, period)
	std := RollingStd(closes, period)

	for i := 0; i < n; i++ {
		if math.IsNaN(middle[i]) {
			upper[i] = math.NaN()
			lower[i] = math.NaN()
		} else {
			upper[i] = middle[i] + stdDev*std[i]
			lower[i] = middle[i] - stdDev*std[i]
		}
	}
	return upper, middle, lower
}

// ATR calculates the Average True Range.
func ATR(highs, lows, closes []float64, period int) []float64 {
	n := len(closes)
	if n == 0 {
		return nil
	}

	atr := volatility.NewAtrWithPeriod[float64](period)
	result := helper.ChanToSlice(atr.Compute(
		helper.SliceToChan(highs),
		helper.SliceToChan(lows),
		helper.SliceToChan(closes),
	))

	// ATR idle period = MA period + 1 (for previous close)
	idle := atr.IdlePeriod()
	out := make([]float64, n)
	for i := 0; i < idle && i < n; i++ {
		out[i] = math.NaN()
	}
	for i, v := range result {
		if i+idle < n {
			out[i+idle] = v
		}
	}
	return out
}

// HeikinAshi calculates Heikin-Ashi open and close values.
// HA Close = (Open + High + Low + Close) / 4
// HA Open  = (prev_Open + prev_Close) / 2
func HeikinAshi(opens, highs, lows, closes []float64) (haOpen, haClose []float64) {
	n := len(closes)
	if n == 0 {
		return nil, nil
	}

	haOpen = make([]float64, n)
	haClose = make([]float64, n)

	// First bar
	haClose[0] = (opens[0] + highs[0] + lows[0] + closes[0]) / 4.0
	haOpen[0] = opens[0]

	for i := 1; i < n; i++ {
		haClose[i] = (opens[i] + highs[i] + lows[i] + closes[i]) / 4.0
		haOpen[i] = (opens[i-1] + closes[i-1]) / 2.0
	}
	return haOpen, haClose
}

// RelativeStrength calculates the rolling relative strength of a stock vs a benchmark.
// RS = rolling_mean(stock_returns / benchmark_returns, period)
func RelativeStrength(stockReturns, benchmarkReturns []float64, period int) []float64 {
	n := len(stockReturns)
	if n == 0 || len(benchmarkReturns) != n {
		return nil
	}

	ratio := make([]float64, n)
	for i := 0; i < n; i++ {
		if math.IsNaN(stockReturns[i]) || math.IsNaN(benchmarkReturns[i]) || benchmarkReturns[i] == 0 {
			ratio[i] = math.NaN()
		} else {
			ratio[i] = stockReturns[i] / benchmarkReturns[i]
		}
	}

	return SMA(ratio, period)
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

// RSI calculates the Relative Strength Index for the given period.
// Returns a slice of the same length as input; values before warmup are NaN.
func RSI(closes []float64, period int) []float64 {
	n := len(closes)
	if n == 0 || period <= 0 {
		return make([]float64, n)
	}

	rsi := momentum.NewRsiWithPeriod[float64](period)
	result := helper.ChanToSlice(rsi.Compute(helper.SliceToChan(closes)))

	idle := rsi.IdlePeriod()
	out := make([]float64, n)
	for i := 0; i < idle && i < n; i++ {
		out[i] = math.NaN()
	}
	for i, v := range result {
		if i+idle < n {
			out[i+idle] = v
		}
	}
	return out
}

// EMA calculates the Exponential Moving Average for the given period.
func EMA(values []float64, period int) []float64 {
	n := len(values)
	if n == 0 || period <= 0 {
		return make([]float64, n)
	}

	ema := trend.NewEmaWithPeriod[float64](period)
	result := helper.ChanToSlice(ema.Compute(helper.SliceToChan(values)))

	idle := ema.IdlePeriod()
	out := make([]float64, n)
	for i := 0; i < idle && i < n; i++ {
		out[i] = math.NaN()
	}
	for i, v := range result {
		if i+idle < n {
			out[i+idle] = v
		}
	}
	return out
}

// MACD calculates the MACD line and signal line.
// Returns (macdLine, signalLine) slices of the same length as input.
func MACD(closes []float64) ([]float64, []float64) {
	n := len(closes)
	if n == 0 {
		return make([]float64, n), make([]float64, n)
	}

	macd := trend.NewMacd[float64]()
	macdCh, signalCh := macd.Compute(helper.SliceToChan(closes))

	macdLine := helper.ChanToSlice(macdCh)
	signalLine := helper.ChanToSlice(signalCh)

	// The MACD idle period (26-1 + 9-1 = 34 for signal)
	macdIdle := macd.Ema2.IdlePeriod() // 25 (for MACD line)
	signalIdle := macdIdle + macd.Ema3.IdlePeriod() // 25 + 8 = 33 (for signal)

	macdOut := make([]float64, n)
	signalOut := make([]float64, n)
	for i := 0; i < n; i++ {
		macdOut[i] = math.NaN()
		signalOut[i] = math.NaN()
	}
	for i, v := range macdLine {
		if i+macdIdle < n {
			macdOut[i+macdIdle] = v
		}
	}
	for i, v := range signalLine {
		if i+signalIdle < n {
			signalOut[i+signalIdle] = v
		}
	}
	return macdOut, signalOut
}
