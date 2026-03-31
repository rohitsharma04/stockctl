package screener

import (
	"math"
	"testing"
	"time"

	"github.com/rohitsharma04/stockctl/internal/marketdata"
)

func TestComputeBreadth_Empty(t *testing.T) {
	b := ComputeBreadth(nil)
	if b.RegimeLabel != "insufficient_data" {
		t.Errorf("expected insufficient_data for empty input, got %q", b.RegimeLabel)
	}
	if b.TotalScanned != 0 {
		t.Errorf("expected 0 scanned, got %d", b.TotalScanned)
	}
}

func TestComputeBreadth_SingleTicker(t *testing.T) {
	tickers := []TickerBreadthData{
		{Ticker: "AAPL", Score: 1.0, FullPass: true, AboveSMA50: true, AboveSMA200: true, NewHigh: true},
	}
	b := ComputeBreadth(tickers)
	if b.TotalScanned != 1 {
		t.Errorf("expected 1 scanned, got %d", b.TotalScanned)
	}
	if b.FullPasses != 1 {
		t.Errorf("expected 1 full pass, got %d", b.FullPasses)
	}
	if b.PassRate != 1.0 {
		t.Errorf("expected pass rate 1.0, got %f", b.PassRate)
	}
	if b.MedianScore != 1.0 {
		t.Errorf("expected median score 1.0, got %f", b.MedianScore)
	}
}

func TestComputeBreadth_MixedResults(t *testing.T) {
	tickers := []TickerBreadthData{
		{Ticker: "A", Score: 1.0, FullPass: true, AboveSMA50: true, AboveSMA200: true, NewHigh: true},
		{Ticker: "B", Score: 0.8, FullPass: false, AboveSMA50: true, AboveSMA200: true},
		{Ticker: "C", Score: 0.5, FullPass: false, AboveSMA50: false, AboveSMA200: true},
		{Ticker: "D", Score: 0.2, FullPass: false, AboveSMA50: false, AboveSMA200: false, NewLow: true},
	}
	b := ComputeBreadth(tickers)

	if b.TotalScanned != 4 {
		t.Errorf("expected 4 scanned, got %d", b.TotalScanned)
	}
	if b.FullPasses != 1 {
		t.Errorf("expected 1 full pass, got %d", b.FullPasses)
	}
	if b.NearMisses != 1 { // score >= 0.67 but not full pass
		t.Errorf("expected 1 near miss, got %d", b.NearMisses)
	}
	if b.NewHighs != 1 {
		t.Errorf("expected 1 new high, got %d", b.NewHighs)
	}
	if b.NewLows != 1 {
		t.Errorf("expected 1 new low, got %d", b.NewLows)
	}
	if b.AboveSMA50Pct != 0.5 {
		t.Errorf("expected above SMA50 pct 0.5, got %f", b.AboveSMA50Pct)
	}
	if b.AboveSMA200Pct != 0.75 {
		t.Errorf("expected above SMA200 pct 0.75, got %f", b.AboveSMA200Pct)
	}
	// Median of [0.2, 0.5, 0.8, 1.0] = (0.5+0.8)/2 = 0.65
	if b.MedianScore < 0.64 || b.MedianScore > 0.66 {
		t.Errorf("expected median ~0.65, got %f", b.MedianScore)
	}
}

func TestClassifyRegime_BroadRiskOn(t *testing.T) {
	b := BreadthSummary{
		TotalScanned:   100,
		FullPasses:     10,
		PassRate:       0.10,
		AboveSMA50Pct:  0.70,
		AboveSMA200Pct: 0.60,
	}
	label := ClassifyRegime(b)
	if label != "broad_risk_on" {
		t.Errorf("expected broad_risk_on, got %q", label)
	}
}

func TestClassifyRegime_NarrowLeadership(t *testing.T) {
	b := BreadthSummary{
		TotalScanned:   100,
		FullPasses:     3,
		PassRate:       0.03,
		AboveSMA50Pct:  0.30,
		AboveSMA200Pct: 0.40,
	}
	label := ClassifyRegime(b)
	if label != "narrow_leadership" {
		t.Errorf("expected narrow_leadership, got %q", label)
	}
}

func TestClassifyRegime_RiskOff(t *testing.T) {
	b := BreadthSummary{
		TotalScanned:   100,
		FullPasses:     0,
		PassRate:       0.0,
		AboveSMA50Pct:  0.20,
		AboveSMA200Pct: 0.30,
	}
	label := ClassifyRegime(b)
	if label != "risk_off" {
		t.Errorf("expected risk_off, got %q", label)
	}
}

func TestClassifyRegime_Mixed(t *testing.T) {
	b := BreadthSummary{
		TotalScanned:   100,
		FullPasses:     0,
		PassRate:       0.0,
		AboveSMA50Pct:  0.50,
		AboveSMA200Pct: 0.40,
	}
	label := ClassifyRegime(b)
	if label != "mixed" {
		t.Errorf("expected mixed, got %q", label)
	}
}

func TestComputeBenchmarkStatus_InsufficientData(t *testing.T) {
	data := make([]marketdata.OHLCV, 100) // need 201
	bs := ComputeBenchmarkStatus("^GSPC", data)
	if bs.TrendLabel != "insufficient_data" {
		t.Errorf("expected insufficient_data, got %q", bs.TrendLabel)
	}
}

func TestComputeBenchmarkStatus_Uptrend(t *testing.T) {
	// Create synthetic data: steadily rising
	data := make([]marketdata.OHLCV, 300)
	price := 100.0
	start := time.Date(2021, 1, 4, 0, 0, 0, 0, time.UTC)
	for i := range data {
		data[i] = marketdata.OHLCV{
			Date: start.AddDate(0, 0, i),
			Close: price,
			High: price * 1.01,
			Low: price * 0.99,
			Volume: 1000000,
		}
		price *= 1.001
	}
	bs := ComputeBenchmarkStatus("^GSPC", data)
	if bs.TrendLabel != "uptrend" {
		t.Errorf("expected uptrend for rising data, got %q", bs.TrendLabel)
	}
	if !bs.AboveSMA50 {
		t.Error("expected above SMA50 in uptrend")
	}
	if !bs.AboveSMA200 {
		t.Error("expected above SMA200 in uptrend")
	}
}

func TestComputeSectorBreadth_Empty(t *testing.T) {
	result := ComputeSectorBreadth(nil)
	if len(result) != 0 {
		t.Errorf("expected empty result, got %d sectors", len(result))
	}
}

func TestComputeSectorBreadth_GroupsCorrectly(t *testing.T) {
	tickers := []TickerBreadthData{
		{Ticker: "A", Score: 1.0, FullPass: true, Sector: "Technology"},
		{Ticker: "B", Score: 0.5, FullPass: false, Sector: "Technology"},
		{Ticker: "C", Score: 1.0, FullPass: true, Sector: "Healthcare"},
		{Ticker: "D", Score: 0.0, FullPass: false, Sector: ""},
	}
	result := ComputeSectorBreadth(tickers)

	if len(result) != 3 {
		t.Fatalf("expected 3 sectors (Tech, Healthcare, Unknown), got %d", len(result))
	}

	// Result is sorted by pass rate descending
	// Healthcare: 1/1 = 100%, Technology: 1/2 = 50%, Unknown: 0/1 = 0%
	if result[0].Sector != "Healthcare" {
		t.Errorf("expected Healthcare first (highest pass rate), got %q", result[0].Sector)
	}
	if result[0].Passes != 1 || result[0].Tickers != 1 {
		t.Errorf("Healthcare: expected 1/1, got %d/%d", result[0].Passes, result[0].Tickers)
	}
	if result[1].Sector != "Technology" {
		t.Errorf("expected Technology second, got %q", result[1].Sector)
	}
	if result[1].Passes != 1 || result[1].Tickers != 2 {
		t.Errorf("Technology: expected 1/2, got %d/%d", result[1].Passes, result[1].Tickers)
	}
}

func TestMakeFilter_NaN_ForcesUnknown(t *testing.T) {
	f := MakeFilter("test", true, nan(), 1.0, ImportanceCritical, "test NaN")
	if f.Status != StatusUnknown {
		t.Errorf("expected status unknown when value is NaN, got %q", f.Status)
	}
	if f.Pass {
		t.Error("expected pass=false when value is NaN")
	}
	if f.Value != 0 {
		t.Errorf("expected sanitized value 0, got %f", f.Value)
	}
}

func TestMakeUnknownFilter(t *testing.T) {
	f := MakeUnknownFilter("benchmark_missing", ImportanceMajor, "no data")
	if f.Status != StatusUnknown {
		t.Errorf("expected unknown, got %q", f.Status)
	}
	if f.Pass {
		t.Error("expected pass=false")
	}
	if f.Importance != ImportanceMajor {
		t.Errorf("expected major importance, got %q", f.Importance)
	}
}

func nan() float64 {
	return math.NaN()
}
