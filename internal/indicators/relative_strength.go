package indicators

import "math"

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

	return RollingMean(ratio, period)
}
