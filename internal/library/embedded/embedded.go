// Package embedded bundles and supervises a Navidrome child process so Reverb
// works out of the box with no external library server. It writes no library
// data itself — the existing subsonic adapter talks to the child over HTTP.
package embedded

type Mode string

const (
	ModeBuiltIn  Mode = "built-in"
	ModeExternal Mode = "external"
)

// Health is the supervisor's view of the child process.
type Health string

const (
	HealthExternal Health = "external" // not managing a child (external mode)
	HealthStarting Health = "starting"
	HealthReady    Health = "ready"
	HealthDegraded Health = "degraded"
)

// AdminUsername is the fixed username Navidrome auto-creates via
// ND_DEVAUTOCREATEADMINPASSWORD; the subsonic adapter authenticates as this.
const AdminUsername = "admin"

// Credentials are the internal admin credentials for the bundled Navidrome.
type Credentials struct {
	Username string
	Password string
}

// ResolveMode determines the effective backend mode. An explicit, valid setting
// wins. When unset/invalid, presence of an enabled library adapter instance
// implies external (so existing deployments are untouched); otherwise built-in.
func ResolveMode(setting string, hasEnabledLibraryInstance bool) Mode {
	switch Mode(setting) {
	case ModeBuiltIn:
		return ModeBuiltIn
	case ModeExternal:
		return ModeExternal
	default:
		if hasEnabledLibraryInstance {
			return ModeExternal
		}
		return ModeBuiltIn
	}
}
