package auth

import "errors"

const (
	CapAdmin           = "is_admin"
	CapManageUsers     = "can_manage_users"
	CapManageLibrary   = "can_manage_library"
	CapRequest         = "request"
	CapAutoApprove     = "auto_approve"
	CapManageRequests  = "manage_requests"
	CapCreatePlaylists = "can_create_playlists"
)

var ErrInvalidCapability = errors.New("unknown capability")

type Capability struct {
	Key         string `json:"key"`
	Label       string `json:"label"`
	Description string `json:"description"`
}

// AllCapabilities is the fixed registry, in display order. Adding a capability
// here is the only way to introduce one; it is the enforcement contract.
func AllCapabilities() []Capability {
	return []Capability{
		{CapAdmin, "Full administrator", "Complete access; bypasses all restrictions. Opens the Admin area."},
		{CapManageUsers, "Manage users & roles", "Create and edit users, edit roles, and control registration & invites. Opens the Admin area."},
		{CapManageLibrary, "Manage library & integrations", "Configure the music backend, search providers, and downloaders. Opens the Admin area."},
		{CapRequest, "Request music", "Ask to add music to the library. Fulfilled instantly if Auto-approve is also granted; otherwise it waits for an administrator's approval."},
		{CapAutoApprove, "Auto-approve music", "Requests to add music are fulfilled immediately, without approval (one-click add). Implies Request."},
		{CapManageRequests, "Approve requests", "Review and approve or deny other users' requests to add music."},
		{CapCreatePlaylists, "Create & edit playlists", "Make and manage their own playlists."},
	}
}

func IsCapability(key string) bool {
	for _, c := range AllCapabilities() {
		if c.Key == key {
			return true
		}
	}
	return false
}

func ValidateCapabilities(keys []string) error {
	for _, k := range keys {
		if !IsCapability(k) {
			return ErrInvalidCapability
		}
	}
	return nil
}

type SeedRole struct {
	ID           string
	Name         string
	IsSystem     bool
	Capabilities []string
}

func DefaultSystemRoles() []SeedRole {
	return []SeedRole{
		{ID: "role-admin", Name: "Admin", IsSystem: true, Capabilities: []string{
			CapAdmin, CapManageUsers, CapManageLibrary, CapRequest, CapAutoApprove, CapManageRequests, CapCreatePlaylists,
		}},
		{ID: "role-user", Name: "User", IsSystem: true, Capabilities: []string{
			CapAutoApprove, CapRequest, CapCreatePlaylists,
		}},
		{ID: "role-requester", Name: "Requester", IsSystem: true, Capabilities: []string{
			CapRequest, CapCreatePlaylists,
		}},
	}
}
