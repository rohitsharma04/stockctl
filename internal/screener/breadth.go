package screener

import (
	"math"
	"sort"

	"github.com/rohitsharma04/stockctl/internal/indicators"
	"github.com/rohitsharma04/stockctl/internal/marketdata"
)

// BreadthSummary aggregates breadth statistics across a scanned universe.
type BreadthSummary struct {
	TotalScanned   int     `json:"total_scanned"`
	FullPasses     int     `json:"full_passes"`
	NearMisses     int     `json:"near_misses"`       // score >= 0.67 but not full pass
	PassRate       float64 `json:"pass_rate"`
	MedianScore    float64 `json:"median_score"`
	AboveSMA50Pct  float64 `json:"above_sma50_pct"`
	AboveSMA200Pct float64 `json:"above_sma200_pct"`
	NewHighs       int     `json:"new_highs"`
	NewLows        int     `json:"new_lows"`
	RegimeLabel    string  `json:"regime_label"`       // broad_risk_on, narrow_leadership, mixed, risk_off
}

// BenchmarkStatus describes the current trend state of the benchmark index.
type BenchmarkStatus struct {
	Symbol      string  `json:"symbol"`
	Close       float64 `json:"close"`
	AboveSMA50  bool    `json:"above_sma50"`
	AboveSMA200 bool    `json:"above_sma200"`
	Momentum22D float64 `json:"momentum_22d"`
	TrendLabel  string  `json:"trend_label"`  // uptrend, downtrend, neutral
}

// SectorBreadth summarizes screening results for a single sector.
type SectorBreadth struct {
	Sector   string  `json:"sector"`
	Tickers  int     `json:"tickers"`
	Passes   int     `json:"passes"`
	PassRate float64 `json:"pass_rate"`
	AvgScore float64 `json:"avg_score"`
}

// MarketSummary provides a top-level market context block for scan output.
type MarketSummary struct {
	MarketID      string          `json:"market_id"`
	MarketName    string          `json:"market_name"`
	AsOfDate      string          `json:"as_of_date"`
	Benchmark     BenchmarkStatus `json:"benchmark"`
	Breadth       BreadthSummary  `json:"breadth"`
	SectorBreadth []SectorBreadth `json:"sector_breadth,omitempty"`
}

// TickerBreadthData holds the per-ticker data needed for breadth computation.
type TickerBreadthData struct {
	Ticker     string
	Score      float64
	FullPass   bool
	AboveSMA50  bool
	AboveSMA200 bool
	NewHigh    bool // 52-week high
	NewLow     bool // 52-week low
	Sector     string
}

// ComputeBreadth aggregates breadth stats from per-ticker data.
func ComputeBreadth(tickers []TickerBreadthData) BreadthSummary {
	n := len(tickers)
	if n == 0 {
		return BreadthSummary{RegimeLabel: "insufficient_data"}
	}

	b := BreadthSummary{TotalScanned: n}

	var scores []float64
	aboveSMA50 := 0
	aboveSMA200 := 0

	for _, t := range tickers {
		scores = append(scores, t.Score)
		if t.FullPass {
			b.FullPasses++
		} else if t.Score >= 0.67 {
			b.NearMisses++
		}
		if t.AboveSMA50 {
			aboveSMA50++
		}
		if t.AboveSMA200 {
			aboveSMA200++
		}
		if t.NewHigh {
			b.NewHighs++
		}
		if t.NewLow {
			b.NewLows++
		}
	}

	b.PassRate = float64(b.FullPasses) / float64(n)
	b.AboveSMA50Pct = float64(aboveSMA50) / float64(n)
	b.AboveSMA200Pct = float64(aboveSMA200) / float64(n)

	// Median score
	sort.Float64s(scores)
	if n%2 == 0 {
		b.MedianScore = (scores[n/2-1] + scores[n/2]) / 2
	} else {
		b.MedianScore = scores[n/2]
	}

	b.RegimeLabel = ClassifyRegime(b)
	return b
}

// ClassifyRegime assigns a market regime label based on breadth metrics.
func ClassifyRegime(b BreadthSummary) string {
	// Strong breadth: many passes, broad SMA participation
	if b.PassRate >= 0.05 && b.AboveSMA50Pct >= 0.60 && b.AboveSMA200Pct >= 0.50 {
		return "broad_risk_on"
	}
	// Some passes but narrow
	if b.FullPasses > 0 && b.AboveSMA50Pct < 0.40 {
		return "narrow_leadership"
	}
	// Very weak: no passes and poor breadth
	if b.FullPasses == 0 && b.AboveSMA50Pct < 0.30 {
		return "risk_off"
	}
	return "mixed"
}

// ComputeBenchmarkStatus calculates benchmark trend context.
func ComputeBenchmarkStatus(symbol string, data []marketdata.OHLCV) BenchmarkStatus {
	bs := BenchmarkStatus{Symbol: symbol}
	if len(data) < 201 {
		bs.TrendLabel = "insufficient_data"
		return bs
	}

	closes := marketdata.Closes(data)
	n := len(closes)
	bs.Close = closes[n-1]

	sma50 := indicators.SMA(closes, 50)
	sma200 := indicators.SMA(closes, 200)

	bs.AboveSMA50 = !math.IsNaN(sma50[n-1]) && closes[n-1] > sma50[n-1]
	bs.AboveSMA200 = !math.IsNaN(sma200[n-1]) && closes[n-1] > sma200[n-1]

	// 22-day momentum
	if n >= 23 {
		bs.Momentum22D = (closes[n-1] - closes[n-23]) / closes[n-23]
	}

	// Trend label
	switch {
	case bs.AboveSMA50 && bs.AboveSMA200 && bs.Momentum22D > 0:
		bs.TrendLabel = "uptrend"
	case !bs.AboveSMA50 && !bs.AboveSMA200:
		bs.TrendLabel = "downtrend"
	default:
		bs.TrendLabel = "neutral"
	}

	return bs
}

// ComputeSectorBreadth groups ticker breadth data by sector and summarizes.
func ComputeSectorBreadth(tickers []TickerBreadthData) []SectorBreadth {
	sectorMap := make(map[string]*SectorBreadth)

	for _, t := range tickers {
		sector := t.Sector
		if sector == "" {
			sector = "Unknown"
		}
		sb, ok := sectorMap[sector]
		if !ok {
			sb = &SectorBreadth{Sector: sector}
			sectorMap[sector] = sb
		}
		sb.Tickers++
		sb.AvgScore += t.Score
		if t.FullPass {
			sb.Passes++
		}
	}

	var result []SectorBreadth
	for _, sb := range sectorMap {
		if sb.Tickers > 0 {
			sb.AvgScore /= float64(sb.Tickers)
			sb.PassRate = float64(sb.Passes) / float64(sb.Tickers)
		}
		result = append(result, *sb)
	}

	sort.Slice(result, func(i, j int) bool {
		return result[i].PassRate > result[j].PassRate
	})

	return result
}
