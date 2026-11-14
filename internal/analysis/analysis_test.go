package analysis

import (
	"math"
	"testing"
)

func TestValidate(t *testing.T) {
	good := make([]float32, 32)
	good[0] = 1
	if err := (Features{Model: "m", Embedding: good, Tempo: 120, Energy: 0.5}).Validate(); err != nil {
		t.Fatal(err)
	}
	nan := append([]float32(nil), good...)
	nan[2] = float32(math.NaN())
	for name, f := range map[string]Features{
		"no model":   {Embedding: good},
		"too short":  {Model: "m", Embedding: good[:4]},
		"zeros":      {Model: "m", Embedding: make([]float32, 32)},
		"nan":        {Model: "m", Embedding: nan},
		"tempo":      {Model: "m", Embedding: good, Tempo: 9000},
		"energy > 1": {Model: "m", Embedding: good, Energy: 3},
	} {
		if err := f.Validate(); err == nil {
			t.Errorf("%s: want an error", name)
		}
	}
}

func TestNormalizeAndDot(t *testing.T) {
	v := []float32{3, 4}
	Normalize(v)
	if math.Abs(float64(Dot(v, v))-1) > 1e-6 || math.Abs(float64(v[0])-0.6) > 1e-6 {
		t.Fatalf("v = %v", v)
	}
}
