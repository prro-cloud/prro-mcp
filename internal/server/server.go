// Package server збирає MCP-сервер prro-mcp: інструменти, ресурс із
// контрактом API і журнал викликів. Транспорт обирає cmd/prro-mcp.
package server

import (
	"cmp"
	"context"
	"log/slog"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/prro-cloud/prro-mcp/internal/clientsrc"
	"github.com/prro-cloud/prro-mcp/internal/spec"
	"github.com/prro-cloud/prro-mcp/internal/tools"
)

// Name — ім'я сервера в MCP і в реєстрі.
const Name = "prro-mcp"

// OpenAPIURI — ресурс із публічним контрактом API.
const OpenAPIURI = "prro://openapi.yaml"

// instructions — що модель має знати про сервер ще до першого виклику.
const instructions = `PRRO.cloud is a Ukrainian fiscalization service: it registers receipts of software cash registers (PRRO) with the DPS (State Tax Service) through a REST API.

This server is READ-ONLY. It shows the real state of the user's cash registers, shifts, fiscal tasks, billing and webhook deliveries so you can help write and debug an integration. It cannot issue receipts, open or close shifts, or change settings; tell the user to do that through their integration or the PRRO.cloud cabinet.

Start with whoami (which client and cash registers the token reaches) and list_cash_registers (cash register ids). A cash register id is a uuid, not the fiscal number.
A shift or task with testing=true produces test documents without fiscal effect.
Receipt and cash amounts are decimal strings in UAH; billing amounts are decimal strings in kopecks.
The resource prro://openapi.yaml is the public API contract (OpenAPI 3.1, descriptions in Ukrainian); read it when writing integration code.`

// Options — параметри сервера.
type Options struct {
	// Version — версія prro-mcp.
	Version string
	// Logger — журнал; nil — без журналу.
	Logger *slog.Logger
	// SDKLogger — журнал самого go-sdk (підключення сесій тощо); nil —
	// Logger.
	SDKLogger *slog.Logger
}

// New повертає сервер з усіма інструментами й ресурсом. Клієнт API для
// кожного виклику інструмента дає src.
func New(src clientsrc.Source, opts Options) *mcp.Server {
	logger := opts.Logger
	if logger == nil {
		logger = slog.New(slog.DiscardHandler)
	}
	s := mcp.NewServer(&mcp.Implementation{
		Name:        Name,
		Title:       "PRRO.cloud",
		Description: "Read-only access to PRRO.cloud cash registers, shifts, fiscal tasks, billing and webhooks.",
		Version:     opts.Version,
		WebsiteURL:  "https://prro.cloud",
	}, &mcp.ServerOptions{
		Instructions: instructions,
		Logger:       cmp.Or(opts.SDKLogger, logger),
	})
	tools.Register(s, src, tools.Options{Version: opts.Version})
	s.AddResource(&mcp.Resource{
		URI:         OpenAPIURI,
		Name:        "openapi.yaml",
		Title:       "PRRO.cloud public API contract",
		Description: "OpenAPI 3.1 contract of the PRRO.cloud machine API (descriptions in Ukrainian): endpoints, schemas, error codes. Read it when writing integration code.",
		MIMEType:    "application/yaml",
		Size:        int64(len(spec.OpenAPI)),
	}, func(_ context.Context, req *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
		return &mcp.ReadResourceResult{Contents: []*mcp.ResourceContents{{
			URI:      req.Params.URI,
			MIMEType: "application/yaml",
			Text:     string(spec.OpenAPI),
		}}}, nil
	})
	s.AddReceivingMiddleware(logCalls(logger))
	return s
}

// logCalls пише в журнал кожен виклик інструмента: ім'я, тривалість і чи
// завершився він помилкою. Аргументи й результати в журнал не потрапляють.
func logCalls(logger *slog.Logger) mcp.Middleware {
	return func(next mcp.MethodHandler) mcp.MethodHandler {
		return func(ctx context.Context, method string, req mcp.Request) (mcp.Result, error) {
			call, ok := req.(*mcp.CallToolRequest)
			if !ok {
				return next(ctx, method, req)
			}
			start := time.Now()
			res, err := next(ctx, method, req)
			attrs := []slog.Attr{
				slog.String("tool", call.Params.Name),
				slog.Duration("duration", time.Since(start)),
			}
			switch r, _ := res.(*mcp.CallToolResult); {
			case err != nil:
				logger.LogAttrs(ctx, slog.LevelError, "tool call failed", append(attrs, slog.Any("error", err))...)
			case r != nil && r.IsError:
				logger.LogAttrs(ctx, slog.LevelWarn, "tool returned error", append(attrs, slog.Any("error", r.GetError()))...)
			default:
				logger.LogAttrs(ctx, slog.LevelInfo, "tool call", attrs...)
			}
			return res, err
		}
	}
}
