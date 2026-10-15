// Package httpserver — віддалений MCP-сервер: Streamable HTTP без сесій на
// [Path]. Клієнт передає машинний токен PRRO.cloud у заголовку
// "Authorization: Bearer" кожного запиту; сервер пересилає його в API і
// нічого не зберігає.
package httpserver

import (
	"cmp"
	"crypto/sha256"
	"log/slog"
	"math"
	"net/http"
	"strconv"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/prro-cloud/prro-mcp/internal/clientsrc"
)

// Path — шлях ендпоінта MCP.
const Path = "/mcp"

// HealthPath — перевірка живості для балансувальника чи оркестратора.
const HealthPath = "/healthz"

// Options — параметри HTTP-сервера.
type Options struct {
	// Logger — журнал; nil — без журналу.
	Logger *slog.Logger
	// RatePerMinute — скільки запитів на хвилину дозволено одному токену;
	// 0 — без обмеження.
	RatePerMinute int
}

// Handler повертає HTTP-обробник із MCP на [Path] і перевіркою живості на
// [HealthPath]. Клієнт API для інструментів srv має брати токен із
// заголовка запиту — через [clientsrc.Bearer].
func Handler(srv *mcp.Server, opts Options) http.Handler {
	logger := cmp.Or(opts.Logger, slog.New(slog.DiscardHandler))
	var h http.Handler = mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return srv }, &mcp.StreamableHTTPOptions{
		// Сесії не потрібні: інструменти лише читають, а сервер має
		// масштабуватися без спільного стану між екземплярами.
		Stateless: true,
		// Звичайна JSON-відповідь замість SSE: сповіщень під час виклику
		// інструменти не шлють, а проксі не треба вимикати буферизацію.
		JSONResponse: true,
		Logger:       logger,
		// Клієнт відключився — відповідь уже нікому не потрібна, тож
		// виклик API скасовується, а не повторюється даремно.
		PropagateRequestCancellation: true,
		// Захист від DNS rebinding відхиляє запити, що прийшли на loopback
		// із чужим Host, — тобто кожен запит від зворотного проксі на тій
		// самій машині. Тут він нічого не захищає: власних облікових даних
		// сервер не має, а токена сторонній сайт у браузері не знає.
		DisableLocalhostProtection: true,
	})
	if opts.RatePerMinute > 0 {
		h = rateLimit(h, newLimiter(opts.RatePerMinute, logger))
	}
	h = requireToken(h)

	mux := http.NewServeMux()
	mux.Handle(Path, h)
	mux.HandleFunc("GET "+HealthPath, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = w.Write([]byte("ok\n"))
	})
	return mux
}

// requireToken відхиляє запити без токена до того, як ними займеться MCP:
// без токена жоден інструмент не працює.
func requireToken(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, ok := clientsrc.BearerToken(r.Header); !ok {
			w.Header().Set("WWW-Authenticate", `Bearer realm="prro-mcp"`)
			http.Error(w, "Pass a PRRO.cloud machine API token in the header \"Authorization: Bearer <token>\". "+
				"Issue one in the PRRO.cloud cabinet: https://app.prro.cloud/", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// rateLimit обмежує частоту запитів кожного токена. Від потоку запитів з
// однієї адреси (зокрема з вигаданими токенами) він не захищає — для цього
// ліміт за IP на зворотному проксі.
func rateLimit(next http.Handler, l *limiter) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token, _ := clientsrc.BearerToken(r.Header)
		if wait, ok := l.allow(sha256.Sum256([]byte(token))); !ok {
			w.Header().Set("Retry-After", strconv.Itoa(int(math.Ceil(wait.Seconds()))))
			http.Error(w, "Too many requests with this token; retry later.", http.StatusTooManyRequests)
			return
		}
		next.ServeHTTP(w, r)
	})
}
