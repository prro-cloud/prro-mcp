package tools

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/prro-cloud/prro-go"

	"github.com/prro-cloud/prro-mcp/internal/clientsrc"
)

// VersionOutput — версії сервісу, контракту і цього сервера.
type VersionOutput struct {
	Service     string `json:"service" jsonschema:"build version of the PRRO.cloud API service: a release tag such as v0.3.0, or dev on a local build"`
	APIContract string `json:"api_contract" jsonschema:"version of the public API contract (info.version of openapi.yaml) this MCP server and the prro://openapi.yaml resource were built against"`
	MCPServer   string `json:"mcp_server" jsonschema:"version of this prro-mcp server"`
	PRROGo      string `json:"prro_go" jsonschema:"version of the prro-go client library inside this server"`
}

func registerSystem(s *mcp.Server, src clientsrc.Source, opts Options) {
	add(s, src, tool[struct{}, *VersionOutput]{
		name:  "get_version",
		title: "PRRO.cloud API version",
		description: "Returns the build version of the PRRO.cloud API service, the API contract version " +
			"this MCP server was built against, and the versions of prro-mcp and prro-go. " +
			"Does not need a valid token. Use it to check connectivity and to tell whether the " +
			"prro://openapi.yaml resource matches the running service.",
		call: func(ctx context.Context, c *prro.Client, _ struct{}) (*VersionOutput, error) {
			v, err := c.System.Version(ctx)
			if err != nil {
				return nil, err
			}
			return &VersionOutput{Service: v, APIContract: prro.APIVersion, MCPServer: opts.Version, PRROGo: prro.Version}, nil
		},
		summary: func(v *VersionOutput) string {
			return fmt.Sprintf("PRRO.cloud API service %s (contract %s); prro-mcp %s, prro-go %s.",
				v.Service, v.APIContract, v.MCPServer, v.PRROGo)
		},
	})

	add(s, src, tool[struct{}, *prro.Identity]{
		name:  "whoami",
		title: "Token identity and scopes",
		description: "Returns the client the configured PRRO.cloud machine token acts for, its scopes, " +
			"and which cash registers it can reach: all_cash_registers=true means every current and future " +
			"cash register of the client, otherwise only the ids in cash_registers. A restricted token cannot " +
			"read billing, invoices or webhooks. jti is the token id used to revoke it in the cabinet; " +
			"expires_at is absent for a token without expiry. Call this first when access fails.",
		call: func(ctx context.Context, c *prro.Client, _ struct{}) (*prro.Identity, error) {
			return c.System.WhoAmI(ctx)
		},
		summary: func(id *prro.Identity) string {
			var b strings.Builder
			fmt.Fprintf(&b, "Token %s acts for client %s with scopes [%s].", id.TokenID, id.ClientID, strings.Join(id.Scopes, ", "))
			switch {
			case id.AllCashRegisters:
				b.WriteString(" Access: all cash registers of the client.")
			case len(id.CashRegisters) == 0:
				b.WriteString(" Access: restricted, but no cash registers are assigned to the token, so it reaches none.")
			default:
				fmt.Fprintf(&b, " Access: restricted to %d cash register(s): %s.", len(id.CashRegisters), strings.Join(id.CashRegisters, ", "))
			}
			if id.ExpiresAt.IsZero() {
				b.WriteString(" Expires: never.")
			} else {
				b.WriteString(" Expires: " + id.ExpiresAt.Format(time.RFC3339) + ".")
			}
			return b.String()
		},
	})

	add(s, src, tool[struct{}, *prro.DPSStatus]{
		name:  "get_dps_status",
		title: "DPS connectivity mode",
		description: "Returns how PRRO.cloud currently works with the DPS (the Ukrainian tax service fiscal server). " +
			"effective is the mode the service follows: online, or offline when the DPS is unreachable and " +
			"cash registers sign documents in offline sessions that are submitted later. verdict is the detector " +
			"opinion and can also be degraded (DPS answers, but slowly). latency_ms, samples and failures describe " +
			"the detector window; tunables are its thresholds. Use it to explain slow or offline fiscalization.",
		call: func(ctx context.Context, c *prro.Client, _ struct{}) (*prro.DPSStatus, error) {
			return c.System.DPS(ctx)
		},
		summary: func(d *prro.DPSStatus) string {
			return fmt.Sprintf("DPS mode: %s (detector verdict: %s)%s. Latency %d ms over %d call(s), %d failed, in a %d s window.",
				d.Effective, d.Verdict, timePart("since", d.Since), d.LatencyMS, d.Samples, d.Failures, d.Tunables.WindowSeconds)
		},
	})
}
