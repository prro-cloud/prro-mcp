package tools_test

import (
	"context"
	"encoding/json"
	"maps"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/prro-cloud/prro-go"

	"github.com/prro-cloud/prro-mcp/internal/clientsrc"
	"github.com/prro-cloud/prro-mcp/internal/tools"
)

const testToken = "test-token"

// fakeAPI — фейковий API PRRO.cloud: відповідає заданим тілом на маршрут
// операції з контракту і запам'ятовує останній запит.
type fakeAPI struct {
	mux *http.ServeMux

	mu   sync.Mutex
	last *http.Request
}

func (f *fakeAPI) lastRequest() *http.Request {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.last
}

// handle відповідає status і body на маршрут операції op.
func (f *fakeAPI) handle(t *testing.T, op string, status int, body string) {
	t.Helper()
	route, ok := operations(t)[op]
	if !ok {
		t.Fatalf("operation %s is not in the contract", op)
	}
	f.mux.HandleFunc(route, func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		f.last = r
		f.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	})
}

// newSession запускає фейковий API і MCP-сервер з інструментами та
// повертає підключену до нього клієнтську сесію.
func newSession(t *testing.T) (*mcp.ClientSession, *fakeAPI) {
	t.Helper()
	api := &fakeAPI{mux: http.NewServeMux()}
	srv := httptest.NewServer(api.mux)
	t.Cleanup(srv.Close)
	return connect(t, prro.NewClient(testToken, prro.WithBaseURL(srv.URL), prro.WithRetry(0))), api
}

func connect(t *testing.T, client *prro.Client) *mcp.ClientSession {
	t.Helper()
	s := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "test"}, nil)
	tools.Register(s, clientsrc.NewStatic(client), tools.Options{Version: "1.2.3"})

	ctx := t.Context()
	st, ct := mcp.NewInMemoryTransports()
	ss, err := s.Connect(ctx, st, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ss.Close() })
	cs, err := mcp.NewClient(&mcp.Implementation{Name: "client", Version: "test"}, nil).Connect(ctx, ct, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cs.Close() })
	return cs
}

// callTool викликає інструмент; помилка протоколу валить тест, помилка
// інструмента повертається в результаті.
func callTool(t *testing.T, cs *mcp.ClientSession, name string, args map[string]any) *mcp.CallToolResult {
	t.Helper()
	if args == nil {
		args = map[string]any{}
	}
	res, err := cs.CallTool(t.Context(), &mcp.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	return res
}

// text склеює текстові блоки результату.
func text(res *mcp.CallToolResult) string {
	var parts []string
	for _, c := range res.Content {
		if tc, ok := c.(*mcp.TextContent); ok {
			parts = append(parts, tc.Text)
		}
	}
	return strings.Join(parts, "\n")
}

// structured декодує structuredContent результату в T.
func structured[T any](t *testing.T, res *mcp.CallToolResult) T {
	t.Helper()
	var out T
	raw, err := json.Marshal(res.StructuredContent)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("structuredContent %s: %v", raw, err)
	}
	return out
}

func mustOK(t *testing.T, res *mcp.CallToolResult) {
	t.Helper()
	if res.IsError {
		t.Fatalf("tool error: %s", text(res))
	}
	if res.StructuredContent == nil {
		t.Fatal("no structuredContent")
	}
	if len(res.Content) != 2 {
		t.Fatalf("content blocks = %d, want summary and JSON", len(res.Content))
	}
	// Другий блок — ті самі дані JSON-ом, що й structuredContent.
	var fromText, fromStruct any
	if err := json.Unmarshal([]byte(res.Content[1].(*mcp.TextContent).Text), &fromText); err != nil {
		t.Fatalf("JSON block: %v", err)
	}
	raw, _ := json.Marshal(res.StructuredContent)
	_ = json.Unmarshal(raw, &fromStruct)
	if !jsonEqual(fromText, fromStruct) {
		t.Errorf("JSON block differs from structuredContent")
	}
}

func jsonEqual(a, b any) bool {
	ja, _ := json.Marshal(a)
	jb, _ := json.Marshal(b)
	return string(ja) == string(jb)
}

func wantContains(t *testing.T, s string, subs ...string) {
	t.Helper()
	for _, sub := range subs {
		if !strings.Contains(s, sub) {
			t.Errorf("missing %q in:\n%s", sub, s)
		}
	}
}

func TestListTools(t *testing.T) {
	cs, _ := newSession(t)
	res, err := cs.ListTools(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, tl := range res.Tools {
		names = append(names, tl.Name)
		a := tl.Annotations
		if a == nil || !a.ReadOnlyHint || !a.IdempotentHint || a.OpenWorldHint == nil || !*a.OpenWorldHint {
			t.Errorf("%s: annotations %+v, want read-only, idempotent, open-world", tl.Name, a)
		}
		if a != nil && a.Title == "" {
			t.Errorf("%s: no title", tl.Name)
		}
		if len(tl.Description) < 80 {
			t.Errorf("%s: description too short: %q", tl.Name, tl.Description)
		}
		if tl.OutputSchema == nil {
			t.Errorf("%s: no output schema", tl.Name)
		}
	}
	slices.Sort(names)
	if want := slices.Sorted(maps.Keys(tools.Operations)); !slices.Equal(names, want) {
		t.Errorf("tools = %v, want %v", names, want)
	}
}

func TestGetVersion(t *testing.T) {
	cs, api := newSession(t)
	api.handle(t, "getVersion", 200, `{"version":"v0.3.0"}`)

	res := callTool(t, cs, "get_version", nil)
	mustOK(t, res)
	got := structured[tools.VersionOutput](t, res)
	want := tools.VersionOutput{Service: "v0.3.0", APIContract: prro.APIVersion, MCPServer: "1.2.3", PRROGo: prro.Version}
	if got != want {
		t.Errorf("got %+v, want %+v", got, want)
	}
	wantContains(t, text(res), "v0.3.0", "prro-mcp 1.2.3")
}

func TestRequestHeaders(t *testing.T) {
	cs, api := newSession(t)
	api.handle(t, "getVersion", 200, `{"version":"v0.3.0"}`)
	mustOK(t, callTool(t, cs, "get_version", nil))

	r := api.lastRequest()
	if got := r.Header.Get("Authorization"); got != "Bearer "+testToken {
		t.Errorf("Authorization = %q", got)
	}
	if got := r.Header.Get("User-Agent"); !strings.Contains(got, "prro-go/") {
		t.Errorf("User-Agent = %q", got)
	}
}

func TestWhoAmI(t *testing.T) {
	cs, api := newSession(t)
	api.handle(t, "whoami", 200, `{"client_id":"c-1","scopes":["fiscal","billing"],"all_cash_registers":false,
		"cash_registers":["r-1","r-2"],"jti":"tok-9","expires_at":"2027-01-01T00:00:00Z"}`)

	res := callTool(t, cs, "whoami", nil)
	mustOK(t, res)
	id := structured[prro.Identity](t, res)
	if id.ClientID != "c-1" || id.TokenID != "tok-9" || len(id.CashRegisters) != 2 || id.ExpiresAt.Year() != 2027 {
		t.Errorf("identity = %+v", id)
	}
	wantContains(t, text(res), "client c-1", "restricted to 2 cash register(s): r-1, r-2", "2027-01-01T00:00:00Z")
}

func TestWhoAmIUnrestricted(t *testing.T) {
	cs, api := newSession(t)
	api.handle(t, "whoami", 200, `{"client_id":"c-1","scopes":["fiscal"],"all_cash_registers":true,"jti":"tok-9"}`)

	res := callTool(t, cs, "whoami", nil)
	mustOK(t, res)
	wantContains(t, text(res), "all cash registers", "Expires: never")
}

func TestGetDPSStatus(t *testing.T) {
	cs, api := newSession(t)
	api.handle(t, "systemDPSStatus", 200, `{"effective":"online","verdict":"degraded","since":"2026-10-14T08:00:00Z",
		"latency_ms":2100,"samples":40,"failures":1,"last_ok_at":"2026-10-14T09:59:00Z",
		"tunables":{"window_seconds":300,"degraded_latency_ms":1500,"probe_interval_seconds":30}}`)

	res := callTool(t, cs, "get_dps_status", nil)
	mustOK(t, res)
	if st := structured[prro.DPSStatus](t, res); st.Verdict != prro.DPSDegraded || st.Tunables.DegradedLatencyMS != 1500 {
		t.Errorf("status = %+v", st)
	}
	wantContains(t, text(res), "DPS mode: online (detector verdict: degraded)", "2100 ms over 40")
}

func TestListCashRegisters(t *testing.T) {
	cs, api := newSession(t)
	api.handle(t, "listCashRegistersForToken", 200, `{"cash_registers":[
		{"id":"r-1","fiscal_number":"4000123456","local_number":"1","org_name":"ФОП Іваненко","point_name":"Кав'ярня",
		 "point_address":"Київ, Хрещатик 1","mode":"test","state":"opened"},
		{"id":"r-2","fiscal_number":"4000123457","local_number":"2","mode":"production","state":"closed"}]}`)

	res := callTool(t, cs, "list_cash_registers", nil)
	mustOK(t, res)
	out := structured[tools.CashRegistersOutput](t, res)
	if len(out.CashRegisters) != 2 || out.CashRegisters[0].Mode != prro.RegisterTest {
		t.Errorf("registers = %+v", out)
	}
	wantContains(t, text(res), "2 cash register(s)", "r-1: fiscal number 4000123456", "mode test, state opened", "Кав'ярня")
}

func TestListCashRegistersEmpty(t *testing.T) {
	cs, api := newSession(t)
	api.handle(t, "listCashRegistersForToken", 200, `{"cash_registers":null}`)

	res := callTool(t, cs, "list_cash_registers", nil)
	mustOK(t, res)
	if !strings.Contains(text(res), `"cash_registers":[]`) {
		t.Errorf("want an empty list, not null: %s", text(res))
	}
}

func TestGetCashRegisterState(t *testing.T) {
	cs, api := newSession(t)
	api.handle(t, "getCashRegisterState", 200, `{"id":"r-1","fiscal_number":"4000123456","local_number":"1",
		"mode":"test","state":"offline","offline_ready":true,"shift_mode":"round_the_clock","shift_close_times":["23:00"],
		"cash_balance":{"amount":"1250.50","as_of":"2026-10-14T09:00:00Z"},
		"current_shift":{"id":"s-1","number":147,"status":"opened","testing":true,"opened_at":"2026-10-14T06:00:00Z","documents":12},
		"shift_totals":{"realiz":{"count":10,"sum":"2500.00","pay_forms":[{"code":0,"name":"ГОТІВКА","sum":"2500.00"}]},
		  "return":{"count":1,"sum":"100.00"},"service_in":"500.00","service_out":"0.00"},
		"offline_session":{"id":"o-1","dps_session_id":77,"started_at":"2026-10-14T08:00:00Z","documents":3},
		"offline_limits":{"session_seconds":129600,"month_seconds":604800,"enforced":true},"offline_month_used_seconds":3600,
		"attention":{"reason":"offline_submission_paused","since":"2026-10-14T09:30:00Z"}}`)

	res := callTool(t, cs, "get_cash_register_state", map[string]any{"cash_register_id": "r-1"})
	mustOK(t, res)
	if got := api.lastRequest().URL.Path; got != "/v1/cash-registers/r-1" {
		t.Errorf("path = %s", got)
	}
	st := structured[prro.CashRegisterState](t, res)
	if st.CurrentShift == nil || !st.CurrentShift.Testing || st.ShiftTotals.Sales.Sum != "2500.00" {
		t.Errorf("state = %+v", st)
	}
	wantContains(t, text(res), "Shift #147 opened since 2026-10-14T06:00:00Z, 12 document(s)", "TESTING shift", "10 sale(s) for 2500.00 UAH",
		"Cash balance: 1250.50 UAH", "Offline session 77", "NEEDS ATTENTION", "offline_submission_paused", "23:00 Kyiv")
}

func TestGetCashRegisterStateClosed(t *testing.T) {
	cs, api := newSession(t)
	api.handle(t, "getCashRegisterState", 200, `{"id":"r-1","fiscal_number":"4000123456","local_number":"1",
		"mode":"production","state":"closed","cash_balance":{"amount":"0.00"},
		"offline_limits":{"session_seconds":129600,"month_seconds":604800,"enforced":true}}`)

	res := callTool(t, cs, "get_cash_register_state", map[string]any{"cash_register_id": "r-1"})
	mustOK(t, res)
	wantContains(t, text(res), "No open shift", "no cash movements seen yet")
	if strings.Contains(text(res), "lifted") {
		t.Errorf("enforced limits reported as lifted: %s", text(res))
	}
}

func TestGetCashRegisterStateOfflinePaused(t *testing.T) {
	cs, api := newSession(t)
	api.handle(t, "getCashRegisterState", 200, `{"id":"r-1","fiscal_number":"4000123456","local_number":"1",
		"mode":"production","state":"offline","cash_balance":{"amount":"0.00"},
		"offline_session":{"id":"o-1","dps_session_id":77,"started_at":"2026-10-14T08:00:00Z","documents":3,
		  "submit_paused_at":"2026-10-14T09:30:00Z",
		  "submit_fault":{"code":"dps.offline_package_rejected","class":"user_action","message":"Пакет відхилено","upstream_message":"Помилка підпису"}},
		"offline_limits":{"session_seconds":129600,"month_seconds":604800,"enforced":false}}`)

	res := callTool(t, cs, "get_cash_register_state", map[string]any{"cash_register_id": "r-1"})
	mustOK(t, res)
	st := structured[prro.CashRegisterState](t, res)
	if st.OfflineSession == nil || st.OfflineSession.SubmitFault == nil || st.OfflineLimits.Enforced {
		t.Errorf("state = %+v", st)
	}
	wantContains(t, text(res), "Submission to the DPS PAUSED since 2026-10-14T09:30:00Z",
		"fault dps.offline_package_rejected (class user_action), Пакет відхилено", "DPS said: Помилка підпису",
		"Offline limits are lifted")
}

func TestGetCashRegisterStateLimitsRestored(t *testing.T) {
	cs, api := newSession(t)
	api.handle(t, "getCashRegisterState", 200, `{"id":"r-1","mode":"production","state":"closed",
		"offline_limits":{"session_seconds":129600,"month_seconds":604800,"enforced":true,"restored_at":"2026-10-20T00:00:00Z"}}`)

	res := callTool(t, cs, "get_cash_register_state", map[string]any{"cash_register_id": "r-1"})
	mustOK(t, res)
	wantContains(t, text(res), "Offline limits were restored at 2026-10-20T00:00:00Z")
	if strings.Contains(text(res), "lifted") {
		t.Errorf("enforced limits reported as lifted: %s", text(res))
	}
}

func TestGetTaskSucceeded(t *testing.T) {
	cs, api := newSession(t)
	api.handle(t, "getTask", 200, `{"task_id":"t-1","type":"receipt","status":"succeeded",
		"created_at":"2026-10-14T09:00:00Z","finished_at":"2026-10-14T09:00:01Z",
		"result":{"prro_id":"r-1","shift_id":"s-1","shift_number":147,"testing":true,
		  "receipt":{"document_id":"d-1","local_number":512,"fiscal_number":"123456789","register_fiscal_number":"4000123456",
		  "receipt_url":"https://r.prro.cloud/abc","tax_url":"https://cabinet.tax.gov.ua/x"}}}`)

	res := callTool(t, cs, "get_task", map[string]any{"task_id": "t-1"})
	mustOK(t, res)
	if got := api.lastRequest().URL.Path; got != "/v1/tasks/t-1" {
		t.Errorf("path = %s", got)
	}
	task := structured[prro.Task](t, res)
	if task.Result == nil || task.Result.Receipt == nil || task.Result.Receipt.FiscalNumber != "123456789" {
		t.Errorf("task = %+v", task)
	}
	wantContains(t, text(res), "Task t-1 (receipt): succeeded", "TEST documents", "Receipt: fiscal number 123456789, local number 512", "https://r.prro.cloud/abc")
}

func TestGetTaskCloseShiftTotals(t *testing.T) {
	cs, api := newSession(t)
	api.handle(t, "getTask", 200, `{"task_id":"t-3","type":"close_shift","status":"succeeded",
		"result":{"prro_id":"r-1","shift_id":"s-1","shift_number":147,
		  "zreport":{"document_id":"d-2","local_number":520,"fiscal_number":"123456790","register_fiscal_number":"4000123456",
		    "totals":{"realiz":{"count":10,"sum":"2500.00","pay_forms":[{"code":0,"name":"Готівка","sum":"2500.00"}],
		      "taxes":[{"letter":"А","name":"ПДВ","rate":"20.00","turnover":"2500.00","sum":"416.67"}]},
		      "return":{"count":0,"sum":"0.00"},"service_in":"500.00","service_out":"0.00"}}}}`)

	res := callTool(t, cs, "get_task", map[string]any{"task_id": "t-3"})
	mustOK(t, res)
	task := structured[prro.Task](t, res)
	if z := task.Result.ZReport; z == nil || z.Totals == nil || len(z.Totals.Sales.Taxes) != 1 || z.Totals.Sales.Taxes[0].Sum != "416.67" {
		t.Errorf("zreport = %+v", task.Result.ZReport)
	}
	wantContains(t, text(res), "Z report: fiscal number 123456790",
		"Z report totals: 10 sale(s) for 2500.00 UAH, 0 return(s) for 0.00 UAH, cash in 500.00, cash out 0.00.")
}

// Завдання, що завершилося помилкою, — дані, а не помилка інструмента.
func TestGetTaskFailed(t *testing.T) {
	cs, api := newSession(t)
	api.handle(t, "getTask", 200, `{"task_id":"t-2","type":"receipt","status":"failed","created_at":"2026-10-14T09:00:00Z",
		"error":"shift is not opened","fault":{"code":"state.shift_not_opened","class":"precursor",
		"message":"Зміну не відкрито","upstream_message":"Зміна не відкрита"}}`)

	res := callTool(t, cs, "get_task", map[string]any{"task_id": "t-2"})
	mustOK(t, res)
	wantContains(t, text(res), "failed", "Fault state.shift_not_opened (class precursor): Зміну не відкрито", "DPS said: Зміна не відкрита")
}

func TestGetBillingUsage(t *testing.T) {
	cs, api := newSession(t)
	api.handle(t, "billingUsage", 200, `{"balance":"15000.0000","soft_limit":"0.0000","blocked":false,
		"period":{"from":"2026-09-01","to":"2026-10-01"},"receipts":120,"charged":"18000.0000",
		"tariff":{"plan_id":"standard","price_per_receipt":"150.0000"}}`)

	res := callTool(t, cs, "get_billing_usage", map[string]any{"from": "2026-09-01", "to": "2026-10-01"})
	mustOK(t, res)
	if q := api.lastRequest().URL.Query(); q.Get("from") != "2026-09-01" || q.Get("to") != "2026-10-01" {
		t.Errorf("query = %v", q)
	}
	if u := structured[prro.BillingUsage](t, res); u.Balance != "15000.0000" || u.Receipts != 120 {
		t.Errorf("usage = %+v", u)
	}
	wantContains(t, text(res), "Balance 150.00 UAH", "receipts are accepted", "120 receipt(s), charged 180.00 UAH", "1.50 UAH per receipt")
}

func TestGetBillingUsageDefaultPeriod(t *testing.T) {
	cs, api := newSession(t)
	api.handle(t, "billingUsage", 200, `{"balance":"-500.0000","soft_limit":"0.0000","blocked":true,
		"period":{"from":"2026-10-01","to":"2026-11-01"},"receipts":0,"charged":"0.0000",
		"tariff":{"plan_id":"standard","price_per_receipt":"150.0000"}}`)

	res := callTool(t, cs, "get_billing_usage", nil)
	mustOK(t, res)
	if q := api.lastRequest().URL.RawQuery; q != "" {
		t.Errorf("query = %q, want none", q)
	}
	wantContains(t, text(res), "Balance -5.00 UAH", "BLOCKED")
}

func TestListInvoices(t *testing.T) {
	cs, api := newSession(t)
	api.handle(t, "listInvoices", 200, `{"invoices":[{"id":"i-1","period_start":"2026-09-01","period_end":"2026-09-30",
		"amount":"18000.0000","currency":"UAH","status":"issued","created_at":"2026-10-01T00:05:00Z"}]}`)

	res := callTool(t, cs, "list_invoices", map[string]any{"limit": 10, "offset": 20})
	mustOK(t, res)
	if q := api.lastRequest().URL.Query(); q.Get("limit") != "10" || q.Get("offset") != "20" {
		t.Errorf("query = %v", q)
	}
	if out := structured[tools.InvoicesOutput](t, res); len(out.Invoices) != 1 || out.Invoices[0].Status != prro.InvoiceIssued {
		t.Errorf("invoices = %+v", out)
	}
	wantContains(t, text(res), "2026-09-01 to 2026-09-30: 180.00 UAH, issued")
}

func TestListWebhooks(t *testing.T) {
	cs, api := newSession(t)
	api.handle(t, "listWebhooks", 200, `{"limit":3,"used":2,"webhooks":[
		{"id":"w-1","url":"https://shop.example/hook","events":["task.completed","task.failed"],"active":true,
		 "created_at":"2026-10-01T00:00:00Z","updated_at":"2026-10-01T00:00:00Z"},
		{"id":"w-2","url":"https://shop.example/all","events":[],"active":false,
		 "created_at":"2026-10-01T00:00:00Z","updated_at":"2026-10-02T00:00:00Z"}]}`)

	res := callTool(t, cs, "list_webhooks", nil)
	mustOK(t, res)
	if l := structured[prro.WebhookList](t, res); l.Limit != 3 || len(l.Webhooks) != 2 {
		t.Errorf("list = %+v", l)
	}
	wantContains(t, text(res), "2 of 3 webhook(s) used", "w-1 https://shop.example/hook (active): task.completed, task.failed",
		"w-2 https://shop.example/all (PAUSED): all events")
}

func TestListWebhookDeliveries(t *testing.T) {
	cs, api := newSession(t)
	api.handle(t, "listWebhookDeliveries", 200, `{"deliveries":[{"id":"d-1","event":"task.failed","status":"dead",
		"attempts":8,"cash_register_id":"r-1","sequence":42,"data":{"task_id":"t-2","fault":{"code":"dps.timeout"}},
		"last_error":"HTTP 500","created_at":"2026-10-13T09:00:00Z"}]}`)

	res := callTool(t, cs, "list_webhook_deliveries", map[string]any{"webhook_id": "w-1", "status": "dead", "limit": 5})
	mustOK(t, res)
	r := api.lastRequest()
	if r.URL.Path != "/v1/webhooks/w-1/deliveries" || r.URL.Query().Get("status") != "dead" || r.URL.Query().Get("limit") != "5" {
		t.Errorf("request = %s", r.URL)
	}
	out := structured[tools.DeliveriesOutput](t, res)
	if len(out.Deliveries) != 1 || !strings.Contains(string(out.Deliveries[0].Data), `"dps.timeout"`) {
		t.Errorf("deliveries = %+v", out)
	}
	wantContains(t, text(res), "d-1 task.failed: dead after 8 attempt(s)", "last error: HTTP 500")
}

func TestInputValidation(t *testing.T) {
	cs, _ := newSession(t)
	for _, tc := range []struct {
		tool string
		args map[string]any
	}{
		{"get_task", nil},
		{"get_task", map[string]any{"task_id": ""}},
		{"get_cash_register_state", map[string]any{}},
		{"get_cash_register_state", map[string]any{"cash_register_id": ""}},
		{"list_webhook_deliveries", map[string]any{"webhook_id": ""}},
		{"list_invoices", map[string]any{"limit": 500}},
		{"list_invoices", map[string]any{"offset": -1}},
		{"list_webhook_deliveries", map[string]any{"webhook_id": "w-1", "status": "lost"}},
		{"list_webhook_deliveries", map[string]any{"webhook_id": "w-1", "limit": 0}},
		{"whoami", map[string]any{"unexpected": true}},
	} {
		if res := callTool(t, cs, tc.tool, tc.args); !res.IsError {
			t.Errorf("%s %v: want a tool error, got %s", tc.tool, tc.args, text(res))
		}
	}
}

func TestAPIErrors(t *testing.T) {
	for _, tc := range []struct {
		name   string
		tool   string
		args   map[string]any
		op     string
		status int
		body   string
		want   []string
	}{
		{
			name: "unauthorized", tool: "whoami", op: "whoami", status: 401,
			body: `{"code":"unauthorized","message":"токен недійсний"}`,
			want: []string{"HTTP 401 unauthorized: токен недійсний", "PRRO_TOKEN"},
		},
		{
			name: "forbidden", tool: "get_billing_usage", op: "billingUsage", status: 403,
			body: `{"code":"forbidden","message":"обмежений токен"}`,
			want: []string{"HTTP 403 forbidden", "all cash registers"},
		},
		{
			name: "not found", tool: "get_cash_register_state", args: map[string]any{"cash_register_id": "nope"},
			op: "getCashRegisterState", status: 404, body: `{"code":"not_found","message":"касу не знайдено"}`,
			want: []string{"HTTP 404 not_found", "list_cash_registers"},
		},
		{
			name: "validation", tool: "get_billing_usage", args: map[string]any{"from": "2020-01-01", "to": "2026-01-01"},
			op: "billingUsage", status: 422,
			body: `{"code":"validation_failed","message":"період задовгий","details":{"to":"більше 366 днів"}}`,
			want: []string{"HTTP 422 validation_failed", `Details: {"to":"більше 366 днів"}`},
		},
		{
			name: "proxy page", tool: "list_webhooks", op: "listWebhooks", status: 502,
			body: `<html>Bad Gateway</html>`,
			want: []string{"HTTP 502", "Bad Gateway"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cs, api := newSession(t)
			api.handle(t, tc.op, tc.status, tc.body)
			res := callTool(t, cs, tc.tool, tc.args)
			if !res.IsError {
				t.Fatalf("want a tool error, got %s", text(res))
			}
			if res.StructuredContent != nil {
				t.Errorf("error result has structuredContent")
			}
			wantContains(t, text(res), tc.want...)
			if strings.Contains(text(res), testToken) {
				t.Errorf("error leaks the token")
			}
		})
	}
}

func TestNetworkError(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	srv.Close()
	cs := connect(t, prro.NewClient(testToken, prro.WithBaseURL(srv.URL), prro.WithRetry(0)))

	res := callTool(t, cs, "get_dps_status", nil)
	if !res.IsError {
		t.Fatalf("want a tool error, got %s", text(res))
	}
	wantContains(t, text(res), "PRRO.cloud API request failed")
}

// failingSource імітує віддалений транспорт без токена.
type failingSource struct{}

func (failingSource) For(context.Context, mcp.Request) (*prro.Client, error) {
	return nil, errNoToken
}

var errNoToken = &tokenError{}

type tokenError struct{}

func (*tokenError) Error() string { return "no bearer token" }

func TestSourceError(t *testing.T) {
	s := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "test"}, nil)
	tools.Register(s, failingSource{}, tools.Options{})
	st, ct := mcp.NewInMemoryTransports()
	ss, err := s.Connect(t.Context(), st, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer ss.Close()
	cs, err := mcp.NewClient(&mcp.Implementation{Name: "client", Version: "test"}, nil).Connect(t.Context(), ct, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer cs.Close()

	res := callTool(t, cs, "whoami", nil)
	if !res.IsError || !strings.Contains(text(res), "no bearer token") {
		t.Errorf("got %+v", res)
	}
}

// Невідомі моменти й порожні переліки не перетворюються у зведенні на
// вигадані факти («opened never», «restricted to 0 cash register(s): .»).
func TestSummariesWithMissingValues(t *testing.T) {
	cs, api := newSession(t)
	api.handle(t, "whoami", 200, `{"client_id":"c-1","scopes":["fiscal"],"all_cash_registers":false,"cash_registers":[],"jti":"tok-9"}`)
	api.handle(t, "systemDPSStatus", 200, `{"effective":"online","verdict":"online","since":"0001-01-01T00:00:00Z",
		"latency_ms":0,"samples":0,"failures":0,"tunables":{"window_seconds":300,"degraded_latency_ms":1500,"probe_interval_seconds":30}}`)
	api.handle(t, "getCashRegisterState", 200, `{"id":"r-1","fiscal_number":"4000123456","local_number":"1",
		"mode":"production","state":"opened","current_shift":{"id":"s-1","number":5,"status":"opening"},
		"attention":{"reason":"auto_close_failing"},"offline_limits":{"session_seconds":129600,"month_seconds":604800,"enforced":true}}`)
	api.handle(t, "listWebhookDeliveries", 200, `{"deliveries":[{"id":"d-1","event":"task.failed","status":"failed",
		"attempts":2,"data":{},"next_attempt_at":"2026-10-14T10:00:00Z","created_at":"2026-10-14T09:00:00Z"}]}`)

	for _, tc := range []struct {
		tool  string
		args  map[string]any
		want  []string
		avoid []string
	}{
		{"whoami", nil, []string{"no cash registers are assigned"}, []string{"restricted to 0"}},
		{"get_dps_status", nil, []string{"DPS mode: online (detector verdict: online). Latency"}, []string{"since", "never"}},
		{
			"get_cash_register_state",
			map[string]any{"cash_register_id": "r-1"},
			[]string{"Shift #5 opening, 0 document(s)", "NEEDS ATTENTION: auto_close_failing"},
			[]string{"never", "since"},
		},
		{
			"list_webhook_deliveries",
			map[string]any{"webhook_id": "w-1"},
			[]string{"failed after 2 attempt(s), created 2026-10-14T09:00:00Z, next attempt 2026-10-14T10:00:00Z"},
			nil,
		},
	} {
		res := callTool(t, cs, tc.tool, tc.args)
		mustOK(t, res)
		summary := res.Content[0].(*mcp.TextContent).Text
		wantContains(t, summary, tc.want...)
		for _, s := range tc.avoid {
			if strings.Contains(summary, s) {
				t.Errorf("%s: summary contains %q:\n%s", tc.tool, s, summary)
			}
		}
	}
}
