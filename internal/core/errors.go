package core

import "errors"

// ErrLibraryItemNotFound is returned by a LibraryAdapter when the backend is
// reachable but has no such item (stale ID, unknown track, missing artwork).
// Handlers map this to 404; transport errors (backend unreachable) are left
// unwrapped so they stay as 502.
var ErrLibraryItemNotFound = errors.New("library item not found")
