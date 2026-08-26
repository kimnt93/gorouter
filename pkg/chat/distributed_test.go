package chat

import (
	"testing"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
)

func TestHealthAndRoundRobinStateIsSharedThroughRedis(t *testing.T) {
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() { _ = client.Close() })
	firstHealth, secondHealth := NewHealth(), NewHealth()
	firstHealth.SetRedis(client)
	secondHealth.SetRedis(client)
	for range 3 {
		firstHealth.Report("cred-a", false)
	}
	if secondHealth.Available("cred-a") {
		t.Fatal("second replica did not observe distributed health ban")
	}
	secondHealth.Report("cred-a", true)
	if !firstHealth.Available("cred-a") {
		t.Fatal("successful replica did not clear distributed health ban")
	}

	firstSelector, secondSelector := &Selector{}, &Selector{}
	firstSelector.SetRedis(client)
	secondSelector.SetRedis(client)
	candidates := []Candidate{{ID: "a"}, {ID: "b"}}
	if got := firstSelector.Order(StrategyRoundRobin, candidates)[0].ID; got != "a" {
		t.Fatalf("first selection=%q", got)
	}
	if got := secondSelector.Order(StrategyRoundRobin, candidates)[0].ID; got != "b" {
		t.Fatalf("second selection=%q", got)
	}
}
