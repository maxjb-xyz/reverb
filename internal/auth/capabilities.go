package auth

import "errors"

const (
	CapAdmin           = "is_admin"
	CapManageUsers     = "can_manage_users"
	CapManageLibrary   = "can_manage_library"
	CapDownload        = "can_download"
	CapRequest         = "can_request"
	CapCreatePlaylists = "can_create_playlists"
)

var ErrInvalidCapability = errors.New("unknown capability")

type Capability struct {
	Key   string `json:"key"`
	Label string `json:"label"`
}

// AllCapabilities is the fixed registry, in display order. Adding a capability
// here is the only way to introduce one; it is the enforcement contract.
func AllCapabilities() []Capability {
	return []Capability{
		{CapAdmin, "Administrator"},
		{CapManageUsers, "Manage users"},
		{CapManageLibrary, "Manage library & integrations"},
		{CapDownload, "Download tracks"},
		{CapRequest, "Request tracks"},
		{CapCreatePlaylists, "Create playlists"},
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
			CapAdmin, CapManageUsers, CapManageLibrary, CapDownload, CapRequest, CapCreatePlaylists,
		}},
		{ID: "role-user", Name: "User", IsSystem: true, Capabilities: []string{
			CapDownload, CapRequest, CapCreatePlaylists,
		}},
		{ID: "role-requester", Name: "Requester", IsSystem: true, Capabilities: []string{
			CapRequest,
		}},
	}
}
