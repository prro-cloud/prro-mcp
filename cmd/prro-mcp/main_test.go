package main

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"
)

func env(m map[string]string) func(string) string {
	return func(k string) string { return m[k] }
}

func TestLoadConfig(t *testing.T) {
	cfg, err := loadConfig(env(map[string]string{"PRRO_TOKEN": " tok "}))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.token != "tok" || cfg.lang != "uk" || cfg.baseURL != "" || cfg.logLevel != slog.LevelInfo {
		t.Errorf("defaults = %+v", cfg)
	}

	cfg, err = loadConfig(env(map[string]string{
		"PRRO_TOKEN": "tok", "PRRO_BASE_URL": "http://localhost:8080", "PRRO_LANG": "EN", "PRRO_MCP_LOG_LEVEL": "debug",
	}))
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
		if _, err := loadConfig(env(m)); err == nil {
			t.Errorf("%s: want an error", name)
		}
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
