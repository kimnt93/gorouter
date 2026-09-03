package quota

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestMemoryCoordinatesQuotaAndRPM(t *testing.T) {
	coordinator := NewMemory()
	now := time.Date(2026, 9, 3, 10, 0, 0, 0, time.UTC)
	reservation, err := coordinator.Reserve(context.Background(), "key", 10, 2, 7, now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := coordinator.Reserve(context.Background(), "key", 10, 2, 2, now); !errors.Is(err, ErrExceeded) {
		t.Fatalf("reserve error = %v", err)
	}
	if err := coordinator.Settle(context.Background(), reservation, 6); err != nil {
		t.Fatal(err)
	}
	if _, err := coordinator.Reserve(context.Background(), "key", 10, 2, 3, now); !errors.Is(err, ErrExceeded) {
		t.Fatalf("settled reserve error = %v", err)
	}
	for index := 0; index < 2; index++ {
		allowed, err := coordinator.AllowRPM(context.Background(), "key", 2, now)
		if err != nil || !allowed {
			t.Fatalf("RPM %d: allowed=%v err=%v", index, allowed, err)
		}
	}
	allowed, err := coordinator.AllowRPM(context.Background(), "key", 2, now)
	if err != nil || allowed {
		t.Fatalf("third RPM: allowed=%v err=%v", allowed, err)
	}
}
