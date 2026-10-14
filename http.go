package main

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

const maxWait = 60 * time.Second

type Person struct {
	ID       int64  `json:"id"`
	Username string `json:"username,omitempty"`
	Name     string `json:"name,omitempty"`
}

// DutyStatus is what GET /api/duty returns.
type DutyStatus struct {
	// Bumps on every change (new pick, absent, tea ready). Pass it back as ?since= to long-poll.
	Version int64 `json:"version"`
	// idle: nothing picked yet; assigned: waiting for tea; ready: tea is ready; nobody: everyone absent.
	Status string `json:"status"`
	// Day of the round (YYYY-MM-DD in the bot's timezone).
	Date string `json:"date,omitempty"`
	// Bumps every time a new person is picked; compare it to detect a new assignee.
	AssignmentID int64      `json:"assignment_id,omitempty"`
	Assignee     *Person    `json:"assignee"`
	AssignedAt   *time.Time `json:"assigned_at,omitempty"`
	ReadyAt      *time.Time `json:"ready_at,omitempty"`
	Absent       []Person   `json:"absent"`
}

func dutyStatus(st *State) DutyStatus {
	s := DutyStatus{Version: st.Version, Status: "idle", Absent: []Person{}}
	d := st.Duty
	if d == nil {
		return s
	}
	s.Date = d.Date
	for _, id := range d.Absent {
		s.Absent = append(s.Absent, person(st, id))
	}
	switch {
	case d.UserID == 0:
		s.Status = "nobody"
		return s
	case d.ReadyAt != nil:
		s.Status = "ready"
	default:
		s.Status = "assigned"
	}
	p := person(st, d.UserID)
	at := d.AssignedAt
	s.Assignee, s.AssignmentID, s.AssignedAt, s.ReadyAt = &p, d.AssignmentID, &at, d.ReadyAt
	return s
}

func person(st *State, id int64) Person {
	if m := st.Members[id]; m != nil {
		return Person{ID: id, Username: m.Username, Name: displayName(m)}
	}
	return Person{ID: id}
}

// Feed holds the latest DutyStatus and wakes long-pollers when it changes.
type Feed struct {
	mu      sync.RWMutex
	current DutyStatus
	changed chan struct{}
}

func NewFeed() *Feed {
	return &Feed{changed: make(chan struct{})}
}

func (f *Feed) Publish(s DutyStatus) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.current = s
	close(f.changed)
	f.changed = make(chan struct{})
}

func (f *Feed) Get() (DutyStatus, <-chan struct{}) {
	f.mu.RLock()
	defer f.mu.RUnlock()
	return f.current, f.changed
}

func (b *Bot) serveHTTP(ctx context.Context) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte("ok\n"))
	})
	mux.Handle("GET /api/duty", b.requireToken(http.HandlerFunc(b.handleDuty)))

	srv := &http.Server{
		Addr:              b.cfg.HTTPAddr,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
		IdleTimeout:       2 * time.Minute,
	}
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		srv.Shutdown(shutdownCtx)
	}()

	if b.cfg.APIToken == "" {
		log.Printf("HTTP API on %s (no API_TOKEN set — anyone who can reach it can read it)", b.cfg.HTTPAddr)
	} else {
		log.Printf("HTTP API on %s", b.cfg.HTTPAddr)
	}
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Printf("HTTP API stopped: %v", err)
	}
}

// requireToken accepts "Authorization: Bearer <token>" or ?token=<token> when API_TOKEN is set.
func (b *Bot) requireToken(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if b.cfg.APIToken != "" {
			got := r.URL.Query().Get("token")
			if h := r.Header.Get("Authorization"); strings.HasPrefix(h, "Bearer ") {
				got = strings.TrimPrefix(h, "Bearer ")
			}
			if subtle.ConstantTimeCompare([]byte(got), []byte(b.cfg.APIToken)) != 1 {
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

// handleDuty returns the current duty. With ?since=<version> it long-polls: the response
// is held until the version differs from since, or ?wait= seconds pass (default 30, max 60).
func (b *Bot) handleDuty(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	status, changed := b.feed.Get()

	if v := q.Get("since"); v != "" {
		since, err := strconv.ParseInt(v, 10, 64)
		if err != nil {
			http.Error(w, "since must be a number", http.StatusBadRequest)
			return
		}
		wait := 30 * time.Second
		if v := q.Get("wait"); v != "" {
			sec, err := strconv.Atoi(v)
			if err != nil || sec < 0 {
				http.Error(w, "wait must be a non-negative number of seconds", http.StatusBadRequest)
				return
			}
			wait = min(time.Duration(sec)*time.Second, maxWait)
		}
		timeout := time.NewTimer(wait)
		defer timeout.Stop()
		for status.Version == since {
			select {
			case <-changed:
				status, changed = b.feed.Get()
			case <-timeout.C:
				writeJSON(w, status)
				return
			case <-r.Context().Done():
				return
			}
		}
	}
	writeJSON(w, status)
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	json.NewEncoder(w).Encode(v)
}
