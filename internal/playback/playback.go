// Package playback keeps one listening session per account, in the manner of
// Spotify Connect. Every client that can play or control music (a browser tab,
// a future desktop or phone app, the Discord bot) connects as a device. At most
// one device is active: it plays the audio, owns the queue, and reports its
// state here. Every other device is a remote control: its commands are relayed
// to the active device, and it shows the state the active device last reported.
//
// The hub is transport-agnostic. The WebSocket handler in internal/api adapts a
// socket to a Conn; an in-process device (the Discord bot) uses a Conn directly.
package playback

import (
	"encoding/json"

	"github.com/maxjb-xyz/reverb/internal/core"
)

// Message types a device sends to the hub.
const (
	// MsgHello registers the connection as a device. Payload: DeviceInfo.
	MsgHello = "playback.hello"
	// MsgState reports the active device's state. Payload: State. Ignored
	// unless the sender is the active device.
	MsgState = "playback.state"
	// MsgClaim makes the sender the active device, carrying its state. A
	// device sends it when it starts playing on its own (the listener pressed
	// play on it). Payload: State.
	MsgClaim = "playback.claim"
	// MsgCommand asks the active device to do something. Payload: Command.
	MsgCommand = "playback.command"
	// MsgTransfer moves playback to another device (or to the sender).
	// Payload: Transfer.
	MsgTransfer = "playback.transfer"
)

// Message types the hub sends to devices.
const (
	// MsgSession is the account's session: devices, the active one, and its
	// state. Sent to every device whenever any of that changes.
	MsgSession = "playback.session"
	// MsgActivate tells a device it is now the active one and hands it the
	// state to continue from. Payload: Activate.
	MsgActivate = "playback.activate"
	// MsgDeactivate tells a device another one took over: stop the audio.
	MsgDeactivate = "playback.deactivate"
	// MsgCommand (above) is also what the active device receives.
)

// Message is one frame between a device and the hub.
type Message struct {
	Type    string `json:"type"`
	Payload any    `json:"payload"`
}

// DeviceInfo describes a device. ID is chosen by the client and stays the same
// across reconnects of the same tab or app install.
type DeviceInfo struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	// Kind is a hint for the icon: "web", "desktop", "phone", "discord".
	Kind string `json:"kind"`
}

// Track is a queue entry: a library track as the player knows it.
type Track struct {
	core.Track
	ArtistExternalID string `json:"artistExternalId,omitempty"`
}

// State is what the active device last reported.
type State struct {
	Queue      []Track `json:"queue"`
	Index      int     `json:"index"`
	PositionMs int64   `json:"positionMs"`
	Playing    bool    `json:"playing"`
	Shuffle    bool    `json:"shuffle"`
	Repeat     string  `json:"repeat"`
	Volume     float64 `json:"volume"`
	// UpdatedAt is the server time (unix ms) the report arrived. A viewer adds
	// the time since then to PositionMs while Playing.
	UpdatedAt int64 `json:"updatedAt"`
}

// Session is the account's view, as broadcast to its devices.
type Session struct {
	Devices        []DeviceInfo `json:"devices"`
	ActiveDeviceID string       `json:"activeDeviceId"`
	State          *State       `json:"state"`
	// ServerTime (unix ms) lets a client correct for its clock being off when
	// it extrapolates the position.
	ServerTime int64 `json:"serverTime"`
}

// Command is a remote control action for the active device.
type Command struct {
	Action string `json:"action"`
	// playTracks
	Tracks  []Track         `json:"tracks,omitempty"`
	Context json.RawMessage `json:"context,omitempty"`
	// playTracks (start index), jumpTo, removeAt
	Index int `json:"index,omitempty"`
	// moveItem
	From int `json:"from,omitempty"`
	To   int `json:"to,omitempty"`
	// seek
	PositionMs int64 `json:"positionMs,omitempty"`
	// setVolume
	Volume float64 `json:"volume,omitempty"`
	// enqueue
	Track *Track `json:"track,omitempty"`
}

// Commands the hub relays. Anything else is dropped.
var commandActions = map[string]bool{
	"play": true, "pause": true, "toggle": true, "next": true, "prev": true,
	"seek": true, "setVolume": true, "toggleShuffle": true, "cycleRepeat": true,
	"jumpTo": true, "playTracks": true, "enqueue": true, "removeAt": true, "moveItem": true,
}

// Transfer moves playback to DeviceID. Play says whether to start playing
// there; when false the target picks up paused.
type Transfer struct {
	DeviceID string `json:"deviceId"`
	Play     bool   `json:"play"`
}

// Activate is sent to the device that becomes active.
type Activate struct {
	State *State `json:"state"`
	Play  bool   `json:"play"`
}

// Limits on what a device may send.
const (
	MaxQueue      = 1000
	maxNameLen    = 64
	maxIDLen      = 128
	maxContextLen = 4096
)

func validRepeat(r string) bool { return r == "off" || r == "all" || r == "one" }

// sanitizeState checks a reported state and normalizes it. It returns false
// for a state that cannot be right.
func sanitizeState(s *State) bool {
	if s == nil || len(s.Queue) > MaxQueue {
		return false
	}
	if s.Repeat == "" {
		s.Repeat = "off"
	}
	if !validRepeat(s.Repeat) {
		return false
	}
	for _, t := range s.Queue {
		if t.ID == "" {
			return false
		}
	}
	if len(s.Queue) == 0 {
		s.Index = -1
	} else if s.Index < 0 || s.Index >= len(s.Queue) {
		return false
	}
	if s.PositionMs < 0 {
		s.PositionMs = 0
	}
	if s.Volume < 0 || s.Volume > 1 || s.Volume != s.Volume {
		s.Volume = 1
	}
	return true
}

func sanitizeCommand(c *Command) bool {
	if c == nil || !commandActions[c.Action] {
		return false
	}
	if len(c.Tracks) > MaxQueue || len(c.Context) > maxContextLen {
		return false
	}
	for _, t := range c.Tracks {
		if t.ID == "" {
			return false
		}
	}
	switch c.Action {
	case "playTracks":
		if len(c.Tracks) == 0 || c.Index < 0 || c.Index >= len(c.Tracks) {
			return false
		}
	case "enqueue":
		if c.Track == nil || c.Track.ID == "" {
			return false
		}
	case "setVolume":
		if c.Volume < 0 || c.Volume > 1 || c.Volume != c.Volume {
			return false
		}
	case "seek":
		if c.PositionMs < 0 {
			return false
		}
	}
	return true
}

func sanitizeDevice(d *DeviceInfo) bool {
	if d.ID == "" || len(d.ID) > maxIDLen {
		return false
	}
	if r := []rune(d.Name); len(r) > maxNameLen {
		d.Name = string(r[:maxNameLen])
	}
	if d.Name == "" {
		d.Name = "Unknown device"
	}
	switch d.Kind {
	case "web", "desktop", "phone", "discord":
	default:
		d.Kind = "web"
	}
	return true
}
