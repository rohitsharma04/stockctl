package config

import (
	"os"
	"path/filepath"

	"github.com/BurntSushi/toml"
)

// Config is the top-level configuration structure.
type Config struct {
	General   GeneralConfig              `toml:"general"`
	Screeners map[string]ScreenerConfig  `toml:"screeners"`
	Pairs     PairsConfig                `toml:"pairs"`
	Backtest  BacktestConfig             `toml:"backtest"`
}

type GeneralConfig struct {
	MinPrice    float64 `toml:"min_price"`
	Workers     int     `toml:"workers"`
	Output      string  `toml:"output"`
	Market      string  `toml:"market"`
	TickersFile string  `toml:"tickers_file"`
}

type ScreenerConfig struct {
	// BreakoutCaution
	BollingerPeriod    int     `toml:"bollinger_period"`
	BollingerStd       float64 `toml:"bollinger_std"`
	VolumeMultiplier   float64 `toml:"volume_multiplier"`
	SMAWindow          int     `toml:"sma_window"`
	ATRWindow          int     `toml:"atr_window"`
	RSThreshold        float64 `toml:"rs_threshold"`
	MomentumPeriod     int     `toml:"momentum_period"`
	MomentumThreshold  float64 `toml:"momentum_threshold"`

	// HighPerformance
	SMA200IncreaseDays   int     `toml:"sma_200_increase_days"`
	MaxCloseCheckpoints  int     `toml:"max_close_checkpoints"`
	MinDataDays          int     `toml:"min_data_days"`
	DoubleFromLow        bool    `toml:"double_from_low"`
	DrawdownFloor        float64 `toml:"drawdown_floor"`
	HighFloor            float64 `toml:"high_floor"`

	// StellarBreakout
	FibonacciLevel       float64 `toml:"fibonacci_level"`
	VolumeSignificance   float64 `toml:"volume_significance"`
	VolumeExplosionRatio float64 `toml:"volume_explosion_ratio"`
	RecentWeeks          int     `toml:"recent_weeks"`

	// DescendingBreakout
	Months                 int `toml:"months"`
	FalseBreakoutTolerance int `toml:"false_breakout_tolerance"`
}

type PairsConfig struct {
	CorrelationThreshold float64  `toml:"correlation_threshold"`
	ZThreshold           float64  `toml:"z_threshold"`
	ZExitLow             float64  `toml:"z_exit_low"`
	ZExitHigh            float64  `toml:"z_exit_high"`
	Window               int      `toml:"window"`
	Capital              float64  `toml:"capital"`
	Stocks               []string `toml:"stocks"`
}

type BacktestConfig struct {
	TPMin          float64 `toml:"tp_min"`
	TPMax          float64 `toml:"tp_max"`
	TPStep         float64 `toml:"tp_step"`
	SLMin          float64 `toml:"sl_min"`
	SLMax          float64 `toml:"sl_max"`
	SLStep         float64 `toml:"sl_step"`
	Capital        float64 `toml:"capital"`
	MinRewardRisk  float64 `toml:"min_reward_risk"`
}

// StockctlDir returns the base directory for all stockctl data: ~/.stockctl/
func StockctlDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ".stockctl"
	}
	return filepath.Join(home, ".stockctl")
}

// DefaultConfigPath returns the default config file path.
// Priority: $STOCKCTL_CONFIG env var → ~/.stockctl/config.toml
func DefaultConfigPath() string {
	if envPath := os.Getenv("STOCKCTL_CONFIG"); envPath != "" {
		return envPath
	}
	return filepath.Join(StockctlDir(), "config.toml")
}

// Load reads and parses the TOML config file.
// If the file doesn't exist, returns a config with sensible defaults.
func Load(path string) (*Config, error) {
	cfg := &Config{}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			setDefaults(cfg)
			return cfg, nil
		}
		return nil, err
	}

	if err := toml.Unmarshal(data, cfg); err != nil {
		return nil, err
	}

	setDefaults(cfg)
	return cfg, nil
}

func setDefaults(cfg *Config) {
	// General
	if cfg.General.MinPrice == 0 {
		cfg.General.MinPrice = 5.0
	}
	if cfg.General.Workers == 0 {
		cfg.General.Workers = 8
	}
	if cfg.General.Output == "" {
		cfg.General.Output = "table"
	}
	if cfg.General.Market == "" {
		cfg.General.Market = "us"
	}

	// Screeners — init map if nil, set per-screener defaults
	if cfg.Screeners == nil {
		cfg.Screeners = make(map[string]ScreenerConfig)
	}
	setScreenerDefault(cfg, "breakout_caution", ScreenerConfig{
		BollingerPeriod: 20, BollingerStd: 2.0, VolumeMultiplier: 1.5,
		SMAWindow: 10, ATRWindow: 14, RSThreshold: 1.05,
		MomentumPeriod: 22, MomentumThreshold: 0.10,
	})
	setScreenerDefault(cfg, "high_performance", ScreenerConfig{
		SMA200IncreaseDays: 90, MaxCloseCheckpoints: 4, MinDataDays: 756,
		DoubleFromLow: true, DrawdownFloor: 0.70, HighFloor: 0.75,
	})
	setScreenerDefault(cfg, "stellar_breakout", ScreenerConfig{
		FibonacciLevel: 0.618, VolumeSignificance: 0.3,
		VolumeExplosionRatio: 0.5, RecentWeeks: 5,
	})
	setScreenerDefault(cfg, "descending_breakout", ScreenerConfig{
		Months: 36, FalseBreakoutTolerance: 6, VolumeMultiplier: 1.5,
	})

	// Pairs
	if cfg.Pairs.CorrelationThreshold == 0 {
		cfg.Pairs.CorrelationThreshold = 0.7
	}
	if cfg.Pairs.ZThreshold == 0 {
		cfg.Pairs.ZThreshold = 2.0
	}
	if cfg.Pairs.ZExitLow == 0 {
		cfg.Pairs.ZExitLow = -0.5
	}
	if cfg.Pairs.ZExitHigh == 0 {
		cfg.Pairs.ZExitHigh = 0.5
	}
	if cfg.Pairs.Window == 0 {
		cfg.Pairs.Window = 50
	}
	if cfg.Pairs.Capital == 0 {
		cfg.Pairs.Capital = 100000
	}

	// Backtest
	if cfg.Backtest.TPMin == 0 {
		cfg.Backtest.TPMin = 0.05
	}
	if cfg.Backtest.TPMax == 0 {
		cfg.Backtest.TPMax = 0.50
	}
	if cfg.Backtest.TPStep == 0 {
		cfg.Backtest.TPStep = 0.05
	}
	if cfg.Backtest.SLMin == 0 {
		cfg.Backtest.SLMin = 0.01
	}
	if cfg.Backtest.SLMax == 0 {
		cfg.Backtest.SLMax = 0.10
	}
	if cfg.Backtest.SLStep == 0 {
		cfg.Backtest.SLStep = 0.01
	}
	if cfg.Backtest.Capital == 0 {
		cfg.Backtest.Capital = 100000
	}
	if cfg.Backtest.MinRewardRisk == 0 {
		cfg.Backtest.MinRewardRisk = 3.0
	}
}

// setScreenerDefault fills in zero-valued fields for a screener config
// without overwriting values provided by the user's config file.
func setScreenerDefault(cfg *Config, name string, defaults ScreenerConfig) {
	existing := cfg.Screeners[name]
	if existing.BollingerPeriod == 0 {
		existing.BollingerPeriod = defaults.BollingerPeriod
	}
	if existing.BollingerStd == 0 {
		existing.BollingerStd = defaults.BollingerStd
	}
	if existing.VolumeMultiplier == 0 {
		existing.VolumeMultiplier = defaults.VolumeMultiplier
	}
	if existing.SMAWindow == 0 {
		existing.SMAWindow = defaults.SMAWindow
	}
	if existing.ATRWindow == 0 {
		existing.ATRWindow = defaults.ATRWindow
	}
	if existing.RSThreshold == 0 {
		existing.RSThreshold = defaults.RSThreshold
	}
	if existing.MomentumPeriod == 0 {
		existing.MomentumPeriod = defaults.MomentumPeriod
	}
	if existing.MomentumThreshold == 0 {
		existing.MomentumThreshold = defaults.MomentumThreshold
	}
	if existing.SMA200IncreaseDays == 0 {
		existing.SMA200IncreaseDays = defaults.SMA200IncreaseDays
	}
	if existing.MaxCloseCheckpoints == 0 {
		existing.MaxCloseCheckpoints = defaults.MaxCloseCheckpoints
	}
	if existing.MinDataDays == 0 {
		existing.MinDataDays = defaults.MinDataDays
	}
	if !existing.DoubleFromLow && defaults.DoubleFromLow {
		existing.DoubleFromLow = defaults.DoubleFromLow
	}
	if existing.DrawdownFloor == 0 {
		existing.DrawdownFloor = defaults.DrawdownFloor
	}
	if existing.HighFloor == 0 {
		existing.HighFloor = defaults.HighFloor
	}
	if existing.FibonacciLevel == 0 {
		existing.FibonacciLevel = defaults.FibonacciLevel
	}
	if existing.VolumeSignificance == 0 {
		existing.VolumeSignificance = defaults.VolumeSignificance
	}
	if existing.VolumeExplosionRatio == 0 {
		existing.VolumeExplosionRatio = defaults.VolumeExplosionRatio
	}
	if existing.RecentWeeks == 0 {
		existing.RecentWeeks = defaults.RecentWeeks
	}
	if existing.Months == 0 {
		existing.Months = defaults.Months
	}
	if existing.FalseBreakoutTolerance == 0 {
		existing.FalseBreakoutTolerance = defaults.FalseBreakoutTolerance
	}
	cfg.Screeners[name] = existing
}
