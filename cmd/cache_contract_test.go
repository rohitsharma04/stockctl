package cmd

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rohitsharma04/stockctl/internal/marketdata"
	"github.com/rohitsharma04/stockctl/internal/output"
)

var errFixtureRemoval = errors.New("fixture removal denied")

func TestCacheCommandsResolveConfigOutputUnlessFlagOverridesIt(t *testing.T) {
	configFile := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(configFile, []byte("[general]\noutput = \"json\"\nmarket = \"us\"\n"), 0600); err != nil {
		t.Fatal(err)
	}

	oldCfg, oldOutput, oldResolved, oldQuiet, oldYes := cfgFile, outputFmt, outputResolved, quiet, cacheClearYes
	t.Cleanup(func() {
		cfgFile, outputFmt, outputResolved, quiet, cacheClearYes = oldCfg, oldOutput, oldResolved, oldQuiet, oldYes
	})
	outputFlag := rootCmd.PersistentFlags().Lookup("output")
	oldOutputChanged := outputFlag.Changed
	t.Cleanup(func() { outputFlag.Changed = oldOutputChanged })
	outputFlag.Changed = false
	cfgFile, outputFmt, quiet = configFile, "table", true
	rootCmd.SetArgs([]string{"--config", configFile, "cache", "stats"})
	if err := executeRoot(rootCmd, &bytes.Buffer{}, &bytes.Buffer{}); err != nil {
		t.Fatalf("cache stats: %v", err)
	}
	if got := selectedOutputFormat(); got != output.FormatJSON {
		t.Fatalf("cache stats resolved format = %q, want json from config", got)
	}

	oldClear := cacheClearWithOptions
	t.Cleanup(func() { cacheClearWithOptions = oldClear })
	cacheClearWithOptions = func(string, bool) (int, int, error) { return 0, 0, nil }
	rootCmd.SetArgs([]string{"--config", configFile, "--output", "table", "--quiet", "cache", "clear", "--yes"})
	if err := executeRoot(rootCmd, &bytes.Buffer{}, &bytes.Buffer{}); err != nil {
		t.Fatalf("cache clear: %v", err)
	}
	if got := selectedOutputFormat(); got != output.FormatTable {
		t.Fatalf("cache clear resolved format = %q, want explicit table", got)
	}
}

func TestCacheCommandsRejectResolvedCSVBeforeCacheWork(t *testing.T) {
	configCSV := filepath.Join(t.TempDir(), "config-csv.toml")
	if err := os.WriteFile(configCSV, []byte("[general]\noutput = \"csv\"\nmarket = \"us\"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	configJSON := filepath.Join(t.TempDir(), "config-json.toml")
	if err := os.WriteFile(configJSON, []byte("[general]\noutput = \"json\"\nmarket = \"us\"\n"), 0600); err != nil {
		t.Fatal(err)
	}

	oldCfg, oldOutput, oldResolved, oldQuiet, oldYes := cfgFile, outputFmt, outputResolved, quiet, cacheClearYes
	oldStats, oldClear := getCacheStats, cacheClearWithOptions
	t.Cleanup(func() {
		cfgFile, outputFmt, outputResolved, quiet, cacheClearYes = oldCfg, oldOutput, oldResolved, oldQuiet, oldYes
		getCacheStats, cacheClearWithOptions = oldStats, oldClear
	})
	outputFlag := rootCmd.PersistentFlags().Lookup("output")
	oldOutputChanged := outputFlag.Changed
	t.Cleanup(func() { outputFlag.Changed = oldOutputChanged })

	tests := []struct {
		name      string
		config    string
		args      []string
		wantError string
	}{
		{"stats from config", configCSV, []string{"cache", "stats"}, "cache stats does not support --output csv"},
		{"clear from config", configCSV, []string{"cache", "clear", "--yes"}, "cache clear does not support --output csv"},
		{"stats from flag", configJSON, []string{"--output", "csv", "cache", "stats"}, "cache stats does not support --output csv"},
		{"clear from flag", configJSON, []string{"--output", "csv", "cache", "clear", "--yes"}, "cache clear does not support --output csv"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			statsCalls, clearCalls := 0, 0
			getCacheStats = func(...bool) marketdata.CacheStats { statsCalls++; return marketdata.CacheStats{} }
			cacheClearWithOptions = func(string, bool) (int, int, error) { clearCalls++; return 0, 0, nil }
			outputFlag.Changed = false
			cfgFile, outputFmt, quiet = tt.config, "table", false
			var stdout, stderr bytes.Buffer
			rootCmd.SetOut(&stdout)
			rootCmd.SetErr(&stderr)
			rootCmd.SetArgs(append([]string{"--config", tt.config}, tt.args...))
			err := executeRoot(rootCmd, &stdout, &stderr)
			if err == nil || err.Error() != tt.wantError {
				t.Fatalf("error = %v, want %q", err, tt.wantError)
			}
			if stdout.Len() != 0 || stderr.String() != "Error: "+tt.wantError+"\n" {
				t.Fatalf("stdout/stderr = %q/%q", stdout.String(), stderr.String())
			}
			if statsCalls != 0 || clearCalls != 0 {
				t.Fatalf("cache work ran: stats=%d clear=%d", statsCalls, clearCalls)
			}
		})
	}
}

func TestCacheCommandsFlagOverrideOfConfigCSVAllowsCacheWork(t *testing.T) {
	configFile := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(configFile, []byte("[general]\noutput = \"csv\"\nmarket = \"us\"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	oldCfg, oldOutput, oldResolved, oldQuiet, oldYes := cfgFile, outputFmt, outputResolved, quiet, cacheClearYes
	oldStats, oldClear := getCacheStats, cacheClearWithOptions
	t.Cleanup(func() {
		cfgFile, outputFmt, outputResolved, quiet, cacheClearYes = oldCfg, oldOutput, oldResolved, oldQuiet, oldYes
		getCacheStats, cacheClearWithOptions = oldStats, oldClear
	})
	outputFlag := rootCmd.PersistentFlags().Lookup("output")
	oldOutputChanged := outputFlag.Changed
	t.Cleanup(func() { outputFlag.Changed = oldOutputChanged })

	statsCalls, clearCalls := 0, 0
	getCacheStats = func(...bool) marketdata.CacheStats { statsCalls++; return marketdata.CacheStats{} }
	cacheClearWithOptions = func(string, bool) (int, int, error) { clearCalls++; return 0, 0, nil }
	for _, args := range [][]string{
		{"--config", configFile, "--output", "json", "cache", "stats"},
		{"--config", configFile, "--output", "json", "cache", "clear", "--yes"},
	} {
		outputFlag.Changed = false
		cfgFile, outputFmt, quiet = configFile, "table", true
		var stdout, stderr bytes.Buffer
		rootCmd.SetOut(&stdout)
		rootCmd.SetErr(&stderr)
		rootCmd.SetArgs(args)
		if err := executeRoot(rootCmd, &stdout, &stderr); err != nil {
			t.Fatalf("args %v: %v", args, err)
		}
		if stderr.Len() != 0 {
			t.Fatalf("args %v wrote stderr: %q", args, stderr.String())
		}
	}
	if statsCalls != 1 || clearCalls != 1 {
		t.Fatalf("override did not run cache work: stats=%d clear=%d", statsCalls, clearCalls)
	}
}

func TestCacheClearUsesResolvedConfigJSONFormat(t *testing.T) {
	configFile := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(configFile, []byte("[general]\noutput = \"json\"\nmarket = \"us\"\n"), 0600); err != nil {
		t.Fatal(err)
	}

	oldClear, oldCfg, oldOutput, oldResolved, oldYes, oldQuiet := cacheClearWithOptions, cfgFile, outputFmt, outputResolved, cacheClearYes, quiet
	t.Cleanup(func() {
		cacheClearWithOptions, cfgFile, outputFmt, outputResolved, cacheClearYes, quiet = oldClear, oldCfg, oldOutput, oldResolved, oldYes, oldQuiet
	})
	outputFlag := rootCmd.PersistentFlags().Lookup("output")
	oldOutputChanged := outputFlag.Changed
	t.Cleanup(func() { outputFlag.Changed = oldOutputChanged })
	outputFlag.Changed = false
	cacheClearWithOptions = func(string, bool) (int, int, error) { return 2, 2, nil }
	var stdout bytes.Buffer
	oldRootOut := rootCmd.OutOrStdout()
	t.Cleanup(func() { rootCmd.SetOut(oldRootOut) })
	rootCmd.SetOut(&stdout)
	oldClearOut := cacheClearCmd.OutOrStdout()
	t.Cleanup(func() { cacheClearCmd.SetOut(oldClearOut) })
	cacheClearCmd.SetOut(&stdout)
	cfgFile, outputFmt, cacheClearYes, quiet = configFile, "table", false, true
	rootCmd.SetArgs([]string{"--config", configFile, "--quiet", "cache", "clear", "--yes"})
	if err := executeRoot(rootCmd, &stdout, &bytes.Buffer{}); err != nil {
		t.Fatalf("cache clear: %v", err)
	}
	var env struct {
		Meta struct {
			Command string `json:"command"`
		} `json:"meta"`
		Results map[string]any `json:"results"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &env); err != nil {
		t.Fatalf("output = %q: %v", stdout.String(), err)
	}
	if env.Meta.Command != "cache-clear" || env.Results["removed"] != float64(2) {
		t.Fatalf("cache clear envelope = %#v", env)
	}
}

func TestQuietJSONCacheClearValidationDoesNotPrintCobraUsage(t *testing.T) {
	oldOutput, oldQuiet := outputFmt, quiet
	t.Cleanup(func() { outputFmt, quiet = oldOutput, oldQuiet })
	var stdout, stderr bytes.Buffer
	rootCmd.SetOut(&stdout)
	rootCmd.SetErr(&stderr)
	rootCmd.SetArgs([]string{"--quiet", "--output", "json", "cache", "clear"})
	err := rootCmd.Execute()
	if err == nil || !strings.Contains(err.Error(), "requires --yes") {
		t.Fatalf("error = %v, want --yes validation error", err)
	}
	if stderr.Len() != 0 {
		t.Fatalf("quiet JSON validation wrote stderr: %q", stderr.String())
	}
}

func TestQuietJSONCacheClearRemovalFailureKeepsProgressAudit(t *testing.T) {
	oldClear, oldOutput, oldQuiet := cacheClearWithOptions, outputFmt, quiet
	t.Cleanup(func() { cacheClearWithOptions, outputFmt, quiet = oldClear, oldOutput, oldQuiet })
	cacheClearWithOptions = func(string, bool) (int, int, error) { return 3, 1, errFixtureRemoval }
	var stdout, stderr bytes.Buffer
	rootCmd.SetOut(&stdout)
	rootCmd.SetErr(&stderr)
	rootCmd.SetArgs([]string{"--quiet", "--output", "json", "cache", "clear", "--yes", "--market", "us"})
	if err := executeRoot(rootCmd, &stdout, &stderr); err == nil {
		t.Fatal("expected removal failure")
	}
	var env struct {
		Results map[string]any     `json:"results"`
		Errors  []output.ErrorInfo `json:"errors"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &env); err != nil {
		t.Fatalf("output = %q: %v", stdout.String(), err)
	}
	if env.Results["matched"] != float64(3) || env.Results["removed"] != float64(1) || env.Results["market"] != "us" || env.Results["dry_run"] != false || len(env.Errors) != 1 {
		t.Fatalf("cache failure envelope = %#v", env)
	}
}

func TestRootRejectsUnsupportedOutputFormatBeforeCommandExecution(t *testing.T) {
	oldOutput := outputFmt
	t.Cleanup(func() { outputFmt = oldOutput })
	var stderr bytes.Buffer
	rootCmd.SetErr(&stderr)
	rootCmd.SetArgs([]string{"--output", "yaml", "markets"})
	err := rootCmd.Execute()
	if err == nil || !strings.Contains(err.Error(), "unsupported output format") {
		t.Fatalf("output validation error = %v", err)
	}
}
