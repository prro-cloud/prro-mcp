// Команда prro-mcp — MCP-сервер для API PRRO.cloud: дає ІІ-асистентам
// доступ на читання до кас, змін, завдань, білінгу і вебхуків.
//
// Сервер працює через stdio і налаштовується змінними оточення:
//
//	PRRO_TOKEN          машинний токен API (обов'язково)
//	PRRO_BASE_URL       адреса API замість https://api.prro.cloud
//	PRRO_LANG           мова помилок API: uk (типово) або en
//	PRRO_MCP_LOG_LEVEL  рівень журналу в stderr: debug, info (типово), warn, error
package main

import (
	"cmp"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net/url"
	"os"
	"os/signal"
	"runtime/debug"
	"strings"
	"syscall"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/prro-cloud/prro-go"

	"github.com/prro-cloud/prro-mcp/internal/clientsrc"
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

	cfg, err := loadConfig(getenv)
	if err != nil {
		return err
	}
	// stdout зайнятий протоколом MCP, тож журнал — лише в stderr.
	logger := slog.New(slog.NewTextHandler(stderr, &slog.HandlerOptions{Level: cfg.logLevel}))

	client := prro.NewClient(cfg.token,
		prro.WithBaseURL(cfg.baseURL),
		prro.WithLanguage(cfg.lang),
		prro.WithUserAgent("prro-mcp/"+v),
		prro.WithLogger(logger),
	)
	srv := server.New(clientsrc.NewStatic(client), server.Options{Version: v, Logger: logger})

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
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

const usage = `prro-mcp — read-only MCP server for the PRRO.cloud API (stdio).

Environment:
  PRRO_TOKEN          machine API token (required)
  PRRO_BASE_URL       API base URL instead of https://api.prro.cloud
  PRRO_LANG           language of API error messages: uk (default) or en
  PRRO_MCP_LOG_LEVEL  stderr log level: debug, info (default), warn, error

Flags:
`

// config — налаштування зі змінних оточення.
type config struct {
	token    string
	baseURL  string
	lang     string
	logLevel slog.Level
}

func loadConfig(getenv func(string) string) (config, error) {
	cfg := config{
		token:   strings.TrimSpace(getenv("PRRO_TOKEN")),
		baseURL: strings.TrimSpace(getenv("PRRO_BASE_URL")),
		lang:    strings.TrimSpace(getenv("PRRO_LANG")),
	}
	if cfg.token == "" {
		return config{}, errors.New("PRRO_TOKEN is not set: issue a machine token in the PRRO.cloud cabinet and pass it in the MCP client config")
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
