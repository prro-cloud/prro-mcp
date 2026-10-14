package tools

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/prro-cloud/prro-go"

	"github.com/prro-cloud/prro-mcp/internal/clientsrc"
)

// BillingUsageInput — період підсумку тарифікації.
type BillingUsageInput struct {
	From string `json:"from,omitempty" jsonschema:"start of the period, inclusive, YYYY-MM-DD (UTC); default: the first day of the current month"`
	To   string `json:"to,omitempty" jsonschema:"end of the period, exclusive, YYYY-MM-DD (UTC); default: the first day of the next month; at most 366 days after from"`
}

// InvoicesInput — сторінка переліку рахунків.
type InvoicesInput struct {
	Limit  int `json:"limit,omitempty" jsonschema:"how many invoices to return, 1-200; default 50"`
	Offset int `json:"offset,omitempty" jsonschema:"how many invoices to skip, for paging"`
}

// InvoicesOutput — сторінка рахунків.
type InvoicesOutput struct {
	Invoices []prro.Invoice `json:"invoices" jsonschema:"invoices, newest period first"`
}

func registerBilling(s *mcp.Server, src clientsrc.Source) {
	add(s, src, tool[BillingUsageInput, *prro.BillingUsage]{
		name:  "get_billing_usage",
		title: "Billing balance and usage",
		description: "Returns the prepaid balance and the billing summary for a period: the number of billed " +
			"receipts, the amount charged and the tariff. All amounts here are KOPECKS as decimal strings with " +
			"up to 4 decimals (\"150.0000\" is 1.50 UAH), unlike receipt amounts which are in UAH. While balance " +
			"is below soft_limit new receipts are rejected with HTTP 402 payment_required (shift operations still " +
			"work); blocked=true means receipts are blocked right now. Billing is asynchronous and may lag the " +
			"latest receipt by a few seconds. Needs a token with access to all cash registers.",
		input: func(s *jsonschema.Schema) {
			property(s, "from").Format = "date"
			property(s, "to").Format = "date"
		},
		call: func(ctx context.Context, c *prro.Client, in BillingUsageInput) (*prro.BillingUsage, error) {
			return c.Billing.Usage(ctx, &prro.UsageParams{From: prro.Date(in.From), To: prro.Date(in.To)})
		},
		summary: func(u *prro.BillingUsage) string {
			blocked := "receipts are accepted"
			if u.Blocked {
				blocked = "receipts are BLOCKED"
			}
			return fmt.Sprintf("Balance %s (soft limit %s), %s.\nPeriod %s to %s (exclusive): %d receipt(s), charged %s; plan %s at %s per receipt.",
				uah(u.Balance), uah(u.SoftLimit), blocked,
				u.Period.From, u.Period.To, u.Receipts, uah(u.Charged), orDash(u.Tariff.PlanID), uah(u.Tariff.PricePerReceipt))
		},
	})

	add(s, src, tool[InvoicesInput, *InvoicesOutput]{
		name:  "list_invoices",
		title: "List invoices",
		description: "Lists the client's invoices, newest billing period first: one invoice per billing period, " +
			"not per payment. period_start and period_end are both inclusive; amount is in KOPECKS as a decimal " +
			"string (\"150.0000\" is 1.50 UAH); status is draft, issued, paid or overdue. Page with limit and " +
			"offset. Needs a token with access to all cash registers.",
		input: func(s *jsonschema.Schema) {
			limit := property(s, "limit")
			limit.Minimum, limit.Maximum = new(1.0), new(200.0)
			property(s, "offset").Minimum = new(0.0)
		},
		call: func(ctx context.Context, c *prro.Client, in InvoicesInput) (*InvoicesOutput, error) {
			list, err := c.Invoices.List(ctx, &prro.ListInvoicesParams{Limit: in.Limit, Offset: in.Offset})
			if err != nil {
				return nil, err
			}
			return &InvoicesOutput{Invoices: nonNil(list)}, nil
		},
		summary: func(out *InvoicesOutput) string {
			if len(out.Invoices) == 0 {
				return "No invoices."
			}
			var b strings.Builder
			fmt.Fprintf(&b, "%d invoice(s):", len(out.Invoices))
			for _, inv := range out.Invoices {
				fmt.Fprintf(&b, "\n- %s to %s: %s, %s (id %s)", inv.PeriodStart, inv.PeriodEnd, uah(inv.Amount), inv.Status, inv.ID)
			}
			return b.String()
		},
	})
}
