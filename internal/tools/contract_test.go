package tools_test

import (
	"bufio"
	"bytes"
	"maps"
	"slices"
	"strings"
	"testing"

	"github.com/prro-cloud/prro-mcp/internal/spec"
	"github.com/prro-cloud/prro-mcp/internal/tools"
)

// excluded — операції контракту, які свідомо не входять у v0.1: усі вони
// пишуть у ДПС або змінюють налаштування. Нова операція в контракті має
// потрапити або в tools.Operations, або сюди — інакше тест падає.
var excluded = []string{
	"createReceipt",
	"openShift",
	"closeShift",
	"setCashBalance",
	"closeOfflineSession",
	"retryOfflineSubmission",
	"abandonOfflineSession",
	"clearRegisterAttention",
	"createWebhook",
	"updateWebhook",
	"deleteWebhook",
	"replayWebhookDelivery",
}

// operations повертає operationId контракту з маршрутом у синтаксисі
// шаблонів net/http: "GET /v1/tasks/{id}". Розбір рядковий: шлях — ключ
// з відступом 2 під paths, метод — ключ з відступом 4.
func operations(t *testing.T) map[string]string {
	t.Helper()
	ops := map[string]string{}
	var path, method string
	inPaths := false
	sc := bufio.NewScanner(bytes.NewReader(spec.OpenAPI))
	for sc.Scan() {
		line := sc.Text()
		switch {
		case line == "paths:":
			inPaths = true
		case inPaths && line != "" && !strings.HasPrefix(line, " ") && !strings.HasPrefix(line, "#"):
			inPaths = false
		case !inPaths:
		case strings.HasPrefix(line, "  /") && strings.HasSuffix(line, ":"):
			path = strings.TrimSuffix(strings.TrimSpace(line), ":")
		case strings.HasPrefix(line, "    ") && !strings.HasPrefix(line, "     ") && strings.HasSuffix(line, ":"):
			method = strings.ToUpper(strings.TrimSuffix(strings.TrimSpace(line), ":"))
		default:
			if id, ok := strings.CutPrefix(strings.TrimSpace(line), "operationId:"); ok {
				ops[strings.TrimSpace(id)] = method + " " + path
			}
		}
	}
	if err := sc.Err(); err != nil {
		t.Fatal(err)
	}
	if len(ops) == 0 {
		t.Fatal("no operations in openapi.yaml")
	}
	return ops
}

func TestContractCoverage(t *testing.T) {
	ops := operations(t)

	covered := map[string]string{} // operationId → інструмент
	for name, op := range tools.Operations {
		if _, ok := ops[op]; !ok {
			t.Errorf("tool %s: operation %s is not in the contract", name, op)
		}
		if prev, dup := covered[op]; dup {
			t.Errorf("operation %s is served by both %s and %s", op, prev, name)
		}
		covered[op] = name
	}
	for _, op := range excluded {
		if _, ok := ops[op]; !ok {
			t.Errorf("excluded operation %s is not in the contract", op)
		}
		if name, ok := covered[op]; ok {
			t.Errorf("operation %s is both excluded and served by %s", op, name)
		}
	}
	for _, op := range slices.Sorted(maps.Keys(ops)) {
		if _, ok := covered[op]; !ok && !slices.Contains(excluded, op) {
			t.Errorf("new operation %s (%s): add a read-only tool for it or list it in excluded", op, ops[op])
		}
	}
}

// Інструменти v0.1 лише читають: жоден не викликає інший метод, ніж GET.
func TestContractReadOnly(t *testing.T) {
	ops := operations(t)
	for name, op := range tools.Operations {
		if route := ops[op]; !strings.HasPrefix(route, "GET ") {
			t.Errorf("tool %s calls %s (%s): v0.1 is read-only", name, op, route)
		}
	}
}
