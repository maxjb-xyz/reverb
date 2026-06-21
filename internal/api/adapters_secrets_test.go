package api

import (
	"testing"

	"github.com/maxjb-xyz/reverb/internal/registry"
)

func schema() registry.ConfigSchema {
	return registry.ConfigSchema{Fields: []registry.ConfigField{
		{Key: "client_id", Label: "Client ID", Type: "string", Required: true},
		{Key: "client_secret", Label: "Client Secret", Type: "string", Required: true, Secret: true},
	}}
}

func TestRedactConfigHidesSecretValueEmitsIsSet(t *testing.T) {
	out := redactConfig(schema(), map[string]any{"client_id": "abc", "client_secret": "shh"})
	if out["client_id"] != "abc" {
		t.Fatalf("non-secret should pass through, got %v", out["client_id"])
	}
	if _, present := out["client_secret"]; present {
		t.Fatalf("secret VALUE must not be returned, got %v", out["client_secret"])
	}
	if out["client_secret__isSet"] != true {
		t.Fatalf("expected client_secret__isSet=true, got %v", out["client_secret__isSet"])
	}
}

func TestRedactConfigUnsetSecretIsSetFalse(t *testing.T) {
	out := redactConfig(schema(), map[string]any{"client_id": "abc"})
	if out["client_secret__isSet"] != false {
		t.Fatalf("expected isSet=false for absent secret, got %v", out["client_secret__isSet"])
	}
	if _, present := out["client_secret"]; present {
		t.Fatal("absent secret must not appear")
	}
}

func TestMergeSecretsBlankPreservesStored(t *testing.T) {
	stored := map[string]any{"client_id": "old", "client_secret": "kept"}
	incoming := map[string]any{"client_id": "new", "client_secret": ""}
	out := mergeSecrets(schema(), stored, incoming)
	if out["client_id"] != "new" {
		t.Fatalf("non-secret should update, got %v", out["client_id"])
	}
	if out["client_secret"] != "kept" {
		t.Fatalf("blank secret must preserve stored value, got %v", out["client_secret"])
	}
}

func TestMergeSecretsSentinelPreservesStored(t *testing.T) {
	stored := map[string]any{"client_secret": "kept"}
	incoming := map[string]any{"client_secret": secretSentinel}
	out := mergeSecrets(schema(), stored, incoming)
	if out["client_secret"] != "kept" {
		t.Fatalf("sentinel must preserve stored value, got %v", out["client_secret"])
	}
}

func TestMergeSecretsNewValueOverwrites(t *testing.T) {
	stored := map[string]any{"client_secret": "old"}
	incoming := map[string]any{"client_secret": "fresh"}
	out := mergeSecrets(schema(), stored, incoming)
	if out["client_secret"] != "fresh" {
		t.Fatalf("non-blank secret must overwrite, got %v", out["client_secret"])
	}
}

func TestMergeSecretsNilIncomingPreservesStored(t *testing.T) {
	// A non-string incoming value (JSON null → Go nil) must never wipe the stored secret.
	stored := map[string]any{"client_secret": "kept"}
	incoming := map[string]any{"client_secret": nil}
	out := mergeSecrets(schema(), stored, incoming)
	if out["client_secret"] != "kept" {
		t.Fatalf("nil incoming must preserve stored secret, got %v", out["client_secret"])
	}
}

func TestMergeSecretsNumberIncomingPreservesStored(t *testing.T) {
	// A non-string incoming value (e.g. a number) must never wipe the stored secret.
	stored := map[string]any{"client_secret": "kept"}
	incoming := map[string]any{"client_secret": 42}
	out := mergeSecrets(schema(), stored, incoming)
	if out["client_secret"] != "kept" {
		t.Fatalf("numeric incoming must preserve stored secret, got %v", out["client_secret"])
	}
}

func TestMergeSecretsOmittedPreservesStored(t *testing.T) {
	// Client omits the secret key entirely → stored secret is preserved (defensive).
	stored := map[string]any{"client_secret": "kept"}
	incoming := map[string]any{"client_id": "abc"}
	out := mergeSecrets(schema(), stored, incoming)
	if out["client_secret"] != "kept" {
		t.Fatalf("omitted secret must preserve stored value, got %v", out["client_secret"])
	}
}

func TestMergeSecretsStripsIsSetKeys(t *testing.T) {
	// The client may echo back the "<key>__isSet" sidecar; it must never be persisted.
	stored := map[string]any{"client_secret": "kept"}
	incoming := map[string]any{"client_id": "abc", "client_secret": "", "client_secret__isSet": true}
	out := mergeSecrets(schema(), stored, incoming)
	if _, present := out["client_secret__isSet"]; present {
		t.Fatal("__isSet sidecar must not be persisted")
	}
}
