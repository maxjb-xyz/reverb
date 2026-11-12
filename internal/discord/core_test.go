package discord

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/maxjb-xyz/reverb/internal/core"
	"github.com/maxjb-xyz/reverb/internal/playback"
)

type memKV struct {
	mu sync.Mutex
	m  map[string]string
}

func (k *memKV) Get(_ context.Context, key string) (string, error) {
	k.mu.Lock()
	defer k.mu.Unlock()
	return k.m[key], nil
}

func (k *memKV) Put(_ context.Context, key, v string) error {
	k.mu.Lock()
	defer k.mu.Unlock()
	k.m[key] = v
	return nil
}

type fakeVoice struct {
	mu        sync.Mutex
	joined    map[string]string
	listeners int
	player    *Player
}

func (v *fakeVoice) Join(_ context.Context, g, c string, p *Player) error {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.joined[g] = c
	v.player = p
	return nil
}

func (v *fakeVoice) Leave(_ context.Context, g string) {
	v.mu.Lock()
	defer v.mu.Unlock()
	delete(v.joined, g)
}

func (v *fakeVoice) Listeners(string, string) int {
	v.mu.Lock()
	defer v.mu.Unlock()
	return v.listeners
}

type fakeLib struct{}

func (fakeLib) Search(_ context.Context, q string, _ []core.EntityType) (core.SearchResults, error) {
	if strings.Contains(q, "nothing") {
		return core.SearchResults{}, nil
	}
	return core.SearchResults{Tracks: []core.Track{{ID: "t-" + q, Title: "Song " + q, Artist: "Band", DurationMs: 60_000}}}, nil
}

type rig struct {
	core  *Core
	hub   *playback.Hub
	voice *fakeVoice
	links *Links
}

func newRig() *rig {
	hub := playback.NewHub(nil, nil, nil)
	links := NewLinks(&memKV{m: map[string]string{}}, nil)
	v := &fakeVoice{joined: map[string]string{}, listeners: 1}
	c := &Core{
		Hub: hub, Links: links, Library: fakeLib{}, Opener: &fakeOpener{frames: 1000}, Voice: v,
		Users: func(_ context.Context, id string) (string, bool) { return "user-" + id, id != "disabled" },
	}
	return &rig{core: c, hub: hub, voice: v, links: links}
}

func (r *rig) link(t *testing.T, reverbID, discordID string) {
	t.Helper()
	code, _ := r.links.NewCode(reverbID)
	rep := r.core.Handle(context.Background(), Invocation{UserID: discordID, UserName: "d" + discordID, Command: "link", Options: map[string]string{"code": code}})
	if !strings.Contains(rep.Text, "Linked") {
		t.Fatalf("link: %q", rep.Text)
	}
}

func inv(cmd, discordID, voice string, opts map[string]string) Invocation {
	return Invocation{GuildID: "g1", UserID: discordID, UserName: "d" + discordID, VoiceChannelID: voice, VoiceChannelName: "Lounge", Command: cmd, Options: opts}
}

func waitFor(t *testing.T, what string, ok func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for !ok() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(2 * time.Millisecond)
	}
}

func TestUnlinkedUsersAreToldToLink(t *testing.T) {
	r := newRig()
	rep := r.core.Handle(context.Background(), inv("join", "d1", "vc", nil))
	if !rep.Ephemeral || !strings.Contains(rep.Text, "/link") {
		t.Fatalf("reply = %+v", rep)
	}
}

func TestLinkCodes(t *testing.T) {
	r := newRig()
	ctx := context.Background()
	code, _ := r.links.NewCode("u1")
	if rep := r.core.Handle(ctx, Invocation{UserID: "d1", Command: "link", Options: map[string]string{"code": "WRONG-CODE"}}); !strings.Contains(rep.Text, "wrong") {
		t.Fatalf("bad code reply = %q", rep.Text)
	}
	// Lowercase and without the dash still works; the code is single use.
	loose := strings.ToLower(strings.ReplaceAll(code, "-", ""))
	if rep := r.core.Handle(ctx, Invocation{UserID: "d1", UserName: "Dee", Command: "link", Options: map[string]string{"code": loose}}); !strings.Contains(rep.Text, "user-u1") {
		t.Fatalf("link reply = %q", rep.Text)
	}
	if rep := r.core.Handle(ctx, Invocation{UserID: "d2", Command: "link", Options: map[string]string{"code": code}}); !strings.Contains(rep.Text, "wrong") {
		t.Fatalf("code reused: %q", rep.Text)
	}
	if l, _ := r.links.ByUser(ctx, "u1"); l.DiscordID != "d1" || l.DiscordName != "Dee" {
		t.Fatalf("link = %+v", l)
	}
	// Linking d1 to another account moves it.
	code2, _ := r.links.NewCode("u2")
	r.core.Handle(ctx, Invocation{UserID: "d1", Command: "link", Options: map[string]string{"code": code2}})
	if l, _ := r.links.ByDiscord(ctx, "d1"); l.ReverbUserID != "u2" {
		t.Fatalf("relink = %+v", l)
	}
	if l, _ := r.links.ByUser(ctx, "u1"); l.DiscordID != "" {
		t.Fatalf("old link kept: %+v", l)
	}
}

func TestLinkBruteForceIsThrottled(t *testing.T) {
	r := newRig()
	ctx := context.Background()
	code, _ := r.links.NewCode("u1")
	for i := 0; i < maxRedeemFailures; i++ {
		r.core.Handle(ctx, Invocation{UserID: "d9", Command: "link", Options: map[string]string{"code": "AAAA-AAAA"}})
	}
	if rep := r.core.Handle(ctx, Invocation{UserID: "d9", Command: "link", Options: map[string]string{"code": code}}); !strings.Contains(rep.Text, "too many") {
		t.Fatalf("not throttled: %q", rep.Text)
	}
}

func TestDisabledAccountsCannotUseTheBot(t *testing.T) {
	r := newRig()
	r.link(t, "disabled", "d1")
	if rep := r.core.Handle(context.Background(), inv("join", "d1", "vc", nil)); !strings.Contains(rep.Text, "disabled") {
		t.Fatalf("reply = %q", rep.Text)
	}
}

func TestJoinNeedsVoiceAndTakesOverTheSession(t *testing.T) {
	r := newRig()
	ctx := context.Background()
	r.link(t, "u1", "d1")
	if rep := r.core.Handle(ctx, inv("join", "d1", "", nil)); !strings.Contains(rep.Text, "voice channel first") {
		t.Fatalf("reply = %q", rep.Text)
	}

	// The user is listening in a browser.
	web, _ := r.hub.Connect(ctx, "u1", playback.DeviceInfo{ID: "web", Name: "Chrome"})
	web.Claim(playback.State{Queue: []playback.Track{ptrack("a"), ptrack("b")}, Index: 1, PositionMs: 5000, Playing: true, Repeat: "off", Volume: 1})

	rep := r.core.Handle(ctx, inv("join", "d1", "vc1", nil))
	if !strings.Contains(rep.Text, "Playing your Reverb session") {
		t.Fatalf("reply = %q", rep.Text)
	}
	if r.voice.joined["g1"] != "vc1" {
		t.Fatalf("joined = %v", r.voice.joined)
	}
	waitFor(t, "the bot to become active", func() bool { return r.hub.Snapshot("u1").ActiveDeviceID == "discord-g1" })
	waitFor(t, "the bot to play b", func() bool {
		cur, ok := r.voice.player.Current()
		return ok && cur.ID == "b" && r.voice.player.State().Playing
	})
	// The device shows up in the picker with a Discord name.
	found := false
	for _, d := range r.hub.Snapshot("u1").Devices {
		if d.ID == "discord-g1" && d.Kind == "discord" && d.Name == "Lounge (Discord)" {
			found = true
		}
	}
	if !found {
		t.Fatalf("devices = %+v", r.hub.Snapshot("u1").Devices)
	}
}

func TestPlayJoinsAndPlaysNow(t *testing.T) {
	r := newRig()
	ctx := context.Background()
	r.link(t, "u1", "d1")
	rep := r.core.Handle(ctx, inv("play", "d1", "vc1", map[string]string{"song": "hello"}))
	if !strings.Contains(rep.Text, "Song hello") {
		t.Fatalf("reply = %q", rep.Text)
	}
	waitFor(t, "hello to play", func() bool {
		cur, ok := r.voice.player.Current()
		return ok && cur.ID == "t-hello" && r.voice.player.State().Playing
	})
	waitFor(t, "the session to show it", func() bool {
		st := r.hub.Snapshot("u1").State
		return st != nil && st.Index >= 0 && st.Queue[st.Index].ID == "t-hello"
	})
	if rep := r.core.Handle(ctx, inv("play", "d1", "vc1", map[string]string{"song": "nothing at all"})); !strings.Contains(rep.Text, "Nothing in your library") {
		t.Fatalf("reply = %q", rep.Text)
	}
	// /add queues it on the device playing (the bot here).
	r.core.Handle(ctx, inv("add", "d1", "vc1", map[string]string{"song": "later"}))
	waitFor(t, "later to be queued", func() bool {
		q := r.voice.player.State().Queue
		return len(q) > 0 && q[len(q)-1].ID == "t-later"
	})
	// Controls reach the session wherever it plays.
	r.core.Handle(ctx, inv("pause", "d1", "", nil))
	waitFor(t, "pause", func() bool { return !r.voice.player.State().Playing })
	if rep := r.core.Handle(ctx, inv("nowplaying", "d1", "", nil)); !strings.Contains(rep.Text, "Song hello") || !strings.Contains(rep.Text, "Lounge (Discord)") {
		t.Fatalf("nowplaying = %q", rep.Text)
	}
	if rep := r.core.Handle(ctx, inv("queue", "d1", "", nil)); !strings.Contains(rep.Text, "Song later") {
		t.Fatalf("queue = %q", rep.Text)
	}
}

func TestAnotherUserCannotTakeTheBot(t *testing.T) {
	r := newRig()
	ctx := context.Background()
	r.link(t, "u1", "d1")
	r.link(t, "u2", "d2")
	r.core.Handle(ctx, inv("join", "d1", "vc1", nil))
	rep := r.core.Handle(ctx, inv("play", "d2", "vc1", map[string]string{"song": "mine"}))
	if !strings.Contains(rep.Text, "playing for dd1") {
		t.Fatalf("reply = %q", rep.Text)
	}
	if rep := r.core.Handle(ctx, inv("leave", "d2", "vc1", nil)); !strings.Contains(rep.Text, "Only dd1") {
		t.Fatalf("leave reply = %q", rep.Text)
	}
	// u2's /skip goes to u2's own session (nothing playing), never to u1's.
	if rep := r.core.Handle(ctx, inv("skip", "d2", "vc1", nil)); !strings.Contains(rep.Text, "Nothing is playing") {
		t.Fatalf("skip reply = %q", rep.Text)
	}
}

func TestLeaveKeepsThePlaceAndIdleChannelsAreLeft(t *testing.T) {
	r := newRig()
	ctx := context.Background()
	r.link(t, "u1", "d1")
	r.core.Handle(ctx, inv("play", "d1", "vc1", map[string]string{"song": "hello"}))
	waitFor(t, "play", func() bool { return r.voice.player.State().Playing })
	pull(t, r.voice.player, 10)
	if rep := r.core.Handle(ctx, inv("leave", "d1", "vc1", nil)); !strings.Contains(rep.Text, "Bye") {
		t.Fatalf("reply = %q", rep.Text)
	}
	if _, ok := r.voice.joined["g1"]; ok {
		t.Fatal("still in voice")
	}
	st := r.hub.Snapshot("u1")
	if st.ActiveDeviceID != "" || st.State == nil || st.State.Playing || st.State.PositionMs < 200 {
		t.Fatalf("session after leave = %+v %+v", st, st.State)
	}

	r.core.Handle(ctx, inv("join", "d1", "vc1", nil))
	r.voice.mu.Lock()
	r.voice.listeners = 0
	r.voice.mu.Unlock()
	now := time.Now()
	r.core.SweepIdle(ctx, now)
	if r.core.Sessions() != 1 {
		t.Fatal("left too early")
	}
	r.core.SweepIdle(ctx, now.Add(3*time.Minute))
	if r.core.Sessions() != 0 {
		t.Fatal("stayed in an empty channel")
	}
}

func TestSuggest(t *testing.T) {
	r := newRig()
	if got := r.core.Suggest(context.Background(), "x"); got != nil {
		t.Fatalf("one letter suggested %v", got)
	}
	if got := r.core.Suggest(context.Background(), "hel"); len(got) != 1 || got[0] != "Song hel Band" {
		t.Fatalf("suggest = %v", got)
	}
}

func TestInviteURL(t *testing.T) {
	u := InviteURL("123")
	if !strings.Contains(u, "client_id=123") || !strings.Contains(u, "applications.commands") || !strings.Contains(u, "permissions=3146752") {
		t.Fatalf("url = %s", u)
	}
}

func TestReplacedDeviceLeavesTheChannel(t *testing.T) {
	r := newRig()
	ctx := context.Background()
	r.link(t, "u1", "d1")
	r.core.Handle(ctx, inv("play", "d1", "vc1", map[string]string{"song": "hello"}))
	waitFor(t, "play", func() bool { return r.voice.player.State().Playing })
	p := r.voice.player
	// Something else connects with the bot's device id: the hub drops ours.
	other, ok := r.hub.Connect(ctx, "u1", playback.DeviceInfo{ID: deviceID("g1"), Name: "x", Kind: "discord"})
	if !ok {
		t.Fatal("connect")
	}
	defer other.Close()
	waitFor(t, "leave", func() bool { return r.core.Sessions() == 0 })
	if p.State().Playing {
		t.Fatal("still playing after the hub dropped the device")
	}
}

func TestVoiceEventsForOtherChannelsAreIgnored(t *testing.T) {
	r := newRig()
	ctx := context.Background()
	r.link(t, "u1", "d1")
	r.core.Handle(ctx, inv("join", "d1", "vc1", nil))
	r.core.VoiceLost(ctx, "g1", "vc0") // late event from an earlier channel
	if r.core.Sessions() != 1 {
		t.Fatal("a stale leave ended the session")
	}
	r.core.VoiceMoved("g1", "vc2", "Den")
	if rep := r.core.Handle(ctx, inv("join", "d1", "vc2", nil)); !strings.Contains(rep.Text, "Den") {
		t.Fatalf("after move: %q", rep.Text)
	}
	r.core.VoiceLost(ctx, "g1", "vc2")
	if r.core.Sessions() != 0 {
		t.Fatal("kicked but still in a session")
	}
}

func TestOwnerInAnotherChannelIsAskedToLeaveFirst(t *testing.T) {
	r := newRig()
	ctx := context.Background()
	r.link(t, "u1", "d1")
	r.core.Handle(ctx, inv("join", "d1", "vc1", nil))
	if rep := r.core.Handle(ctx, inv("join", "d1", "vc2", nil)); !rep.Ephemeral || !strings.Contains(rep.Text, "/leave") {
		t.Fatalf("reply = %+v", rep)
	}
	if r.voice.joined["g1"] != "vc1" {
		t.Fatal("moved channels")
	}
}

func TestUnlinkLeavesTheUsersChannels(t *testing.T) {
	r := newRig()
	ctx := context.Background()
	r.link(t, "u1", "d1")
	r.core.Handle(ctx, inv("join", "d1", "vc1", nil))
	other := inv("join", "d1", "vc9", nil)
	other.GuildID = "g2"
	r.core.Handle(ctx, other)
	if r.core.Sessions() != 2 {
		t.Fatalf("sessions = %d", r.core.Sessions())
	}
	r.core.LeaveUser(ctx, "u1")
	if r.core.Sessions() != 0 {
		t.Fatal("still playing for an unlinked account")
	}
}
