// Package analysis is the optional audio-analysis seam. An Analyzer listens
// to a song and returns an embedding (a vector where songs that sound alike
// sit close together) plus a few plain features. Reverb ships no model: an
// analyzer is a separate service the owner runs, usually on a machine with a
// GPU (see sidecar/analysis). With none configured, recommendations work as
// before from tags, listening and similar artists.
package analysis

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math"

	"github.com/maxjb-xyz/reverb/internal/registry"
)

// Type is the adapter_instance type for analyzers.
const Type = "analyzer"

// Embedding sizes an analyzer may return.
const (
	MinDim = 16
	MaxDim = 4096
)

// ErrUnavailable means the analyzer can't take work right now (still loading
// its model, overloaded). Try again later.
var ErrUnavailable = errors.New("analysis: analyzer unavailable")

// Features describe one song.
type Features struct {
	// Model names the model and version. Embeddings from different models
	// can't be compared, so changing models re-analyzes the library.
	Model     string    `json:"model"`
	Embedding []float32 `json:"embedding"`
	// Tempo in beats per minute, 0 when unknown.
	Tempo float64 `json:"tempo,omitempty"`
	// Energy from 0 (calm) to 1 (intense), negative when unknown.
	Energy float64 `json:"energy"`
}

// Analyzer turns audio into Features.
type Analyzer interface {
	registry.Plugin

	// Analyze reads one song's audio (any format ffmpeg reads). name is the
	// file name, a hint for the format.
	Analyze(ctx context.Context, name string, audio io.Reader) (Features, error)
}

// Validate checks an analyzer's answer before it is stored.
func (f Features) Validate() error {
	if f.Model == "" {
		return fmt.Errorf("analysis: no model name")
	}
	if len(f.Embedding) < MinDim || len(f.Embedding) > MaxDim {
		return fmt.Errorf("analysis: embedding has %d values, want %d to %d", len(f.Embedding), MinDim, MaxDim)
	}
	nonZero := false
	for _, v := range f.Embedding {
		if math.IsNaN(float64(v)) || math.IsInf(float64(v), 0) {
			return fmt.Errorf("analysis: embedding has a non-finite value")
		}
		if v != 0 {
			nonZero = true
		}
	}
	if !nonZero {
		return fmt.Errorf("analysis: embedding is all zeros")
	}
	if math.IsNaN(f.Tempo) || f.Tempo < 0 || f.Tempo > 400 {
		return fmt.Errorf("analysis: tempo %v out of range", f.Tempo)
	}
	if math.IsNaN(f.Energy) || f.Energy > 1 {
		return fmt.Errorf("analysis: energy %v out of range", f.Energy)
	}
	return nil
}

// Normalize scales v to unit length in place (so a dot product is the
// cosine similarity).
func Normalize(v []float32) {
	var s float64
	for _, x := range v {
		s += float64(x) * float64(x)
	}
	if s == 0 {
		return
	}
	n := float32(1 / math.Sqrt(s))
	for i := range v {
		v[i] *= n
	}
}

// Dot is the dot product of two equal-length vectors.
func Dot(a, b []float32) float32 {
	var s float32
	for i := range a {
		s += a[i] * b[i]
	}
	return s
}
