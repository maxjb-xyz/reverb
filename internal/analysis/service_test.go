package analysis

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/maxjb-xyz/reverb/internal/registry"
	"github.com/maxjb-xyz/reverb/internal/store"
	"github.com/maxjb-xyz/reverb/internal/store/db"
)

// fakeAnalyzer maps a song's audio ("vec:1,0,0...") to its embedding.
type fakeAnalyzer struct {
	model string
	calls int
	busy  bool
	fail  map[string]bool
}

func (f *fakeAnalyzer) Type() string                         { return Type }
func (f *fakeAnalyzer) Name() string                         { return "fake" }
func (f *fakeAnalyzer) ConfigSchema() registry.ConfigSchema  { return registry.ConfigSchema{} }
func (f *fakeAnalyzer) Init(map[string]any) error            { return nil }
func (f *fakeAnalyzer) TestConnection(context.Context) error { return nil }
func (f *fakeAnalyzer) Analyze(_ context.Context, name string, r io.Reader) (Features, error) {
	f.calls++
	if f.busy {
		return Features{}, ErrUnavailable
	}
	b, _ := io.ReadAll(r)
	id := strings.SplitN(string(b), "|", 2)[0]
	if f.fail[id] {
		return Features{}, errors.New("could not decode")
	}
	var v [MinDim]float32
	var a, c float32
	_, _ = fmt.Sscanf(strings.SplitN(string(b), "|", 2)[1], "%f,%f", &a, &c)
	v[0], v[1], v[2] = a, c, 0.01
	return Features{Model: f.model, Embedding: v[:], Tempo: 120, Energy: 0.5}, nil
}

type env struct {
	q   *db.Queries
	svc *Service
	an  *fakeAnalyzer
	now time.Time
	// vecs gives each track's 2-D direction.
	vecs map[string][2]float32
}

func newEnv(t *testing.T) *env {
	t.Helper()
	st, err := store.Open(t.TempDir() + "/a.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	if err := st.Migrate(); err != nil {
		t.Fatal(err)
	}
	e := &env{q: st.Q(), an: &fakeAnalyzer{model: "clap-v1", fail: map[string]bool{}}, now: time.Unix(1_800_000_000, 0), vecs: map[string][2]float32{}}
	audio := func(_ context.Context, id, suffix string) (string, io.ReadCloser, error) {
		if id == "missing" {
			return "", nil, errors.New("file not found")
		}
		v := e.vecs[id]
		return id + "." + suffix, io.NopCloser(strings.NewReader(fmt.Sprintf("%s|%g,%g", id, v[0], v[1]))), nil
	}
	e.svc = NewService(e.q, audio, func() time.Time { return e.now })
	return e
}

func (e *env) track(t *testing.T, id string, x, y float32) {
	t.Helper()
	e.vecs[id] = [2]float32{x, y}
	if err := e.q.UpsertLibraryTrack(context.Background(), db.UpsertLibraryTrackParams{LibraryTrackID: id, Title: id, Artist: "a", Suffix: "mp3", Epoch: 1}); err != nil {
		t.Fatal(err)
	}
}

func TestRun_AnalyzesTheLibraryOnceAndFindsNeighbours(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	e.track(t, "calm1", 1, 0)
	e.track(t, "calm2", 0.95, 0.1)
	e.track(t, "loud1", 0, 1)
	e.track(t, "loud2", 0.1, 0.9)
	e.track(t, "mid", 0.7, 0.7)
	if n, _ := e.svc.Run(ctx); n != 0 {
		t.Fatal("ran with no analyzer")
	}
	e.svc.Set(e.an)
	n, err := e.svc.Run(ctx)
	if err != nil || n != 5 {
		t.Fatalf("run = %d, %v", n, err)
	}
	if n, _ := e.svc.Run(ctx); n != 0 || e.an.calls != 5 {
		t.Fatalf("second run re-analyzed: %d (calls %d)", n, e.an.calls)
	}
	st, err := e.svc.Status(ctx)
	if err != nil || st.Analyzed != 5 || st.Total != 5 || st.Model != "clap-v1" || !st.Enabled {
		t.Fatalf("status = %+v, %v", st, err)
	}
	got, err := e.svc.SimilarTo(ctx, []string{"calm1"}, 2)
	if err != nil || len(got) != 2 || got[0].ID != "calm2" || got[1].ID != "mid" {
		t.Fatalf("similar to calm1 = %+v, %v", got, err)
	}
	// Several seeds: their average, never the seeds themselves.
	got, _ = e.svc.SimilarTo(ctx, []string{"loud1", "loud2", "unknown"}, 10)
	if len(got) != 3 || got[0].ID != "mid" {
		t.Fatalf("similar to loud = %+v", got)
	}
	if got, _ := e.svc.SimilarTo(ctx, []string{"unknown"}, 5); len(got) != 0 {
		t.Fatalf("unanalyzed seed = %+v", got)
	}
	if e.svc.Ready(ctx) {
		t.Error("5 songs is not enough to lean on")
	}
}

func TestRun_FailuresAreRetriedLaterAndBusyStops(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	e.track(t, "ok", 1, 0)
	e.track(t, "bad", 0, 1)
	e.track(t, "missing", 0, 1)
	e.an.fail["bad"] = true
	e.svc.Set(e.an)
	if n, err := e.svc.Run(ctx); n != 1 || err != nil {
		t.Fatalf("run = %d, %v", n, err)
	}
	st, _ := e.svc.Status(ctx)
	if st.Failed != 2 || st.Analyzed != 1 || st.LastError == "" {
		t.Fatalf("status = %+v", st)
	}
	// Not retried straight away...
	if n, _ := e.svc.Run(ctx); n != 0 {
		t.Fatal("retried a failure at once")
	}
	// ...but after a week.
	delete(e.an.fail, "bad")
	e.now = e.now.Add(8 * 24 * time.Hour)
	if n, _ := e.svc.Run(ctx); n != 1 {
		t.Fatalf("retry analyzed %d", n)
	}
	// A busy service stops the run without marking songs failed.
	e.track(t, "new", 1, 1)
	e.an.busy = true
	if n, _ := e.svc.Run(ctx); n != 0 {
		t.Fatal("busy service analyzed something")
	}
	if st, _ := e.svc.Status(ctx); st.Failed != 1 {
		t.Fatalf("busy marked songs failed: %+v", st)
	}
}

func TestRun_NewModelRedoesTheLibrary(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	e.track(t, "a", 1, 0)
	e.track(t, "b", 0, 1)
	e.svc.Set(e.an)
	_, _ = e.svc.Run(ctx)
	e.an.model = "clap-v2"
	e.track(t, "c", 1, 1)
	n, _ := e.svc.Run(ctx)
	if n != 3 {
		t.Fatalf("after a model change analyzed %d, want all 3", n)
	}
	if st, _ := e.svc.Status(ctx); st.Model != "clap-v2" || st.Analyzed != 3 {
		t.Fatalf("status = %+v", st)
	}
}

func TestEncodeDecode(t *testing.T) {
	v := []float32{1.5, -2, 0}
	got := decode(encode(v))
	if len(got) != 3 || got[0] != 1.5 || got[1] != -2 {
		t.Fatalf("got %v", got)
	}
	if decode([]byte{1, 2, 3}) != nil {
		t.Fatal("odd length should decode to nil")
	}
}
