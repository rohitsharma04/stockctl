package screener

import (
	"context"
	"fmt"
	"math"

	"github.com/rohitsharma04/stockctl/internal/config"
	"github.com/rohitsharma04/stockctl/internal/indicators"
	"github.com/rohitsharma04/stockctl/internal/marketdata"
)

// StellarBreakout screens for volume-confirmed breakouts with Heikin-Ashi
// trend confirmation and bull consolidation patterns.
type StellarBreakout struct {
	cfg config.ScreenerConfig
}

func NewStellarBreakout(cfg config.ScreenerConfig) *StellarBreakout {
	if cfg.FibonacciLevel == 0 {
		cfg.FibonacciLevel = 0.618
	}
	if cfg.VolumeSignificance == 0 {
		cfg.VolumeSignificance = 0.3
	}
	if cfg.VolumeExplosionRatio == 0 {
		cfg.VolumeExplosionRatio = 0.5
	}
	if cfg.RecentWeeks == 0 {
		cfg.RecentWeeks = 5
	}
	if cfg.VolumeMultiplier == 0 {
		cfg.VolumeMultiplier = 1.5
	}
	if cfg.MinDataDays == 0 {
		cfg.MinDataDays = 756
	}
	return &StellarBreakout{cfg: cfg}
}

func (s *StellarBreakout) Name() string        { return "stellar-breakout" }
func (s *StellarBreakout) Description() string  { return "Volume explosion + Heikin-Ashi confirmation" }

func (s *StellarBreakout) Screen(_ context.Context, data []marketdata.OHLCV, _ []marketdata.OHLCV) (*ScreenResult, error) {
	if len(data) < s.cfg.MinDataDays {
		return &ScreenResult{Pass: false, Score: 0, Filters: []FilterResult{
			{Name: "min_data", Pass: false, Value: float64(len(data)), Threshold: float64(s.cfg.MinDataDays)},
		}}, nil
	}

	closes := marketdata.Closes(data)
	opens := marketdata.Opens(data)
	highs := marketdata.Highs(data)
	lows := marketdata.Lows(data)
	n := len(closes)
	var filters []FilterResult

	// 1. Min price
	filters = append(filters, FilterResult{
		Name: "min_price", Pass: closes[n-1] > 5.0, Value: closes[n-1], Threshold: 5.0,
	})

	// Resample to weekly
	weekly := marketdata.ToWeekly(data)
	wn := len(weekly)
	if wn < 55 {
		return &ScreenResult{Pass: false, Score: 0, Filters: []FilterResult{
			{Name: "min_weekly_data", Pass: false, Value: float64(wn), Threshold: 55},
		}}, nil
	}

	// 2. Volume explosion: recent 5-week max > 50% of 3-year max (excl last 3 weeks)
	volExpPass, volExpRatio := s.checkVolumeCondition(weekly)
	filters = append(filters, FilterResult{
		Name: "volume_explosion", Pass: volExpPass, Value: volExpRatio, Threshold: s.cfg.VolumeExplosionRatio,
		Detail: fmt.Sprintf("recent/historical ratio %.2f vs %.2f required", volExpRatio, s.cfg.VolumeExplosionRatio),
	})

	// 3. Fibonacci proximity: close 2 weeks ago > 61.8% of 52-week high
	fibPass, fibRatio := s.checkCloseCondition(weekly)
	filters = append(filters, FilterResult{
		Name: "fibonacci_proximity", Pass: fibPass, Value: fibRatio, Threshold: s.cfg.FibonacciLevel,
		Detail: fmt.Sprintf("close ratio %.2f vs %.2f threshold", fibRatio, s.cfg.FibonacciLevel),
	})

	// 4. Bull consolidation: up week → down week (lower volume, close holds)
	consPass := s.checkConsolidation(weekly)
	filters = append(filters, FilterResult{
		Name: "bull_consolidation", Pass: consPass, Value: 1, Threshold: 1,
		Detail: "up week → down week with lower volume and held close",
	})

	// 5. Heikin-Ashi bullish: HA Close >= HA Open today
	haOpen, haClose := indicators.HeikinAshi(opens, highs, lows, closes)
	haPass := haClose[n-1] >= haOpen[n-1]
	filters = append(filters, FilterResult{
		Name: "heikinashi_bullish", Pass: haPass, Value: haClose[n-1], Threshold: haOpen[n-1],
		Detail: fmt.Sprintf("HA close=%.2f vs HA open=%.2f", haClose[n-1], haOpen[n-1]),
	})

	// 6. Volume significance: 5-week avg >= 30% of historical max
	sigPass, sigRatio := s.checkRecentVolumeSignificance(weekly)
	filters = append(filters, FilterResult{
		Name: "volume_significance", Pass: sigPass, Value: sigRatio, Threshold: s.cfg.VolumeSignificance,
		Detail: fmt.Sprintf("avg/max ratio %.2f vs %.2f required", sigRatio, s.cfg.VolumeSignificance),
	})

	return NewScreenResult(filters), nil
}

func (s *StellarBreakout) checkVolumeCondition(weekly []marketdata.WeeklyBar) (bool, float64) {
	wn := len(weekly)
	recentWeeks := s.cfg.RecentWeeks
	if wn < recentWeeks+3 {
		return false, 0
	}

	maxRecentVol := 0.0
	for i := wn - recentWeeks; i < wn; i++ {
		if weekly[i].Volume > maxRecentVol {
			maxRecentVol = weekly[i].Volume
		}
	}

	maxHistVol := 0.0
	for i := 0; i < wn-3; i++ {
		if weekly[i].Volume > maxHistVol {
			maxHistVol = weekly[i].Volume
		}
	}

	ratio := 0.0
	if maxHistVol > 0 {
		ratio = maxRecentVol / maxHistVol
	}
	return maxRecentVol > maxHistVol*s.cfg.VolumeExplosionRatio, ratio
}

func (s *StellarBreakout) checkCloseCondition(weekly []marketdata.WeeklyBar) (bool, float64) {
	wn := len(weekly)
	if wn < 55 {
		return false, 0
	}

	close2WeeksAgo := weekly[wn-3].Close

	maxClose := 0.0
	start := wn - 55
	if start < 0 {
		start = 0
	}
	for i := start; i < wn-3; i++ {
		if weekly[i].Close > maxClose {
			maxClose = weekly[i].Close
		}
	}

	ratio := 0.0
	if maxClose > 0 {
		ratio = close2WeeksAgo / maxClose
	}
	return close2WeeksAgo > maxClose*s.cfg.FibonacciLevel, ratio
}

func (s *StellarBreakout) checkConsolidation(weekly []marketdata.WeeklyBar) bool {
	wn := len(weekly)
	if wn < 4 {
		return false
	}

	twoWeeksAgo := weekly[wn-3]
	oneWeekAgo := weekly[wn-2]
	prevClose := weekly[wn-4].Close

	pctTwoWeeks := 0.0
	if prevClose != 0 {
		pctTwoWeeks = (twoWeeksAgo.Close - prevClose) / prevClose
	}

	pctOneWeek := 0.0
	if twoWeeksAgo.Close != 0 {
		pctOneWeek = (oneWeekAgo.Close - twoWeeksAgo.Close) / twoWeeksAgo.Close
	}

	return pctTwoWeeks > 0 && pctOneWeek < 0 &&
		oneWeekAgo.Volume < twoWeeksAgo.Volume &&
		twoWeeksAgo.Open < oneWeekAgo.Close
}

func (s *StellarBreakout) checkRecentVolumeSignificance(weekly []marketdata.WeeklyBar) (bool, float64) {
	wn := len(weekly)
	recentWeeks := s.cfg.RecentWeeks
	if wn < recentWeeks+3 {
		return false, 0
	}

	totalVol := 0.0
	for i := wn - recentWeeks; i < wn; i++ {
		totalVol += weekly[i].Volume
	}
	avgRecent := totalVol / float64(recentWeeks)

	maxHistVol := 0.0
	for i := 0; i < wn-3; i++ {
		if weekly[i].Volume > maxHistVol {
			maxHistVol = weekly[i].Volume
		}
	}

	if maxHistVol == 0 || math.IsNaN(maxHistVol) {
		return false, 0
	}

	ratio := avgRecent / maxHistVol
	return avgRecent >= s.cfg.VolumeSignificance*maxHistVol, ratio
}
