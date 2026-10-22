package main

import (
	"encoding/json"
	"errors"
	"hash/fnv"
	"os"
	"path/filepath"
	"strings"
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
	changed := false
	if u.Username != "" {
		changed = s.resolvePlaceholder(placeholderID(u.Username), u.ID)
	}
	next := Member{ID: u.ID, Username: u.Username, FirstName: u.FirstName, LastName: u.LastName}
	if cur, ok := s.Members[u.ID]; ok && *cur == next {
		return changed
	}
	s.Members[u.ID] = &next
	return true
}

// AddUsername adds someone known only by @username: the Bot API can't look up user ids,
// so they get a placeholder id until the bot sees them. Reports whether anything changed.
func (s *Store) AddUsername(username string) bool {
	if s.FindUsername(username) != nil {
		return false
	}
	id := placeholderID(username)
	s.Members[id] = &Member{ID: id, Username: username}
	return true
}

func (s *Store) FindUsername(username string) *Member {
	for _, m := range s.Members {
		if strings.EqualFold(m.Username, username) {
			return m
		}
	}
	return nil
}

// resolvePlaceholder swaps a placeholder member for the real user id everywhere it is used.
func (s *Store) resolvePlaceholder(placeholder, id int64) bool {
	if _, ok := s.Members[placeholder]; !ok {
		return false
	}
	delete(s.Members, placeholder)
	if d := s.Duty; d != nil {
		if d.UserID == placeholder {
			d.UserID = id
		}
		for i, a := range d.Absent {
			if a == placeholder {
				d.Absent[i] = id
			}
		}
	}
	return true
}

// placeholderID is a stable negative id for a username; real user ids are positive.
func placeholderID(username string) int64 {
	h := fnv.New64a()
	h.Write([]byte(strings.ToLower(username)))
	return -int64(h.Sum64()>>1) - 1
}

// IsPlaceholder reports whether the member was added by username and hasn't been seen yet.
func (m *Member) IsPlaceholder() bool {
	return m.ID < 0
}

func (s *Store) Remove(id int64) bool {
	if _, ok := s.Members[id]; !ok {
		return false
	}
	delete(s.Members, id)
	return true
}
