package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"time"

	"github.com/rohitsharma04/stockctl/internal/config"
	"github.com/rohitsharma04/stockctl/internal/indicators"
	"github.com/rohitsharma04/stockctl/internal/marketdata"
	"github.com/rohitsharma04/stockctl/internal/output"
	"github.com/rohitsharma04/stockctl/internal/screener"
	"github.com/spf13/cobra"
)

var inspectCmd = &cobra.Command{
	Use:   "inspect <ticker>",
	Short: "Deep-analyze a single stock's technical indicators and screener results",
	Long: `Inspect provides a comprehensive technical analysis of a single stock,
including all indicator values and per-screener filter breakdown.`,
	Args: cobra.ExactArgs(1),
	RunE: runInspect,
}

func init() {
	rootCmd.AddCommand(inspectCmd)
}

type inspectResult struct {
	Ticker     string                `json:"ticker"`
	Market     string                `json:"market"`
	Date       string                `json:"date"`
	Price      priceInfo             `json:"price"`
	Indicators indicatorValues       `json:"indicators"`
	Screeners  []screenerInspection  `json:"screeners"`
}

type priceInfo struct {
	Close    float64 `json:"close"`
	Open     float64 `json:"open"`
	High     float64 `json:"high"`
	Low      float64 `json:"low"`
	Volume   float64 `json:"volume"`
	High52W  float64 `json:"high_52w"`
	Low52W   float64 `json:"low_52w"`
}

type indicatorValues struct {
	SMA50           float64 `json:"sma_50"`
	SMA200          float64 `json:"sma_200"`
	BollingerUpper  float64 `json:"bollinger_upper"`
	BollingerMiddle float64 `json:"bollinger_middle"`
	BollingerLower  float64 `json:"bollinger_lower"`
	ATR14           float64 `json:"atr_14"`
	HeikinAshiOpen  float64 `json:"heikinashi_open"`
	HeikinAshiClose float64 `json:"heikinashi_close"`
	Momentum22D     float64 `json:"momentum_22d"`
}

type screenerInspection struct {
	Name    string                  `json:"name"`
	Pass    bool                    `json:"pass"`
	Score   float64                 `json:"score"`
	Filters []screener.FilterResult `json:"filters"`
}

func runInspect(cmd *cobra.Command, args []string) error {
	ticker := args[0]

	appConfig, err := config.Load(cfgFile)
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}

	activeMarket, ok := marketdata.Markets[appConfig.General.Market]
	if !ok {
		return fmt.Errorf("unknown market: %s", appConfig.General.Market)
	}

	fullTicker := activeMarket.ApplySuffix(ticker)
	provider := marketdata.NewYahooProvider(5)
	ctx := context.Background()

	// Fetch 5 years of daily data
	fmt.Fprintf(os.Stderr, "📊 Fetching data for %s (%s)...\n", fullTicker, activeMarket.Name)
	data, err := provider.GetHistory(ctx, fullTicker, "5y", "1d")
	if err != nil {
		return fmt.Errorf("fetching data for %s: %w", fullTicker, err)
	}

	if len(data) < 50 {
		return fmt.Errorf("insufficient data for %s: only %d bars", fullTicker, len(data))
	}

	// Fetch benchmark
	var benchmark []marketdata.OHLCV
	benchmark, _ = provider.GetHistory(ctx, activeMarket.Benchmark, "5y", "1d")

	closes := marketdata.Closes(data)
	opens := marketdata.Opens(data)
	highs := marketdata.Highs(data)
	lows := marketdata.Lows(data)
	n := len(closes)

	// Calculate indicators
	sma50 := indicators.SMA(closes, 50)
	sma200 := indicators.SMA(closes, 200)
	upper, middle, lower := indicators.BollingerBands(closes, 20, 2.0)
	atr := indicators.ATR(highs, lows, closes, 14)
	haOpen, haClose := indicators.HeikinAshi(opens, highs, lows, closes)

	// 52-week high/low
	tail252 := indicators.Tail(closes, 252)
	high52w := indicators.Max(tail252)
	low52w := indicators.Min(tail252)

	// Momentum
	momentum := 0.0
	if n >= 23 {
		momentum = (closes[n-1] - closes[n-23]) / closes[n-23]
	}

	result := inspectResult{
		Ticker: fullTicker,
		Market: activeMarket.ID,
		Date:   time.Now().Format("2006-01-02"),
		Price: priceInfo{
			Close:   closes[n-1],
			Open:    opens[n-1],
			High:    highs[n-1],
			Low:     lows[n-1],
			Volume:  marketdata.Volumes(data)[n-1],
			High52W: high52w,
			Low52W:  low52w,
		},
		Indicators: indicatorValues{
			SMA50:           safeFloat(sma50[n-1]),
			SMA200:          safeFloat(sma200[n-1]),
			BollingerUpper:  safeFloat(upper[n-1]),
			BollingerMiddle: safeFloat(middle[n-1]),
			BollingerLower:  safeFloat(lower[n-1]),
			ATR14:           safeFloat(atr[n-1]),
			HeikinAshiOpen:  safeFloat(haOpen[n-1]),
			HeikinAshiClose: safeFloat(haClose[n-1]),
			Momentum22D:     momentum,
		},
	}

	// Run all screeners
	allScreeners := screener.Registry(appConfig)
	for _, scr := range allScreeners {
		sr, err := scr.Screen(ctx, data, benchmark)
		if err != nil {
			continue
		}
		result.Screeners = append(result.Screeners, screenerInspection{
			Name:    scr.Name(),
			Pass:    sr.Pass,
			Score:   sr.Score,
			Filters: sr.Filters,
		})
	}

	// Output
	switch output.Format(appConfig.General.Output) {
	case output.FormatJSON:
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(result)

	default:
		cs := activeMarket.CurrencySymbol
		fmt.Printf("\n📊 %s — %s\n", fullTicker, activeMarket.Name)
		fmt.Printf("─────────────────────────────\n")
		fmt.Printf("  Close:   %s%.2f\n", cs, result.Price.Close)
		fmt.Printf("  Open:    %s%.2f\n", cs, result.Price.Open)
		fmt.Printf("  High:    %s%.2f\n", cs, result.Price.High)
		fmt.Printf("  Low:     %s%.2f\n", cs, result.Price.Low)
		fmt.Printf("  Volume:  %.0f\n", result.Price.Volume)
		fmt.Printf("  52W H:   %s%.2f\n", cs, result.Price.High52W)
		fmt.Printf("  52W L:   %s%.2f\n", cs, result.Price.Low52W)
		fmt.Printf("\n📈 Indicators\n")
		fmt.Printf("─────────────────────────────\n")
		fmt.Printf("  SMA(50):     %.2f\n", result.Indicators.SMA50)
		fmt.Printf("  SMA(200):    %.2f\n", result.Indicators.SMA200)
		fmt.Printf("  BB Upper:    %.2f\n", result.Indicators.BollingerUpper)
		fmt.Printf("  BB Middle:   %.2f\n", result.Indicators.BollingerMiddle)
		fmt.Printf("  BB Lower:    %.2f\n", result.Indicators.BollingerLower)
		fmt.Printf("  ATR(14):     %.2f\n", result.Indicators.ATR14)
		fmt.Printf("  HA Open:     %.2f\n", result.Indicators.HeikinAshiOpen)
		fmt.Printf("  HA Close:    %.2f\n", result.Indicators.HeikinAshiClose)
		fmt.Printf("  Mom (22d):   %.1f%%\n", result.Indicators.Momentum22D*100)

		fmt.Printf("\n🔍 Screener Results\n")
		fmt.Printf("─────────────────────────────\n")
		for _, si := range result.Screeners {
			passStr := "❌"
			if si.Pass {
				passStr = "✅"
			}
			fmt.Printf("  %s %s — Score: %.0f%%\n", passStr, si.Name, si.Score*100)
			for _, f := range si.Filters {
				fPass := "✗"
				if f.Pass {
					fPass = "✓"
				}
				detail := ""
				if f.Detail != "" {
					detail = fmt.Sprintf(" (%s)", f.Detail)
				}
				fmt.Printf("    %s %s%s\n", fPass, f.Name, detail)
			}
		}
		fmt.Println()
	}

	return nil
}

func safeFloat(v float64) float64 {
	if math.IsNaN(v) || math.IsInf(v, 0) {
		return 0
	}
	return v
}
