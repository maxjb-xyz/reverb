package discord

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/disgoorg/disgo"
	"github.com/disgoorg/disgo/bot"
	"github.com/disgoorg/disgo/cache"
	"github.com/disgoorg/disgo/discord"
	"github.com/disgoorg/disgo/events"
	"github.com/disgoorg/disgo/gateway"
	"github.com/disgoorg/disgo/voice"
	"github.com/disgoorg/snowflake/v2"
	"github.com/thomas-vilte/dave-go/session"
)

// invitePermissions is View Channel + Connect + Speak.
const invitePermissions = discord.PermissionViewChannel | discord.PermissionConnect | discord.PermissionSpeak

// InviteURL is where an admin adds the bot to a server.
func InviteURL(appID string) string {
	return fmt.Sprintf("https://discord.com/oauth2/authorize?client_id=%s&scope=bot+applications.commands&permissions=%d", appID, invitePermissions)
}

// replyWithin is how long a command may take before the reply is deferred
// (Discord drops an interaction not answered in 3 s).
const replyWithin = 2 * time.Second

// gatewayBot connects a Core to Discord with disgo. Voice uses Discord's
// end-to-end encryption (DAVE) through a pure-Go implementation, so the binary
// stays free of cgo.
type gatewayBot struct {
	client *bot.Client
	core   *Core
	log    *slog.Logger

	mu    sync.Mutex
	links map[snowflake.ID]*voiceLink
}

// voiceLink is the bot's audio in one guild. Discord's audio sender stops
// for good when the voice connection drops (a voice server move, a resume);
// the link notices and starts a new sender on the same player.
type voiceLink struct {
	conn   voice.Conn
	player *Player
	cur    atomic.Pointer[linkProvider]
	stop   chan struct{}
}

// linkProvider feeds one audio sender from the player and remembers that
// sender, so leaving can stop it (disgo's Conn.Close doesn't).
type linkProvider struct {
	player *Player
	sender voice.AudioSender
	dead   atomic.Bool
}

func (lp *linkProvider) ProvideOpusFrame() ([]byte, error) { return lp.player.ProvideOpusFrame() }

// Close is called by the sender when it stops.
func (lp *linkProvider) Close() {
	if lp.dead.CompareAndSwap(false, true) {
		lp.player.Close()
	}
}

// newAudioSender is disgo's sender, with the sender kept on our provider.
func newAudioSender(log *slog.Logger, provider voice.OpusFrameProvider, conn voice.Conn) voice.AudioSender {
	s := voice.NewAudioSender(log, provider, conn)
	if lp, ok := provider.(*linkProvider); ok {
		lp.sender = s
	}
	return s
}

func (l *voiceLink) attach() {
	lp := &linkProvider{player: l.player}
	l.conn.SetOpusFrameProvider(lp) // runs newAudioSender, which sets lp.sender
	l.cur.Store(lp)
}

// watch restarts the sender after it died while the bot is still in voice.
func (l *voiceLink) watch(log *slog.Logger) {
	t := time.NewTicker(2 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-l.stop:
			return
		case <-t.C:
			if lp := l.cur.Load(); lp != nil && lp.dead.Load() {
				log.Info("discord: voice audio restarted")
				l.attach()
			}
		}
	}
}

func (l *voiceLink) close(ctx context.Context) {
	close(l.stop)
	if lp := l.cur.Load(); lp != nil && lp.sender != nil {
		lp.sender.Close()
	}
	l.conn.Close(ctx)
}

func newGatewayBot(token string, core *Core, log *slog.Logger, onReady func(name string)) (*gatewayBot, error) {
	g := &gatewayBot{core: core, log: log, links: map[snowflake.ID]*voiceLink{}}
	client, err := disgo.New(token,
		bot.WithLogger(log),
		// Handlers run concurrently: a slow /join (opening voice waits for
		// gateway events) must not block the event loop.
		bot.WithEventManagerConfigOpts(bot.WithAsyncEventsEnabled()),
		bot.WithGatewayConfigOpts(gateway.WithIntents(gateway.IntentGuilds, gateway.IntentGuildVoiceStates)),
		bot.WithCacheConfigOpts(cache.WithCaches(cache.FlagGuilds, cache.FlagChannels, cache.FlagVoiceStates, cache.FlagMembers)),
		bot.WithVoiceManagerConfigOpts(
			voice.WithDaveSessionCreateFunc(session.CreateFunc()),
			voice.WithConnConfigOpts(voice.WithConnAudioSenderCreateFunc(newAudioSender)),
		),
		bot.WithEventListenerFunc(func(e *events.Ready) {
			if onReady != nil {
				onReady(e.User.Username)
			}
		}),
		bot.WithEventListenerFunc(g.onCommand),
		bot.WithEventListenerFunc(g.onAutocomplete),
		bot.WithEventListenerFunc(g.onVoiceLeave),
		bot.WithEventListenerFunc(g.onVoiceMove),
	)
	if err != nil {
		return nil, err
	}
	g.client = client
	return g, nil
}

func (g *gatewayBot) appID() string { return g.client.ApplicationID.String() }

func (g *gatewayBot) open(ctx context.Context) error {
	if _, err := g.client.Rest.SetGlobalCommands(g.client.ApplicationID, commandCreates()); err != nil {
		return fmt.Errorf("register commands: %w", err)
	}
	return g.client.OpenGateway(ctx)
}

func (g *gatewayBot) close(ctx context.Context) {
	g.core.LeaveAll(ctx)
	g.client.Close(ctx)
}

func commandCreates() []discord.ApplicationCommandCreate {
	out := make([]discord.ApplicationCommandCreate, 0, len(Commands))
	noDM := []discord.InteractionContextType{discord.InteractionContextTypeGuild}
	for _, c := range Commands {
		var opts []discord.ApplicationCommandOption
		for _, o := range c.Options {
			opts = append(opts, discord.ApplicationCommandOptionString{
				Name: o.Name, Description: o.Description, Required: o.Required, Autocomplete: o.Autocomplete,
			})
		}
		out = append(out, discord.SlashCommandCreate{Name: c.Name, Description: c.Description, Options: opts, Contexts: noDM})
	}
	return out
}

func (g *gatewayBot) invocation(e *events.ApplicationCommandInteractionCreate) Invocation {
	data := e.SlashCommandInteractionData()
	user := e.User()
	inv := Invocation{
		UserID:   user.ID.String(),
		UserName: user.EffectiveName(),
		Command:  data.CommandName(),
		Options:  map[string]string{},
	}
	if m := e.Member(); m != nil {
		inv.UserName = m.EffectiveName()
	}
	for _, c := range Commands {
		if c.Name != inv.Command {
			continue
		}
		for _, o := range c.Options {
			if v, ok := data.OptString(o.Name); ok {
				inv.Options[o.Name] = v
			}
		}
	}
	if gid := e.GuildID(); gid != nil {
		inv.GuildID = gid.String()
		if vs, ok := g.client.Caches.VoiceState(*gid, user.ID); ok && vs.ChannelID != nil {
			inv.VoiceChannelID = vs.ChannelID.String()
			if ch, ok := g.client.Caches.GuildAudioChannel(*vs.ChannelID); ok {
				inv.VoiceChannelName = ch.Name()
			}
		}
	}
	return inv
}

func (g *gatewayBot) onCommand(e *events.ApplicationCommandInteractionCreate) {
	if e.Data.Type() != discord.ApplicationCommandTypeSlash {
		return
	}
	inv := g.invocation(e)
	done := make(chan Reply, 1)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		done <- g.core.Handle(ctx, inv)
	}()
	select {
	case r := <-done:
		if err := e.CreateMessage(discord.NewMessageCreate().WithContent(r.Text).WithEphemeral(r.Ephemeral)); err != nil {
			g.log.Warn("discord: reply", "err", err)
		}
	case <-time.After(replyWithin):
		// Slow (joining voice, a cold library): show "thinking…" and edit it.
		if err := e.DeferCreateMessage(false); err != nil {
			g.log.Warn("discord: defer", "err", err)
		}
		r := <-done
		if r.Ephemeral {
			// The deferred reply is public; swap it for a private one.
			_ = g.client.Rest.DeleteInteractionResponse(e.ApplicationID(), e.Token())
			if _, err := g.client.Rest.CreateFollowupMessage(e.ApplicationID(), e.Token(), discord.NewMessageCreate().WithContent(r.Text).WithEphemeral(true)); err != nil {
				g.log.Warn("discord: reply", "err", err)
			}
			return
		}
		if _, err := g.client.Rest.UpdateInteractionResponse(e.ApplicationID(), e.Token(), discord.NewMessageUpdate().WithContent(r.Text)); err != nil {
			g.log.Warn("discord: reply", "err", err)
		}
	}
}

func (g *gatewayBot) onAutocomplete(e *events.AutocompleteInteractionCreate) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	focused := e.Data.Focused()
	q, _ := e.Data.OptString(focused.Name)
	// Only linked people see their library.
	if link, err := g.core.Links.ByDiscord(ctx, e.User().ID.String()); err != nil || link.ReverbUserID == "" {
		_ = e.AutocompleteResult(nil)
		return
	}
	var choices []discord.AutocompleteChoice
	for _, s := range g.core.Suggest(ctx, q) {
		choices = append(choices, discord.AutocompleteChoiceString{Name: s, Value: s})
	}
	_ = e.AutocompleteResult(choices)
}

// onVoiceLeave notices the bot being disconnected (kicked, channel deleted).
func (g *gatewayBot) onVoiceLeave(e *events.GuildVoiceLeave) {
	if e.VoiceState.UserID != g.client.ID() || e.OldVoiceState.ChannelID == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	g.core.VoiceLost(ctx, e.VoiceState.GuildID.String(), e.OldVoiceState.ChannelID.String())
}

// onVoiceMove follows the bot being dragged to another channel.
func (g *gatewayBot) onVoiceMove(e *events.GuildVoiceMove) {
	if e.VoiceState.UserID != g.client.ID() || e.VoiceState.ChannelID == nil {
		return
	}
	name := ""
	if ch, ok := g.client.Caches.GuildAudioChannel(*e.VoiceState.ChannelID); ok {
		name = ch.Name()
	}
	g.core.VoiceMoved(e.VoiceState.GuildID.String(), e.VoiceState.ChannelID.String(), name)
}

// Join implements Voice.
func (g *gatewayBot) Join(ctx context.Context, guildID, channelID string, p *Player) error {
	gid, err := snowflake.Parse(guildID)
	if err != nil {
		return err
	}
	cid, err := snowflake.Parse(channelID)
	if err != nil {
		return err
	}
	conn := g.client.VoiceManager.CreateConn(gid)
	openCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if err := conn.Open(openCtx, cid, false, true); err != nil {
		closeCtx, cancel2 := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel2()
		conn.Close(closeCtx)
		if errors.Is(err, context.DeadlineExceeded) {
			return errors.New("timed out (does the bot have Connect and Speak in that channel?)")
		}
		return err
	}
	l := &voiceLink{conn: conn, player: p, stop: make(chan struct{})}
	l.attach()
	g.mu.Lock()
	old := g.links[gid]
	g.links[gid] = l
	g.mu.Unlock()
	if old != nil {
		close(old.stop)
	}
	go l.watch(g.log)
	return nil
}

// Leave implements Voice.
func (g *gatewayBot) Leave(ctx context.Context, guildID string) {
	gid, err := snowflake.Parse(guildID)
	if err != nil {
		return
	}
	g.mu.Lock()
	l := g.links[gid]
	delete(g.links, gid)
	g.mu.Unlock()
	if l != nil {
		l.close(ctx)
		return
	}
	if conn := g.client.VoiceManager.GetConn(gid); conn != nil {
		conn.Close(ctx)
	}
}

// Listeners implements Voice: people (not bots) in the channel.
func (g *gatewayBot) Listeners(guildID, channelID string) int {
	gid, err := snowflake.Parse(guildID)
	if err != nil {
		return 0
	}
	cid, err := snowflake.Parse(channelID)
	if err != nil {
		return 0
	}
	n := 0
	for vs := range g.client.Caches.VoiceStates(gid) {
		if vs.ChannelID == nil || *vs.ChannelID != cid || vs.UserID == g.client.ID() {
			continue
		}
		if m, ok := g.client.Caches.Member(gid, vs.UserID); ok && m.User.Bot {
			continue
		}
		n++
	}
	return n
}
