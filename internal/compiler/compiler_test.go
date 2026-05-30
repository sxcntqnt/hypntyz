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
		{VehicleID: "v1", TimestampNS: 100, Lat: 1.0, Lon: 2.0, Speed: 30.0, Heading: 0.5, Confidence: 0.80, DataSource: types.SourceRedis},
		{VehicleID: "v1", TimestampNS: 200, Lat: 1.1, Lon: 2.1, Speed: 31.0, Heading: 0.6, Confidence: 0.85, DataSource: types.SourceClickHouse},
		{VehicleID: "v1", TimestampNS: 300, Lat: 1.2, Lon: 2.2, Speed: 32.0, Heading: 0.7, Confidence: 0.90, DataSource: types.SourceMerged},
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
		t.Fatal("compiler output should be deterministic: token count mismatch")
	}
	for i := range a.Tokens {
		for j := range a.Tokens[i] {
			if a.Tokens[i][j] != b.Tokens[i][j] {
				t.Errorf("non-deterministic output at token[%d][%d]: %f vs %f",
					i, j, a.Tokens[i][j], b.Tokens[i][j])
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
			t.Errorf("timestamp ordering violated at index %d: %d < %d",
				i, out.Timestamps[i], out.Timestamps[i-1])
		}
	}
}

func TestFixedFeatureDim(t *testing.T) {
	c := compiler.NewTemporalSequenceCompiler()

	window := []types.VehicleState{
		{VehicleID: "v1", TimestampNS: 100, Lat: 1.0, Lon: 2.0, Speed: 30.0, Heading: 0.5, Confidence: 0.80, DataSource: types.SourceRedis},
		{VehicleID: "v1", TimestampNS: 200, Lat: 1.1, Lon: 2.1, Speed: 31.0, Heading: 0.6, Confidence: 0.85, DataSource: types.SourceClickHouse},
	}

	out, err := c.Compile("v1", window)
	if err != nil {
		t.Fatal(err)
	}
	for i, token := range out.Tokens {
		if len(token) != types.FeatureDim {
			t.Errorf("token[%d] has wrong dimension: got %d, want %d", i, len(token), types.FeatureDim)
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
			t.Errorf("confidence not preserved at index %d: expected %f, got %f",
				i, window[i].Confidence, out.Confidence[i])
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
		t.Errorf("empty window should produce 0 tokens, got %d", len(out.Tokens))
	}
	if !out.IsTimeSorted {
		t.Error("empty window should be marked as time-sorted")
	}
}

func TestSourceEncoding(t *testing.T) {
	c := compiler.NewTemporalSequenceCompiler()

	window := []types.VehicleState{
		{VehicleID: "v1", TimestampNS: 100, DataSource: types.SourceRedis},
		{VehicleID: "v1", TimestampNS: 200, DataSource: types.SourceClickHouse},
		{VehicleID: "v1", TimestampNS: 300, DataSource: types.SourceMerged},
	}

	out, err := c.Compile("v1", window)
	if err != nil {
		t.Fatal(err)
	}

	// Feature index 6 carries the source encoding.
	expected := []float64{1.0, 2.0, 1.5}
	for i, exp := range expected {
		got := out.Tokens[i][6]
		if got != exp {
			t.Errorf("source encoding at token[%d]: expected %f, got %f", i, exp, got)
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

	// Feature index 5 carries the normalised time delta.
	if out.Tokens[0][5] != 0.0 {
		t.Errorf("first token time_delta: expected 0.0, got %f", out.Tokens[0][5])
	}
	if out.Tokens[len(out.Tokens)-1][5] != 1.0 {
		t.Errorf("last token time_delta: expected 1.0, got %f", out.Tokens[len(out.Tokens)-1][5])
	}
}

func TestUnsortedInputReturnsError(t *testing.T) {
	c := compiler.NewTemporalSequenceCompiler()

	window := []types.VehicleState{
		{VehicleID: "v1", TimestampNS: 300},
		{VehicleID: "v1", TimestampNS: 100}, // out of order
	}

	_, err := c.Compile("v1", window)
	if err == nil {
		t.Error("expected ErrUnsortedInput, got nil")
	}
}
