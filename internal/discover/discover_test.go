package discover

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/maxjb-xyz/reverb/internal/core"
	"github.com/maxjb-xyz/reverb/internal/engine"
	"github.com/maxjb-xyz/reverb/internal/store"
	"github.com/maxjb-xyz/reverb/internal/store/db"
)

type fakeEngine struct {
	discoverCalls int
	nextCalls     int
	lastExclude   map[string]bool
	week          int
}

func (f *fakeEngine) Discover(_ context.Context, _ string, n int, exclude map[string]bool) ([]engine.Candidate, error) {
	f.discoverCalls++
	f.lastExclude = exclude
	var out []engine.Candidate
	for i := 0; len(out) < n && i < 100; i++ {
		id := fmt.Sprintf("w%d-%d", f.week, i)
		if exclude["deezer:"+id] {
			continue
		}
		out = append(out, engine.Candidate{
			External: &core.ExternalResult{Source: "deezer", ExternalID: id, Title: "New " + id, Artist: "Artist " + id},
			Reason:   engine.Reason{Kind: engine.ReasonDiscovery, Artist: "Artist " + id, SeedArtist: "Bonobo"},
		})
	}
	return out, nil
}

func (f *fakeEngine) Next(_ context.Context, _ string, req engine.Request) (engine.Result, error) {
	f.nextCalls++
	var out []engine.Candidate
	skip := map[string]bool{}
	for _, id := range req.Exclude {
		skip[id] = true
	}
	for i := 0; len(out) < req.N && i < 200; i++ {
		id := fmt.Sprintf("own-%d-%d", f.week, i)
		if skip[id] {
			continue
		}
		out = append(out, engine.Candidate{Owned: true, Track: &core.Track{ID: id, Title: "Owned " + id, Artist: "Lib"}, Reason: engine.Reason{Kind: engine.ReasonNewToYou}})
	}
	return engine.Result{Items: out}, nil
}

type fakeDL struct {
	jobs     map[string]core.DownloadJob
	enqueued []core.DownloadRequest
	n        int
	created  int64
	existing map[string]core.DownloadJob // by external id: already downloaded before
}

func (f *fakeDL) Enqueue(_ context.Context, req core.DownloadRequest) (core.DownloadJob, error) {
	if j, ok := f.existing[req.ExternalID]; ok {
		return j, nil
	}
	f.n++
	f.enqueued = append(f.enqueued, req)
	j := core.DownloadJob{ID: fmt.Sprintf("job-%d", f.n), Source: req.Source, ExternalID: req.ExternalID, Title: req.Title, Artist: req.Artist,
		Status: core.DownloadQueued, CreatedAt: f.created}
	f.jobs[j.ID] = j
	return j, nil
}

func (f *fakeDL) Status(_ context.Context, id string) (core.DownloadJob, error) {
	j, ok := f.jobs[id]
	if !ok {
		return j, errors.New("no job")
	}
	return j, nil
}

// land marks a job downloaded and scanned into the library.
func (f *fakeDL) land(id, libID, path string) {
	j := f.jobs[id]
	j.Status, j.LibraryTrackID, j.CanonicalID, j.OutputPath, j.Progress = core.DownloadCompleted, libID, "cat-"+libID, path, 100
	f.jobs[id] = j
}

type env struct {
	q       *db.Queries
	svc     *Service
	eng     *fakeEngine
	dl      *fakeDL
	removed []string
	rescans int
	now     time.Time
}

func newEnv(t *testing.T) *env {
	t.Helper()
	st, err := store.Open(t.TempDir() + "/discover.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	if err := st.Migrate(); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	q := st.Q()
	for _, r := range []struct{ id, caps string }{{"r-dl", `["auto_approve","request"]`}, {"r-req", `["request"]`}} {
		if err := q.CreateRole(ctx, db.CreateRoleParams{ID: r.id, Name: r.id, Capabilities: r.caps}); err != nil {
			t.Fatal(err)
		}
	}
	for _, u := range []struct{ id, role string }{{"u1", "r-dl"}, {"u2", "r-req"}, {"u3", "r-dl"}} {
		if err := q.CreateUser(ctx, db.CreateUserParams{ID: u.id, Username: u.id, PasswordHash: "x", RoleID: u.role}); err != nil {
			t.Fatal(err)
		}
	}
	// Thursday 14 Jan 2027, noon.
	e := &env{q: q, eng: &fakeEngine{}, dl: &fakeDL{jobs: map[string]core.DownloadJob{}, existing: map[string]core.DownloadJob{}}, now: time.Date(2027, 1, 14, 12, 0, 0, 0, time.Local)}
	e.dl.created = e.now.Unix()
	n := 0
	e.svc = New(q, e.eng, Options{
		Downloads: func() Downloader { return e.dl },
		Remove:    func(p string) error { e.removed = append(e.removed, p); return nil },
		Rescan:    func(context.Context) { e.rescans++ },
	}, func() time.Time { return e.now }, func() string { n++; return fmt.Sprintf("item-%d", n) })
	return e
}

func (e *env) setPrefs(t *testing.T, user string, p Prefs) {
	t.Helper()
	if _, err := e.svc.SetPrefs(context.Background(), user, p); err != nil {
		t.Fatal(err)
	}
}

func (e *env) current(t *testing.T, user string) Week {
	t.Helper()
	w, err := e.svc.Current(context.Background(), user)
	if err != nil {
		t.Fatal(err)
	}
	return w
}

func TestWeekOf_StartsOnMonday(t *testing.T) {
	key, start, end := weekOf(time.Date(2027, 1, 14, 12, 0, 0, 0, time.Local)) // a Thursday
	if key != "2027-W02" || start.Weekday() != time.Monday || start.Day() != 11 || end.Sub(start) != 7*24*time.Hour {
		t.Fatalf("week = %s %v %v", key, start, end)
	}
	key, start, _ = weekOf(time.Date(2027, 1, 17, 23, 59, 0, 0, time.Local)) // Sunday night
	if key != "2027-W02" || start.Day() != 11 {
		t.Fatalf("sunday = %s %v", key, start)
	}
}

func TestBuild_MostlyNewSongsDownloadedAheadForUsersWhoMayAddMusic(t *testing.T) {
	e := newEnv(t)
	e.setPrefs(t, "u1", Prefs{Enabled: true, Size: 10, NewShare: 70, KeepRule: KeepPlaylist})
	w := e.current(t, "u1")
	if w.Week != "2027-W02" || len(w.Items) != 10 || !w.Downloads {
		t.Fatalf("week %s, %d items, downloads %v", w.Week, len(w.Items), w.Downloads)
	}
	ext, own := 0, 0
	for i, it := range w.Items {
		if it.External != nil {
			ext++
			if it.State != StateDownloading || it.JobID == "" || it.JobStatus != string(core.DownloadQueued) {
				t.Errorf("item %d not downloading: %+v", i, it)
			}
		} else if it.Owned {
			own++
		}
	}
	if ext != 7 || own != 3 || len(e.dl.enqueued) != 7 {
		t.Fatalf("ext %d own %d enqueued %d", ext, own, len(e.dl.enqueued))
	}
	// New songs are spread through the list, not bunched at one end.
	if w.Items[0].External == nil && w.Items[1].External == nil && w.Items[2].External == nil {
		t.Errorf("new songs bunched: %+v", w.Items[:3])
	}
	// A download that lands makes the song playable.
	e.dl.land(w.Items[0].JobID, "lib-0", "/music/a.mp3")
	w = e.current(t, "u1")
	if !w.Items[0].Owned || w.Items[0].Track == nil || w.Items[0].Track.ID != "lib-0" {
		t.Fatalf("landed item = %+v", w.Items[0])
	}
	// The same week isn't rebuilt.
	if e.eng.discoverCalls != 1 {
		t.Fatalf("discover called %d times", e.eng.discoverCalls)
	}
}

func TestBuild_NoDownloadsWithoutPermissionSwitchOrBudget(t *testing.T) {
	e := newEnv(t)
	w := e.current(t, "u2") // request-only user
	if w.Downloads || len(e.dl.enqueued) != 0 {
		t.Fatalf("request-only user got downloads: %v %d", w.Downloads, len(e.dl.enqueued))
	}
	for _, it := range w.Items {
		if it.External != nil && it.State != StateSuggested {
			t.Fatalf("item = %+v", it)
		}
	}
	ctx := context.Background()
	if err := e.q.UpsertSetting(ctx, db.UpsertSettingParams{Key: SettingWeeklyCap, Value: "4"}); err != nil {
		t.Fatal(err)
	}
	e.current(t, "u1")
	if len(e.dl.enqueued) != 4 {
		t.Fatalf("cap 4: enqueued %d", len(e.dl.enqueued))
	}
	if err := e.q.UpsertSetting(ctx, db.UpsertSettingParams{Key: SettingDownloads, Value: "false"}); err != nil {
		t.Fatal(err)
	}
	if w := e.current(t, "u3"); w.Downloads || len(e.dl.enqueued) != 4 {
		t.Fatalf("downloads switched off: %v %d", w.Downloads, len(e.dl.enqueued))
	}
}

func TestBuild_SongAlreadyDownloadedIsLinkedButNeverOurs(t *testing.T) {
	e := newEnv(t)
	e.dl.existing["w0-0"] = core.DownloadJob{ID: "old-job", Status: core.DownloadCompleted, LibraryTrackID: "lib-old", CreatedAt: e.now.Add(-30 * 24 * time.Hour).Unix()}
	e.dl.jobs["old-job"] = e.dl.existing["w0-0"]
	e.setPrefs(t, "u1", Prefs{Enabled: true, Size: 10, NewShare: 100, KeepRule: KeepPlaylist})
	e.current(t, "u1")
	e.now = e.now.Add(7 * 24 * time.Hour)
	e.eng.week = 1
	e.current(t, "u1")
	if len(e.removed) != 0 {
		t.Fatalf("removed files that were never landed: %v", e.removed)
	}
	if e.dl.jobs["old-job"].LibraryTrackID != "lib-old" {
		t.Fatal("old job touched")
	}
}

func TestWeekEnd_KeepRules(t *testing.T) {
	ctx := context.Background()
	for _, tc := range []struct {
		rule string
		// expected state per scenario
		playlisted, playedByMe, playedByOther, untouched string
	}{
		{KeepPlaylist, StateKept, StateRemoved, StateKept, StateRemoved},
		{KeepPlayed, StateKept, StateKept, StateKept, StateRemoved},
		{KeepAlways, StateKept, StateKept, StateKept, StateKept},
	} {
		t.Run(tc.rule, func(t *testing.T) {
			e := newEnv(t)
			e.setPrefs(t, "u1", Prefs{Enabled: true, Size: 10, NewShare: 60, KeepRule: tc.rule})
			w := e.current(t, "u1")
			var ext []Item
			for _, it := range w.Items {
				if it.External != nil {
					ext = append(ext, it)
				}
			}
			if len(ext) != 6 {
				t.Fatalf("ext = %d", len(ext))
			}
			for i, it := range ext[:5] {
				e.dl.land(it.JobID, fmt.Sprintf("lib-%d", i), fmt.Sprintf("/music/%d.mp3", i))
			}
			// ext[0]: in someone's playlist.
			if _, err := e.q.UpsertSyncedPlaylist(ctx, db.UpsertSyncedPlaylistParams{
				ID: "pl", Source: "local", ExternalID: "pl", Name: "Mine", TracksJson: `[{"source":"library","externalId":"lib-0","title":"x"}]`, Mode: "once", CreatedAt: 1,
			}); err != nil {
				t.Fatal(err)
			}
			// ext[1]: I played it through. ext[2]: someone else did.
			listen := func(user, lib string) {
				if err := e.q.InsertCatalogEntity(ctx, db.InsertCatalogEntityParams{ID: "cat-" + lib, Kind: "track", Title: lib, Artist: "a", CreatedAt: 1}); err != nil {
					t.Fatal(err)
				}
				if err := e.q.InsertListenEvent(ctx, db.InsertListenEventParams{ID: "le-" + lib, UserID: user, CatalogID: "cat-" + lib, LibraryTrackID: lib,
					StartedAt: 1, MsPlayed: 200000, DurationMs: 200000, Outcome: "completed", CreatedAt: 1}); err != nil {
					t.Fatal(err)
				}
			}
			listen("u1", "lib-1")
			listen("u3", "lib-2")
			// ext[3], ext[4]: landed, untouched. ext[5]: still downloading.
			e.now = e.now.Add(7 * 24 * time.Hour)
			e.eng.week = 1
			e.current(t, "u1")

			states := map[string]string{}
			rows, err := e.q.ListDiscoverItems(ctx, db.ListDiscoverItemsParams{UserID: "u1", Week: "2027-W02"})
			if err != nil {
				t.Fatal(err)
			}
			for _, r := range rows {
				states[r.JobID] = r.State
			}
			got := []string{states[ext[0].JobID], states[ext[1].JobID], states[ext[2].JobID], states[ext[3].JobID]}
			want := []string{tc.playlisted, tc.playedByMe, tc.playedByOther, tc.untouched}
			for i := range want {
				if got[i] != want[i] {
					t.Errorf("scenario %d: state %s, want %s", i, got[i], want[i])
				}
			}
			if states[ext[5].JobID] != StateDownloading {
				t.Errorf("in-flight download decided early: %s", states[ext[5].JobID])
			}
			wantRemoved := 0
			for _, s := range want {
				if s == StateRemoved {
					wantRemoved++
				}
			}
			wantRemoved++ // ext[4] follows ext[3]
			if tc.untouched == StateKept {
				wantRemoved--
			}
			if len(e.removed) != wantRemoved {
				t.Errorf("removed %v, want %d files", e.removed, wantRemoved)
			}
			if wantRemoved > 0 && e.rescans != 1 {
				t.Errorf("rescans = %d", e.rescans)
			}
			// The new week doesn't offer last week's songs again.
			for i := 0; i < 6; i++ {
				if !e.eng.lastExclude[fmt.Sprintf("deezer:w0-%d", i)] {
					t.Errorf("w0-%d not excluded from the new week", i)
				}
			}
		})
	}
}

func TestWeekEnd_RemovesTheTrackFileNotTheDownloadFolder(t *testing.T) {
	e := newEnv(t)
	// spotDL reports only its output folder; the library knows the file.
	e.svc.opt.Locate = func(id string) (string, bool) {
		if id == "lib-0" {
			return "/music/Artist/Album/01 Song.mp3", true
		}
		return "", false
	}
	e.setPrefs(t, "u1", Prefs{Enabled: true, Size: 10, NewShare: 20, KeepRule: KeepPlaylist})
	w := e.current(t, "u1")
	var ext []Item
	for _, it := range w.Items {
		if it.External != nil {
			ext = append(ext, it)
		}
	}
	if len(ext) != 2 {
		t.Fatalf("ext = %d", len(ext))
	}
	e.dl.land(ext[0].JobID, "lib-0", "/music")
	e.dl.land(ext[1].JobID, "lib-1", "/music")
	e.now = e.now.Add(7 * 24 * time.Hour)
	e.eng.week = 1
	e.current(t, "u1")
	want := []string{"/music/Artist/Album/01 Song.mp3", "/music"}
	if len(e.removed) != 2 || e.removed[0] != want[0] || e.removed[1] != want[1] {
		t.Fatalf("removed %v, want %v", e.removed, want)
	}
}

func TestWeekEnd_FailedDownloadExpiresAndSharedSongStays(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	e.setPrefs(t, "u1", Prefs{Enabled: true, Size: 10, NewShare: 100, KeepRule: KeepPlaylist})
	w := e.current(t, "u1")
	j := e.dl.jobs[w.Items[0].JobID]
	j.Status = core.DownloadFailed
	e.dl.jobs[j.ID] = j
	e.dl.land(w.Items[1].JobID, "lib-shared", "/music/shared.mp3")
	// u3 has the same download in their current Discover.
	if err := e.q.InsertDiscoverItem(ctx, db.InsertDiscoverItemParams{ID: "x", UserID: "u3", Week: "2027-W03", Title: "t", Artist: "a",
		JobID: w.Items[1].JobID, State: StateDownloading, CreatedAt: 1}); err != nil {
		t.Fatal(err)
	}
	e.now = e.now.Add(7 * 24 * time.Hour)
	e.eng.week = 1
	e.current(t, "u1")
	rows, _ := e.q.ListDiscoverItems(ctx, db.ListDiscoverItemsParams{UserID: "u1", Week: "2027-W02"})
	if rows[0].State != StateExpired || rows[1].State != StateKept {
		t.Fatalf("states = %s %s", rows[0].State, rows[1].State)
	}
	for _, p := range e.removed {
		if p == "/music/shared.mp3" {
			t.Fatal("removed a song in another user's Discover")
		}
	}
}

func TestPrefs_DefaultsValidationAndDisable(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	p, err := e.svc.Prefs(ctx, "u1")
	if err != nil || p != DefaultPrefs || p.KeepRule != KeepPlaylist {
		t.Fatalf("defaults = %+v %v", p, err)
	}
	for _, bad := range []Prefs{
		{Enabled: true, Size: 30, NewShare: 70, KeepRule: "never"},
		{Enabled: true, Size: 5, NewShare: 70, KeepRule: KeepPlaylist},
		{Enabled: true, Size: 30, NewShare: 120, KeepRule: KeepPlaylist},
	} {
		if _, err := e.svc.SetPrefs(ctx, "u1", bad); !errors.Is(err, ErrInvalid) {
			t.Errorf("%+v: %v", bad, err)
		}
	}
	e.setPrefs(t, "u1", Prefs{Enabled: false, Size: 30, NewShare: 70, KeepRule: KeepAlways})
	w := e.current(t, "u1")
	if len(w.Items) != 0 || e.eng.discoverCalls != 0 || w.Prefs.Enabled {
		t.Fatalf("disabled discover built a week: %+v", w)
	}
}

func TestRunAll_BuildsEveryUsersWeek(t *testing.T) {
	e := newEnv(t)
	e.svc.RunAll(context.Background())
	for _, u := range []string{"u1", "u2", "u3"} {
		rows, err := e.q.ListDiscoverItems(context.Background(), db.ListDiscoverItemsParams{UserID: u, Week: "2027-W02"})
		if err != nil || len(rows) != DefaultPrefs.Size {
			t.Errorf("%s: %d rows %v", u, len(rows), err)
		}
	}
}

func TestSafeRemover(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	rm := SafeRemover([]string{root, ""})
	song := filepath.Join(root, "Artist", "Album", "song.mp3")
	keep := filepath.Join(root, "Artist", "Other", "keep.mp3")
	for _, p := range []string{song, keep} {
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := rm(song); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "Artist", "Album")); !os.IsNotExist(err) {
		t.Error("empty album folder left behind")
	}
	if _, err := os.Stat(keep); err != nil {
		t.Error("removed a neighbour")
	}
	if _, err := os.Stat(root); err != nil {
		t.Error("removed the root")
	}
	out := filepath.Join(outside, "x.mp3")
	if err := os.WriteFile(out, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "link.mp3")
	if err := os.Symlink(out, link); err != nil {
		t.Fatal(err)
	}
	for name, p := range map[string]string{
		"outside":        out,
		"symlink escape": link,
		"traversal":      filepath.Join(root, "..", filepath.Base(outside), "x.mp3"),
		"a folder":       filepath.Join(root, "Artist"),
		"the root":       root,
		"empty":          "",
	} {
		if err := rm(p); err == nil {
			t.Errorf("%s: removed", name)
		}
	}
	if _, err := os.Stat(out); err != nil {
		t.Fatal("file outside the root was deleted")
	}
}

func TestRegenerate_ReplacesPicksNotDownloadedAndKeepsDownloads(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	e.setPrefs(t, "u1", Prefs{Enabled: true, Size: 10, NewShare: 70, KeepRule: KeepPlaylist})
	before := e.current(t, "u1")
	downloading := map[string]bool{}
	oldOwned := map[string]bool{}
	for _, it := range before.Items {
		if it.State == StateDownloading {
			downloading[it.ID] = true
		} else if it.Track != nil {
			oldOwned[it.Track.ID] = true
		}
	}
	if len(downloading) != 7 || len(oldOwned) != 3 {
		t.Fatalf("setup: %d downloading, %d owned", len(downloading), len(oldOwned))
	}
	// Prefs changed mid-week apply to the regenerated list.
	e.setPrefs(t, "u1", Prefs{Enabled: true, Size: 12, NewShare: 70, KeepRule: KeepPlaylist})
	w, err := e.svc.Regenerate(ctx, "u1")
	if err != nil {
		t.Fatal(err)
	}
	if len(w.Items) != 12 {
		t.Fatalf("regenerated %d items, want 12", len(w.Items))
	}
	kept := 0
	for _, it := range w.Items {
		if downloading[it.ID] {
			kept++
			continue
		}
		if it.Track != nil && oldOwned[it.Track.ID] {
			t.Errorf("old pick %s offered again", it.Track.ID)
		}
		if it.External != nil && it.State == StateDownloading && it.JobID == "" {
			t.Errorf("new download without a job: %+v", it)
		}
	}
	if kept != 7 {
		t.Fatalf("downloaded songs kept = %d, want 7", kept)
	}
	// The weekly cap still holds across regenerations (30 by default).
	if len(e.dl.enqueued) > DefaultWeeklyCap {
		t.Fatalf("enqueued %d, over the weekly cap", len(e.dl.enqueued))
	}
}

func TestRegenerate_EverythingNewWithoutDownloadsAndRefusedWhenOff(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	e.setPrefs(t, "u2", Prefs{Enabled: true, Size: 10, NewShare: 50, KeepRule: KeepPlaylist})
	before := e.current(t, "u2")
	old := map[string]bool{}
	for _, it := range before.Items {
		if it.External != nil {
			old[it.External.ExternalID] = true
		} else if it.Track != nil {
			old[it.Track.ID] = true
		}
	}
	w, err := e.svc.Regenerate(ctx, "u2")
	if err != nil {
		t.Fatal(err)
	}
	if len(w.Items) != 10 {
		t.Fatalf("regenerated %d items", len(w.Items))
	}
	for _, it := range w.Items {
		id := ""
		if it.External != nil {
			id = it.External.ExternalID
		} else if it.Track != nil {
			id = it.Track.ID
		}
		if old[id] {
			t.Errorf("%s was in the old list", id)
		}
	}
	e.setPrefs(t, "u2", Prefs{Enabled: false, Size: 10, NewShare: 50, KeepRule: KeepPlaylist})
	if _, err := e.svc.Regenerate(ctx, "u2"); !errors.Is(err, ErrDisabled) {
		t.Fatalf("regenerate while off = %v, want ErrDisabled", err)
	}
}
