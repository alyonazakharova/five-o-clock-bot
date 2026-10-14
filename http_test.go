package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"
)

func newTestBot(t *testing.T, token string) *Bot {
	path := filepath.Join(t.TempDir(), "state.json")
	b := NewBot(nil, &Config{APIToken: token}, &Store{path: path, State: State{Members: map[int64]*Member{
		1: {ID: 1, Username: "alice", FirstName: "Alice"},
		2: {ID: 2, FirstName: "Bob"},
	}}})
	b.publish()
	return b
}

func getDuty(t *testing.T, h http.Handler, url string, header http.Header) (int, DutyStatus) {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, url, nil)
	for k, v := range header {
		req.Header[k] = v
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	var s DutyStatus
	if rec.Code == http.StatusOK {
		if err := json.Unmarshal(rec.Body.Bytes(), &s); err != nil {
			t.Fatal(err)
		}
	}
	return rec.Code, s
}

func TestDutyStatus(t *testing.T) {
	b := newTestBot(t, "")
	h := http.HandlerFunc(b.handleDuty)

	if _, s := getDuty(t, h, "/api/duty", nil); s.Status != "idle" || s.Assignee != nil {
		t.Fatalf("initial = %+v", s)
	}

	b.store.Duty = &Duty{Date: "2026-10-14", UserID: 1, Absent: []int64{2}, AssignmentID: 3, AssignedAt: time.Now()}
	b.dutyChanged()
	_, s := getDuty(t, h, "/api/duty", nil)
	if s.Status != "assigned" || s.Assignee.Username != "alice" || s.AssignmentID != 3 || s.Absent[0].Name != "Bob" {
		t.Fatalf("assigned = %+v", s)
	}

	now := time.Now()
	b.store.Duty.ReadyAt = &now
	b.dutyChanged()
	if _, s := getDuty(t, h, "/api/duty", nil); s.Status != "ready" || s.ReadyAt == nil {
		t.Fatalf("ready = %+v", s)
	}
}

func TestDutyLongPoll(t *testing.T) {
	b := newTestBot(t, "")
	h := http.HandlerFunc(b.handleDuty)
	_, initial := getDuty(t, h, "/api/duty", nil)

	go func() {
		time.Sleep(50 * time.Millisecond)
		b.store.Duty = &Duty{Date: "2026-10-14", UserID: 2, AssignmentID: 1}
		b.dutyChanged()
	}()
	start := time.Now()
	_, s := getDuty(t, h, "/api/duty?since="+itoa(initial.Version)+"&wait=5", nil)
	if s.Version == initial.Version || s.Assignee == nil || s.Assignee.ID != 2 {
		t.Fatalf("long poll got %+v", s)
	}
	if time.Since(start) > 2*time.Second {
		t.Fatal("long poll did not wake on change")
	}

	// No change: returns the same version once wait elapses.
	_, same := getDuty(t, h, "/api/duty?since="+itoa(s.Version)+"&wait=0", nil)
	if same.Version != s.Version {
		t.Fatalf("timeout response = %+v", same)
	}
}

func TestDutyToken(t *testing.T) {
	b := newTestBot(t, "secret")
	h := b.requireToken(http.HandlerFunc(b.handleDuty))

	if code, _ := getDuty(t, h, "/api/duty", nil); code != http.StatusUnauthorized {
		t.Errorf("no token: %d", code)
	}
	if code, _ := getDuty(t, h, "/api/duty?token=wrong", nil); code != http.StatusUnauthorized {
		t.Errorf("wrong token: %d", code)
	}
	if code, _ := getDuty(t, h, "/api/duty?token=secret", nil); code != http.StatusOK {
		t.Errorf("query token: %d", code)
	}
	if code, _ := getDuty(t, h, "/api/duty", http.Header{"Authorization": {"Bearer secret"}}); code != http.StatusOK {
		t.Errorf("bearer token: %d", code)
	}
}

func itoa(n int64) string {
	b, _ := json.Marshal(n)
	return string(b)
}
