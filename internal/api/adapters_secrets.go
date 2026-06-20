package api

import (
	"strings"

	"github.com/maximusjb/crate/internal/registry"
)

// secretSentinel is the placeholder returned for a SET secret. The browser never
// receives the real value. Submitting the sentinel (or a blank string) back means
// "keep the stored secret".
const secretSentinel = "••••••••"

// isSetSuffix is appended to a secret field key to carry a boolean indicating
// whether a value is stored, without ever exposing the value itself.
const isSetSuffix = "__isSet"

// secretKeys returns the set of Secret:true field keys from a schema.
func secretKeys(schema registry.ConfigSchema) map[string]bool {
	out := map[string]bool{}
	for _, f := range schema.Fields {
		if f.Secret {
			out[f.Key] = true
		}
	}
	return out
}

// redactConfig copies cfg, removing every Secret:true value and replacing it with a
// parallel "<key>__isSet" boolean. Non-secret fields pass through unchanged. Generic:
// it consults the schema only, never a per-adapter hardcoded list.
func redactConfig(schema registry.ConfigSchema, cfg map[string]any) map[string]any {
	secrets := secretKeys(schema)
	out := map[string]any{}
	for k, v := range cfg {
		if secrets[k] {
			continue // drop the secret value entirely
		}
		out[k] = v
	}
	for key := range secrets {
		_, present := cfg[key]
		set := present && !isBlank(cfg[key])
		out[key+isSetSuffix] = set
	}
	return out
}

// mergeSecrets builds the config to PERSIST. Non-secret fields take the incoming
// value. Secret fields: if incoming is blank or the sentinel, restore from stored
// (preserve-on-blank); otherwise take the new incoming value. Any "<key>__isSet"
// sidecars are stripped so they never reach config_json.
func mergeSecrets(schema registry.ConfigSchema, stored, incoming map[string]any) map[string]any {
	secrets := secretKeys(schema)
	out := map[string]any{}
	for k, v := range incoming {
		if strings.HasSuffix(k, isSetSuffix) {
			continue // never persist the sidecar
		}
		if secrets[k] {
			if isBlank(v) || asString(v) == secretSentinel {
				if sv, ok := stored[k]; ok {
					out[k] = sv // preserve
				}
				continue
			}
		}
		out[k] = v
	}
	// Carry over any stored secret the client omitted entirely (defensive preserve).
	for key := range secrets {
		if _, ok := out[key]; ok {
			continue
		}
		if sv, ok := stored[key]; ok {
			out[key] = sv
		}
	}
	return out
}

func isBlank(v any) bool {
	s, ok := v.(string)
	return ok && s == ""
}

func asString(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}
