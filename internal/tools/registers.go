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

// CashRegistersOutput — каси, доступні токену.
type CashRegistersOutput struct {
	CashRegisters []prro.CashRegister `json:"cash_registers" jsonschema:"cash registers the token can reach, oldest first"`
}

// CashRegisterInput — каса, про яку питають.
type CashRegisterInput struct {
	CashRegisterID string `json:"cash_register_id" jsonschema:"cash register id (uuid) from list_cash_registers; not the fiscal number"`
}

func registerRegisters(s *mcp.Server, src clientsrc.Source) {
	add(s, src, tool[struct{}, *CashRegistersOutput]{
		name:  "list_cash_registers",
		title: "List cash registers",
		description: "Lists the cash registers (PRRO, software registrars of settlement operations) the token can reach, " +
			"oldest first. id is the cash register id every other call and every receipt (cash_register_id) uses; " +
			"fiscal_number is the number assigned by the DPS and is NOT accepted as an id. mode is production or " +
			"test (test documents are marked as test and have no fiscal effect); state is closed (no open shift), " +
			"opened (shift open) or offline (an offline session is in progress). " +
			"Call get_cash_register_state for the shift, cash balance and offline details of one register.",
		call: func(ctx context.Context, c *prro.Client, _ struct{}) (*CashRegistersOutput, error) {
			list, err := c.CashRegisters.List(ctx)
			if err != nil {
				return nil, err
			}
			return &CashRegistersOutput{CashRegisters: nonNil(list)}, nil
		},
		summary: func(out *CashRegistersOutput) string {
			if len(out.CashRegisters) == 0 {
				return "The token has no cash registers."
			}
			var b strings.Builder
			fmt.Fprintf(&b, "%d cash register(s):", len(out.CashRegisters))
			for _, r := range out.CashRegisters {
				fmt.Fprintf(&b, "\n- %s: fiscal number %s, local number %s, mode %s, state %s",
					r.ID, orDash(r.FiscalNumber), orDash(r.LocalNumber), r.Mode, r.State)
				if r.PointName != "" || r.PointAddress != "" {
					fmt.Fprintf(&b, ", point %q %s", r.PointName, r.PointAddress)
				}
			}
			return b.String()
		},
	})

	add(s, src, tool[CashRegisterInput, *prro.CashRegisterState]{
		name:  "get_cash_register_state",
		title: "Cash register state",
		description: "Returns the current state of one cash register: mode (production or test), state " +
			"(closed, opened, offline), shift_mode (manual; round_the_clock or working_hours close the shift " +
			"automatically at shift_close_times, Kyiv time). current_shift is the open shift: number is the " +
			"running shift number, status opening/opened/closing/closed, and testing=true means every document " +
			"of the shift is a test document without fiscal effect — always check it before telling the user a " +
			"receipt was fiscalized. shift_totals are the running totals the Z report will be built from " +
			"(realiz = sales, return = returns, service_in/service_out = cash deposits/withdrawals). " +
			"cash_balance counts only documents that went through the service; a zero as_of means no data, not " +
			"an empty drawer. offline_session is present while the register works offline; offline_limits are the " +
			"legal limits (36 h per session, 168 h per month) and offline_month_used_seconds the time used this " +
			"month. attention is present when the register needs a human (reason recurring_remediation, " +
			"offline_submission_paused or auto_close_failing). Amounts are decimal strings in UAH.",
		input: func(s *jsonschema.Schema) { requireID(s, "cash_register_id") },
		call: func(ctx context.Context, c *prro.Client, in CashRegisterInput) (*prro.CashRegisterState, error) {
			return c.CashRegisters.Get(ctx, in.CashRegisterID)
		},
		summary: summarizeRegister,
	})
}

func summarizeRegister(st *prro.CashRegisterState) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Cash register %s (fiscal number %s, local number %s): mode %s, state %s",
		st.ID, orDash(st.FiscalNumber), orDash(st.LocalNumber), st.Mode, st.State)
	if st.ShiftMode != "" {
		fmt.Fprintf(&b, ", shift mode %s", st.ShiftMode)
		if len(st.ShiftCloseTimes) > 0 {
			fmt.Fprintf(&b, " (closes at %s Kyiv time)", strings.Join(st.ShiftCloseTimes, ", "))
		}
	}
	b.WriteString(".")
	if sh := st.CurrentShift; sh != nil {
		fmt.Fprintf(&b, "\nShift #%d %s%s, %d document(s)", sh.Number, sh.Status, timePart("since", sh.OpenedAt), sh.Documents)
		if sh.Testing {
			b.WriteString("; TESTING shift: its documents have no fiscal effect")
		}
		b.WriteString(".")
	} else {
		b.WriteString("\nNo open shift.")
	}
	if t := st.ShiftTotals; t != nil {
		fmt.Fprintf(&b, "\nShift totals: %d sale(s) for %s UAH, %d return(s) for %s UAH, cash in %s, cash out %s.",
			t.Sales.Count, t.Sales.Sum, t.Returns.Count, t.Returns.Sum, t.ServiceIn, t.ServiceOut)
	}
	if cb := st.CashBalance; cb != nil {
		if cb.AsOf.IsZero() {
			fmt.Fprintf(&b, "\nCash balance: %s UAH (no cash movements seen yet).", cb.Amount)
		} else {
			fmt.Fprintf(&b, "\nCash balance: %s UAH%s.", cb.Amount, timePart("as of", cb.AsOf))
		}
	}
	if off := st.OfflineSession; off != nil {
		fmt.Fprintf(&b, "\nOffline session %d%s, %d document(s) awaiting submission to the DPS.",
			off.DPSSessionID, timePart("started", off.StartedAt), off.Documents)
	}
	if a := st.Attention; a != nil {
		fmt.Fprintf(&b, "\nNEEDS ATTENTION%s: %s.", timePart("since", a.Since), a.Reason)
	}
	return b.String()
}

// nonNil замінює nil-зріз порожнім: модель бачить [], а не null.
func nonNil[T any](s []T) []T {
	if s == nil {
		return []T{}
	}
	return s
}
