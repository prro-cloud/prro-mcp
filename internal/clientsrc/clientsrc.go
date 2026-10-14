// Package clientsrc вирішує, яким [*prro.Client] обслуговувати запит MCP.
//
// Інструменти не тримають глобального клієнта, а щоразу питають [Source]:
// у stdio це один клієнт із PRRO_TOKEN, а віддалений сервер братиме токен
// із заголовка Authorization конкретного HTTP-запиту. Тож новий транспорт
// додає лише новий Source, а інструменти не змінюються.
package clientsrc

import (
	"context"

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
