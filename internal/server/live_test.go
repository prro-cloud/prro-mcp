package server_test

import (
	"encoding/json"
	"log/slog"
	"os"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/prro-cloud/prro-go"

	"github.com/prro-cloud/prro-mcp/internal/clientsrc"
	"github.com/prro-cloud/prro-mcp/internal/server"
)

// TestLive проганяє всі інструменти проти справжнього API — локального
// стенда або тестового токена. Головне, що він перевіряє: реальні відповіді
// проходять вихідні схеми інструментів (збій схеми — помилка протоколу).
// Помилки API (403 для обмеженого токена, 404) лише журналюються.
//
//	PRRO_MCP_LIVE=1 PRRO_BASE_URL=http://localhost:8083 PRRO_TOKEN=… \
//	  go test -run TestLive -v ./internal/server/
//
// PRRO_TEST_TASK_ID — завдання для get_task; без нього get_task викликається
// з неіснуючим ідентифікатором і має повернути 404.
func TestLive(t *testing.T) {
	if os.Getenv("PRRO_MCP_LIVE") != "1" {
		t.Skip("set PRRO_MCP_LIVE=1, PRRO_TOKEN and PRRO_BASE_URL to run against a real API")
	}
	token := os.Getenv("PRRO_TOKEN")
	if token == "" {
		t.Fatal("PRRO_TOKEN is not set")
	}
	client := prro.NewClient(token, prro.WithBaseURL(os.Getenv("PRRO_BASE_URL")), prro.WithLanguage("en"))
	logger := slog.New(slog.NewTextHandler(t.Output(), &slog.HandlerOptions{Level: slog.LevelError}))
	s := server.New(clientsrc.NewStatic(client), server.Options{Version: "live-test", Logger: logger})

	st, ct := mcp.NewInMemoryTransports()
	ss, err := s.Connect(t.Context(), st, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer ss.Close()
	cs, err := mcp.NewClient(&mcp.Implementation{Name: "live", Version: "test"}, nil).Connect(t.Context(), ct, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer cs.Close()

	// call викликає інструмент і повертає structuredContent, якщо виклик
	// успішний; помилка протоколу валить тест.
	call := func(name string, args map[string]any) map[string]any {
		t.Helper()
		if args == nil {
			args = map[string]any{}
		}
		res, err := cs.CallTool(t.Context(), &mcp.CallToolParams{Name: name, Arguments: args})
		if err != nil {
			t.Errorf("%s: protocol error (output schema?): %v", name, err)
			return nil
		}
		summary := res.Content[0].(*mcp.TextContent).Text
		if res.IsError {
			t.Logf("%s → TOOL ERROR\n%s\n", name, summary)
			return nil
		}
		t.Logf("%s →\n%s\n", name, summary)
		var out map[string]any
		raw, _ := json.Marshal(res.StructuredContent)
		if err := json.Unmarshal(raw, &out); err != nil {
			t.Errorf("%s: structuredContent: %v", name, err)
		}
		return out
	}
	first := func(out map[string]any, list string) string {
		items, _ := out[list].([]any)
		if len(items) == 0 {
			return ""
		}
		id, _ := items[0].(map[string]any)["id"].(string)
		return id
	}

	call("get_version", nil)
	call("whoami", nil)
	call("get_dps_status", nil)

	if id := first(call("list_cash_registers", nil), "cash_registers"); id != "" {
		call("get_cash_register_state", map[string]any{"cash_register_id": id})
	} else {
		t.Log("no cash registers: get_cash_register_state skipped")
	}

	call("get_billing_usage", nil)
	call("list_invoices", map[string]any{"limit": 5})

	// Завдання для get_task: із PRRO_TEST_TASK_ID, інакше з даних доставки
	// task.*, інакше неіснуюче — тоді очікуваний результат 404.
	taskID := os.Getenv("PRRO_TEST_TASK_ID")
	if id := first(call("list_webhooks", nil), "webhooks"); id != "" {
		out := call("list_webhook_deliveries", map[string]any{"webhook_id": id, "limit": 20})
		deliveries, _ := out["deliveries"].([]any)
		for _, d := range deliveries {
			data, _ := d.(map[string]any)["data"].(map[string]any)
			if id, _ := data["task_id"].(string); taskID == "" && id != "" {
				taskID = id
			}
		}
	} else {
		t.Log("no webhooks: list_webhook_deliveries skipped")
	}
	if taskID == "" {
		taskID = "00000000-0000-7000-8000-000000000000"
	}
	call("get_task", map[string]any{"task_id": taskID})
}
