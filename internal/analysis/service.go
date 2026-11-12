package analysis

import (
	"context"
	"encoding/binary"
	"errors"
	"io"
	"log"
	"math"
	"sort"
	"sync"
	"time"

	"github.com/maxjb-xyz/reverb/internal/store/db"
)

// SettingModel stores the model the library was last analyzed with.
const SettingModel = "analysis_model"

const (
	batchSize  = 25
	retryAfter = 7 * 24 * time.Hour
	// MinReady is how many analyzed songs the index needs before the engine
	// leans on it.
	MinReady = 50
)

// Querier is the store surface the service needs.
type Querier interface {
	ListTracksToAnalyze(ctx context.Context, arg db.ListTracksToAnalyzeParams) ([]db.ListTracksToAnalyzeRow, error)
	UpsertTrackFeatures(ctx context.Context, arg db.UpsertTrackFeaturesParams) error
	ListTrackEmbeddings(ctx context.Context, model string) ([]db.ListTrackEmbeddingsRow, error)
	CountTrackFeatures(ctx context.Context, model string) (db.CountTrackFeaturesRow, error)
	CountLibraryTracks(ctx context.Context) (int64, error)
	DeleteOrphanTrackFeatures(ctx context.Context) (int64, error)
	GetSetting(ctx context.Context, key string) (string, error)
	UpsertSetting(ctx context.Context, arg db.UpsertSettingParams) error
}

// AudioSource opens a library song's audio. name is a file name hint.
type AudioSource func(ctx context.Context, libraryTrackID, suffix string) (name string, audio io.ReadCloser, err error)

// Neighbor is a library song that sounds like the query.
type Neighbor struct {
	ID    string
	Score float32 // cosine similarity, 1 = identical
}

// Status reports progress for the settings page.
type Status struct {
	Enabled   bool   `json:"enabled"`
	Running   bool   `json:"running"`
	Model     string `json:"model,omitempty"`
	Analyzed  int64  `json:"analyzed"`
	Failed    int64  `json:"failed"`
	Total     int64  `json:"total"`
	LastError string `json:"lastError,omitempty"`
}

// Service analyzes the library in the background with whichever analyzer is
// configured, and answers "what sounds like this" from the stored embeddings.
type Service struct {
	q     Querier
	audio AudioSource
	now   func() time.Time

	mu       sync.Mutex
	analyzer Analyzer
	running  bool
	lastErr  string
	kick     chan struct{}

	idxMu    sync.RWMutex
	idxModel string
	idxStale bool
	ids      []string
	vecs     [][]float32
	pos      map[string]int
}

// NewService builds the service. audio may be nil (nothing is analyzed).
func NewService(q Querier, audio AudioSource, now func() time.Time) *Service {
	return &Service{q: q, audio: audio, now: now, kick: make(chan struct{}, 1), idxStale: true}
}

// Set installs the live analyzer (nil when none is enabled) and starts a run.
func (s *Service) Set(a Analyzer) {
	s.mu.Lock()
	s.analyzer = a
	s.mu.Unlock()
	s.Kick()
}

func (s *Service) current() Analyzer {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.analyzer
}

// Kick asks for a run soon (after a library change, a new analyzer).
func (s *Service) Kick() {
	select {
	case s.kick <- struct{}{}:
	default:
	}
}

// Start runs analysis in the background: after initialDelay, on every Kick
// and every interval.
func (s *Service) Start(ctx context.Context, initialDelay, interval time.Duration) {
	go func() {
		t := time.NewTimer(initialDelay)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
			case <-s.kick:
			}
			if _, err := s.Run(ctx); err != nil && ctx.Err() == nil {
				log.Printf("analysis: %v", err)
			}
			if !t.Stop() {
				select {
				case <-t.C:
				default:
				}
			}
			t.Reset(interval)
		}
	}()
}

func (s *Service) model(ctx context.Context) string {
	m, err := s.q.GetSetting(ctx, SettingModel)
	if err != nil {
		return ""
	}
	return m
}

// Run analyzes every song still waiting, a batch at a time, until none are
// left, the analyzer goes away or says it is busy. It returns how many songs
// were analyzed.
func (s *Service) Run(ctx context.Context) (int, error) {
	s.mu.Lock()
	if s.running || s.analyzer == nil || s.audio == nil {
		s.mu.Unlock()
		return 0, nil
	}
	s.running = true
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		s.running = false
		s.mu.Unlock()
	}()

	if _, err := s.q.DeleteOrphanTrackFeatures(ctx); err != nil {
		return 0, err
	}
	done := 0
	runErr := ""
	defer func() {
		if done > 0 {
			s.markStale()
		}
	}()
	for ctx.Err() == nil {
		a := s.current()
		if a == nil {
			return done, nil
		}
		model := s.model(ctx)
		rows, err := s.q.ListTracksToAnalyze(ctx, db.ListTracksToAnalyzeParams{
			Model: model, RetryBefore: s.now().Add(-retryAfter).Unix(), Lim: batchSize,
		})
		if err != nil {
			return done, err
		}
		if len(rows) == 0 {
			if done > 0 || runErr != "" {
				// Report this run's last failure, or clear an old one.
				s.setErr(runErr)
			}
			return done, nil
		}
		progressed := false
		for _, r := range rows {
			if ctx.Err() != nil {
				return done, ctx.Err()
			}
			f, err := s.analyzeOne(ctx, a, r)
			if errors.Is(err, ErrUnavailable) {
				s.setErr("The analysis service is busy or still loading. Reverb will try again later.")
				return done, nil
			}
			if err != nil && ctx.Err() != nil {
				return done, ctx.Err()
			}
			row := db.UpsertTrackFeaturesParams{LibraryTrackID: r.LibraryTrackID, AnalyzedAt: s.now().Unix(), Energy: -1}
			if err != nil {
				row.Error = trim(err.Error(), 300)
				runErr = row.Error
				s.setErr(row.Error)
			} else {
				if f.Model != model {
					// A new model: songs analyzed with the old one are redone.
					if err := s.q.UpsertSetting(ctx, db.UpsertSettingParams{Key: SettingModel, Value: f.Model}); err != nil {
						return done, err
					}
					model = f.Model
				}
				Normalize(f.Embedding)
				row.Model, row.Embedding, row.Tempo, row.Energy = f.Model, encode(f.Embedding), f.Tempo, f.Energy
				done++
				progressed = true
			}
			if err := s.q.UpsertTrackFeatures(ctx, row); err != nil {
				return done, err
			}
		}
		if !progressed {
			// Every song in the batch failed: the service is probably broken.
			// Stop until the next tick rather than burning through the library.
			return done, nil
		}
	}
	return done, ctx.Err()
}

func (s *Service) analyzeOne(ctx context.Context, a Analyzer, r db.ListTracksToAnalyzeRow) (Features, error) {
	name, audio, err := s.audio(ctx, r.LibraryTrackID, r.Suffix)
	if err != nil {
		return Features{}, err
	}
	defer audio.Close()
	f, err := a.Analyze(ctx, name, audio)
	if err != nil {
		return Features{}, err
	}
	return f, f.Validate()
}

func (s *Service) setErr(msg string) {
	s.mu.Lock()
	s.lastErr = msg
	s.mu.Unlock()
}

// Status reports how far analysis has got.
func (s *Service) Status(ctx context.Context) (Status, error) {
	s.mu.Lock()
	st := Status{Enabled: s.analyzer != nil, Running: s.running, LastError: s.lastErr}
	s.mu.Unlock()
	st.Model = s.model(ctx)
	c, err := s.q.CountTrackFeatures(ctx, st.Model)
	if err != nil {
		return st, err
	}
	st.Analyzed, st.Failed = c.Analyzed, c.Failed
	st.Total, err = s.q.CountLibraryTracks(ctx)
	return st, err
}

func (s *Service) markStale() {
	s.idxMu.Lock()
	s.idxStale = true
	s.idxMu.Unlock()
}

// load (re)builds the in-memory index when it is stale or the model changed.
func (s *Service) load(ctx context.Context) error {
	model := s.model(ctx)
	s.idxMu.RLock()
	fresh := !s.idxStale && s.idxModel == model
	s.idxMu.RUnlock()
	if fresh {
		return nil
	}
	var ids []string
	var vecs [][]float32
	if model != "" {
		rows, err := s.q.ListTrackEmbeddings(ctx, model)
		if err != nil {
			return err
		}
		dim := 0
		for _, r := range rows {
			v := decode(r.Embedding)
			if len(v) == 0 || (dim != 0 && len(v) != dim) {
				continue
			}
			dim = len(v)
			ids = append(ids, r.LibraryTrackID)
			vecs = append(vecs, v)
		}
	}
	pos := make(map[string]int, len(ids))
	for i, id := range ids {
		pos[id] = i
	}
	s.idxMu.Lock()
	s.ids, s.vecs, s.pos, s.idxModel, s.idxStale = ids, vecs, pos, model, false
	s.idxMu.Unlock()
	return nil
}

// Ready says whether enough of the library is analyzed to recommend from.
func (s *Service) Ready(ctx context.Context) bool {
	if err := s.load(ctx); err != nil {
		return false
	}
	s.idxMu.RLock()
	defer s.idxMu.RUnlock()
	return len(s.ids) >= MinReady
}

// SimilarTo returns up to n library songs that sound most like the given
// ones (their average), best first, never including them. Songs that aren't
// analyzed yet are ignored; with none analyzed the answer is empty.
func (s *Service) SimilarTo(ctx context.Context, ids []string, n int) ([]Neighbor, error) {
	if err := s.load(ctx); err != nil {
		return nil, err
	}
	s.idxMu.RLock()
	defer s.idxMu.RUnlock()
	if n <= 0 || len(s.vecs) == 0 {
		return nil, nil
	}
	dim := len(s.vecs[0])
	q := make([]float32, dim)
	skip := map[int]bool{}
	for _, id := range ids {
		i, ok := s.pos[id]
		if !ok {
			continue
		}
		skip[i] = true
		for d, x := range s.vecs[i] {
			q[d] += x
		}
	}
	if len(skip) == 0 {
		return nil, nil
	}
	Normalize(q)
	out := make([]Neighbor, 0, len(s.vecs))
	for i, v := range s.vecs {
		if skip[i] {
			continue
		}
		out = append(out, Neighbor{ID: s.ids[i], Score: Dot(q, v)})
	}
	sort.Slice(out, func(a, b int) bool {
		if out[a].Score != out[b].Score {
			return out[a].Score > out[b].Score
		}
		return out[a].ID < out[b].ID
	})
	if len(out) > n {
		out = out[:n]
	}
	return out, nil
}

func encode(v []float32) []byte {
	b := make([]byte, 4*len(v))
	for i, x := range v {
		binary.LittleEndian.PutUint32(b[4*i:], math.Float32bits(x))
	}
	return b
}

func decode(b []byte) []float32 {
	if len(b)%4 != 0 {
		return nil
	}
	v := make([]float32, len(b)/4)
	for i := range v {
		v[i] = math.Float32frombits(binary.LittleEndian.Uint32(b[4*i:]))
	}
	return v
}

func trim(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}
