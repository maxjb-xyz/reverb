package subsonic

import (
	"encoding/json"
	"testing"
)

// TestFlexStringISRC guards the OpenSubsonic regression: Navidrome returns
// song.isrc as an ARRAY, classic Subsonic as a string (or omits it). All forms
// must decode — an array into a plain `string` 502s every search / song listing.
func TestFlexStringISRC(t *testing.T) {
	cases := []struct{ name, in, want string }{
		{"array", `{"isrc":["USX1","USX2"]}`, "USX1"},
		{"string", `{"isrc":"USX1"}`, "USX1"},
		{"empty-array", `{"isrc":[]}`, ""},
		{"absent", `{}`, ""},
		{"null", `{"isrc":null}`, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var c childDTO
			if err := json.Unmarshal([]byte(tc.in), &c); err != nil {
				t.Fatalf("decode failed: %v", err)
			}
			if got := string(c.Isrc); got != tc.want {
				t.Fatalf("isrc = %q, want %q", got, tc.want)
			}
		})
	}
}
