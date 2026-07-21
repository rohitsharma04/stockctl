package screener

import (
	"testing"

	"github.com/rohitsharma04/stockctl/internal/config"
)

func TestNewDescendingBreakoutNormalizesUnsafeMonthWindow(t *testing.T) {
	for _, months := range []int{-1, 0, 1} {
		screener := NewDescendingBreakout(config.ScreenerConfig{Months: months}, config.ScoringConfig{})
		if screener.cfg.Months != 36 {
			t.Fatalf("months=%d created unsafe month window %d, want default 36", months, screener.cfg.Months)
		}
	}
}
