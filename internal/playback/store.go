package playback

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
)

// SettingsStore keeps each account's state as JSON in the settings table
// under "playback:<user id>".
type SettingsStore struct {
	get func(ctx context.Context, key string) (string, error)
	put func(ctx context.Context, key, value string) error
}

// NewSettingsStore builds a store from the settings getters and setters.
func NewSettingsStore(get func(ctx context.Context, key string) (string, error), put func(ctx context.Context, key, value string) error) *SettingsStore {
	return &SettingsStore{get: get, put: put}
}

func settingsKey(userID string) string { return "playback:" + userID }

// LoadState implements Store.
func (s *SettingsStore) LoadState(ctx context.Context, userID string) (*State, error) {
	raw, err := s.get(ctx, settingsKey(userID))
	if errors.Is(err, sql.ErrNoRows) || raw == "" {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var st State
	if err := json.Unmarshal([]byte(raw), &st); err != nil {
		return nil, nil // a corrupt row is as good as none
	}
	return &st, nil
}

// SaveState implements Store.
func (s *SettingsStore) SaveState(ctx context.Context, userID string, st *State) error {
	b, err := json.Marshal(st)
	if err != nil {
		return err
	}
	return s.put(ctx, settingsKey(userID), string(b))
}
