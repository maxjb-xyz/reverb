// Package kokoro is Reverb's built-in DJ voice: Kokoro-82M (Apache-2.0) run on
// the CPU through kokoro-onnx. Nothing is bundled in the image. The first time
// the voice is enabled, Reverb makes a Python virtualenv under its data dir,
// installs kokoro-onnx there and downloads the int8 model (about 170 MB in all,
// 300 MB on disk). It then keeps one worker process (worker.py) warm while the
// DJ talks and stops it after a quiet spell to give the memory back.
package kokoro

import (
	"bufio"
	"context"
	"crypto/sha256"
	_ "embed"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	goruntime "runtime"
	"strings"
	"sync"
	"time"
)

//go:embed worker.py
var workerScript []byte

// PackageSpec is the pinned kokoro-onnx release the worker runs on.
const PackageSpec = "kokoro-onnx==0.6.1"

type modelFile struct {
	name, url, sha256 string
	size              int64
}

var modelFiles = []modelFile{
	{"kokoro-v1.0.int8.onnx", "https://github.com/thewh1teagle/kokoro-onnx/releases/download/model-files-v1.0/kokoro-v1.0.int8.onnx",
		"6e742170d309016e5891a994e1ce1559c702a2ccd0075e67ef7157974f6406cb", 92361271},
	{"voices-v1.0.bin", "https://github.com/thewh1teagle/kokoro-onnx/releases/download/model-files-v1.0/voices-v1.0.bin",
		"bca610b8308e8d99f32e6fe4197e7ec01679264efed0cac9140fe9c29f1fbf7d", 28214398},
}

// installStamp is written once setup finishes. A different stamp (after an
// upgrade changes the package or model) makes Reverb set up again.
const installStamp = PackageSpec + " model-v1.0-int8"

const markerFile = "installed"

// State is where the built-in voice is in its setup.
type State string

const (
	StateMissing    State = "missing"
	StateInstalling State = "installing"
	StateReady      State = "ready"
	StateFailed     State = "failed"
)

const (
	defaultIdle  = 10 * time.Minute
	loadTimeout  = 2 * time.Minute
	stopGrace    = 3 * time.Second
	maxLineBytes = 16 << 20
)

// Runtime owns the install and the worker process. One Runtime is shared by
// every Adapter, so a provider reload never starts a second worker.
type Runtime struct {
	dir    string
	getenv func(string) string

	// Seams for tests.
	install func(ctx context.Context, progress func(string)) error
	command func(dir string) *exec.Cmd
	idle    time.Duration
	hc      *http.Client

	mu     sync.Mutex
	state  State
	msg    string
	cancel context.CancelFunc

	wmu   sync.Mutex // serialises requests and guards the worker
	w     *worker
	timer *time.Timer
	gen   uint64
}

type worker struct {
	cmd *exec.Cmd
	in  io.WriteCloser
	out *bufio.Reader
}

// NewRuntime prepares the built-in voice under dir (for example
// <data dir>/kokoro). Nothing is installed or started until it is needed.
func NewRuntime(dir string, getenv func(string) string) *Runtime {
	if getenv == nil {
		getenv = os.Getenv
	}
	r := &Runtime{dir: dir, getenv: getenv, idle: defaultIdle, hc: &http.Client{}, state: StateMissing}
	r.install = r.realInstall
	r.command = r.realCommand
	if b, err := os.ReadFile(filepath.Join(dir, markerFile)); err == nil && strings.TrimSpace(string(b)) == installStamp {
		r.state = StateReady
	}
	return r
}

// Status reports the setup state and a human-readable note (progress while
// installing, the error after a failure).
func (r *Runtime) Status() (State, string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.state, r.msg
}

// EnsureSetup starts the install in the background when it has not been done
// (or failed last time) and returns the state after that decision.
func (r *Runtime) EnsureSetup() State {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.state != StateMissing && r.state != StateFailed {
		return r.state
	}
	r.state, r.msg = StateInstalling, "Starting setup"
	ctx, cancel := context.WithCancel(context.Background())
	r.cancel = cancel
	go func() {
		defer cancel()
		err := r.install(ctx, func(m string) {
			r.mu.Lock()
			r.msg = m
			r.mu.Unlock()
		})
		if err == nil {
			err = os.MkdirAll(r.dir, 0o755)
		}
		if err == nil {
			err = os.WriteFile(filepath.Join(r.dir, markerFile), []byte(installStamp+"\n"), 0o644)
		}
		r.mu.Lock()
		defer r.mu.Unlock()
		if err != nil {
			r.state, r.msg = StateFailed, err.Error()
			return
		}
		r.state, r.msg = StateReady, ""
	}()
	return r.state
}

// Close stops an install in progress and the worker.
func (r *Runtime) Close() {
	r.mu.Lock()
	if r.cancel != nil {
		r.cancel()
	}
	r.mu.Unlock()
	r.wmu.Lock()
	defer r.wmu.Unlock()
	r.stopLocked()
}

func (r *Runtime) running() bool {
	r.wmu.Lock()
	defer r.wmu.Unlock()
	return r.w != nil
}

type request struct {
	Text  string  `json:"text"`
	Voice string  `json:"voice"`
	Speed float64 `json:"speed"`
}

type answer struct {
	Ready bool   `json:"ready"`
	OK    bool   `json:"ok"`
	Audio string `json:"audio"`
	Error string `json:"error"`
}

// Synthesize renders text to WAV bytes. While setup is still running it
// returns an error wrapping voice.ErrWarmingUp (via the caller).
func (r *Runtime) synthesize(ctx context.Context, req request) ([]byte, error) {
	r.wmu.Lock()
	defer r.wmu.Unlock()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if r.w == nil {
		if err := r.startLocked(ctx); err != nil {
			return nil, err
		}
	}
	line, _ := json.Marshal(req)
	if _, err := r.w.in.Write(append(line, '\n')); err != nil {
		r.stopLocked()
		return nil, fmt.Errorf("kokoro: worker went away: %w", err)
	}
	a, err := r.readLocked(ctx, 0)
	if err != nil {
		r.stopLocked()
		return nil, err
	}
	r.armIdleLocked()
	if !a.OK {
		return nil, fmt.Errorf("kokoro: %s", a.Error)
	}
	return base64.StdEncoding.DecodeString(a.Audio)
}

func (r *Runtime) startLocked(ctx context.Context) error {
	cmd := r.command(r.dir)
	cmd.Stderr = &tailWriter{max: 2000}
	in, err := cmd.StdinPipe()
	if err != nil {
		return err
	}
	out, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("kokoro: start worker: %w", err)
	}
	r.w = &worker{cmd: cmd, in: in, out: bufio.NewReaderSize(out, 64<<10)}
	a, err := r.readLocked(ctx, loadTimeout)
	if err == nil && !a.Ready {
		err = errors.New("kokoro: worker did not report ready")
	}
	if err != nil {
		tail := cmd.Stderr.(*tailWriter).String()
		r.stopLocked()
		if tail != "" {
			return fmt.Errorf("%w: %s", err, tail)
		}
		return err
	}
	return nil
}

// readLocked reads one answer line, giving up when ctx ends or after timeout
// (0 = no timeout beyond ctx).
func (r *Runtime) readLocked(ctx context.Context, timeout time.Duration) (answer, error) {
	type res struct {
		b   []byte
		err error
	}
	ch := make(chan res, 1)
	rd := r.w.out
	go func() {
		var buf []byte
		for {
			chunk, err := rd.ReadSlice('\n')
			buf = append(buf, chunk...)
			if err == bufio.ErrBufferFull && len(buf) < maxLineBytes {
				continue
			}
			ch <- res{buf, err}
			return
		}
	}()
	var expire <-chan time.Time
	if timeout > 0 {
		t := time.NewTimer(timeout)
		defer t.Stop()
		expire = t.C
	}
	select {
	case got := <-ch:
		if got.err != nil {
			return answer{}, fmt.Errorf("kokoro: worker went away: %w", got.err)
		}
		var a answer
		if err := json.Unmarshal(got.b, &a); err != nil {
			return answer{}, fmt.Errorf("kokoro: bad answer from worker: %w", err)
		}
		return a, nil
	case <-ctx.Done():
		return answer{}, ctx.Err()
	case <-expire:
		return answer{}, errors.New("kokoro: worker took too long to load")
	}
}

func (r *Runtime) armIdleLocked() {
	if r.timer != nil {
		r.timer.Stop()
	}
	r.gen++
	gen := r.gen
	r.timer = time.AfterFunc(r.idle, func() {
		r.wmu.Lock()
		defer r.wmu.Unlock()
		if r.gen == gen {
			r.stopLocked()
		}
	})
}

func (r *Runtime) stopLocked() {
	if r.timer != nil {
		r.timer.Stop()
		r.timer = nil
	}
	w := r.w
	r.w = nil
	if w == nil {
		return
	}
	_ = w.in.Close() // the worker exits when stdin closes
	go func() {
		done := make(chan struct{})
		go func() { _ = w.cmd.Wait(); close(done) }()
		select {
		case <-done:
		case <-time.After(stopGrace):
			_ = w.cmd.Process.Kill()
			<-done
		}
	}()
}

// --- the real install and command ---

func (r *Runtime) venvPython() string {
	if goruntime.GOOS == "windows" {
		return filepath.Join(r.dir, "venv", "Scripts", "python.exe")
	}
	return filepath.Join(r.dir, "venv", "bin", "python")
}

func (r *Runtime) realCommand(dir string) *exec.Cmd {
	script := filepath.Join(dir, "worker.py")
	_ = os.WriteFile(script, workerScript, 0o644)
	cmd := exec.Command(r.venvPython(), script,
		"--model", filepath.Join(dir, "models", modelFiles[0].name),
		"--voices", filepath.Join(dir, "models", modelFiles[1].name))
	// Keep onnxruntime from taking every core away from the music server.
	cmd.Env = append(os.Environ(), "OMP_NUM_THREADS="+threads(r.getenv))
	return cmd
}

func threads(getenv func(string) string) string {
	if v := getenv("REVERB_VOICE_THREADS"); v != "" {
		return v
	}
	n := goruntime.NumCPU() / 2
	if n < 1 {
		n = 1
	}
	if n > 4 {
		n = 4
	}
	return fmt.Sprint(n)
}

func (r *Runtime) realInstall(ctx context.Context, progress func(string)) error {
	if err := os.MkdirAll(filepath.Join(r.dir, "models"), 0o755); err != nil {
		return err
	}
	if _, err := os.Stat(r.venvPython()); err != nil {
		progress("Preparing Python")
		py := r.getenv("REVERB_PYTHON")
		if py == "" {
			py = "python3"
		}
		if err := run(ctx, py, "-m", "venv", filepath.Join(r.dir, "venv")); err != nil {
			return fmt.Errorf("could not create a Python environment (is python3 installed?): %w", err)
		}
	}
	progress("Installing Kokoro")
	if err := run(ctx, r.venvPython(), "-m", "pip", "install", "--disable-pip-version-check", "--no-cache-dir", "--quiet", PackageSpec); err != nil {
		return fmt.Errorf("could not install %s: %w", PackageSpec, err)
	}
	var total, done int64
	for _, f := range modelFiles {
		total += f.size
	}
	for _, f := range modelFiles {
		path := filepath.Join(r.dir, "models", f.name)
		if ok, _ := fileHas(path, f.sha256); ok {
			done += f.size
			continue
		}
		base := done
		err := download(ctx, r.hc, f, path, func(n int64) {
			progress(fmt.Sprintf("Downloading the voice model (%d of %d MB)", (base+n)>>20, total>>20))
		})
		if err != nil {
			return fmt.Errorf("could not download %s: %w", f.name, err)
		}
		done += f.size
	}
	progress("Finishing setup")
	return nil
}

func run(ctx context.Context, name string, args ...string) error {
	cmd := exec.CommandContext(ctx, name, args...)
	out := &tailWriter{max: 600}
	cmd.Stdout, cmd.Stderr = out, out
	if err := cmd.Run(); err != nil {
		if t := out.String(); t != "" {
			return fmt.Errorf("%w: %s", err, t)
		}
		return err
	}
	return nil
}

func fileHas(path, sum string) (bool, error) {
	f, err := os.Open(path)
	if err != nil {
		return false, err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return false, err
	}
	return hex.EncodeToString(h.Sum(nil)) == sum, nil
}

func download(ctx context.Context, hc *http.Client, f modelFile, path string, progress func(int64)) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, f.url, nil)
	if err != nil {
		return err
	}
	resp, err := hc.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".download-*")
	if err != nil {
		return err
	}
	defer func() { _ = os.Remove(tmp.Name()) }()
	h := sha256.New()
	var n int64
	buf := make([]byte, 256<<10)
	var last int64
	for {
		k, rerr := resp.Body.Read(buf)
		if k > 0 {
			if _, err := tmp.Write(buf[:k]); err != nil {
				_ = tmp.Close()
				return err
			}
			h.Write(buf[:k])
			n += int64(k)
			if n-last >= 4<<20 {
				last = n
				progress(n)
			}
		}
		if rerr == io.EOF {
			break
		}
		if rerr != nil {
			_ = tmp.Close()
			return rerr
		}
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if got := hex.EncodeToString(h.Sum(nil)); got != f.sha256 {
		return fmt.Errorf("checksum mismatch (got %s)", got[:12])
	}
	return os.Rename(tmp.Name(), path)
}

// tailWriter keeps the last max bytes written to it, for error messages.
type tailWriter struct {
	mu  sync.Mutex
	max int
	b   []byte
}

func (t *tailWriter) Write(p []byte) (int, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.b = append(t.b, p...)
	if len(t.b) > t.max {
		t.b = t.b[len(t.b)-t.max:]
	}
	return len(p), nil
}

func (t *tailWriter) String() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	return strings.TrimSpace(string(t.b))
}
