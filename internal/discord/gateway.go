package discord

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
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
}

func newGatewayBot(token string, core *Core, log *slog.Logger, onReady func(name string)) (*gatewayBot, error) {
	g := &gatewayBot{core: core, log: log}
	client, err := disgo.New(token,
		bot.WithLogger(log),
		bot.WithGatewayConfigOpts(gateway.WithIntents(gateway.IntentGuilds, gateway.IntentGuildVoiceStates)),
		bot.WithCacheConfigOpts(cache.WithCaches(cache.FlagGuilds, cache.FlagChannels, cache.FlagVoiceStates, cache.FlagMembers)),
		bot.WithVoiceManagerConfigOpts(voice.WithDaveSessionCreateFunc(session.CreateFunc())),
		bot.WithEventListenerFunc(func(e *events.Ready) {
			if onReady != nil {
				onReady(e.User.Username)
			}
		}),
		bot.WithEventListenerFunc(g.onCommand),
		bot.WithEventListenerFunc(g.onAutocomplete),
		bot.WithEventListenerFunc(g.onVoiceLeave),
	)
	if err != nil {
		return nil, err
	}
	g.client = client
	core.Voice = g
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
	if e.VoiceState.UserID != g.client.ID() {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	g.core.VoiceLost(ctx, e.VoiceState.GuildID.String())
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
	conn.SetOpusFrameProvider(p)
	return nil
}

// Leave implements Voice.
func (g *gatewayBot) Leave(ctx context.Context, guildID string) {
	gid, err := snowflake.Parse(guildID)
	if err != nil {
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
