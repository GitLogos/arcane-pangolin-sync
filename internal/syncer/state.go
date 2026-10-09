package syncer

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
)

func loadState(path string) (State, error) {
	state := State{
		Resources:                map[string]ManagedResource{},
		Seen:                     map[string]bool{},
		PolicyIgnoredResourceIDs: map[string]bool{},
		IgnoredSubdomains:        map[string]bool{},
	}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return state, nil
	}
	if err != nil {
		return state, err
	}
	if len(data) == 0 {
		return state, nil
	}
	if err := json.Unmarshal(data, &state); err != nil {
		return state, err
	}
	if state.Resources == nil {
		state.Resources = map[string]ManagedResource{}
	}
	if state.Seen == nil {
		state.Seen = map[string]bool{}
	}
	if state.PolicyIgnoredResourceIDs == nil {
		state.PolicyIgnoredResourceIDs = map[string]bool{}
	}
	if state.IgnoredSubdomains == nil {
		state.IgnoredSubdomains = map[string]bool{}
	}
	return state, nil
}

func saveState(path string, state State) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
