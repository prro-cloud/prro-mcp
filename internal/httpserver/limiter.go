package httpserver

import (
	"encoding/hex"
	"log/slog"
	"sync"
	"time"

	"golang.org/x/time/rate"
)

// maxBuckets обмежує пам'ять під ліміти: близько 200 байт на токен, тож
// до 20 МБ. Чи справжній токен, знає лише API, тому потік запитів із
// вигаданими токенами інакше заповнював би таблицю без меж.
const maxBuckets = 100_000

// limiter — ліміт запитів на хвилину для кожного токена (ключ — SHA-256
// токена, щоб не тримати в пам'яті самі токени).
type limiter struct {
	perMinute int
	max       int
	logger    *slog.Logger
	now       func() time.Time

	mu       sync.Mutex
	buckets  map[[32]byte]*bucket
	swept    time.Time
	fullSeen time.Time
}

type bucket struct {
	lim      *rate.Limiter
	seen     time.Time
	rejected time.Time
}

func newLimiter(perMinute int, logger *slog.Logger) *limiter {
	return &limiter{
		perMinute: perMinute,
		max:       maxBuckets,
		logger:    logger,
		now:       time.Now,
		buckets:   make(map[[32]byte]*bucket),
	}
}

// allow витрачає один запит токена key. Якщо ліміт вичерпано, повертає
// false і час до наступного дозволеного запиту. Відмови журналюються раз
// на хвилину для кожного токена, щоб клієнт у циклі повторів не залив
// журнал.
func (l *limiter) allow(key [32]byte) (time.Duration, bool) {
	now := l.now()
	l.mu.Lock()
	defer l.mu.Unlock()
	l.sweep(now)
	b := l.buckets[key]
	if b == nil {
		if len(l.buckets) >= l.max {
			// Таблиця повна — скоріше за все, потік вигаданих токенів.
			// Пропускаємо без ліміту: відмова тут заблокувала б справжніх
			// нових клієнтів, а від потоку з однієї адреси захищає ліміт
			// за IP на зворотному проксі.
			if now.Sub(l.fullSeen) >= time.Minute {
				l.fullSeen = now
				l.logger.Warn("rate limiter table is full, new tokens pass unlimited", "tokens", len(l.buckets))
			}
			return 0, true
		}
		// Сплеск до perMinute запитів, далі — рівномірно perMinute на
		// хвилину.
		b = &bucket{lim: rate.NewLimiter(rate.Limit(float64(l.perMinute)/60), l.perMinute)}
		l.buckets[key] = b
	}
	b.seen = now
	r := b.lim.ReserveN(now, 1)
	wait := r.DelayFrom(now)
	if wait == 0 {
		return 0, true
	}
	r.CancelAt(now)
	if now.Sub(b.rejected) >= time.Minute {
		b.rejected = now
		// Відбиток токена — щоб знайти клієнта в журналі, не пишучи туди
		// сам токен.
		l.logger.Warn("rate limit exceeded", "token_sha256", hex.EncodeToString(key[:4]), "per_minute", l.perMinute)
	}
	return wait, false
}

// sweep раз на хвилину прибирає токени, що не з'являлися понад хвилину:
// за цей час їхній ліміт відновився повністю, тож новий такий самий.
func (l *limiter) sweep(now time.Time) {
	if now.Sub(l.swept) < time.Minute {
		return
	}
	for k, b := range l.buckets {
		if now.Sub(b.seen) > time.Minute {
			delete(l.buckets, k)
		}
	}
	l.swept = now
}
