package quota

import (
	"context"
	"errors"
	"fmt"
	"time"
)

var (
	ErrExceeded    = errors.New("monthly quota exceeded")
	ErrClosed      = errors.New("quota reservation is already settled or released")
	ErrUnavailable = errors.New("redis quota coordinator unavailable")
)

type Policy string

const (
	PolicyStrict Policy = "strict"
	PolicyOpen   Policy = "open"
)

func ParsePolicy(v string) (Policy, error) {
	switch Policy(v) {
	case PolicyStrict, "":
		return PolicyStrict, nil
	case PolicyOpen:
		return PolicyOpen, nil
	default:
		return "", fmt.Errorf("unknown Redis outage policy %q", v)
	}
}

type Reservation struct {
	ID        string
	KeyID     string
	Month     string
	Estimated float64
	Bypassed  bool
}

type Coordinator interface {
	Reserve(ctx context.Context, keyID string, monthlyLimit, durableSpentUSD, estimatedUSD float64, now time.Time) (*Reservation, error)
	Settle(ctx context.Context, reservation *Reservation, actualUSD float64) error
	Release(ctx context.Context, reservation *Reservation) error
	AllowRPM(ctx context.Context, keyID string, limit int, now time.Time) (bool, error)
}
