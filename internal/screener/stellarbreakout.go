package screener

import (
	"context"
	"math"

	"github.com/rohitsharma04/stockctl/internal/config"
	"github.com/rohitsharma04/stockctl/internal/indicators"
	"github.com/rohitsharma04/stockctl/internal/marketdata"
)

// StellarBreakout screens for volume-confirmed breakouts with Heikin-Ashi
// trend confirmation and bull consolidation patterns.
// Port of stellarbreakout.py.
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

func (s *StellarBreakout) Screen(_ context.Context, data []marketdata.OHLCV, _ []marketdata.OHLCV) (bool, error) {
	if len(data) < s.cfg.MinDataDays {
		return false, nil
	}

	closes := marketdata.Closes(data)
	opens := marketdata.Opens(data)
	highs := marketdata.Highs(data)
	lows := marketdata.Lows(data)
	n := len(closes)

	// Price > $5
	if closes[n-1] <= 5.0 {
		return false, nil
	}

	// Resample to weekly
	weekly := marketdata.ToWeekly(data)
	wn := len(weekly)
	if wn < 55 {
		return false, nil
	}

	// Check volume condition: recent 5-week max > 50% of 3-year max (excl last 3 weeks)
	if !s.checkVolumeCondition(weekly) {
		return false, nil
	}

	// Close 2 weeks ago > 61.8% of 52-week high
	if !s.checkCloseCondition(weekly) {
		return false, nil
	}

	// Bull consolidation: up week → down week (lower volume, close holds)
	if !s.checkConsolidation(weekly) {
		return false, nil
	}

	// Heikin-Ashi confirmation: HA Close >= HA Open today
	haOpen, haClose := indicators.HeikinAshi(opens, highs, lows, closes)
	if haClose[n-1] < haOpen[n-1] {
		return false, nil
	}

	// Recent volume significant: 5-week avg >= 30% of historical max
	if !s.checkRecentVolumeSignificance(weekly) {
		return false, nil
	}

	return true, nil
}

func (s *StellarBreakout) checkVolumeCondition(weekly []marketdata.WeeklyBar) bool {
	wn := len(weekly)
	recentWeeks := s.cfg.RecentWeeks
	if wn < recentWeeks+3 {
		return false
	}

	// Max volume of last 5 weeks
	maxRecentVol := 0.0
	for i := wn - recentWeeks; i < wn; i++ {
		if weekly[i].Volume > maxRecentVol {
			maxRecentVol = weekly[i].Volume
		}
	}

	// Max volume excluding last 3 weeks
	maxHistVol := 0.0
	for i := 0; i < wn-3; i++ {
		if weekly[i].Volume > maxHistVol {
			maxHistVol = weekly[i].Volume
		}
	}

	return maxRecentVol > maxHistVol*s.cfg.VolumeExplosionRatio
}

func (s *StellarBreakout) checkCloseCondition(weekly []marketdata.WeeklyBar) bool {
	wn := len(weekly)
	if wn < 55 {
		return false
	}

	// Close from 2 weeks ago
	close2WeeksAgo := weekly[wn-3].Close

	// 52-week range starting from 3 weeks ago
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

	return close2WeeksAgo > maxClose*s.cfg.FibonacciLevel
}

func (s *StellarBreakout) checkConsolidation(weekly []marketdata.WeeklyBar) bool {
	wn := len(weekly)
	if wn < 3 {
		return false
	}

	twoWeeksAgo := weekly[wn-3]
	oneWeekAgo := weekly[wn-2]

	// Calculate pct changes
	prevClose := 0.0
	if wn >= 4 {
		prevClose = weekly[wn-4].Close
	} else {
		return false
	}

	pctTwoWeeks := 0.0
	if prevClose != 0 {
		pctTwoWeeks = (twoWeeksAgo.Close - prevClose) / prevClose
	}

	pctOneWeek := 0.0
	if twoWeeksAgo.Close != 0 {
		pctOneWeek = (oneWeekAgo.Close - twoWeeksAgo.Close) / twoWeeksAgo.Close
	}

	// Up week followed by down week, lower volume, close holds above the up week's open
	condition1 := pctTwoWeeks > 0       // Two weeks ago was up
	condition2 := pctOneWeek < 0         // One week ago was down
	condition3 := oneWeekAgo.Volume < twoWeeksAgo.Volume // Lower volume
	condition4 := twoWeeksAgo.Open < oneWeekAgo.Close    // Close holds

	return condition1 && condition2 && condition3 && condition4
}

func (s *StellarBreakout) checkRecentVolumeSignificance(weekly []marketdata.WeeklyBar) bool {
	wn := len(weekly)
	recentWeeks := s.cfg.RecentWeeks
	if wn < recentWeeks+3 {
		return false
	}

	// 5-week average volume
	totalVol := 0.0
	for i := wn - recentWeeks; i < wn; i++ {
		totalVol += weekly[i].Volume
	}
	avgRecent := totalVol / float64(recentWeeks)

	// Historical max (excl last 3 weeks)
	maxHistVol := 0.0
	for i := 0; i < wn-3; i++ {
		if weekly[i].Volume > maxHistVol {
			maxHistVol = weekly[i].Volume
		}
	}

	if maxHistVol == 0 || math.IsNaN(maxHistVol) {
		return false
	}

	return avgRecent >= s.cfg.VolumeSignificance*maxHistVol
}
