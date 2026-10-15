// Команда prro-mcp — MCP-сервер для API PRRO.cloud: дає ІІ-асистентам
// доступ на читання до кас, змін, завдань, білінгу і вебхуків.
//
// Типово сервер працює через stdio з токеном зі змінної оточення. З
// прапорцем -http він стає віддаленим сервером (Streamable HTTP на /mcp):
// кожен клієнт передає свій токен у заголовку "Authorization: Bearer".
//
// Змінні оточення:
//
//	PRRO_TOKEN           машинний токен API (обов'язково для stdio, заборонено з -http)
//	PRRO_BASE_URL        адреса API замість https://api.prro.cloud
//	PRRO_LANG            мова помилок API: uk (типово) або en
//	PRRO_MCP_LOG_LEVEL   рівень журналу в stderr: debug, info (типово), warn, error
//	PRRO_MCP_RATE_LIMIT  з -http: запитів на хвилину з одним токеном (типово 120, 0 — без ліміту)
package main

import (
	"cmp"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"runtime/debug"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/prro-cloud/prro-go"

	"github.com/prro-cloud/prro-mcp/internal/clientsrc"
	"github.com/prro-cloud/prro-mcp/internal/httpserver"
	"github.com/prro-cloud/prro-mcp/internal/server"
)

// version задає збірка: -ldflags "-X main.version=0.1.0".
var version string

func main() {
	if err := run(os.Args[1:], os.Getenv, os.Stdout, os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, "prro-mcp:", err)
		os.Exit(1)
	}
}

func run(args []string, getenv func(string) string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("prro-mcp", flag.ContinueOnError)
	fs.SetOutput(stderr)
	showVersion := fs.Bool("version", false, "print the version and exit")
	httpAddr := fs.String("http", "", "serve Streamable HTTP on `addr` (e.g. :8080) instead of stdio")
	fs.Usage = func() {
		fmt.Fprint(stderr, usage)
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	v := buildVersion()
	if *showVersion {
		fmt.Fprintf(stdout, "prro-mcp %s (prro-go %s, API contract %s)\n", v, prro.Version, prro.APIVersion)
		return nil
	}

	remote := *httpAddr != ""
	cfg, err := loadConfig(getenv, remote)
	if err != nil {
		return err
	}
	// stdout зайнятий протоколом MCP, тож журнал — лише в stderr.
	logger := slog.New(slog.NewTextHandler(stderr, &slog.HandlerOptions{Level: cfg.logLevel}))
	clientOpts := []prro.Option{
		prro.WithBaseURL(cfg.baseURL),
		prro.WithLanguage(cfg.lang),
		prro.WithUserAgent("prro-mcp/" + v),
		prro.WithLogger(logger),
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if remote {
		// Без сесій go-sdk журналює підключення на кожен HTTP-запит.
		sdkLogger := slog.New(minLevel{logger.Handler(), slog.LevelWarn})
		srv := server.New(clientsrc.NewBearer(clientOpts...), server.Options{Version: v, Logger: logger, SDKLogger: sdkLogger})
		return serveHTTP(ctx, *httpAddr, srv, cfg, v, logger)
	}

	client := prro.NewClient(cfg.token, clientOpts...)
	srv := server.New(clientsrc.NewStatic(client), server.Options{Version: v, Logger: logger})
	logger.Info("prro-mcp started", "version", v, "transport", "stdio", "base_url", cmp.Or(cfg.baseURL, prro.DefaultBaseURL))
	// Не srv.Run: той пише ERROR на кожну зупинку сигналом, а це штатне
	// завершення, яке клієнт MCP робить при кожному перезапуску.
	ss, err := srv.Connect(ctx, &mcp.StdioTransport{}, nil)
	if err != nil {
		return err
	}
	stopped := context.AfterFunc(ctx, func() { _ = ss.Close() })
	defer stopped()
	if err := ss.Wait(); err != nil && ctx.Err() == nil {
		return err
	}
	logger.Info("prro-mcp stopped")
	return nil
}

// serveHTTP обслуговує srv як віддалений сервер на addr до скасування ctx.
func serveHTTP(ctx context.Context, addr string, srv *mcp.Server, cfg config, v string, logger *slog.Logger) error {
	ln, err := new(net.ListenConfig).Listen(ctx, "tcp", addr)
	if err != nil {
		return err
	}
	hs := &http.Server{
		Handler:           httpserver.Handler(srv, httpserver.Options{Logger: logger, RatePerMinute: cfg.ratePerMinute}),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		// Із запасом на найповільніший виклик: prro-go чекає на API до 60
		// секунд і повторює безпечні запити.
		WriteTimeout: 5 * time.Minute,
		IdleTimeout:  2 * time.Minute,
		ErrorLog:     slog.NewLogLogger(logger.Handler(), slog.LevelWarn),
	}
	logger.Info("prro-mcp started", "version", v, "transport", "http", "addr", ln.Addr().String(),
		"path", httpserver.Path, "rate_per_minute", cfg.ratePerMinute, "base_url", cmp.Or(cfg.baseURL, prro.DefaultBaseURL))

	errc := make(chan error, 1)
	go func() { errc <- hs.Serve(ln) }()
	select {
	case err := <-errc:
		return err
	case <-ctx.Done():
	}
	// Дати завершитися викликам, що вже йдуть; хто не встиг — обірвати.
	// Зупинка штатна, тож це не помилка процесу.
	sctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), shutdownTimeout)
	defer cancel()
	if err := hs.Shutdown(sctx); err != nil {
		logger.Warn("in-flight requests did not finish, closing them", "timeout", shutdownTimeout)
		_ = hs.Close()
	}
	logger.Info("prro-mcp stopped")
	return nil
}

// shutdownTimeout — скільки чекати на виклики, що вже йдуть, після SIGTERM.
// Менше за --stop-timeout 30 у прикладі docker run з README, щоб процес
// встиг завершитися сам до SIGKILL.
const shutdownTimeout = 25 * time.Second

// minLevel пропускає в Handler лише записи від рівня level.
type minLevel struct {
	slog.Handler
	level slog.Level
}

func (h minLevel) Enabled(ctx context.Context, l slog.Level) bool {
	return l >= h.level && h.Handler.Enabled(ctx, l)
}

func (h minLevel) WithAttrs(attrs []slog.Attr) slog.Handler {
	return minLevel{h.Handler.WithAttrs(attrs), h.level}
}

func (h minLevel) WithGroup(name string) slog.Handler {
	return minLevel{h.Handler.WithGroup(name), h.level}
}

const usage = `prro-mcp — read-only MCP server for the PRRO.cloud API.

By default it speaks stdio and uses the token from PRRO_TOKEN. With -http it
is a remote server (Streamable HTTP on /mcp, health check on /healthz): each
client passes its own token in the header "Authorization: Bearer <token>".

Environment:
  PRRO_TOKEN           machine API token (required for stdio, not allowed with -http)
  PRRO_BASE_URL        API base URL instead of https://api.prro.cloud
  PRRO_LANG            language of API error messages: uk (default) or en
  PRRO_MCP_LOG_LEVEL   stderr log level: debug, info (default), warn, error
  PRRO_MCP_RATE_LIMIT  with -http: requests per minute per token (default 120, 0 = unlimited)

Flags:
`

// defaultRatePerMinute — типовий ліміт запитів одного токена до віддаленого
// сервера. Підключення клієнта — кілька запитів (initialize, tools/list),
// далі по одному на виклик інструмента.
const defaultRatePerMinute = 120

// config — налаштування зі змінних оточення.
type config struct {
	token         string
	baseURL       string
	lang          string
	logLevel      slog.Level
	ratePerMinute int
}

// loadConfig читає налаштування; remote — сервер працює через HTTP.
func loadConfig(getenv func(string) string, remote bool) (config, error) {
	cfg := config{
		token:   strings.TrimSpace(getenv("PRRO_TOKEN")),
		baseURL: strings.TrimSpace(getenv("PRRO_BASE_URL")),
		lang:    strings.TrimSpace(getenv("PRRO_LANG")),
	}
	switch {
	case remote && cfg.token != "":
		// Інакше легко повірити, що сервер ходить в API з цим токеном.
		return config{}, errors.New("PRRO_TOKEN must not be set with -http: the remote server uses the token from each request's Authorization header")
	case !remote && cfg.token == "":
		return config{}, errors.New("PRRO_TOKEN is not set: issue a machine token in the PRRO.cloud cabinet and pass it in the MCP client config")
	}
	if remote {
		cfg.ratePerMinute = defaultRatePerMinute
		if s := strings.TrimSpace(getenv("PRRO_MCP_RATE_LIMIT")); s != "" {
			n, err := strconv.Atoi(s)
			if err != nil || n < 0 {
				return config{}, fmt.Errorf("PRRO_MCP_RATE_LIMIT=%q: want requests per minute per token, 0 for no limit", s)
			}
			cfg.ratePerMinute = n
		}
	}
	if cfg.baseURL != "" {
		u, err := url.Parse(cfg.baseURL)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
			return config{}, fmt.Errorf("PRRO_BASE_URL=%q: want an absolute http(s) URL such as http://localhost:8080", cfg.baseURL)
		}
	}
	switch cfg.lang = strings.ToLower(cfg.lang); cfg.lang {
	case "":
		cfg.lang = "uk"
	case "uk", "en":
	default:
		return config{}, fmt.Errorf("PRRO_LANG=%q: want uk or en", cfg.lang)
	}
	if lvl := strings.TrimSpace(getenv("PRRO_MCP_LOG_LEVEL")); lvl != "" {
		if err := cfg.logLevel.UnmarshalText([]byte(lvl)); err != nil {
			return config{}, fmt.Errorf("PRRO_MCP_LOG_LEVEL=%q: want debug, info, warn or error", lvl)
		}
	}
	return cfg, nil
}

// buildVersion — версія з -ldflags, інакше з go install (v0.1.0), інакше
// "dev".
func buildVersion() string {
	if version != "" {
		return strings.TrimPrefix(version, "v")
	}
	if bi, ok := debug.ReadBuildInfo(); ok && bi.Main.Version != "" && bi.Main.Version != "(devel)" {
		return strings.TrimPrefix(bi.Main.Version, "v")
	}
	return "dev"
}
