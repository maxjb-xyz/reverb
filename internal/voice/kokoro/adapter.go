package kokoro

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/maxjb-xyz/reverb/internal/registry"
	"github.com/maxjb-xyz/reverb/internal/voice"
)

const (
	DefaultVoice = "af_heart"
	minSpeed     = 0.5
	maxSpeed     = 2.0
)

// Adapter is the built-in Voice. Its Runtime is shared across instances.
type Adapter struct {
	rt    *Runtime
	voice string
	speed float64
}

// New returns the adapter (the registry factory) over a shared runtime.
func New(rt *Runtime) *Adapter {
	return &Adapter{rt: rt, voice: DefaultVoice, speed: 1}
}

func (a *Adapter) Type() string { return voice.Type }
func (a *Adapter) Name() string { return "kokoro" }

func (a *Adapter) ConfigSchema() registry.ConfigSchema {
	return registry.ConfigSchema{Fields: []registry.ConfigField{
		{Key: "voice", Label: "Voice", Type: "string",
			Help: "Defaults to af_heart. Others include af_bella, af_nicole, am_michael, am_adam, bf_emma and bm_george (the first letters say the accent and gender). Runs on the CPU. The first time it is enabled, Reverb downloads about 170 MB (about 300 MB on disk) and sets it up in the background."},
		{Key: "speed", Label: "Speed", Type: "number", Help: "From 0.5 to 2. Defaults to 1."},
	}}
}

func (a *Adapter) Init(cfg map[string]any) error {
	if v, ok := cfg["voice"].(string); ok && strings.TrimSpace(v) != "" {
		a.voice = strings.TrimSpace(v)
	}
	switch v := cfg["speed"].(type) {
	case float64:
		a.speed = v
	case int:
		a.speed = float64(v)
	case string:
		if strings.TrimSpace(v) != "" {
			f, err := strconv.ParseFloat(strings.TrimSpace(v), 64)
			if err != nil {
				return fmt.Errorf("kokoro: speed must be a number")
			}
			a.speed = f
		}
	}
	if a.speed == 0 {
		a.speed = 1
	}
	if a.speed < minSpeed || a.speed > maxSpeed {
		return fmt.Errorf("kokoro: speed must be between %.1f and %.0f", minSpeed, maxSpeed)
	}
	// Enabling the voice starts the one-time setup right away, so it is
	// usually done by the time the DJ first speaks.
	a.rt.EnsureSetup()
	return nil
}

// Identity names this voice and its settings, for clip caching.
func (a *Adapter) Identity() string {
	return fmt.Sprintf("kokoro:%s:%.2f", a.voice, a.speed)
}

// TestConnection renders a short line once setup is done; before that it
// reports how setup is going (and retries a failed setup).
func (a *Adapter) TestConnection(ctx context.Context) error {
	st, msg := a.rt.Status()
	switch st {
	case StateReady:
		_, err := a.Synthesize(ctx, "Testing.")
		return err
	case StateFailed:
		a.rt.EnsureSetup()
		return fmt.Errorf("setup failed: %s. Trying again now", msg)
	default:
		a.rt.EnsureSetup()
		_, msg = a.rt.Status()
		return errors.New("setting up the built-in voice: " + msg + ". This takes a few minutes the first time; test again when it is done")
	}
}

func (a *Adapter) Synthesize(ctx context.Context, text string) (voice.Audio, error) {
	if err := voice.CheckText(text); err != nil {
		return voice.Audio{}, err
	}
	if err := ctx.Err(); err != nil {
		return voice.Audio{}, err
	}
	if st, _ := a.rt.Status(); st != StateReady {
		a.rt.EnsureSetup()
		_, msg := a.rt.Status()
		return voice.Audio{}, fmt.Errorf("%w: %s", voice.ErrWarmingUp, msg)
	}
	b, err := a.rt.synthesize(ctx, request{Text: text, Voice: a.voice, Speed: a.speed})
	if err != nil {
		return voice.Audio{}, err
	}
	return voice.Audio{Data: b, ContentType: "audio/wav"}, nil
}
