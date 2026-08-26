package quota

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/redis/go-redis/v9"
)

const quotaPrefix = "gorouter:quota:"
const rpmPrefix = "gorouter:rpm:"

var reserveScript = redis.NewScript("redis.call('HSETNX', KEYS[1], 'spent', ARGV[5])\n" +
	"local spent = tonumber(redis.call('HGET', KEYS[1], 'spent') or '0')\n" +
	"local reserved = tonumber(redis.call('HGET', KEYS[1], 'reserved') or '0')\n" +
	"local amount = tonumber(ARGV[1])\nlocal limit = tonumber(ARGV[2])\n" +
	"if spent + reserved + amount > limit then return 0 end\n" +
	"redis.call('HINCRBYFLOAT', KEYS[1], 'reserved', amount)\n" +
	"redis.call('HSET', KEYS[1], 'r:' .. ARGV[3], ARGV[1])\n" +
	"redis.call('EXPIREAT', KEYS[1], ARGV[4])\nreturn 1")

var settleScript = redis.NewScript("local field = 'r:' .. ARGV[1]\n" +
	"local estimated = redis.call('HGET', KEYS[1], field)\n" +
	"if not estimated then return 0 end\nredis.call('HDEL', KEYS[1], field)\n" +
	"redis.call('HINCRBYFLOAT', KEYS[1], 'reserved', -tonumber(estimated))\n" +
	"redis.call('HINCRBYFLOAT', KEYS[1], 'spent', tonumber(ARGV[2]))\nreturn 1")

var releaseScript = redis.NewScript("local field = 'r:' .. ARGV[1]\n" +
	"local estimated = redis.call('HGET', KEYS[1], field)\n" +
	"if not estimated then return 0 end\nredis.call('HDEL', KEYS[1], field)\n" +
	"redis.call('HINCRBYFLOAT', KEYS[1], 'reserved', -tonumber(estimated))\nreturn 1")

var rpmScript = redis.NewScript("local count = redis.call('INCR', KEYS[1])\n" +
	"if count == 1 then redis.call('EXPIREAT', KEYS[1], ARGV[2]) end\n" +
	"if count > tonumber(ARGV[1]) then return 0 end\nreturn 1")

type Redis struct {
	rdb    redis.UniversalClient
	policy Policy
}

func NewRedis(rdb redis.UniversalClient, policy Policy) (*Redis, error) {
	if rdb == nil {
		return nil, errors.New("nil Redis client")
	}
	if policy != PolicyStrict && policy != PolicyOpen {
		return nil, fmt.Errorf("invalid policy %q", policy)
	}
	return &Redis{rdb: rdb, policy: policy}, nil
}

func (r *Redis) Reserve(ctx context.Context, keyID string, limit, durableSpent, estimated float64, now time.Time) (*Reservation, error) {
	return r.ReserveForPeriod(ctx, keyID, limit, durableSpent, estimated, "week", now)
}

func (r *Redis) ReserveForPeriod(ctx context.Context, keyID string, limit, durableSpent, estimated float64, period string, now time.Time) (*Reservation, error) {
	if keyID == "" || limit < 0 || durableSpent < 0 || estimated < 0 {
		return nil, errors.New("invalid quota reservation")
	}
	_, end, window, err := Window(period, now)
	if err != nil {
		return nil, err
	}
	if period == "none" {
		return &Reservation{KeyID: keyID, Window: window, Period: period, Bypassed: true}, nil
	}
	id, err := reservationID()
	if err != nil {
		return nil, err
	}
	res := &Reservation{ID: id, KeyID: keyID, Month: window, Window: window, Period: period, Estimated: estimated}
	ok, err := reserveScript.Run(ctx, r.rdb, []string{quotaKey(keyID, window)}, decimal(estimated), decimal(limit), id, end.Unix(), decimal(durableSpent)).Int()
	if err != nil {
		if r.policy == PolicyOpen {
			res.Bypassed = true
			return res, nil
		}
		return nil, fmt.Errorf("%w: %v", ErrUnavailable, err)
	}
	if ok == 0 {
		return nil, ErrExceeded
	}
	return res, nil
}

func (r *Redis) Settle(ctx context.Context, res *Reservation, actual float64) error {
	if res == nil || res.Bypassed {
		return nil
	}
	if actual < 0 {
		return errors.New("actual cost cannot be negative")
	}
	ok, err := settleScript.Run(ctx, r.rdb, []string{quotaKey(res.KeyID, reservationWindow(res))}, res.ID, decimal(actual)).Int()
	return r.mutationResult(ok, err)
}

func (r *Redis) Release(ctx context.Context, res *Reservation) error {
	if res == nil || res.Bypassed {
		return nil
	}
	ok, err := releaseScript.Run(ctx, r.rdb, []string{quotaKey(res.KeyID, reservationWindow(res))}, res.ID).Int()
	return r.mutationResult(ok, err)
}

func (r *Redis) AllowRPM(ctx context.Context, keyID string, limit int, now time.Time) (bool, error) {
	if limit <= 0 {
		return true, nil
	}
	minute := now.UTC().Truncate(time.Minute)
	key := rpmPrefix + keyID + ":" + minute.Format("200601021504")
	ok, err := rpmScript.Run(ctx, r.rdb, []string{key}, limit, minute.Add(time.Minute).Unix()).Int()
	if err != nil {
		if r.policy == PolicyOpen {
			return true, nil
		}
		return false, fmt.Errorf("%w: %v", ErrUnavailable, err)
	}
	return ok == 1, nil
}

func (r *Redis) mutationResult(ok int, err error) error {
	if err != nil {
		if r.policy == PolicyOpen {
			return nil
		}
		return fmt.Errorf("%w: %v", ErrUnavailable, err)
	}
	if ok == 0 {
		return ErrClosed
	}
	return nil
}

func quotaKey(keyID, month string) string { return quotaPrefix + keyID + ":" + month }
func reservationWindow(res *Reservation) string {
	if res.Window != "" {
		return res.Window
	}
	return res.Month
}
func decimal(v float64) string { return strconv.FormatFloat(v, 'f', -1, 64) }

func reservationID() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(b[:]), nil
}

var _ Coordinator = (*Redis)(nil)
var _ PeriodCoordinator = (*Redis)(nil)
