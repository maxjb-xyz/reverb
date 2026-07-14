package registry

import (
	"context"
	"fmt"
	"sort"
	"sync"
)

type ConfigField struct {
	Key      string `json:"key"`
	Label    string `json:"label"`
	Type     string `json:"type"`
	Required bool   `json:"required"`
	Secret   bool   `json:"secret"`
}

type ConfigSchema struct {
	Fields []ConfigField `json:"fields"`
}

type Plugin interface {
	Type() string
	Name() string
	ConfigSchema() ConfigSchema
	Init(cfg map[string]any) error
	TestConnection(ctx context.Context) error
}

type Factory func() Plugin

type Registry struct {
	kind      string
	mu        sync.RWMutex
	factories map[string]Factory
}

func NewRegistry(kind string) *Registry {
	return &Registry{kind: kind, factories: map[string]Factory{}}
}

// Register adds a factory. Safe to call at init() or at runtime.
func (r *Registry) Register(name string, f Factory) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.factories[name] = f
}

func (r *Registry) Create(name string) (Plugin, error) {
	r.mu.RLock()
	f, ok := r.factories[name]
	r.mu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("registry %q: unknown adapter %q", r.kind, name)
	}
	return f(), nil
}

func (r *Registry) Names() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]string, 0, len(r.factories))
	for n := range r.factories {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}

// --- capability probes ---

var (
	capMu     sync.RWMutex
	capProbes = map[string]func(Plugin) bool{}
)

// RegisterCapability registers a runtime probe that detects an optional
// interface. Later milestones (M2/M3) register probes for DiscographyProvider,
// QualityProfileDownloader, etc. — the registry stays domain-agnostic.
func RegisterCapability(name string, probe func(Plugin) bool) {
	capMu.Lock()
	defer capMu.Unlock()
	capProbes[name] = probe
}

func DescribeCapabilities(p Plugin) []string {
	capMu.RLock()
	defer capMu.RUnlock()
	var out []string
	for name, probe := range capProbes {
		if probe(p) {
			out = append(out, name)
		}
	}
	sort.Strings(out)
	return out
}

// SnapshotCapProbes saves the current global capability probes and replaces
// them with an empty map. Call RestoreCapProbes (typically via t.Cleanup) to
// restore the saved state. This is a test-isolation helper for packages that
// cannot reach capProbes directly (e.g. due to import cycles).
func SnapshotCapProbes() {
	capMu.Lock()
	defer capMu.Unlock()
	saved := capProbes
	capProbes = map[string]func(Plugin) bool{}
	savedCapProbes = saved
}

// RestoreCapProbes restores the probes that were saved by SnapshotCapProbes.
func RestoreCapProbes() {
	capMu.Lock()
	defer capMu.Unlock()
	capProbes = savedCapProbes
	savedCapProbes = nil
}

// savedCapProbes holds the snapshot for SnapshotCapProbes / RestoreCapProbes.
var savedCapProbes map[string]func(Plugin) bool
