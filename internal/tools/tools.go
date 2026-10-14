// Package tools — інструменти MCP поверх prro-go: каси, зміни, завдання,
// білінг і вебхуки PRRO.cloud. Версія v0.1 лише читає: жоден інструмент не
// реєструє документів у ДПС і не змінює налаштувань.
//
// Описи інструментів і схем — англійською: їх читає модель.
package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/prro-cloud/prro-go"

	"github.com/prro-cloud/prro-mcp/internal/clientsrc"
)

// Options — параметри реєстрації інструментів.
type Options struct {
	// Version — версія prro-mcp; її повідомляє get_version.
	Version string
}

// Register додає до s усі інструменти v0.1. Клієнт API для кожного виклику
// дає src.
func Register(s *mcp.Server, src clientsrc.Source, opts Options) {
	registerSystem(s, src, opts)
	registerRegisters(s, src)
	registerTasks(s, src)
	registerBilling(s, src)
	registerWebhooks(s, src)
}

// Operations — operationId контракту API, який викликає кожен інструмент.
// Тести звіряють її з зареєстрованими інструментами і з
// internal/spec/openapi.yaml.
var Operations = map[string]string{
	"get_version":             "getVersion",
	"whoami":                  "whoami",
	"get_dps_status":          "systemDPSStatus",
	"list_cash_registers":     "listCashRegistersForToken",
	"get_cash_register_state": "getCashRegisterState",
	"get_task":                "getTask",
	"get_billing_usage":       "billingUsage",
	"list_invoices":           "listInvoices",
	"list_webhooks":           "listWebhooks",
	"list_webhook_deliveries": "listWebhookDeliveries",
}

// tool — опис одного інструмента: читає дані через prro-go і повертає їх
// структуровано разом із коротким текстовим зведенням.
type tool[In, Out any] struct {
	name        string
	title       string
	description string
	// input уточнює виведену з In схему аргументів: межі, переліки,
	// формати. nil — схема як є.
	input func(*jsonschema.Schema)
	// call виконує запит до API.
	call func(ctx context.Context, c *prro.Client, in In) (Out, error)
	// summary — кілька рядків для моделі; повні дані йдуть окремим JSON.
	summary func(Out) string
}

// add реєструє t на сервері s.
func add[In, Out any](s *mcp.Server, src clientsrc.Source, t tool[In, Out]) {
	in := mustSchema[In]()
	if t.input != nil {
		t.input(in)
	}
	mt := &mcp.Tool{
		Name:         t.name,
		Title:        t.title,
		Description:  t.description,
		InputSchema:  in,
		OutputSchema: mustSchema[Out](),
		Annotations: &mcp.ToolAnnotations{
			Title:          t.title,
			ReadOnlyHint:   true,
			IdempotentHint: true,
			OpenWorldHint:  new(true),
		},
	}
	mcp.AddTool(s, mt, func(ctx context.Context, req *mcp.CallToolRequest, in In) (*mcp.CallToolResult, Out, error) {
		var zero Out
		c, err := src.For(ctx, req)
		if err != nil {
			return nil, zero, err
		}
		out, err := t.call(ctx, c, in)
		if err != nil {
			return nil, zero, toolError(err)
		}
		// Структурований результат go-sdk заповнить сам з out. Content —
		// для клієнтів, які structuredContent моделі не показують: зведення
		// і ті самі дані JSON-ом.
		data, err := json.Marshal(out)
		if err != nil {
			return nil, zero, fmt.Errorf("encode result: %w", err)
		}
		return &mcp.CallToolResult{Content: []mcp.Content{
			&mcp.TextContent{Text: t.summary(out)},
			&mcp.TextContent{Text: string(data)},
		}}, out, nil
	})
}

// schemaTypes — типи, схему яких jsonschema-go виводить невірно.
var schemaTypes = map[reflect.Type]*jsonschema.Schema{
	// Довільний JSON (дані події вебхука), а не масив байтів.
	reflect.TypeFor[json.RawMessage](): {},
}

// mustSchema виводить JSON-схему типу T (вказівник — схема його елемента).
// Помилка тут — помилка програміста, її ловлять тести.
func mustSchema[T any]() *jsonschema.Schema {
	rt := reflect.TypeFor[T]()
	if rt.Kind() == reflect.Pointer {
		rt = rt.Elem()
	}
	s, err := jsonschema.ForType(rt, &jsonschema.ForOptions{TypeSchemas: schemaTypes})
	if err != nil {
		panic(fmt.Sprintf("tools: schema for %v: %v", rt, err))
	}
	return s
}

// requireID забороняє порожній ідентифікатор name: інакше виклик дійшов би
// до prro-go і повернувся б як збій API, а не як хибний аргумент.
func requireID(s *jsonschema.Schema, name string) {
	property(s, name).MinLength = new(1)
}

// property повертає схему властивості name; відсутня — помилка програміста.
func property(s *jsonschema.Schema, name string) *jsonschema.Schema {
	p, ok := s.Properties[name]
	if !ok {
		panic("tools: no property " + name)
	}
	return p
}
