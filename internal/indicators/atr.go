package indicators

import "math"

// ATR calculates the Average True Range.
func ATR(highs, lows, closes []float64, period int) []float64 {
	n := len(closes)
	if n == 0 {
		return nil
	}

	tr := make([]float64, n)
	tr[0] = highs[0] - lows[0]

	for i := 1; i < n; i++ {
		hl := highs[i] - lows[i]
		hc := math.Abs(highs[i] - closes[i-1])
		lc := math.Abs(lows[i] - closes[i-1])
		tr[i] = math.Max(hl, math.Max(hc, lc))
	}

	return SMA(tr, period)
}
