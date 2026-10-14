package main

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"time"
)

// Member is someone the bot has seen in the group.
type Member struct {
	ID        int64  `json:"id"`
	Username  string `json:"username,omitempty"`
	FirstName string `json:"first_name"`
	LastName  string `json:"last_name,omitempty"`
}

// Duty is the current day's tea round.
type Duty struct {
	Date         string     `json:"date"`       // YYYY-MM-DD in the configured timezone
	UserID       int64      `json:"user_id"`    // currently selected person, 0 if nobody
	MessageID    int        `json:"message_id"` // message carrying the buttons, 0 if none
	Absent       []int64    `json:"absent,omitempty"`
	AssignmentID int64      `json:"assignment_id,omitempty"`
	AssignedAt   time.Time  `json:"assigned_at"`
	ReadyAt      *time.Time `json:"ready_at,omitempty"`
}

type State struct {
	Members map[int64]*Member `json:"members"`
	Duty    *Duty             `json:"duty,omitempty"`
	// Version bumps on every duty change; LastAssignment on every new person picked.
	Version        int64 `json:"version"`
	LastAssignment int64 `json:"last_assignment"`
}

// Store persists State as JSON. Not safe for concurrent use; the bot serialises access.
type Store struct {
	path string
	State
}

func LoadStore(path string) (*Store, error) {
	s := &Store{path: path, State: State{Members: map[int64]*Member{}}}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return s, nil
	}
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(data, &s.State); err != nil {
		return nil, err
	}
	if s.Members == nil {
		s.Members = map[int64]*Member{}
	}
	return s, nil
}

func (s *Store) Save() error {
	data, err := json.MarshalIndent(s.State, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}

// Upsert adds or refreshes a member. Reports whether anything changed.
func (s *Store) Upsert(u User) bool {
	if u.IsBot {
		return false
	}
	next := Member{ID: u.ID, Username: u.Username, FirstName: u.FirstName, LastName: u.LastName}
	if cur, ok := s.Members[u.ID]; ok && *cur == next {
		return false
	}
	s.Members[u.ID] = &next
	return true
}

func (s *Store) Remove(id int64) bool {
	if _, ok := s.Members[id]; !ok {
		return false
	}
	delete(s.Members, id)
	return true
}
