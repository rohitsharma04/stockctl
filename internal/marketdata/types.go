package marketdata

import "time"

// OHLCV represents a single candlestick bar.
type OHLCV struct {
	Date   time.Time
	Open   float64
	High   float64
	Low    float64
	Close  float64
	Volume float64
}

// Quote represents a real-time stock quote.
type Quote struct {
	Symbol        string
	Price         float64
	Change        float64
	ChangePercent float64
	Volume        float64
	MarketCap     float64
}

// StockData holds complete data for a screener to evaluate.
type StockData struct {
	Symbol string
	Daily  []OHLCV
}

// WeeklyBar represents a weekly aggregated bar.
type WeeklyBar struct {
	Date   time.Time
	Open   float64
	High   float64
	Low    float64
	Close  float64
	Volume float64
}

// MonthlyBar represents a monthly aggregated bar.
type MonthlyBar struct {
	Date   time.Time
	Close  float64
	High   float64
	Low    float64
	Volume float64
}

// ToWeekly resamples daily OHLCV data into weekly bars.
func ToWeekly(daily []OHLCV) []WeeklyBar {
	if len(daily) == 0 {
		return nil
	}

	var weekly []WeeklyBar
	var current WeeklyBar
	var started bool

	for _, d := range daily {
		year, week := d.Date.ISOWeek()
		var curYear, curWeek int
		if started {
			curYear, curWeek = current.Date.ISOWeek()
		}

		if !started || year != curYear || week != curWeek {
			if started {
				weekly = append(weekly, current)
			}
			current = WeeklyBar{
				Date:   d.Date,
				Open:   d.Open,
				High:   d.High,
				Low:    d.Low,
				Close:  d.Close,
				Volume: d.Volume,
			}
			started = true
		} else {
			if d.High > current.High {
				current.High = d.High
			}
			if d.Low < current.Low {
				current.Low = d.Low
			}
			current.Close = d.Close
			current.Volume += d.Volume
		}
	}
	if started {
		weekly = append(weekly, current)
	}
	return weekly
}

// ToMonthly resamples daily OHLCV data into monthly bars.
func ToMonthly(daily []OHLCV) []MonthlyBar {
	if len(daily) == 0 {
		return nil
	}

	var monthly []MonthlyBar
	var current MonthlyBar
	var curMonth time.Month
	var curYear int
	var started bool

	for _, d := range daily {
		m := d.Date.Month()
		y := d.Date.Year()

		if !started || m != curMonth || y != curYear {
			if started {
				monthly = append(monthly, current)
			}
			current = MonthlyBar{
				Date:   d.Date,
				Close:  d.Close,
				High:   d.High,
				Low:    d.Low,
				Volume: d.Volume,
			}
			curMonth = m
			curYear = y
			started = true
		} else {
			if d.High > current.High {
				current.High = d.High
			}
			if d.Low < current.Low {
				current.Low = d.Low
			}
			current.Close = d.Close
			current.Volume += d.Volume
		}
	}
	if started {
		monthly = append(monthly, current)
	}
	return monthly
}

// Closes extracts a slice of closing prices from OHLCV data.
func Closes(data []OHLCV) []float64 {
	out := make([]float64, len(data))
	for i, d := range data {
		out[i] = d.Close
	}
	return out
}

// Highs extracts a slice of high prices from OHLCV data.
func Highs(data []OHLCV) []float64 {
	out := make([]float64, len(data))
	for i, d := range data {
		out[i] = d.High
	}
	return out
}

// Lows extracts a slice of low prices from OHLCV data.
func Lows(data []OHLCV) []float64 {
	out := make([]float64, len(data))
	for i, d := range data {
		out[i] = d.Low
	}
	return out
}

// Opens extracts a slice of opening prices from OHLCV data.
func Opens(data []OHLCV) []float64 {
	out := make([]float64, len(data))
	for i, d := range data {
		out[i] = d.Open
	}
	return out
}

// Volumes extracts a slice of volumes from OHLCV data.
func Volumes(data []OHLCV) []float64 {
	out := make([]float64, len(data))
	for i, d := range data {
		out[i] = d.Volume
	}
	return out
}
