package server_test

import (
	"bytes"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/prro-cloud/prro-go"

	"github.com/prro-cloud/prro-mcp/internal/clientsrc"
	"github.com/prro-cloud/prro-mcp/internal/server"
	"github.com/prro-cloud/prro-mcp/internal/spec"
)

// syncBuffer — журнал, у який пишуть горутини сервера.
type syncBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *syncBuffer) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *syncBuffer) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}

func connect(t *testing.T, h http.Handler, log *syncBuffer) *mcp.ClientSession {
	t.Helper()
	api := httptest.NewServer(h)
	t.Cleanup(api.Close)
	client := prro.NewClient("secret-token", prro.WithBaseURL(api.URL), prro.WithRetry(0))
	logger := slog.New(slog.NewTextHandler(log, &slog.HandlerOptions{Level: slog.LevelDebug}))
	s := server.New(clientsrc.NewStatic(client), server.Options{Version: "0.1.0", Logger: logger})

	st, ct := mcp.NewInMemoryTransports()
	ss, err := s.Connect(t.Context(), st, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ss.Close() })
	cs, err := mcp.NewClient(&mcp.Implementation{Name: "client", Version: "test"}, nil).Connect(t.Context(), ct, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cs.Close() })
	return cs
}

func TestServerInfo(t *testing.T) {
	cs := connect(t, http.NotFoundHandler(), &syncBuffer{})
	init := cs.InitializeResult()
	if init.ServerInfo.Name != server.Name || init.ServerInfo.Version != "0.1.0" {
		t.Errorf("server info = %+v", init.ServerInfo)
	}
	if !strings.Contains(init.Instructions, "READ-ONLY") {
		t.Errorf("instructions = %q", init.Instructions)
	}
	if init.Capabilities.Tools == nil || init.Capabilities.Resources == nil {
		t.Errorf("capabilities = %+v", init.Capabilities)
	}
}

func TestOpenAPIResource(t *testing.T) {
	cs := connect(t, http.NotFoundHandler(), &syncBuffer{})

	list, err := cs.ListResources(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(list.Resources) != 1 || list.Resources[0].URI != server.OpenAPIURI {
		t.Fatalf("resources = %+v", list.Resources)
	}

	res, err := cs.ReadResource(t.Context(), &mcp.ReadResourceParams{URI: server.OpenAPIURI})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Contents) != 1 || res.Contents[0].Text != string(spec.OpenAPI) {
		t.Fatal("resource content differs from the embedded contract")
	}
	if !strings.Contains(res.Contents[0].Text, "version: "+prro.APIVersion) {
		t.Errorf("embedded contract is not version %s of prro-go; run make sync-spec", prro.APIVersion)
	}
}

func TestCallLogging(t *testing.T) {
	log := &syncBuffer{}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/version", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"version":"v0.3.0"}`))
	})
	mux.HandleFunc("GET /v1/whoami", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"code":"unauthorized","message":"bad token"}`))
	})
	cs := connect(t, mux, log)

	for _, name := range []string{"get_version", "whoami"} {
		if _, err := cs.CallTool(t.Context(), &mcp.CallToolParams{Name: name, Arguments: map[string]any{}}); err != nil {
			t.Fatal(err)
		}
	}
	out := log.String()
	for _, want := range []string{"tool call", "tool=get_version", "tool returned error", "tool=whoami", "HTTP 401"} {
		if !strings.Contains(out, want) {
			t.Errorf("log lacks %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "secret-token") {
		t.Error("log leaks the token")
	}
}
