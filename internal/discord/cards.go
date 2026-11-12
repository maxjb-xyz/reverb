package discord

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/maxjb-xyz/reverb/internal/playback"
)

// Now-playing card buttons. The ID carries the Discord user who asked for
// the card, since the buttons drive that person's session.
const npPrefix = "np:"

var npActions = map[string]string{"prev": "prev", "toggle": "toggle", "next": "next", "refresh": ""}

func npButton(action, discordID, label string) Button {
	return Button{ID: npPrefix + action + ":" + discordID, Label: label}
}

// nowPlayingCard is /nowplaying with playback buttons.
func (c *Core) nowPlayingCard(discordID, userID string) Reply {
	r := c.nowPlaying(userID)
	if r.Ephemeral {
		return r
	}
	toggle := "⏸ Pause"
	if st := c.Hub.Snapshot(userID).State; st != nil && !st.Playing {
		toggle = "▶ Play"
	}
	r.Buttons = []Button{
		npButton("prev", discordID, "⏮"),
		npButton("toggle", discordID, toggle),
		npButton("next", discordID, "⏭"),
		npButton("refresh", discordID, "↻"),
	}
	return r
}

// Press handles a button. A reply that isn't ephemeral replaces the card;
// an ephemeral one is shown to the presser only.
func (c *Core) Press(ctx context.Context, presserID, buttonID string) Reply {
	rest, ok := strings.CutPrefix(buttonID, npPrefix)
	if !ok {
		return Reply{Text: "That button is from an older version of the bot. Run the command again.", Ephemeral: true}
	}
	action, owner, ok := strings.Cut(rest, ":")
	cmd, known := npActions[action]
	if !ok || !known {
		return Reply{Text: "That button is from an older version of the bot. Run the command again.", Ephemeral: true}
	}
	if presserID != owner {
		return Reply{Text: "These buttons control the session of whoever asked. Use /nowplaying for your own.", Ephemeral: true}
	}
	userID, errReply := c.linkedUser(ctx, Invocation{UserID: presserID})
	if errReply != nil {
		return *errReply
	}
	if cmd != "" {
		before := c.Hub.Snapshot(userID).State
		if c.Hub.Command(userID, playback.Command{Action: cmd}) && before != nil {
			c.waitForState(ctx, userID, *before)
		}
	}
	r := c.nowPlayingCard(presserID, userID)
	if r.Ephemeral {
		// Nothing playing any more: say so on the card.
		r.Ephemeral = false
	}
	return r
}

// waitForState gives the active device a moment to apply a command and
// report back, so the refreshed card shows the result. Routine position
// reports don't count: only play/pause, the track or a jump back does.
func (c *Core) waitForState(ctx context.Context, userID string, before playback.State) {
	deadline := time.Now().Add(1500 * time.Millisecond)
	for time.Now().Before(deadline) && ctx.Err() == nil {
		if st := c.Hub.Snapshot(userID).State; st != nil && (st.Playing != before.Playing || st.Index != before.Index || st.PositionMs < before.PositionMs) {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// request handles /request: ask for a song that isn't in the library.
func (c *Core) request(ctx context.Context, inv Invocation, userID string) Reply {
	if c.Requests == nil {
		return Reply{Text: "Requests aren't set up on this Reverb server.", Ephemeral: true}
	}
	q := strings.TrimSpace(inv.Options["song"])
	if q == "" {
		return Reply{Text: "Tell me what to request.", Ephemeral: true}
	}
	out, err := c.Requests.Request(ctx, userID, q)
	if err != nil {
		c.log().Warn("discord: request", "err", err)
		return Reply{Text: "Something went wrong with that request. Try again from Reverb.", Ephemeral: true}
	}
	song := fmt.Sprintf("**%s** by %s", out.Title, out.Artist)
	switch out.Status {
	case RequestOwned:
		return Reply{Text: song + " is already in the library. Use /play to hear it.", Ephemeral: true}
	case RequestDownloading:
		return Reply{Text: "⬇️ Getting " + song + ". It'll be in the library in a few minutes."}
	case RequestPending:
		return Reply{Text: "📝 Requested " + song + ". An admin will take a look."}
	case RequestExists:
		return Reply{Text: song + " has already been requested.", Ephemeral: true}
	case RequestNotFound:
		return Reply{Text: fmt.Sprintf("Couldn't find “%s” online.", q), Ephemeral: true}
	case RequestDenied:
		return Reply{Text: "Your Reverb account can't request songs. Ask an admin.", Ephemeral: true}
	case RequestSearchFailed:
		return Reply{Text: "Online search isn't answering right now. Try again in a bit.", Ephemeral: true}
	case RequestUnchecked:
		return Reply{Text: fmt.Sprintf("Couldn't check whether the library already has %s. Try again in a bit, or request it from Reverb.", song), Ephemeral: true}
	case RequestQuota:
		return Reply{Text: "You've reached your limit of pending requests. Wait for some to be reviewed.", Ephemeral: true}
	}
	return Reply{Text: "Requests aren't set up on this Reverb server.", Ephemeral: true}
}
