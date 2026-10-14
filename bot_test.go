package main

import (
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func testConfig(t *testing.T) *Config {
	t.Helper()
	loc, err := time.LoadLocation("Europe/Moscow")
	if err != nil {
		t.Fatal(err)
	}
	return &Config{
		ExcludedUsernames: map[string]bool{"boss": true},
		ExcludedIDs:       map[int64]bool{4: true},
		Hour:              17,
		Location:          loc,
	}
}

func ids(ms []*Member) []int64 {
	var out []int64
	for _, m := range ms {
		out = append(out, m.ID)
	}
	return out
}

func TestEligible(t *testing.T) {
	cfg := testConfig(t)
	members := map[int64]*Member{
		1: {ID: 1, Username: "alice"},
		2: {ID: 2, Username: "Boss"}, // excluded by username, case-insensitive
		3: {ID: 3, FirstName: "NoUsername"},
		4: {ID: 4, Username: "carol"}, // excluded by id
		5: {ID: 5, Username: "dave"},
	}

	if got := ids(eligible(members, cfg, nil, 0)); !equal(got, []int64{1, 3, 5}) {
		t.Errorf("no absences: got %v", got)
	}
	if got := ids(eligible(members, cfg, []int64{1}, 1)); !equal(got, []int64{3, 5}) {
		t.Errorf("1 absent: got %v", got)
	}
	if got := ids(eligible(members, cfg, nil, 3)); !equal(got, []int64{1, 5}) {
		t.Errorf("previous pick skipped: got %v", got)
	}
	if got := ids(eligible(members, cfg, []int64{1, 3}, 5)); !equal(got, []int64{5}) {
		t.Errorf("only previous left: got %v", got)
	}
	if got := ids(eligible(members, cfg, []int64{1, 3, 5}, 5)); len(got) != 0 {
		t.Errorf("all absent: got %v", got)
	}
}

func TestNextRun(t *testing.T) {
	cfg := testConfig(t)
	at := func(s string) time.Time {
		tm, err := time.ParseInLocation("2006-01-02 15:04", s, cfg.Location)
		if err != nil {
			t.Fatal(err)
		}
		return tm
	}

	cases := []struct{ now, want string }{
		{"2026-10-14 09:00", "2026-10-14 17:00"}, // Wed morning -> same day
		{"2026-10-14 17:00", "2026-10-15 17:00"}, // Wed at 17:00 -> Thu
		{"2026-10-16 18:00", "2026-10-19 17:00"}, // Fri evening -> Mon
		{"2026-10-17 10:00", "2026-10-19 17:00"}, // Sat -> Mon
		{"2026-10-18 23:59", "2026-10-19 17:00"}, // Sun -> Mon
	}
	for _, c := range cases {
		if got := nextRun(at(c.now), cfg); !got.Equal(at(c.want)) {
			t.Errorf("nextRun(%s) = %s, want %s", c.now, got, c.want)
		}
	}
}

func TestMessageTemplates(t *testing.T) {
	for _, tpl := range append(append([]string{}, pickMessages...), readyMessages...) {
		if strings.Count(tpl, "{who}") != 1 {
			t.Errorf("template must mention {who} exactly once: %q", tpl)
		}
		if strings.ContainsAny(tpl, "<>&") {
			t.Errorf("template must be HTML-safe: %q", tpl)
		}
	}
	if got := pickMessage("@alice"); !strings.Contains(got, "@alice") || strings.Contains(got, "{who}") {
		t.Errorf("pickMessage = %q", got)
	}
}

func TestCommand(t *testing.T) {
	b := &Bot{username: "FiveOClockBot"}
	cases := map[string]string{
		"/tea":                   "tea",
		"/tea@FiveOClockBot":     "tea",
		"/tea@fiveoclockbot now": "tea",
		"/tea@OtherBot":          "",
		"tea":                    "",
		"/":                      "",
	}
	for in, want := range cases {
		if got := b.command(in); got != want {
			t.Errorf("command(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestStoreRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sub", "state.json")
	s, err := LoadStore(path)
	if err != nil {
		t.Fatal(err)
	}
	if !s.Upsert(User{ID: 1, Username: "alice", FirstName: "Alice"}) {
		t.Fatal("first upsert should change")
	}
	if s.Upsert(User{ID: 1, Username: "alice", FirstName: "Alice"}) {
		t.Fatal("identical upsert should not change")
	}
	if s.Upsert(User{ID: 2, IsBot: true}) {
		t.Fatal("bots are not members")
	}
	s.Duty = &Duty{Date: "2026-10-14", UserID: 1, MessageID: 7, Absent: []int64{3}}
	if err := s.Save(); err != nil {
		t.Fatal(err)
	}

	loaded, err := LoadStore(path)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Members[1].Username != "alice" || loaded.Duty.MessageID != 7 || loaded.Duty.Absent[0] != 3 {
		t.Fatalf("round trip mismatch: %+v %+v", loaded.Members, loaded.Duty)
	}
}

func equal(a, b []int64) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
