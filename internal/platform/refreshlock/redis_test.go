package refreshlock

import (
	"context"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
)

func TestRedisLockSkipsContendingReplicaAndReleasesOwner(t *testing.T) {
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	first, _ := NewRedis(client, time.Minute)
	second, _ := NewRedis(client, time.Minute)
	entered := make(chan struct{})
	release := make(chan struct{})
	done := make(chan error, 1)
	go func() {
		_, err := first.WithLock(context.Background(), "models", func() error {
			close(entered)
			<-release
			return nil
		})
		done <- err
	}()
	<-entered
	called := false
	acquired, err := second.WithLock(context.Background(), "models", func() error { called = true; return nil })
	if err != nil || acquired || called {
		t.Fatalf("contention acquired=%v called=%v err=%v", acquired, called, err)
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	acquired, err = second.WithLock(context.Background(), "models", func() error { called = true; return nil })
	if err != nil || !acquired || !called {
		t.Fatalf("released acquired=%v called=%v err=%v", acquired, called, err)
	}
}
