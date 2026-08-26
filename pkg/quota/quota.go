package quota

import (
	"context"
	"errors"
	"fmt"
	"sync/atomic"
	"time"
)

var (
	ErrExceeded    = errors.New("quota exceeded")
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
	Window    string
	Period    string
	Estimated float64
	Bypassed  bool
}

var weekStart atomic.Int32

func init() { weekStart.Store(int32(time.Sunday)) }

func SetWeekStart(day time.Weekday) {
	if day >= time.Sunday && day <= time.Saturday {
		weekStart.Store(int32(day))
	}
}

type Coordinator interface {
	Reserve(ctx context.Context, keyID string, weeklyLimit, durableSpentUSD, estimatedUSD float64, now time.Time) (*Reservation, error)
	Settle(ctx context.Context, reservation *Reservation, actualUSD float64) error
	Release(ctx context.Context, reservation *Reservation) error
	AllowRPM(ctx context.Context, keyID string, limit int, now time.Time) (bool, error)
}

// PeriodCoordinator is retained for gateway compatibility; only week is valid.
type PeriodCoordinator interface {
	Coordinator
	ReserveForPeriod(ctx context.Context, keyID string, limit, durableSpentUSD, estimatedUSD float64, period string, now time.Time) (*Reservation, error)
}

// Window returns the inclusive start and exclusive end of a UTC quota window,
// plus the stable suffix used by Redis reservation keys.
func Window(period string, now time.Time) (start, end time.Time, suffix string, err error) {
	u := now.UTC()
	switch period {
	case "week", "":
		daysSinceStart := (int(u.Weekday()) - int(weekStart.Load()) + 7) % 7
		start = time.Date(u.Year(), u.Month(), u.Day(), 0, 0, 0, 0, time.UTC).AddDate(0, 0, -daysSinceStart)
		end = start.AddDate(0, 0, 7)
		suffix = start.Format("2006-01-02")
	case "none":
		return time.Time{}, time.Time{}, "none", nil
	default:
		return time.Time{}, time.Time{}, "", fmt.Errorf("invalid quota period %q", period)
	}
	return start, end, suffix, nil
}
