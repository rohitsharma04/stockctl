package output

import (
	"encoding/json"
	"io"
	"time"
)

// Envelope wraps all JSON output in a standard structure for agent consumption.
type Envelope struct {
	Meta    Meta        `json:"meta"`
	Results interface{} `json:"results"`
	Errors  []ErrorInfo `json:"errors,omitempty"`
}

// Meta holds metadata about the command execution.
type Meta struct {
	Command        string `json:"command"`
	Market         string `json:"market,omitempty"`
	Strategy       string `json:"strategy,omitempty"`
	AsOfDate       string `json:"as_of_date,omitempty"`
	TickersScanned int    `json:"tickers_scanned,omitempty"`
	TickersFailed  int    `json:"tickers_failed,omitempty"`
	DurationMs     int64  `json:"duration_ms"`
	Timestamp      string `json:"timestamp"`
}

// ErrorInfo represents a per-ticker or per-operation error.
type ErrorInfo struct {
	Ticker string `json:"ticker,omitempty"`
	Error  string `json:"error"`
}

// NewMeta creates a Meta with the current timestamp.
func NewMeta(command string) Meta {
	return Meta{
		Command:   command,
		Timestamp: time.Now().UTC().Format(time.RFC3339),
	}
}

// WriteEnvelope writes an Envelope as indented JSON to the given writer.
func WriteEnvelope(w io.Writer, env Envelope) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(env)
}
