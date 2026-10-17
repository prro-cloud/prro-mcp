package main

import (
	"bytes"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func env(m map[string]string) func(string) string {
	return func(k string) string { return m[k] }
}

func TestLoadConfig(t *testing.T) {
	cfg, err := loadConfig(env(map[string]string{"PRRO_TOKEN": " tok "}), false)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.token != "tok" || cfg.lang != "uk" || cfg.baseURL != "" || cfg.logLevel != slog.LevelInfo {
		t.Errorf("defaults = %+v", cfg)
	}

	cfg, err = loadConfig(env(map[string]string{
		"PRRO_TOKEN": "tok", "PRRO_BASE_URL": "http://localhost:8080", "PRRO_LANG": "EN", "PRRO_MCP_LOG_LEVEL": "debug",
	}), false)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.baseURL != "http://localhost:8080" || cfg.lang != "en" || cfg.logLevel != slog.LevelDebug {
		t.Errorf("config = %+v", cfg)
	}
}

func TestLoadConfigErrors(t *testing.T) {
	for name, m := range map[string]map[string]string{
		"no token":  {},
		"blank":     {"PRRO_TOKEN": "  "},
		"bad lang":  {"PRRO_TOKEN": "tok", "PRRO_LANG": "ru"},
		"bad level": {"PRRO_TOKEN": "tok", "PRRO_MCP_LOG_LEVEL": "loud"},
		"no scheme": {"PRRO_TOKEN": "tok", "PRRO_BASE_URL": "api.prro.cloud"},
		"host only": {"PRRO_TOKEN": "tok", "PRRO_BASE_URL": "localhost:8080"},
		"ftp":       {"PRRO_TOKEN": "tok", "PRRO_BASE_URL": "ftp://api.prro.cloud"},
	} {
		if _, err := loadConfig(env(m), false); err == nil {
			t.Errorf("%s: want an error", name)
		}
	}
}

func TestLoadConfigRemote(t *testing.T) {
	cfg, err := loadConfig(env(nil), true)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.token != "" || cfg.ratePerMinute != defaultRatePerMinute {
		t.Errorf("defaults = %+v", cfg)
	}

	cfg, err = loadConfig(env(map[string]string{"PRRO_MCP_RATE_LIMIT": "0"}), true)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ratePerMinute != 0 {
		t.Errorf("ratePerMinute = %d, want 0", cfg.ratePerMinute)
	}

	for name, m := range map[string]map[string]string{
		"token":         {"PRRO_TOKEN": "tok"},
		"bad rate":      {"PRRO_MCP_RATE_LIMIT": "fast"},
		"negative rate": {"PRRO_MCP_RATE_LIMIT": "-1"},
		"bad base url":  {"PRRO_BASE_URL": "api.prro.cloud"},
	} {
		if _, err := loadConfig(env(m), true); err == nil {
			t.Errorf("%s: want an error", name)
		}
	}
}

func TestRunHealthcheck(t *testing.T) {
	ok := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("ok")) }))
	defer ok.Close()
	var stdout, stderr bytes.Buffer
	if err := run([]string{"-healthcheck", ok.URL}, env(nil), &stdout, &stderr); err != nil {
		t.Errorf("healthy server: %v", err)
	}
	down := httptest.NewServer(http.NotFoundHandler())
	defer down.Close()
	if err := run([]string{"-healthcheck", down.URL}, env(nil), &stdout, &stderr); err == nil {
		t.Error("404: want an error")
	}
}

func TestRunHTTPWithToken(t *testing.T) {
	var stdout, stderr bytes.Buffer
	err := run([]string{"-http", "127.0.0.1:0"}, env(map[string]string{"PRRO_TOKEN": "tok"}), &stdout, &stderr)
	if err == nil || !strings.Contains(err.Error(), "Authorization") {
		t.Errorf("err = %v", err)
	}
}

func TestRunVersion(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if err := run([]string{"-version"}, env(nil), &stdout, &stderr); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(stdout.String(), "prro-mcp ") || !strings.Contains(stdout.String(), "prro-go ") {
		t.Errorf("stdout = %q", stdout.String())
	}
}

func TestRunWithoutToken(t *testing.T) {
	var stdout, stderr bytes.Buffer
	err := run(nil, env(nil), &stdout, &stderr)
	if err == nil || !strings.Contains(err.Error(), "PRRO_TOKEN") {
		t.Errorf("err = %v", err)
	}
	if stdout.Len() != 0 {
		t.Errorf("stdout must stay clean for the protocol: %q", stdout.String())
	}
}
