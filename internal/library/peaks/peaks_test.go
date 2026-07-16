package peaks

import (
	"context"
	"errors"
	"testing"
)

func TestBucketRMS_TwoHalves(t *testing.T) {
	samples := make([]int16, 400)
	for i := 200; i < 400; i++ {
		samples[i] = 16000
	}
	got := BucketRMS(samples, 2)
	if len(got) != 2 {
		t.Fatalf("len = %d, want 2", len(got))
	}
	if got[0] != 0 {
		t.Errorf("bucket 0 = %v, want 0", got[0])
	}
	if got[1] != 1 {
		t.Errorf("bucket 1 = %v, want 1", got[1])
	}
}

func TestBucketRMS_NGreaterThanLenDoesNotPanic(t *testing.T) {
	samples := make([]int16, 5)
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("panicked: %v", r)
		}
	}()
	got := BucketRMS(samples, 200)
	if len(got) != 200 {
		t.Fatalf("len = %d, want 200", len(got))
	}
}

func TestBucketRMS_EmptySamplesAllZero(t *testing.T) {
	got := BucketRMS(nil, 10)
	if len(got) != 10 {
		t.Fatalf("len = %d, want 10", len(got))
	}
	for i, v := range got {
		if v != 0 {
			t.Errorf("bucket %d = %v, want 0", i, v)
		}
	}
}

func TestBucketRMS_ZeroOrNegativeNReturnsEmpty(t *testing.T) {
	if got := BucketRMS([]int16{1, 2, 3}, 0); len(got) != 0 {
		t.Fatalf("n=0: len = %d, want 0", len(got))
	}
	if got := BucketRMS([]int16{1, 2, 3}, -1); len(got) != 0 {
		t.Fatalf("n=-1: len = %d, want 0", len(got))
	}
}

func TestCompute_BogusFfmpegPathReturnsErrUnavailable(t *testing.T) {
	_, err := Compute(context.Background(), "/no/such/ffmpeg-binary-xyz", "/tmp/whatever.mp3", 200)
	if !errors.Is(err, ErrUnavailable) {
		t.Fatalf("err = %v, want ErrUnavailable", err)
	}
}
