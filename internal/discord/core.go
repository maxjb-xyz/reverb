package discord

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/maxjb-xyz/reverb/internal/core"
	"github.com/maxjb-xyz/reverb/internal/playback"
)

// Searcher finds library tracks for /play and /add.
type Searcher interface {
	Search(ctx context.Context, q string, types []core.EntityType) (core.SearchResults, error)
}

// Voice joins and leaves voice channels. The disgo adapter implements it;
// tests use a fake.
type Voice interface {
	// Join connects to channelID and starts pulling frames from p.
	Join(ctx context.Context, guildID, channelID string, p *Player) error
	// Leave disconnects from the guild's voice channel.
	Leave(ctx context.Context, guildID string)
	// Listeners counts the people (not bots) in a voice channel.
	Listeners(guildID, channelID string) int
}

// Invocation is a slash command as the core sees it, free of Discord types.
type Invocation struct {
	GuildID  string
	UserID   string
	UserName string
	// VoiceChannelID is where the user is in voice in this guild ("" if not).
	VoiceChannelID   string
	VoiceChannelName string
	Command          string
	// Options holds the string options by name.
	Options map[string]string
}

// Reply is the bot's answer. Ephemeral replies are seen only by the caller.
type Reply struct {
	Text      string
	Ephemeral bool
	// Buttons go in one row under the text.
	Buttons []Button
}

// Button is a message button. Its ID comes back in Core.Press.
type Button struct {
	ID    string
	Label string
}

// Request outcomes.
const (
	RequestOwned        = "owned"        // already in the library
	RequestDownloading  = "downloading"  // approved and queued for download
	RequestPending      = "pending"      // waiting for an admin
	RequestExists       = "exists"       // asked for before
	RequestNotFound     = "notfound"     // no online match
	RequestDenied       = "denied"       // the account can't request
	RequestQuota        = "quota"        // too many pending requests
	RequestUnavailable  = "unavailable"  // no online search or downloader
	RequestSearchFailed = "searchfailed" // every search source failed
	RequestUnchecked    = "unchecked"    // couldn't check the library
)

// RequestOutcome is what happened to a /request.
type RequestOutcome struct {
	Status string
	Title  string
	Artist string
}

// Requester asks for a song that isn't in the library, as a Reverb user and
// with their role's rights (the same path as a request from the web).
type Requester interface {
	Request(ctx context.Context, userID, query string) (RequestOutcome, error)
}

// UserCheck reports whether a Reverb account may use the bot (exists, not
// disabled) and its name.
type UserCheck func(ctx context.Context, userID string) (name string, ok bool)

// Core is the bot's brain: slash commands, links and one player per guild.
type Core struct {
	Hub     *playback.Hub
	Links   *Links
	Library Searcher
	Opener  SourceOpener
	// Voice is the Discord connection. Set it with SetVoice once the core
	// is in use: the bot service swaps it on a reconnect.
	Voice Voice
	Users UserCheck
	// Requests backs /request; nil hides the feature.
	Requests Requester
	Log      *slog.Logger
	// IdleLeave is how long the bot stays in a channel with nobody listening.
	IdleLeave time.Duration

	mu     sync.Mutex
	guilds map[string]*guildSession
	// joinMu serializes joins so two people can't join one guild at once.
	joinMu sync.Mutex
}

type guildSession struct {
	guildID     string
	channelID   string
	channelName string
	owner       string // Reverb user id
	ownerName   string // Discord display name
	player      *Player
	dev         *playback.Conn
	changed     chan struct{}
	stop        chan struct{}
	emptySince  time.Time
}

// SetVoice swaps the Discord connection.
func (c *Core) SetVoice(v Voice) {
	c.mu.Lock()
	c.Voice = v
	c.mu.Unlock()
}

func (c *Core) voice() Voice {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.Voice
}

func (c *Core) log() *slog.Logger {
	if c.Log != nil {
		return c.Log
	}
	return slog.Default()
}

func deviceID(guildID string) string { return "discord-" + guildID }

// Commands is the slash command list (name -> description, string options).
var Commands = []CommandSpec{
	{Name: "link", Description: "Link your Discord account to Reverb", Options: []OptionSpec{{Name: "code", Description: "The code from Reverb > Settings > Integrations", Required: true}}},
	{Name: "unlink", Description: "Unlink your Discord account from Reverb"},
	{Name: "join", Description: "Play your Reverb session in your voice channel"},
	{Name: "play", Description: "Play a song from your library now", Options: []OptionSpec{{Name: "song", Description: "Song, artist or album", Required: true, Autocomplete: true}}},
	{Name: "add", Description: "Add a song from your library to the queue", Options: []OptionSpec{{Name: "song", Description: "Song, artist or album", Required: true, Autocomplete: true}}},
	{Name: "pause", Description: "Pause"},
	{Name: "resume", Description: "Resume"},
	{Name: "skip", Description: "Skip to the next song"},
	{Name: "back", Description: "Go back a song (or to the start of this one)"},
	{Name: "nowplaying", Description: "What's playing"},
	{Name: "queue", Description: "What's coming up"},
	{Name: "leave", Description: "Stop and leave the voice channel"},
	{Name: "request", Description: "Ask for a song that isn't in the library yet", Options: []OptionSpec{{Name: "song", Description: "Song and artist", Required: true}}},
}

// CommandSpec describes one slash command.
type CommandSpec struct {
	Name        string
	Description string
	Options     []OptionSpec
}

// OptionSpec is a string option of a slash command.
type OptionSpec struct {
	Name         string
	Description  string
	Required     bool
	Autocomplete bool
}

// Handle runs a slash command.
func (c *Core) Handle(ctx context.Context, inv Invocation) Reply {
	if inv.Command == "link" {
		return c.link(ctx, inv)
	}
	if inv.GuildID == "" {
		return Reply{Text: "Use Reverb's commands in a server, not in DMs.", Ephemeral: true}
	}
	userID, errReply := c.linkedUser(ctx, inv)
	if errReply != nil {
		return *errReply
	}
	switch inv.Command {
	case "unlink":
		if err := c.Links.Unlink(ctx, userID); err != nil {
			return Reply{Text: "Couldn't unlink: " + err.Error(), Ephemeral: true}
		}
		c.LeaveUser(ctx, userID)
		return Reply{Text: "Unlinked. Link again any time with a code from Reverb.", Ephemeral: true}
	case "join":
		return c.join(ctx, inv, userID)
	case "play", "add":
		return c.playOrAdd(ctx, inv, userID)
	case "pause", "resume", "skip", "back":
		action := map[string]string{"pause": "pause", "resume": "play", "skip": "next", "back": "prev"}[inv.Command]
		if !c.Hub.Command(userID, playback.Command{Action: action}) {
			return Reply{Text: "Nothing is playing. Use /join to play here.", Ephemeral: true}
		}
		verb := map[string]string{"pause": "Paused.", "resume": "Playing.", "skip": "Skipped.", "back": "Back."}[inv.Command]
		return Reply{Text: verb}
	case "nowplaying":
		return c.nowPlayingCard(inv.UserID, userID)
	case "request":
		return c.request(ctx, inv, userID)
	case "queue":
		return c.queue(userID)
	case "leave":
		gs := c.session(inv.GuildID)
		if gs == nil {
			return Reply{Text: "I'm not in a voice channel here.", Ephemeral: true}
		}
		if gs.owner != userID {
			return Reply{Text: fmt.Sprintf("Only %s can stop this session.", gs.ownerName), Ephemeral: true}
		}
		c.Leave(ctx, inv.GuildID)
		return Reply{Text: "Bye 👋"}
	}
	return Reply{Text: "I don't know that command.", Ephemeral: true}
}

func (c *Core) link(ctx context.Context, inv Invocation) Reply {
	link, err := c.Links.Redeem(ctx, inv.Options["code"], inv.UserID, inv.UserName)
	if err != nil {
		return Reply{Text: "Couldn't link: " + err.Error() + ".", Ephemeral: true}
	}
	name := ""
	if c.Users != nil {
		name, _ = c.Users(ctx, link.ReverbUserID)
	}
	if name != "" {
		return Reply{Text: fmt.Sprintf("Linked to Reverb as **%s**. Hop in a voice channel and use /join.", name), Ephemeral: true}
	}
	return Reply{Text: "Linked. Hop in a voice channel and use /join.", Ephemeral: true}
}

// linkedUser maps the caller to their Reverb account.
func (c *Core) linkedUser(ctx context.Context, inv Invocation) (string, *Reply) {
	link, err := c.Links.ByDiscord(ctx, inv.UserID)
	if err != nil {
		return "", &Reply{Text: "Something went wrong looking up your account.", Ephemeral: true}
	}
	if link.ReverbUserID == "" {
		return "", &Reply{Text: "Link your Reverb account first: in Reverb open Settings > Integrations > Discord, get a code, then run /link here.", Ephemeral: true}
	}
	if c.Users != nil {
		if _, ok := c.Users(ctx, link.ReverbUserID); !ok {
			return "", &Reply{Text: "That Reverb account is disabled or gone.", Ephemeral: true}
		}
	}
	return link.ReverbUserID, nil
}

// channelOf reads where gs is (it changes when the bot is moved).
func (c *Core) channelOf(gs *guildSession) (id, name string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return gs.channelID, gs.channelName
}

func (c *Core) session(guildID string) *guildSession {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.guilds[guildID]
}

// join puts the bot in the caller's voice channel as a device of their
// account and moves their session there.
func (c *Core) join(ctx context.Context, inv Invocation, userID string) Reply {
	gs, reply := c.ensureJoined(ctx, inv, userID)
	if reply != nil {
		return *reply
	}
	_, chName := c.channelOf(gs)
	snap := c.Hub.Snapshot(userID)
	if snap.ActiveDeviceID == deviceID(inv.GuildID) {
		return Reply{Text: fmt.Sprintf("Already playing in **%s**.", chName)}
	}
	if snap.State == nil || len(snap.State.Queue) == 0 {
		return Reply{Text: fmt.Sprintf("Joined **%s**. Nothing queued yet: use /play.", chName)}
	}
	gs.dev.Transfer(playback.Transfer{DeviceID: deviceID(inv.GuildID), Play: true})
	return Reply{Text: fmt.Sprintf("Playing your Reverb session in **%s**.", chName)}
}

// ensureJoined returns the guild's session for userID, joining the caller's
// voice channel when the bot is not there yet.
func (c *Core) ensureJoined(ctx context.Context, inv Invocation, userID string) (*guildSession, *Reply) {
	c.joinMu.Lock()
	defer c.joinMu.Unlock()
	if gs := c.session(inv.GuildID); gs != nil {
		chID, chName := c.channelOf(gs)
		if gs.owner != userID {
			return nil, &Reply{Text: fmt.Sprintf("I'm playing for %s in **%s** right now.", gs.ownerName, chName), Ephemeral: true}
		}
		if inv.VoiceChannelID == "" || inv.VoiceChannelID == chID {
			return gs, nil
		}
		// Leaving and rejoining in one go races Discord's voice events, so
		// the owner moves the bot with /leave (or by dragging it).
		return nil, &Reply{Text: fmt.Sprintf("I'm in **%s**. Use /leave first to bring me to your channel, or drag me over.", chName), Ephemeral: true}
	}
	if inv.VoiceChannelID == "" {
		return nil, &Reply{Text: "Join a voice channel first, then try again.", Ephemeral: true}
	}
	gs := &guildSession{
		guildID:     inv.GuildID,
		channelID:   inv.VoiceChannelID,
		channelName: inv.VoiceChannelName,
		owner:       userID,
		ownerName:   inv.UserName,
		changed:     make(chan struct{}, 1),
		stop:        make(chan struct{}),
	}
	if gs.channelName == "" {
		gs.channelName = "voice"
	}
	gs.player = NewPlayer(c.Opener, c.log(), func() {
		select {
		case gs.changed <- struct{}{}:
		default:
		}
	})
	dev, ok := c.Hub.Connect(ctx, userID, playback.DeviceInfo{
		ID:   deviceID(inv.GuildID),
		Name: gs.channelName + " (Discord)",
		Kind: "discord",
	})
	if !ok {
		return nil, &Reply{Text: "Couldn't add Discord as a device on your account (too many devices?).", Ephemeral: true}
	}
	gs.dev = dev
	v := c.voice()
	if v == nil {
		dev.Close()
		return nil, &Reply{Text: "The bot isn't connected to Discord right now.", Ephemeral: true}
	}
	if err := v.Join(ctx, inv.GuildID, inv.VoiceChannelID, gs.player); err != nil {
		dev.Close()
		c.log().Warn("discord: join voice", "err", err)
		return nil, &Reply{Text: "Couldn't join the voice channel: " + err.Error(), Ephemeral: true}
	}
	c.mu.Lock()
	if c.guilds == nil {
		c.guilds = map[string]*guildSession{}
	}
	c.guilds[inv.GuildID] = gs
	c.mu.Unlock()
	go c.runDevice(gs)
	return gs, nil
}

// runDevice is the bot's half of the playback session for one guild.
func (c *Core) runDevice(gs *guildSession) {
	me := deviceID(gs.guildID)
	active := false
	tick := time.NewTicker(15 * time.Second)
	defer tick.Stop()
	report := func() {
		if active {
			gs.dev.State(gs.player.State())
		}
	}
	for {
		select {
		case <-gs.stop:
			return
		case <-gs.dev.Done():
			// The hub dropped us (replaced or too slow). Playing on would
			// be audio the session doesn't know about: leave the channel.
			// The session lives on with the other devices.
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			c.leaveSession(ctx, gs)
			cancel()
			return
		case m := <-gs.dev.Out():
			switch m.Type {
			case playback.MsgActivate:
				a, _ := m.Payload.(playback.Activate)
				active = true
				gs.player.Adopt(a.State, a.Play)
				report()
			case playback.MsgDeactivate:
				active = false
				gs.player.Pause()
			case playback.MsgCommand:
				if cmd, ok := m.Payload.(*playback.Command); ok && active {
					gs.player.Apply(cmd)
				}
			case playback.MsgSession:
				s, _ := m.Payload.(playback.Session)
				was := active
				active = s.ActiveDeviceID == me
				if active && !was {
					report() // we just claimed: send what changed meanwhile
				}
			}
		case <-gs.changed:
			report()
		case <-tick.C:
			if st := gs.player.State(); active && st.Playing {
				gs.dev.State(st)
			}
		}
	}
}

// playOrAdd handles /play (play now) and /add (queue at the end).
func (c *Core) playOrAdd(ctx context.Context, inv Invocation, userID string) Reply {
	q := strings.TrimSpace(inv.Options["song"])
	if q == "" {
		return Reply{Text: "Tell me what to play.", Ephemeral: true}
	}
	t, ok := c.findTrack(ctx, q)
	if !ok {
		return Reply{Text: fmt.Sprintf("Nothing in your library matches “%s”.", q), Ephemeral: true}
	}
	if inv.Command == "add" {
		// Queue it wherever the session is playing (this channel or not).
		if c.Hub.Command(userID, playback.Command{Action: "enqueue", Track: &t}) {
			return Reply{Text: fmt.Sprintf("Added **%s** by %s to the queue.", t.Title, t.Artist)}
		}
	}
	gs, reply := c.ensureJoined(ctx, inv, userID)
	if reply != nil {
		return *reply
	}
	// Make the bot the device playing, keeping the account's queue.
	if c.Hub.Snapshot(userID).ActiveDeviceID != deviceID(inv.GuildID) {
		snap := c.Hub.Snapshot(userID)
		if snap.State != nil {
			gs.player.Adopt(snap.State, false)
		}
		gs.dev.Claim(gs.player.State())
	}
	if inv.Command == "add" {
		gs.player.Enqueue(t)
		if _, ok := gs.player.Current(); ok && !gs.player.State().Playing {
			gs.player.Play()
		}
		return Reply{Text: fmt.Sprintf("Added **%s** by %s to the queue.", t.Title, t.Artist)}
	}
	gs.player.PlayNext(t)
	return Reply{Text: fmt.Sprintf("▶️ **%s** by %s", t.Title, t.Artist)}
}

func (c *Core) findTrack(ctx context.Context, q string) (playback.Track, bool) {
	if c.Library == nil {
		return playback.Track{}, false
	}
	res, err := c.Library.Search(ctx, q, []core.EntityType{core.EntityTrack})
	if err != nil || len(res.Tracks) == 0 {
		return playback.Track{}, false
	}
	return playback.Track{Track: res.Tracks[0]}, true
}

// Suggest answers autocomplete for the song option: up to 25 "Title — Artist"
// choices whose value is the same text (searched again on submit).
func (c *Core) Suggest(ctx context.Context, q string) []string {
	q = strings.TrimSpace(q)
	if c.Library == nil || len([]rune(q)) < 2 {
		return nil
	}
	res, err := c.Library.Search(ctx, q, []core.EntityType{core.EntityTrack})
	if err != nil {
		return nil
	}
	var out []string
	seen := map[string]bool{}
	for _, t := range res.Tracks {
		s := t.Title + " " + t.Artist
		if r := []rune(s); len(r) > 100 {
			s = string(r[:100])
		}
		if seen[s] {
			continue
		}
		seen[s] = true
		out = append(out, s)
		if len(out) == 25 {
			break
		}
	}
	return out
}

func fmtTime(ms int64) string {
	s := ms / 1000
	return fmt.Sprintf("%d:%02d", s/60, s%60)
}

func (c *Core) nowPlaying(userID string) Reply {
	snap := c.Hub.Snapshot(userID)
	st := snap.State
	if st == nil || st.Index < 0 || st.Index >= len(st.Queue) {
		return Reply{Text: "Nothing is playing.", Ephemeral: true}
	}
	t := st.Queue[st.Index]
	pos := st.PositionMs
	if st.Playing && snap.ServerTime > st.UpdatedAt {
		pos += snap.ServerTime - st.UpdatedAt
	}
	if t.DurationMs > 0 && pos > int64(t.DurationMs) {
		pos = int64(t.DurationMs)
	}
	where := ""
	for _, d := range snap.Devices {
		if d.ID == snap.ActiveDeviceID {
			where = " on " + d.Name
		}
	}
	state := "▶️"
	if !st.Playing {
		state = "⏸️"
	}
	return Reply{Text: fmt.Sprintf("%s **%s** by %s · %s / %s%s", state, t.Title, t.Artist, fmtTime(pos), fmtTime(int64(t.DurationMs)), where)}
}

func (c *Core) queue(userID string) Reply {
	st := c.Hub.Snapshot(userID).State
	if st == nil || len(st.Queue) == 0 || st.Index < 0 {
		return Reply{Text: "The queue is empty.", Ephemeral: true}
	}
	var b strings.Builder
	cur := st.Queue[st.Index]
	fmt.Fprintf(&b, "Now: **%s** by %s\n", cur.Title, cur.Artist)
	n := 0
	for i := st.Index + 1; i < len(st.Queue) && n < 10; i++ {
		n++
		fmt.Fprintf(&b, "%d. %s by %s\n", n, st.Queue[i].Title, st.Queue[i].Artist)
	}
	if rest := len(st.Queue) - st.Index - 1 - n; rest > 0 {
		fmt.Fprintf(&b, "…and %d more", rest)
	}
	if n == 0 {
		b.WriteString("Nothing after this.")
	}
	return Reply{Text: b.String(), Ephemeral: true}
}

// LeaveUser leaves every channel playing userID's session (they unlinked).
func (c *Core) LeaveUser(ctx context.Context, userID string) {
	c.mu.Lock()
	var mine []*guildSession
	for _, gs := range c.guilds {
		if gs.owner == userID {
			mine = append(mine, gs)
		}
	}
	c.mu.Unlock()
	for _, gs := range mine {
		c.leaveSession(ctx, gs)
	}
}

// Leave stops playing in a guild and leaves its voice channel. The account's
// session keeps its place (the hub pauses it).
func (c *Core) Leave(ctx context.Context, guildID string) {
	if gs := c.session(guildID); gs != nil {
		c.leaveSession(ctx, gs)
	}
}

// leaveSession ends gs if it is still the guild's session.
func (c *Core) leaveSession(ctx context.Context, gs *guildSession) {
	c.mu.Lock()
	if c.guilds[gs.guildID] != gs {
		c.mu.Unlock()
		return
	}
	delete(c.guilds, gs.guildID)
	v := c.Voice
	c.mu.Unlock()
	close(gs.stop)
	// Report the final position first so the session resumes from here.
	gs.dev.State(gs.player.State())
	gs.player.Shutdown()
	gs.dev.Close()
	if v != nil {
		v.Leave(ctx, gs.guildID)
	}
}

// LeaveAll leaves every guild (bot shutdown).
func (c *Core) LeaveAll(ctx context.Context) {
	c.mu.Lock()
	ids := make([]string, 0, len(c.guilds))
	for id := range c.guilds {
		ids = append(ids, id)
	}
	c.mu.Unlock()
	for _, id := range ids {
		c.Leave(ctx, id)
	}
}

// SweepIdle leaves channels where nobody has been listening for IdleLeave.
// Call it periodically.
func (c *Core) SweepIdle(ctx context.Context, now time.Time) {
	idle := c.IdleLeave
	if idle <= 0 {
		idle = 2 * time.Minute
	}
	c.mu.Lock()
	var leave []string
	for id, gs := range c.guilds {
		if c.Voice == nil || c.Voice.Listeners(id, gs.channelID) > 0 {
			gs.emptySince = time.Time{}
			continue
		}
		if gs.emptySince.IsZero() {
			gs.emptySince = now
		} else if now.Sub(gs.emptySince) >= idle {
			leave = append(leave, id)
		}
	}
	c.mu.Unlock()
	for _, id := range leave {
		c.log().Info("discord: leaving an empty channel", "guild", id)
		c.Leave(ctx, id)
	}
}

// VoiceLost is called when Discord disconnects the bot from channelID
// (kicked, channel deleted). A late event for a channel the bot already left
// is ignored.
func (c *Core) VoiceLost(ctx context.Context, guildID, channelID string) {
	if gs := c.session(guildID); gs != nil {
		if id, _ := c.channelOf(gs); id == channelID {
			c.leaveSession(ctx, gs)
		}
	}
}

// VoiceMoved is called when the bot was moved to another channel.
func (c *Core) VoiceMoved(guildID, channelID, channelName string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	gs := c.guilds[guildID]
	if gs == nil {
		return
	}
	gs.channelID = channelID
	if channelName != "" {
		gs.channelName = channelName
	}
	gs.emptySince = time.Time{}
}

// Sessions lists where the bot is playing (for the admin status).
func (c *Core) Sessions() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.guilds)
}
