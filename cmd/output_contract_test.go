package cmd

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

func TestConfigResolvedCSVIsRejectedBeforeQuoteWork(t *testing.T) {
	configFile := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(configFile, []byte("[general]\noutput = \"csv\"\nmarket = \"us\"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	oldCfg, oldOutput, oldResolved := cfgFile, outputFmt, outputResolved
	t.Cleanup(func() { cfgFile, outputFmt, outputResolved = oldCfg, oldOutput, oldResolved })
	outputFlag := rootCmd.PersistentFlags().Lookup("output")
	oldChanged := outputFlag.Changed
	outputFlag.Changed = false
	t.Cleanup(func() { outputFlag.Changed = oldChanged })
	var stdout, stderr bytes.Buffer
	rootCmd.SetOut(&stdout)
	rootCmd.SetErr(&stderr)
	rootCmd.SetArgs([]string{"--config", configFile, "quote", "AAPL"})
	err := executeRoot(rootCmd, &stdout, &stderr)
	if err == nil || err.Error() != "quote does not support --output csv" {
		t.Fatalf("error = %v", err)
	}
	if stdout.Len() != 0 || stderr.String() != "Error: quote does not support --output csv\n" {
		t.Fatalf("stdout/stderr = %q/%q", stdout.String(), stderr.String())
	}
}

func TestQuoteCSVIsExplicitlyRejectedInsteadOfRenderingTable(t *testing.T) {
	oldOutput, oldResolved := outputFmt, outputResolved
	t.Cleanup(func() { outputFmt, outputResolved = oldOutput, oldResolved })
	outputResolved = false
	outputFmt = "csv"
	err := runQuote(&cobra.Command{}, []string{"AAPL"})
	if err == nil || err.Error() != "quote does not support --output csv" {
		t.Fatalf("CSV error = %v", err)
	}
}

func TestTickersCSVIsExplicitlyRejectedInsteadOfRenderingTable(t *testing.T) {
	oldOutput, oldMarket, oldResolved := outputFmt, activeMarket, outputResolved
	t.Cleanup(func() { outputFmt, activeMarket, outputResolved = oldOutput, oldMarket, oldResolved })
	outputResolved = false
	outputFmt = "csv"
	var stdout bytes.Buffer
	command := &cobra.Command{}
	command.SetOut(&stdout)
	err := runTickers(command, nil)
	if err == nil || err.Error() != "tickers does not support --output csv" {
		t.Fatalf("CSV error = %v", err)
	}
	if strings.Contains(stdout.String(), "📊") {
		t.Fatalf("CSV unexpectedly rendered table: %q", stdout.String())
	}
}
