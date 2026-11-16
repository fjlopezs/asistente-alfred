// SPDX-License-Identifier: MIT

package main

import (
	"context"
	"sync"
)

// lazy caches a value once it initializes successfully. Unlike sync.Once, a
// failure (e.g. an SSM timeout) is retried on the next call instead of
// breaking the execution environment until Lambda recycles it.
type lazy[T any] struct {
	mu sync.Mutex
	ok bool
	v  T
}

func (l *lazy[T]) get(ctx context.Context, init func(context.Context) (T, error)) (T, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.ok {
		return l.v, nil
	}
	v, err := init(ctx)
	if err != nil {
		var zero T
		return zero, err
	}
	l.v, l.ok = v, true
	return v, nil
}

// set replaces the cached value (tests).
func (l *lazy[T]) set(v T) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.v, l.ok = v, true
}
