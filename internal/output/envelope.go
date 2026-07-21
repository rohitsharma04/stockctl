package output

import (
	"encoding/json"
	"io"
	"time"
)

// Envelope wraps all JSON output in a standard structure for agent consumption.
type Envelope struct {
	Meta     Meta        `json:"meta"`
	Results  interface{} `json:"results"`
	Errors   []ErrorInfo `json:"errors,omitempty"`
	Warnings []Warning   `json:"warnings,omitempty"`
}

// Meta holds metadata about the command execution.
type Meta struct {
	SchemaVersion   string              `json:"schema_version"`
	Command         string              `json:"command"`
	Market          string              `json:"market,omitempty"`
	Strategy        string              `json:"strategy,omitempty"`
	AsOfDate        string              `json:"as_of_date,omitempty"`
	Currency        string              `json:"currency,omitempty"`
	CurrencySymbol  string              `json:"currency_symbol,omitempty"`
	TickersScanned  int                 `json:"tickers_scanned,omitempty"`
	TickersFailed   int                 `json:"tickers_failed,omitempty"`
	DurationMs      int64               `json:"duration_ms"`
	Timestamp       string              `json:"timestamp"`
	DataQuality     *DataQualitySummary `json:"data_quality,omitempty"`
	EffectivePolicy *EffectivePolicy    `json:"effective_policy,omitempty"`
}

// DataQualitySummary provides visibility into data health for a scan.
type DataQualitySummary struct {
	BenchmarkAvailable    bool           `json:"benchmark_available"`
	BenchmarkSymbol       string         `json:"benchmark_symbol"`
	BenchmarkBars         int            `json:"benchmark_bars"`
	DataAsOf              string         `json:"data_as_of,omitempty"`
	ProviderFetchedAt     string         `json:"provider_fetched_at,omitempty"`
	TickersComplete       int            `json:"tickers_complete"`
	TickersPartial        int            `json:"tickers_partial"`
	TickersFailed         int            `json:"tickers_failed"`
	TickersStaleFallback  int            `json:"stale_tickers"`
	CacheOnlyTickers      int            `json:"cache_only_tickers"`
	DeltaRefreshedTickers int            `json:"delta_refreshed_tickers"`
	UpstreamFailures      int            `json:"upstream_failures"`
	SourceCounts          map[string]int `json:"source_counts,omitempty"`
	AgeDistribution       map[string]int `json:"age_distribution,omitempty"`
}

// EffectivePolicy reports the market-aware filters applied to a scan.
type EffectivePolicy struct {
	MinPrice       float64 `json:"min_price"`
	MinTradedValue float64 `json:"min_traded_value,omitempty"`
}

// ErrorInfo represents a per-ticker or per-operation error.
type ErrorInfo struct {
	Ticker string `json:"ticker,omitempty"`
	Error  string `json:"error"`
}

// Warning represents a non-fatal issue surfaced in output.
type Warning struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Ticker  string `json:"ticker,omitempty"`
}

// NewMeta creates a Meta with the current timestamp.
func NewMeta(command string) Meta {
	return Meta{
		SchemaVersion: "2.0",
		Command:       command,
		Timestamp:     time.Now().UTC().Format(time.RFC3339),
	}
}

// WriteEnvelope writes an Envelope as indented JSON to the given writer.
func WriteEnvelope(w io.Writer, env Envelope) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(env)
}
