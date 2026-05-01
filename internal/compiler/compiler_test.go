package compiler_test

import (
	"math"
	"testing"

	"hypnotz/internal/compiler"
	"hypnotz/internal/types"
)

func TestCompilerDeterminism(t *testing.T) {
	c := compiler.NewTemporalSequenceCompiler()

	window := []types.VehicleState{
		{VehicleID: "v1", TimestampNS: 100, Lat: 1.0, Lon: 2.0, Speed: 30.0, Heading: 0.5, Confidence: 0.8, DataSource: types.Redis},
		{VehicleID: "v1", TimestampNS: 200, Lat: 1.1, Lon: 2.1, Speed: 31.0, Heading: 0.6, Confidence: 0.85, DataSource: types.ClickHouse},
		{VehicleID: "v1", TimestampNS: 300, Lat: 1.2, Lon: 2.2, Speed: 32.0, Heading: 0.7, Confidence: 0.9, DataSource: types.Merged},
	}

	a, err := c.Compile("v1", window)
	if err != nil {
		t.Fatal(err)
	}

	b, err := c.Compile("v1", window)
	if err != nil {
		t.Fatal(err)
	}

	if len(a.Tokens) != len(b.Tokens) {
		t.Error("Compiler output should be deterministic")
	}

	for i := range a.Tokens {
		for j := range a.Tokens[i] {
			if a.Tokens[i][j] != b.Tokens[i][j] {
				t.Error("Compiler output should be deterministic")
			}
		}
	}
}

func TestNoReordering(t *testing.T) {
	c := compiler.NewTemporalSequenceCompiler()

	window := []types.VehicleState{
		{VehicleID: "v1", TimestampNS: 100},
		{VehicleID: "v1", TimestampNS: 200},
		{VehicleID: "v1", TimestampNS: 300},
	}

	out, err := c.Compile("v1", window)
	if err != nil {
		t.Fatal(err)
	}

	for i := 1; i < len(out.Timestamps); i++ {
		if out.Timestamps[i] < out.Timestamps[i-1] {
			t.Error("Compiler should preserve timestamp ordering")
		}
	}
}

func TestFixedFeatureDim(t *testing.T) {
	c := compiler.NewTemporalSequenceCompiler()

	window := []types.VehicleState{
		{VehicleID: "v1", TimestampNS: 100, Lat: 1.0, Lon: 2.0, Speed: 30.0, Heading: 0.5, Confidence: 0.8, DataSource: types.Redis},
		{VehicleID: "v1", TimestampNS: 200, Lat: 1.1, Lon: 2.1, Speed: 31.0, Heading: 0.6, Confidence: 0.85, DataSource: types.ClickHouse},
	}

	out, err := c.Compile("v1", window)
	if err != nil {
		t.Fatal(err)
	}

	for i, token := range out.Tokens {
		if len(token) != 7 {
			t.Errorf("Token %d has wrong dimension: %d", i, len(token))
		}
	}
}

func TestConfidencePassthrough(t *testing.T) {
	c := compiler.NewTemporalSequenceCompiler()

	window := []types.VehicleState{
		{VehicleID: "v1", TimestampNS: 100, Confidence: 0.5},
		{VehicleID: "v1", TimestampNS: 200, Confidence: 0.7},
		{VehicleID: "v1", TimestampNS: 300, Confidence: 0.9},
	}

	out, err := c.Compile("v1", window)
	if err != nil {
		t.Fatal(err)
	}

	for i := range window {
		if math.Abs(out.Confidence[i]-window[i].Confidence) > 1e-9 {
			t.Errorf("Confidence not preserved: expected %f, got %f", window[i].Confidence, out.Confidence[i])
		}
	}
}

func TestEmptyWindow(t *testing.T) {
	c := compiler.NewTemporalSequenceCompiler()

	out, err := c.Compile("v1", []types.VehicleState{})
	if err != nil {
		t.Fatal(err)
	}

	if len(out.Tokens) != 0 {
		t.Error("Empty window should produce empty tokens")
	}

	if !out.IsTimeSorted {
		t.Error("Empty window should be marked as sorted")
	}
}

func TestSourceEncoding(t *testing.T) {
	c := compiler.NewTemporalSequenceCompiler()

	window := []types.VehicleState{
		{VehicleID: "v1", TimestampNS: 100, DataSource: types.Redis},
		{VehicleID: "v1", TimestampNS: 200, DataSource: types.ClickHouse},
		{VehicleID: "v1", TimestampNS: 300, DataSource: types.Merged},
	}

	out, err := c.Compile("v1", window)
	if err != nil {
		t.Fatal(err)
	}

	expected := []float64{1.0, 2.0, 1.5}
	for i, exp := range expected {
		if out.Tokens[i][6] != exp {
			t.Errorf("Source encoding mismatch: expected %f, got %f", exp, out.Tokens[i][6])
		}
	}
}

func TestTimeNormalization(t *testing.T) {
	c := compiler.NewTemporalSequenceCompiler()

	window := []types.VehicleState{
		{VehicleID: "v1", TimestampNS: 0},
		{VehicleID: "v1", TimestampNS: 50},
		{VehicleID: "v1", TimestampNS: 100},
	}

	out, err := c.Compile("v1", window)
	if err != nil {
		t.Fatal(err)
	}

	if out.Tokens[0][5] != 0.0 {
		t.Error("First token should have time_delta = 0")
	}

	if out.Tokens[len(out.Tokens)-1][5] != 1.0 {
		t.Error("Last token should have time_delta = 1")
	}
}
