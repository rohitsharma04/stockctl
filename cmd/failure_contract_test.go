package cmd

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"testing"
)

func TestExecuteQuietJSONNormalFailureWritesOnlyOneErrorEnvelope(t *testing.T) {
	if os.Getenv("STOCKCTL_QUIET_JSON_HELPER") == "1" {
		rootCmd.SetArgs([]string{"--quiet", "--output", "json", "pairs", "--stocks", "AAA"})
		Execute()
		return
	}

	cmd := exec.Command(os.Args[0], "-test.run=TestExecuteQuietJSONNormalFailureWritesOnlyOneErrorEnvelope")
	cmd.Env = append(os.Environ(), "STOCKCTL_QUIET_JSON_HELPER=1")
	stdout, err := cmd.Output()
	if err == nil {
		t.Fatal("quiet JSON failure exited successfully")
	}
	if exit, ok := err.(*exec.ExitError); !ok || exit.ExitCode() != 1 {
		t.Fatalf("exit error = %v, want exit status 1", err)
	} else if len(exit.Stderr) != 0 {
		t.Fatalf("stderr = %q, want no Cobra diagnostics", exit.Stderr)
	}
	var env struct {
		Meta struct {
			Command string `json:"command"`
		} `json:"meta"`
		Errors []struct {
			Error string `json:"error"`
		} `json:"errors"`
	}
	if err := json.Unmarshal(stdout, &env); err != nil {
		t.Fatalf("stdout is not one JSON envelope: %q: %v", stdout, err)
	}
	if env.Meta.Command != "error" || len(env.Errors) != 1 || env.Errors[0].Error == "" {
		t.Fatalf("error envelope = %#v", env)
	}
}

func TestExecuteKeepsNonQuietFailureDiagnostics(t *testing.T) {
	oldOutput, oldQuiet := outputFmt, quiet
	t.Cleanup(func() { outputFmt, quiet = oldOutput, oldQuiet })
	var stdout, stderr bytes.Buffer
	rootCmd.SetOut(&stdout)
	rootCmd.SetErr(&stderr)
	rootCmd.SetArgs([]string{"--output", "table", "pairs", "--stocks", "AAA"})
	if err := executeRoot(rootCmd, &stdout, &stderr); err == nil {
		t.Fatal("non-quiet failure unexpectedly succeeded")
	}
	if stderr.Len() == 0 {
		t.Fatal("non-quiet failure suppressed its diagnostic")
	}
}
