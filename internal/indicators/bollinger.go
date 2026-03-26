package indicators

import "math"

// BollingerBands calculates Bollinger Bands.
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
