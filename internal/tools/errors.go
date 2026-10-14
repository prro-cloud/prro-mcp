package tools

import (
	"context"
	"errors"
	"strconv"
	"strings"

	"github.com/prro-cloud/prro-go"
)

// toolError перетворює помилку prro-go на текст для моделі: що відповів
// API і що з цим робити. go-sdk кладе його в результат з isError: true —
// модель бачить збій і може виправити виклик, а протокол MCP лишається
// цілим.
func toolError(err error) error {
	var b strings.Builder
	var e *prro.Error
	switch {
	case errors.As(err, &e):
		b.WriteString("PRRO.cloud API error: HTTP " + strconv.Itoa(e.StatusCode))
		if e.Code != "" {
			b.WriteString(" " + e.Code)
		}
		if e.Message != "" {
			b.WriteString(": " + e.Message)
		}
		if f := e.Fault; f != nil {
			b.WriteString("\nFault: " + f.Code + " (class " + string(f.Class) + ")")
			if f.Message != "" {
				b.WriteString(": " + f.Message)
			}
			if f.UpstreamMessage != "" {
				b.WriteString("\nDPS said: " + f.UpstreamMessage)
			}
		}
		if len(e.Details) > 0 && e.Fault == nil {
			b.WriteString("\nDetails: " + string(e.Details))
		}
		if hint := hintFor(err); hint != "" {
			b.WriteString("\n" + hint)
		}
	case errors.Is(err, context.DeadlineExceeded):
		b.WriteString("PRRO.cloud API did not answer in time: " + err.Error())
	case errors.Is(err, context.Canceled):
		b.WriteString("Request canceled: " + err.Error())
	default:
		b.WriteString("PRRO.cloud API request failed: " + err.Error())
	}
	return errors.New(b.String())
}

// hintFor підказує, що робити з помилкою, якщо підказка не очевидна з
// повідомлення API.
func hintFor(err error) string {
	switch {
	case errors.Is(err, prro.ErrUnauthorized):
		return "The PRRO_TOKEN is missing, invalid, expired or revoked. " +
			"Ask the user to issue a new machine token in the PRRO.cloud cabinet and restart the MCP server."
	case errors.Is(err, prro.ErrForbidden):
		return "This token is restricted to specific cash registers and cannot read client-level " +
			"resources (billing, invoices, webhooks). A token with access to all cash registers is required."
	case errors.Is(err, prro.ErrNotFound):
		return "The resource does not exist or this token has no access to it. " +
			"Check the id; cash register ids come from list_cash_registers, webhook ids from list_webhooks."
	case errors.Is(err, prro.ErrValidation):
		return "The arguments were rejected by the API; fix them according to the details and retry."
	}
	return ""
}
