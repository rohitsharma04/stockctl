package pairs

import (
	"math"

	"github.com/rohitsharma04/stockctl/internal/indicators"
)

// CorrelationMatrix builds a correlation matrix from multiple return series.
func CorrelationMatrix(returns [][]float64) [][]float64 {
	n := len(returns)
	matrix := make([][]float64, n)
	for i := range matrix {
		matrix[i] = make([]float64, n)
	}

	for i := 0; i < n; i++ {
		for j := i; j < n; j++ {
			corr := pearsonCorrelation(returns[i], returns[j])
			matrix[i][j] = corr
			matrix[j][i] = corr
		}
	}
	return matrix
}

// CorrelatedPair represents a pair of stocks with strong correlation.
type CorrelatedPair struct {
	Stock1      string
	Stock2      string
	Correlation float64
}

// FindCorrelatedPairs finds pairs with correlation above a threshold.
func FindCorrelatedPairs(symbols []string, returns [][]float64, threshold float64) []CorrelatedPair {
	matrix := CorrelationMatrix(returns)
	var pairs []CorrelatedPair

	for i := 0; i < len(symbols); i++ {
		for j := i + 1; j < len(symbols); j++ {
			if matrix[i][j] > threshold {
				pairs = append(pairs, CorrelatedPair{
					Stock1:      symbols[i],
					Stock2:      symbols[j],
					Correlation: matrix[i][j],
				})
			}
		}
	}
	return pairs
}

func pearsonCorrelation(x, y []float64) float64 {
	n := len(x)
	if n == 0 || len(y) != n {
		return 0
	}

	// Filter NaN pairs
	var xClean, yClean []float64
	for i := 0; i < n; i++ {
		if !math.IsNaN(x[i]) && !math.IsNaN(y[i]) {
			xClean = append(xClean, x[i])
			yClean = append(yClean, y[i])
		}
	}

	if len(xClean) < 3 {
		return 0
	}

	mx := indicators.Mean(xClean)
	my := indicators.Mean(yClean)

	var sumXY, sumXX, sumYY float64
	for i := range xClean {
		dx := xClean[i] - mx
		dy := yClean[i] - my
		sumXY += dx * dy
		sumXX += dx * dx
		sumYY += dy * dy
	}

	denom := math.Sqrt(sumXX * sumYY)
	if denom == 0 {
		return 0
	}
	return sumXY / denom
}
