package redis_lock

import (
	"context"
	"errors"
	"time"
)

//红锁中每个节点默认的处理时间未50ms

const DefaultSingleLockTimeout = 50 * time.Millisecond

type RedLock struct {
	locks []*RedisLock
	RedLockOptions
}

func NewRedLock(key string, confs []*SingleNodeConf, opts ...RedLockOption) (*RedLock, error) {
	//3个以上的节点，红锁才有意义
	if len(confs) < 3 {
		return nil, errors.New("can not use redlock less than 3 nodes ")
	}

	r := RedLock{}
	for _, opt := range opts {
		opt(&r.RedLockOptions)
	}
	repairRedLock(&r.RedLockOptions)
	if r.expireDuration > 0 && time.Duration(len(confs))*r.singleNodesTimeout*10 > r.expireDuration {
		// 要求所有节点的累计超过的阈值要小于分布式锁过期时间的1/10
		return nil, errors.New("expire thresholds of single node is too long")
	}

	r.locks = make([]*RedisLock, 0, len(confs))
	for _, conf := range confs {
		client := NewClient(conf.Network, conf.Address, conf.Password, conf.Opts...)
		r.locks = append(r.locks, NewRedisLock(key, client, WithExpireSeconds(int64(r.expireDuration.Seconds()))))
	}

	return &r, nil
}

func (r *RedLock) Lock(ctx context.Context) error {
	// 计数
	var successCnt int
	for _, lock := range r.locks {
		startTime := time.Now()
		err := lock.Lock(ctx)
		cost := time.Since(startTime)
		if err == nil && cost <= r.singleNodesTimeout {
			successCnt++
		}
	}

	// 未超过半数，加锁失败
	if successCnt < len(r.locks)>>1+1 {
		return errors.New("lock failed")
	}

	return nil
}

// 解锁时,对所有节点广播解锁
func (r *RedLock) Unlock(ctx context.Context) error {
	var err error
	for _, lock := range r.locks {
		if _err := lock.Unlock(ctx); _err != nil {
			if err != nil {
				err = _err
			}
		}
	}

	return err
}
