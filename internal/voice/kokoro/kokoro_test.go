package kokoro

import (
	"bufio"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/maxjb-xyz/reverb/internal/voice"
)

// TestHelperWorker is not a real test: it is the fake Kokoro worker that the
// tests below start as a subprocess. It speaks the same line protocol as
// worker.py and answers each line with "RIFF" + the text.
func TestHelperWorker(t *testing.T) {
	if os.Getenv("REVERB_FAKE_KOKORO") != "1" {
		t.Skip("helper process")
	}
	out := json.NewEncoder(os.Stdout)
	_ = out.Encode(map[string]any{"ready": true, "voices": []string{"af_heart", "am_michael"}})
	sc := bufio.NewScanner(os.Stdin)
	for sc.Scan() {
		var req struct {
			Text  string  `json:"text"`
			Voice string  `json:"voice"`
			Speed float64 `json:"speed"`
		}
		_ = json.Unmarshal(sc.Bytes(), &req)
		if req.Voice == "bad" {
			_ = out.Encode(map[string]any{"ok": false, "error": "unknown voice 'bad'"})
			continue
		}
		body := fmt.Sprintf("RIFF%s|%s|%.1f", req.Text, req.Voice, req.Speed)
		_ = out.Encode(map[string]any{"ok": true, "audio": base64.StdEncoding.EncodeToString([]byte(body))})
	}
	os.Exit(0)
}

func fakeCommand(starts *atomic.Int32) func(dir string) *exec.Cmd {
	return func(string) *exec.Cmd {
		starts.Add(1)
		cmd := exec.Command(os.Args[0], "-test.run=^TestHelperWorker$")
		cmd.Env = append(os.Environ(), "REVERB_FAKE_KOKORO=1")
		return cmd
	}
}

// readyRuntime returns a runtime whose install step succeeds at once and whose
// worker is the fake above.
func readyRuntime(t *testing.T, starts *atomic.Int32) *Runtime {
	t.Helper()
	rt := NewRuntime(t.TempDir(), func(string) string { return "" })
	rt.install = func(ctx context.Context, progress func(string)) error { return nil }
	rt.command = fakeCommand(starts)
	t.Cleanup(rt.Close)
	waitState(t, rt, StateReady)
	return rt
}

func waitState(t *testing.T, rt *Runtime, want State) {
	t.Helper()
	rt.EnsureSetup()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if st, _ := rt.Status(); st == want {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	st, msg := rt.Status()
	t.Fatalf("state = %s (%s), want %s", st, msg, want)
}

func configured(t *testing.T, rt *Runtime, cfg map[string]any) *Adapter {
	t.Helper()
	a := New(rt)
	if cfg == nil {
		cfg = map[string]any{}
	}
	if err := a.Init(cfg); err != nil {
		t.Fatal(err)
	}
	return a
}

func TestConformance(t *testing.T) {
	var starts atomic.Int32
	voice.RunConformance(t, configured(t, readyRuntime(t, &starts), nil))
}

func TestSynthesizesWithDefaultsAndReusesTheWorker(t *testing.T) {
	var starts atomic.Int32
	a := configured(t, readyRuntime(t, &starts), nil)
	for i := 0; i < 3; i++ {
		got, err := a.Synthesize(context.Background(), "Hello")
		if err != nil {
			t.Fatal(err)
		}
		if string(got.Data) != "RIFFHello|af_heart|1.0" || got.ContentType != "audio/wav" {
			t.Fatalf("got %q %s", got.Data, got.ContentType)
		}
	}
	if starts.Load() != 1 {
		t.Fatalf("worker started %d times, want 1", starts.Load())
	}
}

func TestUsesConfiguredVoiceAndSpeed(t *testing.T) {
	var starts atomic.Int32
	a := configured(t, readyRuntime(t, &starts), map[string]any{"voice": "am_michael", "speed": 1.2})
	got, err := a.Synthesize(context.Background(), "Hi")
	if err != nil {
		t.Fatal(err)
	}
	if string(got.Data) != "RIFFHi|am_michael|1.2" {
		t.Fatalf("got %q", got.Data)
	}
	if !strings.Contains(a.Identity(), "am_michael") {
		t.Fatalf("identity %q should name the voice", a.Identity())
	}
}

func TestInitRejectsOutOfRangeSpeed(t *testing.T) {
	if err := New(NewRuntime(t.TempDir(), os.Getenv)).Init(map[string]any{"speed": 3.0}); err == nil {
		t.Fatal("want error for speed 3.0")
	}
}

func TestWorkerErrorIsReported(t *testing.T) {
	var starts atomic.Int32
	a := configured(t, readyRuntime(t, &starts), map[string]any{"voice": "bad"})
	if _, err := a.Synthesize(context.Background(), "Hi"); err == nil || !strings.Contains(err.Error(), "unknown voice") {
		t.Fatalf("err = %v", err)
	}
	// The worker survives a bad request.
	a2 := configured(t, a.rt, nil)
	if _, err := a2.Synthesize(context.Background(), "Hi"); err != nil {
		t.Fatal(err)
	}
	if starts.Load() != 1 {
		t.Fatalf("worker restarted after a request error (%d starts)", starts.Load())
	}
}

func TestNotReadyWhileSettingUp(t *testing.T) {
	release := make(chan struct{})
	rt := NewRuntime(t.TempDir(), os.Getenv)
	rt.install = func(ctx context.Context, progress func(string)) error {
		progress("Downloading the voice model")
		<-release
		return nil
	}
	var starts atomic.Int32
	rt.command = fakeCommand(&starts)
	t.Cleanup(rt.Close)

	a := configured(t, rt, nil) // Init starts the setup in the background
	waitState(t, rt, StateInstalling)
	for i := 0; i < 500; i++ {
		if _, msg := rt.Status(); msg == "Downloading the voice model" {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	_, err := a.Synthesize(context.Background(), "Hi")
	if !errors.Is(err, voice.ErrWarmingUp) {
		t.Fatalf("err = %v, want ErrWarmingUp", err)
	}
	if err := a.TestConnection(context.Background()); err == nil || !strings.Contains(err.Error(), "Downloading the voice model") {
		t.Fatalf("TestConnection = %v, want the setup progress", err)
	}
	close(release)
	waitState(t, rt, StateReady)
	if _, err := a.Synthesize(context.Background(), "Hi"); err != nil {
		t.Fatal(err)
	}
}

func TestFailedSetupIsReportedAndRetried(t *testing.T) {
	var calls atomic.Int32
	rt := NewRuntime(t.TempDir(), os.Getenv)
	rt.install = func(ctx context.Context, progress func(string)) error {
		if calls.Add(1) == 1 {
			return errors.New("no network")
		}
		return nil
	}
	var starts atomic.Int32
	rt.command = fakeCommand(&starts)
	t.Cleanup(rt.Close)

	waitState(t, rt, StateFailed)
	a := New(rt) // not Init: Init would already start the retry
	if err := a.TestConnection(context.Background()); err == nil || !strings.Contains(err.Error(), "no network") {
		t.Fatalf("TestConnection = %v", err)
	}
	// TestConnection (and Init) after a failure try again.
	waitState(t, rt, StateReady)
}

func TestSetupIsSkippedWhenAlreadyInstalled(t *testing.T) {
	dir := t.TempDir()
	var calls atomic.Int32
	mk := func() *Runtime {
		rt := NewRuntime(dir, os.Getenv)
		rt.install = func(ctx context.Context, progress func(string)) error { calls.Add(1); return nil }
		var starts atomic.Int32
		rt.command = fakeCommand(&starts)
		t.Cleanup(rt.Close)
		return rt
	}
	waitState(t, mk(), StateReady)
	rt2 := mk()
	if st, _ := rt2.Status(); st != StateReady {
		t.Fatalf("a finished install should be found on the next start; state = %s", st)
	}
	if calls.Load() != 1 {
		t.Fatalf("install ran %d times, want 1", calls.Load())
	}
}

func TestIdleWorkerStopsAndRestarts(t *testing.T) {
	var starts atomic.Int32
	rt := readyRuntime(t, &starts)
	rt.idle = 50 * time.Millisecond
	a := configured(t, rt, nil)
	if _, err := a.Synthesize(context.Background(), "one"); err != nil {
		t.Fatal(err)
	}
	time.Sleep(200 * time.Millisecond)
	if rt.running() {
		t.Fatal("idle worker should have stopped")
	}
	if _, err := a.Synthesize(context.Background(), "two"); err != nil {
		t.Fatal(err)
	}
	if starts.Load() != 2 {
		t.Fatalf("starts = %d, want 2", starts.Load())
	}
}
