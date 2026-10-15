package main

import (
	"encoding/json"
	"maps"
	"os"
	"slices"
	"testing"

	"github.com/prro-cloud/prro-mcp/internal/httpserver"
	"github.com/prro-cloud/prro-mcp/internal/server"
	"github.com/prro-cloud/prro-mcp/internal/tools"
)

func readJSON(t *testing.T, path string, v any) {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(b, v); err != nil {
		t.Fatalf("%s: %v", path, err)
	}
}

// Перелік інструментів у пакеті .mcpb показує Claude Desktop до запуску
// сервера, тож він має збігатися з тим, що сервер реєструє.
func TestMCPBManifest(t *testing.T) {
	var m struct {
		Name  string `json:"name"`
		Tools []struct {
			Name string `json:"name"`
		} `json:"tools"`
	}
	readJSON(t, "../../mcpb/manifest.json", &m)
	if m.Name != server.Name {
		t.Errorf("name = %q, want %q", m.Name, server.Name)
	}
	var names []string
	for _, tl := range m.Tools {
		names = append(names, tl.Name)
	}
	slices.Sort(names)
	if want := slices.Sorted(maps.Keys(tools.Operations)); !slices.Equal(names, want) {
		t.Errorf("manifest tools = %v, want %v", names, want)
	}
}

// envVars — змінні оточення локального сервера, які читає loadConfig;
// клієнти з реєстру MCP будують з environmentVariables форму налаштувань.
// PRRO_MCP_RATE_LIMIT сюди не входить: це налаштування віддаленого сервера,
// а не клієнта.
var envVars = []string{"PRRO_TOKEN", "PRRO_BASE_URL", "PRRO_LANG", "PRRO_MCP_LOG_LEVEL"}

func TestServerJSON(t *testing.T) {
	var s struct {
		Name        string `json:"name"`
		Description string `json:"description"`
		Packages    []struct {
			RegistryType         string `json:"registryType"`
			EnvironmentVariables []struct {
				Name string `json:"name"`
			} `json:"environmentVariables"`
		} `json:"packages"`
		Remotes []struct {
			Type    string `json:"type"`
			URL     string `json:"url"`
			Headers []struct {
				Name     string `json:"name"`
				IsSecret bool   `json:"isSecret"`
			} `json:"headers"`
		} `json:"remotes"`
	}
	readJSON(t, "../../server.json", &s)
	if s.Name != "io.github.prro-cloud/prro-mcp" {
		t.Errorf("name = %q", s.Name)
	}
	if n := len([]rune(s.Description)); n == 0 || n > 100 {
		t.Errorf("description has %d characters, want 1-100", n)
	}
	var types []string
	for _, p := range s.Packages {
		types = append(types, p.RegistryType)
		var names []string
		for _, v := range p.EnvironmentVariables {
			names = append(names, v.Name)
		}
		if !slices.Equal(names, envVars) {
			t.Errorf("%s package: environment variables %v, want %v", p.RegistryType, names, envVars)
		}
	}
	if !slices.Equal(types, []string{"mcpb", "oci"}) {
		t.Errorf("packages = %v", types)
	}
	if len(s.Remotes) != 1 {
		t.Fatalf("remotes = %+v, want one", s.Remotes)
	}
	r := s.Remotes[0]
	if r.Type != "streamable-http" || r.URL != "https://mcp.prro.cloud"+httpserver.Path {
		t.Errorf("remote = %s %s", r.Type, r.URL)
	}
	if len(r.Headers) != 1 || r.Headers[0].Name != "Authorization" || !r.Headers[0].IsSecret {
		t.Errorf("remote headers = %+v, want a secret Authorization", r.Headers)
	}
}
