package auth

import "testing"

func TestAllCapabilitiesContainsKnownKeys(t *testing.T) {
	caps := AllCapabilities()
	if len(caps) != 7 {
		t.Fatalf("want 7 capabilities, got %d", len(caps))
	}
	want := map[string]bool{CapAdmin: false, CapManageUsers: false, CapManageLibrary: false, CapAutoApprove: false, CapRequest: false, CapCreatePlaylists: false, CapManageRequests: false}
	for _, c := range caps {
		if _, ok := want[c.Key]; !ok {
			t.Errorf("unexpected capability %q", c.Key)
		}
		if c.Label == "" || c.Description == "" {
			t.Errorf("capability %q missing label or description", c.Key)
		}
		want[c.Key] = true
	}
	for k, seen := range want {
		if !seen {
			t.Errorf("missing capability %q", k)
		}
	}
	// the two renamed keys are present, the old keys are gone
	if !IsCapability("auto_approve") || !IsCapability("request") {
		t.Error("renamed keys missing")
	}
	if IsCapability("can_download") || IsCapability("can_request") {
		t.Error("old keys should be gone")
	}
	if !IsCapability("manage_requests") {
		t.Error("manage_requests capability missing")
	}
}

func TestValidateCapabilities(t *testing.T) {
	if err := ValidateCapabilities([]string{CapAutoApprove, CapRequest}); err != nil {
		t.Fatalf("valid caps rejected: %v", err)
	}
	if err := ValidateCapabilities([]string{"can_teleport"}); err == nil {
		t.Fatal("expected error for unknown capability")
	}
}

func TestDefaultSystemRoles(t *testing.T) {
	byID := map[string]SeedRole{}
	for _, r := range DefaultSystemRoles() {
		byID[r.ID] = r
	}
	if got := byID["role-admin"]; len(got.Capabilities) != 7 || !got.IsSystem {
		t.Fatalf("admin seed wrong: %+v", got)
	}
	if got := byID["role-user"]; len(got.Capabilities) != 3 {
		t.Errorf("user seed should have 3 caps, got %d", len(got.Capabilities))
	}
	req := byID["role-requester"]
	hasReq, hasPlaylists := false, false
	for _, c := range req.Capabilities {
		if c == CapRequest {
			hasReq = true
		}
		if c == CapCreatePlaylists {
			hasPlaylists = true
		}
	}
	if !hasReq || !hasPlaylists || len(req.Capabilities) != 2 {
		t.Errorf("requester seed should be [request, create_playlists], got %+v", req.Capabilities)
	}
}
