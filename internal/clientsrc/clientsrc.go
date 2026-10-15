// Package clientsrc вирішує, яким [*prro.Client] обслуговувати запит MCP.
//
// Інструменти не тримають глобального клієнта, а щоразу питають [Source]:
// у stdio це один клієнт із PRRO_TOKEN, а віддалений сервер бере токен із
// заголовка Authorization конкретного HTTP-запиту. Тож новий транспорт
// додає лише новий Source, а інструменти не змінюються.
package clientsrc

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/prro-cloud/prro-go"
)

// Source повертає клієнт API для запиту req.
type Source interface {
	For(ctx context.Context, req mcp.Request) (*prro.Client, error)
}

// Static — один клієнт на весь процес: stdio-сервер із токеном зі змінної
// оточення.
type Static struct{ client *prro.Client }

// NewStatic повертає Source, що завжди віддає client.
func NewStatic(client *prro.Client) *Static { return &Static{client: client} }

// For повертає спільний клієнт.
func (s *Static) For(context.Context, mcp.Request) (*prro.Client, error) { return s.client, nil }

// ErrNoToken — у HTTP-запиті немає токена. HTTP-сервер відхиляє такі
// запити ще до MCP, тож ця помилка означає помилку в збиранні сервера.
var ErrNoToken = errors.New("no bearer token in the HTTP request")

// Bearer — віддалений сервер: клієнт із токена в заголовку Authorization
// поточного HTTP-запиту. Токен іде лише в API і ніде не зберігається.
type Bearer struct{ opts []prro.Option }

// NewBearer повертає Source, що створює клієнт з opts для токена кожного
// запиту.
func NewBearer(opts ...prro.Option) *Bearer { return &Bearer{opts: opts} }

// For повертає клієнт із токеном запиту req.
func (b *Bearer) For(_ context.Context, req mcp.Request) (*prro.Client, error) {
	extra := req.GetExtra()
	if extra == nil {
		return nil, ErrNoToken
	}
	token, ok := BearerToken(extra.Header)
	if !ok {
		return nil, ErrNoToken
	}
	return prro.NewClient(token, b.opts...), nil
}

// BearerToken повертає токен із заголовка "Authorization: Bearer <токен>".
func BearerToken(h http.Header) (string, bool) {
	scheme, token, ok := strings.Cut(h.Get("Authorization"), " ")
	if !ok || !strings.EqualFold(scheme, "Bearer") {
		return "", false
	}
	token = strings.TrimSpace(token)
	return token, token != ""
}
