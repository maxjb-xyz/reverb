// Package peaks computes compact waveform data for locally available audio.
package peaks

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"os/exec"
)

var ErrUnavailable = errors.New("peaks: ffmpeg not available")

// BucketRMS reduces signed PCM samples into n normalized RMS buckets.
func BucketRMS(samples []int16, n int) []float32 {
	if n <= 0 {
		return []float32{}
	}
	out := make([]float32, n)
	var max float64
	for i := range out {
		start, end := len(samples)*i/n, len(samples)*(i+1)/n
		if end <= start {
			continue
		}
		var sum float64
		for _, sample := range samples[start:end] {
			v := float64(sample)
			sum += v * v
		}
		rms := math.Sqrt(sum / float64(end-start))
		out[i] = float32(rms)
		if rms > max {
			max = rms
		}
	}
	if max == 0 {
		return out
	}
	for i := range out {
		out[i] /= float32(max)
	}
	return out
}

// Compute decodes to mono 8 kHz signed PCM and returns normalized RMS buckets.
func Compute(ctx context.Context, ffmpegPath, path string, n int) ([]float32, error) {
	if ffmpegPath == "" {
		ffmpegPath = "ffmpeg"
	}
	if _, err := exec.LookPath(ffmpegPath); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrUnavailable, err)
	}
	cmd := exec.CommandContext(ctx, ffmpegPath, "-v", "error", "-i", path, "-ac", "1", "-ar", "8000", "-f", "s16le", "-")
	data, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("decode waveform: %w", err)
	}
	samples := make([]int16, len(data)/2)
	for i := range samples {
		samples[i] = int16(binary.LittleEndian.Uint16(data[i*2:]))
	}
	return BucketRMS(samples, n), nil
}
