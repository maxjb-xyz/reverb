package embedded

import "testing"

func TestResolveMode(t *testing.T) {
	cases := []struct {
		name       string
		setting    string
		hasLibInst bool
		want       Mode
	}{
		{"explicit built-in", "built-in", true, ModeBuiltIn},
		{"explicit external", "external", false, ModeExternal},
		{"unset with existing library instance -> external (untouched)", "", true, ModeExternal},
		{"unset fresh install -> built-in", "", false, ModeBuiltIn},
		{"garbage value falls back to inference", "nonsense", false, ModeBuiltIn},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := ResolveMode(c.setting, c.hasLibInst); got != c.want {
				t.Errorf("ResolveMode(%q,%v) = %q, want %q", c.setting, c.hasLibInst, got, c.want)
			}
		})
	}
}
