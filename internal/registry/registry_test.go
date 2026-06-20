package registry

import (
	"context"
	"testing"
)

type fakeAdapter struct{ initialized bool }

func (f *fakeAdapter) Type() string                              { return "library" }
func (f *fakeAdapter) Name() string                              { return "fake" }
func (f *fakeAdapter) ConfigSchema() ConfigSchema                { return ConfigSchema{Fields: []ConfigField{{Key: "url", Label: "URL", Type: "string", Required: true}}} }
func (f *fakeAdapter) Init(cfg map[string]any) error             { f.initialized = true; return nil }
func (f *fakeAdapter) TestConnection(ctx context.Context) error  { return nil }

// optional capability interface (mimics future DiscographyProvider)
type discographyProvider interface{ Discography() }

func (f *fakeAdapter) Discography() {}

func TestRegisterCreateNames(t *testing.T) {
	reg := NewRegistry("library")
	reg.Register("fake", func() Plugin { return &fakeAdapter{} })

	if got := reg.Names(); len(got) != 1 || got[0] != "fake" {
		t.Fatalf("names = %v", got)
	}
	p, err := reg.Create("fake")
	if err != nil {
		t.Fatal(err)
	}
	if p.Name() != "fake" {
		t.Fatalf("name = %q", p.Name())
	}
	if _, err := reg.Create("missing"); err == nil {
		t.Fatal("expected error for unknown adapter")
	}
}

func TestDescribeCapabilities(t *testing.T) {
	RegisterCapability("discography", func(p Plugin) bool {
		_, ok := p.(discographyProvider)
		return ok
	})
	caps := DescribeCapabilities(&fakeAdapter{})
	found := false
	for _, c := range caps {
		if c == "discography" {
			found = true
		}
	}
	if !found {
		t.Fatalf("discography not detected: %v", caps)
	}
}
