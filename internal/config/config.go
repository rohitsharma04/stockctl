package config

import (
	"os"

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
	if cfg.General.MinPrice == 0 {
		cfg.General.MinPrice = 5.0
	}
	if cfg.General.Workers == 0 {
		cfg.General.Workers = 8
	}
	if cfg.General.Output == "" {
		cfg.General.Output = "table"
	}
	// TickersFile defaults to empty — auto-resolve from universe
	if cfg.General.Market == "" {
		cfg.General.Market = "india"
	}
}
