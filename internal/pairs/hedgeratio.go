package pairs

// HedgeRatio calculates the OLS hedge ratio (beta) between two price series.
// y ≈ beta * x + alpha
// Uses simple OLS regression: beta = Cov(x,y) / Var(x)
func HedgeRatio(x, y []float64) float64 {
	n := len(x)
	if n == 0 || len(y) != n {
		return 0
	}

	sumX, sumY, sumXY, sumXX := 0.0, 0.0, 0.0, 0.0
	for i := 0; i < n; i++ {
		sumX += x[i]
		sumY += y[i]
		sumXY += x[i] * y[i]
		sumXX += x[i] * x[i]
	}

	fn := float64(n)
	denom := sumXX - (sumX*sumX)/fn
	if denom == 0 {
		return 0
	}
	return (sumXY - (sumX*sumY)/fn) / denom
}
