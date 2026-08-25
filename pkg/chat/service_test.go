package chat

import (
	"sync"
	"testing"
	"time"
)

func candidateIDs(candidates []Candidate) []string {
	out := make([]string, len(candidates))
	for i, candidate := range candidates {
		out[i] = candidate.ID
	}
	return out
}

func TestPriorityOrderAndTieBreak(t *testing.T) {
	selector := &Selector{}
	got := candidateIDs(selector.Order(StrategyPriority, []Candidate{{ID: "b", Priority: 5}, {ID: "c", Priority: 10}, {ID: "a", Priority: 10}}))
	want := []string{"a", "c", "b"}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("priority order=%v want=%v", got, want)
		}
	}
}

func TestRoundRobinRotatesAndIsConcurrentSafe(t *testing.T) {
	selector := &Selector{}
	input := []Candidate{{ID: "a"}, {ID: "b"}, {ID: "c"}}
	if got := selector.Order(StrategyRoundRobin, input); got[0].ID != "a" {
		t.Fatalf("first rotation=%v", got)
	}
	if got := selector.Order(StrategyRoundRobin, input); got[0].ID != "b" {
		t.Fatalf("second rotation=%v", got)
	}
	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			got := selector.Order(StrategyRoundRobin, input)
			if len(got) != len(input) {
				t.Errorf("rotation length=%d", len(got))
			}
		}()
	}
	wg.Wait()
}

func TestHealthCooldownAndRecovery(t *testing.T) {
	health := NewHealth()
	health.Report("credential", false)
	health.Report("credential", false)
	if !health.Available("credential") {
		t.Fatal("credential cooled down before three consecutive failures")
	}
	health.Report("credential", false)
	if health.Available("credential") {
		t.Fatal("credential remained available after three failures")
	}
	health.Report("credential", true)
	if !health.Available("credential") {
		t.Fatal("successful request did not clear cooldown")
	}
	health.Report("credential", false)
	health.Report("credential", false)
	health.Report("credential", false)
	health.mu.Lock()
	health.banned["credential"] = time.Now().Add(-time.Second)
	health.mu.Unlock()
	if !health.Available("credential") {
		t.Fatal("expired cooldown remained active")
	}
}
