// SPDX-License-Identifier: MIT

package main

import (
	"context"
	"errors"
	"testing"
)

func TestLazyRetriesAfterError(t *testing.T) {
	var l lazy[int]
	calls := 0
	init := func(context.Context) (int, error) {
		calls++
		if calls == 1 {
			return 0, errors.New("ssm timeout")
		}
		return 42, nil
	}
	if _, err := l.get(t.Context(), init); err == nil {
		t.Fatal("expected first call to fail")
	}
	for range 2 {
		if v, err := l.get(t.Context(), init); err != nil || v != 42 {
			t.Fatalf("got %d, %v", v, err)
		}
	}
	if calls != 2 {
		t.Fatalf("init ran %d times, want 2 (fail, then cached success)", calls)
	}
}
