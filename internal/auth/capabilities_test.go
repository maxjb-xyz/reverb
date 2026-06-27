package auth

import "testing"

func TestAllCapabilitiesContainsKnownKeys(t *testing.T) {
	caps := AllCapabilities()
	if len(caps) != 6 {
		t.Fatalf("want 6 capabilities, got %d", len(caps))
	}
	want := map[string]bool{CapAdmin: false, CapManageUsers: false, CapManageLibrary: false, CapDownload: false, CapRequest: false, CapCreatePlaylists: false}
	for _, c := range caps {
		if _, ok := want[c.Key]; !ok {
			t.Errorf("unexpected capability %q", c.Key)
		}
		if c.Label == "" {
			t.Errorf("capability %q has empty label", c.Key)
		}
		want[c.Key] = true
	}
	for k, seen := range want {
		if !seen {
			t.Errorf("missing capability %q", k)
		}
	}
}

func TestValidateCapabilities(t *testing.T) {
	if err := ValidateCapabilities([]string{CapDownload, CapRequest}); err != nil {
		t.Fatalf("valid caps rejected: %v", err)
	}
	if err := ValidateCapabilities([]string{"can_teleport"}); err == nil {
		t.Fatal("expected error for unknown capability")
	}
}

func TestDefaultSystemRoles(t *testing.T) {
	roles := DefaultSystemRoles()
	byID := map[string]SeedRole{}
	for _, r := range roles {
		byID[r.ID] = r
	}
	admin, ok := byID["role-admin"]
	if !ok || len(admin.Capabilities) != 6 || !admin.IsSystem {
		t.Fatalf("admin seed wrong: %+v", admin)
	}
	user := byID["role-user"]
	if len(user.Capabilities) != 3 {
		t.Errorf("user seed should have 3 caps, got %d", len(user.Capabilities))
	}
	req := byID["role-requester"]
	if len(req.Capabilities) != 1 || req.Capabilities[0] != CapRequest {
		t.Errorf("requester seed wrong: %+v", req)
	}
}
