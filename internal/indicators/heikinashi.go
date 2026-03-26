package indicators

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
