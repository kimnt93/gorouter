package quota

import (
	"context"
	"sync"
	"time"

	"github.com/kimnt93/gorouter/pkg/entities"
)

type memoryWindow struct {
	spent        float64
	reservations map[string]float64
	expires      time.Time
}

type memoryRPM struct {
	count   int
	expires time.Time
}

// Memory coordinates quota and RPM state for the explicitly single-process
// local backend. Durable usage remains the source of the spent baseline.
type Memory struct {
	mu      sync.Mutex
	windows map[string]*memoryWindow
	rpm     map[string]*memoryRPM
}

func NewMemory() *Memory {
	return &Memory{windows: make(map[string]*memoryWindow), rpm: make(map[string]*memoryRPM)}
}

func (m *Memory) Reserve(ctx context.Context, keyID string, limit, durableSpent, estimated float64, now time.Time) (*Reservation, error) {
	return m.ReserveForPeriod(ctx, keyID, limit, durableSpent, estimated, "week", now)
}

func (m *Memory) ReserveForPeriod(ctx context.Context, keyID string, limit, durableSpent, estimated float64, period string, now time.Time) (*Reservation, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	_, end, suffix, err := Window(period, now)
	if err != nil {
		return nil, err
	}
	reservation := &Reservation{ID: entities.NewID("quota"), KeyID: keyID, Month: suffix, Window: suffix, Period: period, Estimated: estimated}
	if period == "none" || limit <= 0 {
		reservation.Bypassed = true
		return reservation, nil
	}
	key := keyID + "\x00" + suffix
	m.mu.Lock()
	defer m.mu.Unlock()
	window := m.windows[key]
	if window == nil || !now.Before(window.expires) {
		window = &memoryWindow{spent: durableSpent, reservations: make(map[string]float64), expires: end}
		m.windows[key] = window
	}
	if durableSpent > window.spent {
		window.spent = durableSpent
	}
	reserved := 0.0
	for _, value := range window.reservations {
		reserved += value
	}
	if window.spent+reserved+estimated > limit {
		return nil, ErrExceeded
	}
	window.reservations[reservation.ID] = estimated
	return reservation, nil
}

func (m *Memory) Settle(ctx context.Context, reservation *Reservation, actual float64) error {
	return m.finish(ctx, reservation, actual, true)
}
func (m *Memory) Release(ctx context.Context, reservation *Reservation) error {
	return m.finish(ctx, reservation, 0, false)
}
func (m *Memory) finish(ctx context.Context, reservation *Reservation, actual float64, settle bool) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if reservation == nil || reservation.Bypassed {
		return nil
	}
	key := reservation.KeyID + "\x00" + reservation.Window
	m.mu.Lock()
	defer m.mu.Unlock()
	window := m.windows[key]
	if window == nil {
		return ErrClosed
	}
	if _, ok := window.reservations[reservation.ID]; !ok {
		return ErrClosed
	}
	delete(window.reservations, reservation.ID)
	if settle {
		window.spent += actual
	}
	return nil
}

func (m *Memory) AllowRPM(ctx context.Context, keyID string, limit int, now time.Time) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	if limit <= 0 {
		return true, nil
	}
	minute := now.UTC().Truncate(time.Minute)
	key := keyID + "\x00" + minute.Format(time.RFC3339)
	m.mu.Lock()
	defer m.mu.Unlock()
	window := m.rpm[key]
	if window == nil || !now.Before(window.expires) {
		window = &memoryRPM{expires: minute.Add(time.Minute)}
		m.rpm[key] = window
	}
	window.count++
	return window.count <= limit, nil
}

var _ PeriodCoordinator = (*Memory)(nil)
