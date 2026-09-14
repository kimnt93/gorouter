package clickhouse

import (
	"context"
	"errors"
	"time"

	"github.com/kimnt93/gorouter/pkg/entities"
	"github.com/kimnt93/gorouter/pkg/orgmodel"
)

// Budget locks deliberately do not expire: losing a process or an uncertain
// database acknowledgement must not reopen capacity. Operators reconcile the
// durable holds before removing an orphaned lock. Normal contention waits only
// within the caller's bounded deadline.
func (s *Store) budgetMutation(ctx context.Context, org string, fn func() error) error {
	locker, distributed := s.locker.(*RedisMutationLocker)
	if !distributed {
		return s.mutate(ctx, "org-budget:"+org, fn)
	}
	lockKey := "gorouter:budget-lock:" + org
	token := entities.NewID("lock")
	deadline, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	for {
		ok, err := locker.client.SetNX(deadline, lockKey, token, 0).Result()
		if err != nil {
			return ErrMutationLockUnavailable
		}
		if ok {
			break
		}
		select {
		case <-deadline.Done():
			return ErrMutationLockUnavailable
		case <-time.After(10 * time.Millisecond):
		}
	}
	err := fn()
	// Keep the fence after any uncertain storage error. Quota denial made no write.
	if err != nil && !errors.Is(err, orgmodel.ErrBudget) && !errors.Is(err, orgmodel.ErrInvalid) && !errors.Is(err, entities.ErrConflict) {
		return err
	}
	releaseCtx, releaseCancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer releaseCancel()
	if releaseErr := locker.client.Eval(releaseCtx, `if redis.call('get',KEYS[1]) == ARGV[1] then return redis.call('del',KEYS[1]) else return 0 end`, []string{lockKey}, token).Err(); releaseErr != nil {
		return ErrMutationLockUnavailable
	}
	return err
}
