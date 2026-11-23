package ratelimit_test

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"hotelpms/api/internal/db/testdb"
	"hotelpms/api/internal/ratelimit"
)

var ctx = context.Background()

func TestTheLimitIsKeptInTheDatabaseSoEveryServerSharesIt(t *testing.T) {
	pool := testdb.New(t)
	now := time.Date(2026, 11, 23, 12, 0, 0, 0, time.UTC)
	for i := 0; i < 3; i++ {
		if !ratelimit.Allow(ctx, pool, "book:1.2.3.4", now, 3, time.Hour) {
			t.Fatalf("hit %d was refused", i+1)
		}
	}
	if ratelimit.Allow(ctx, pool, "book:1.2.3.4", now, 3, time.Hour) {
		t.Fatal("the fourth hit was allowed")
	}
	if !ratelimit.Allow(ctx, pool, "book:5.6.7.8", now, 3, time.Hour) {
		t.Fatal("another address shares the limit")
	}
	if !ratelimit.Allow(ctx, pool, "book:1.2.3.4", now.Add(time.Hour+time.Second), 3, time.Hour) {
		t.Fatal("the window did not slide")
	}
}

func TestRequestsAtTheSameMomentCannotAllPass(t *testing.T) {
	pool := testdb.New(t)
	now := time.Date(2026, 11, 23, 12, 0, 0, 0, time.UTC)
	var ok atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if ratelimit.Allow(ctx, pool, "pay:9.9.9.9", now, 5, time.Hour) {
				ok.Add(1)
			}
		}()
	}
	wg.Wait()
	if ok.Load() != 5 {
		t.Fatalf("%d allowed, want 5", ok.Load())
	}
}
