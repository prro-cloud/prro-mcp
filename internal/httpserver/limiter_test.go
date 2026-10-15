package httpserver

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"
	"time"
)

func newTestLimiter(perMinute int) (*limiter, *bytes.Buffer, *time.Time) {
	var log bytes.Buffer
	now := time.Date(2026, 10, 15, 12, 0, 0, 0, time.UTC)
	l := newLimiter(perMinute, slog.New(slog.NewTextHandler(&log, nil)))
	l.now = func() time.Time { return now }
	return l, &log, &now
}

func TestLimiter(t *testing.T) {
	l, _, now := newTestLimiter(60)
	a, b := [32]byte{1}, [32]byte{2}

	for i := range 60 {
		if _, ok := l.allow(a); !ok {
			t.Fatalf("request %d rejected within the burst", i+1)
		}
	}
	wait, ok := l.allow(a)
	if ok || wait <= 0 || wait > time.Second {
		t.Errorf("61st request: ok=%v wait=%v, want rejected for up to 1s", ok, wait)
	}
	if _, ok := l.allow(b); !ok {
		t.Error("another token must have its own limit")
	}

	*now = now.Add(time.Second)
	if _, ok := l.allow(a); !ok {
		t.Error("one request per second must be refilled")
	}

	*now = now.Add(2 * time.Minute)
	l.allow(b)
	if _, ok := l.buckets[a]; ok {
		t.Error("an idle token must be swept")
	}
}

func TestLimiterLogsRejectionsOncePerMinute(t *testing.T) {
	l, log, now := newTestLimiter(1)
	key := [32]byte{1}
	l.allow(key)
	for range 5 {
		l.allow(key)
	}
	if n := strings.Count(log.String(), "rate limit exceeded"); n != 1 {
		t.Errorf("logged %d rejections in a burst, want 1", n)
	}
	*now = now.Add(61 * time.Second)
	l.allow(key)
	l.allow(key)
	if n := strings.Count(log.String(), "rate limit exceeded"); n != 2 {
		t.Errorf("logged %d rejections after a minute, want 2", n)
	}
}

func TestLimiterFullTable(t *testing.T) {
	l, log, _ := newTestLimiter(1)
	l.max = 2
	l.allow([32]byte{1})
	l.allow([32]byte{2})
	for range 3 {
		if _, ok := l.allow([32]byte{3}); !ok {
			t.Error("a new token must pass while the table is full")
		}
	}
	if len(l.buckets) != 2 {
		t.Errorf("table has %d tokens, want at most 2", len(l.buckets))
	}
	if n := strings.Count(log.String(), "table is full"); n != 1 {
		t.Errorf("logged a full table %d times, want 1", n)
	}
	// Відомі токени лімітуються й далі.
	if _, ok := l.allow([32]byte{1}); ok {
		t.Error("a known token must stay limited")
	}
}
