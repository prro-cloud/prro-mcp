package httpserver_test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/prro-cloud/prro-go"

	"github.com/prro-cloud/prro-mcp/internal/clientsrc"
	"github.com/prro-cloud/prro-mcp/internal/httpserver"
	"github.com/prro-cloud/prro-mcp/internal/server"
)

// fakeAPI відповідає на /v1/version і запам'ятовує токени запитів.
type fakeAPI struct {
	mu     sync.Mutex
	tokens []string
}

func (f *fakeAPI) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/v1/version" {
		http.NotFound(w, r)
		return
	}
	token, _ := clientsrc.BearerToken(r.Header)
	f.mu.Lock()
	f.tokens = append(f.tokens, token)
	f.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	_, _ = io.WriteString(w, `{"version":"v0.3.0"}`)
}

func (f *fakeAPI) seen() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.tokens...)
}

// newServer запускає фейковий API і віддалений MCP-сервер перед ним.
func newServer(t *testing.T, opts httpserver.Options) (*httptest.Server, *fakeAPI) {
	t.Helper()
	api := &fakeAPI{}
	apiSrv := httptest.NewServer(api)
	t.Cleanup(apiSrv.Close)
	src := clientsrc.NewBearer(prro.WithBaseURL(apiSrv.URL), prro.WithRetry(0))
	srv := httptest.NewServer(httpserver.Handler(server.New(src, server.Options{Version: "0.2.0"}), opts))
	t.Cleanup(srv.Close)
	return srv, api
}

// bearer додає токен до кожного запиту клієнта MCP.
type bearer struct{ token string }

func (b bearer) RoundTrip(r *http.Request) (*http.Response, error) {
	r = r.Clone(r.Context())
	r.Header.Set("Authorization", "Bearer "+b.token)
	return http.DefaultTransport.RoundTrip(r)
}

func connect(t *testing.T, url, token string) (*mcp.ClientSession, error) {
	t.Helper()
	tr := &mcp.StreamableClientTransport{
		Endpoint:   url + httpserver.Path,
		HTTPClient: &http.Client{Transport: bearer{token}},
	}
	cs, err := mcp.NewClient(&mcp.Implementation{Name: "client", Version: "test"}, nil).Connect(t.Context(), tr, nil)
	if err == nil {
		t.Cleanup(func() { _ = cs.Close() })
	}
	return cs, err
}

func getVersion(t *testing.T, cs *mcp.ClientSession) {
	t.Helper()
	res, err := cs.CallTool(t.Context(), &mcp.CallToolParams{Name: "get_version", Arguments: map[string]any{}})
	if err != nil {
		t.Fatal(err)
	}
	if res.IsError {
		t.Fatalf("tool error: %v", res.GetError())
	}
}

func TestEachClientUsesItsToken(t *testing.T) {
	srv, api := newServer(t, httpserver.Options{})
	for _, token := range []string{"token-a", "token-b"} {
		cs, err := connect(t, srv.URL, token)
		if err != nil {
			t.Fatal(err)
		}
		getVersion(t, cs)
	}
	if got := api.seen(); len(got) != 2 || got[0] != "token-a" || got[1] != "token-b" {
		t.Errorf("API saw tokens %q, want [token-a token-b]", got)
	}
}

func TestMissingToken(t *testing.T) {
	srv, _ := newServer(t, httpserver.Options{})
	for _, auth := range []string{"", "Bearer ", "Basic dXNlcjpwYXNz"} {
		req, _ := http.NewRequestWithContext(t.Context(), http.MethodPost, srv.URL+httpserver.Path,
			strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept", "application/json, text/event-stream")
		if auth != "" {
			req.Header.Set("Authorization", auth)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		_ = resp.Body.Close()
		if resp.StatusCode != http.StatusUnauthorized || !strings.HasPrefix(resp.Header.Get("WWW-Authenticate"), "Bearer") {
			t.Errorf("Authorization %q: status %d, WWW-Authenticate %q", auth, resp.StatusCode, resp.Header.Get("WWW-Authenticate"))
		}
	}
}

func TestRateLimit(t *testing.T) {
	const limit = 4
	srv, _ := newServer(t, httpserver.Options{RatePerMinute: limit})
	cs, err := connect(t, srv.URL, "busy")
	if err != nil {
		t.Fatal(err)
	}
	// Скільки запитів іде на підключення, залежить від версії протоколу,
	// тож просто вичерпуємо ліміт викликами.
	for i := range limit {
		_, err = cs.CallTool(t.Context(), &mcp.CallToolParams{Name: "get_version", Arguments: map[string]any{}})
		if err != nil {
			break
		}
		if i == limit-1 {
			t.Fatalf("%d calls after connecting passed a limit of %d", limit, limit)
		}
	}
	if !strings.Contains(err.Error(), http.StatusText(http.StatusTooManyRequests)) {
		t.Errorf("err = %v, want Too Many Requests", err)
	}
	// Ліміт — окремо для кожного токена.
	other, err := connect(t, srv.URL, "quiet")
	if err != nil {
		t.Fatal(err)
	}
	getVersion(t, other)
}

func TestHealth(t *testing.T) {
	srv, _ := newServer(t, httpserver.Options{})
	resp, err := srv.Client().Get(srv.URL + httpserver.HealthPath) //nolint:noctx // test
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("status %d", resp.StatusCode)
	}
}
