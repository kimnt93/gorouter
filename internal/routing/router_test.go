package routing

import (
	"testing"
	"time"
)

func ids(cs []Candidate) []string {
	out := make([]string, len(cs))
	for i, c := range cs {
		out[i] = c.ID
	}
	return out
}

func TestPriorityOrder(t *testing.T) {
	s := &Selector{}
	in := []Candidate{
		{ID: "b", Priority: 5}, {ID: "a", Priority: 10}, {ID: "c", Priority: 10},
	}
	got := ids(s.Order(StrategyPriority, in))
	want := []string{"a", "c", "b"}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %v want %v", got, want)
		}
	}
}

func TestRoundRobinRotates(t *testing.T) {
	s := &Selector{}
	in := []Candidate{{ID: "a"}, {ID: "b"}, {ID: "c"}}
	first := ids(s.Order(StrategyRoundRobin, in))
	if first[0] != "a" {
		t.Fatalf("first rotation should start at a, got %v", first)
	}
	second := ids(s.Order(StrategyRoundRobin, in))
	if second[0] != "b" || second[1] != "c" || second[2] != "a" {
		t.Fatalf("second rotation wrong: %v", second)
	}
	third := ids(s.Order(StrategyRoundRobin, in))
	fourth := ids(s.Order(StrategyRoundRobin, in))
	if third[0] != "c" || fourth[0] != "a" {
		t.Fatalf("rotation wrap wrong: %v %v", third, fourth)
	}
}

func TestPriorityIgnoresCallOrder(t *testing.T) {
	s := &Selector{}
	in := []Candidate{{ID: "x", Priority: 1}, {ID: "y", Priority: 9}}
	a := ids(s.Order(StrategyPriority, in))
	b := ids(s.Order(StrategyPriority, in))
	if a[0] != "y" || b[0] != "y" {
		t.Fatalf("priority must be stable, got %v %v", a, b)
	}
}

func TestHealthBanAndRecover(t *testing.T) {
	h := NewHealth()
	for i := 0; i < 2; i++ {
		h.Report("cred1", false)
	}
	if !h.Available("cred1") {
		t.Fatal("should still be available after 2 failures")
	}
	h.Report("cred1", false)
	if h.Available("cred1") {
		t.Fatal("expected ban after 3 consecutive failures")
	}
	h.Report("cred1", true)
	if !h.Available("cred1") {
		t.Fatal("success should clear ban")
	}
}

func TestHealthBanExpires(t *testing.T) {
	h := NewHealth()
	for i := 0; i < 3; i++ {
		h.Report("x", false)
	}
	h.mu.Lock()
	h.banned["x"] = time.Now().Add(-time.Second)
	h.mu.Unlock()
	if !h.Available("x") {
		t.Fatal("expired ban should be available")
	}
}

func TestEmptyCandidates(t *testing.T) {
	s := &Selector{}
	if got := s.Order(StrategyPriority, nil); len(got) != 0 {
		t.Fatal("expected empty")
	}
	if got := s.Order(StrategyRoundRobin, []Candidate{}); len(got) != 0 {
		t.Fatal("expected empty")
	}
}
