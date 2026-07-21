package cmd

import (
	"math"
	"testing"
)

func TestParseBacktestRange(t *testing.T) {
	tests := []struct {
		input      string
		defaultMin float64
		defaultMax float64
		wantMin    float64
		wantMax    float64
		wantErr    bool
	}{
		{"", 0.05, 0.50, 0.05, 0.50, false},
		{"0.05:0.50", 0, 0, 0.05, 0.50, false},
		{"0.05", 0, 0, 0, 0, true},
		{"abc:0.50", 0, 0, 0, 0, true},
		{"0.50:0.05", 0, 0, 0, 0, true},
		{"-0.05:0.50", 0, 0, 0, 0, true},
		{"NaN:0.50", 0, 0, 0, 0, true},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			min, max, err := parseBacktestRange("--tp-range", tt.input, tt.defaultMin, tt.defaultMax)
			if (err != nil) != tt.wantErr {
				t.Fatalf("parseBacktestRange(%q) error = %v, wantErr %v", tt.input, err, tt.wantErr)
			}
			if !tt.wantErr && (min != tt.wantMin || max != tt.wantMax) {
				t.Fatalf("parseBacktestRange(%q) = %v:%v, want %v:%v", tt.input, min, max, tt.wantMin, tt.wantMax)
			}
		})
	}
}

func TestValidateBacktestParameters(t *testing.T) {
	valid := backtestParameters{tpMin: 0.05, tpMax: 0.5, tpStep: 0.05, slMin: 0.01, slMax: 0.1, slStep: 0.01, minRewardRisk: 3, capital: 100000}
	if err := validateBacktestParameters(valid); err != nil {
		t.Fatalf("valid parameters rejected: %v", err)
	}
	for _, mutate := range []func(*backtestParameters){
		func(p *backtestParameters) { p.tpStep = 0 },
		func(p *backtestParameters) { p.slStep = -0.01 },
		func(p *backtestParameters) { p.tpMin = math.NaN() },
		func(p *backtestParameters) { p.slMax = math.Inf(1) },
		func(p *backtestParameters) { p.capital = 0 },
		func(p *backtestParameters) { p.minRewardRisk = -1 },
	} {
		params := valid
		mutate(&params)
		if err := validateBacktestParameters(params); err == nil {
			t.Fatal("invalid parameters accepted")
		}
	}
}
