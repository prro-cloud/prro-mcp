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

// TaskInput — завдання, про яке питають.
type TaskInput struct {
	TaskID string `json:"task_id" jsonschema:"task id (uuid) returned by a fiscal operation (task_id in the API response or in a task.* webhook)"`
}

func registerTasks(s *mcp.Server, src clientsrc.Source) {
	add(s, src, tool[TaskInput, *prro.Task]{
		name:  "get_task",
		title: "Fiscal task status",
		description: "Returns a fiscal task in its current state. Every fiscal operation (receipt, opening or " +
			"closing a shift, closing or abandoning an offline session, reconciliation) becomes a task; the " +
			"integration gets task_id from the API response or a webhook. type is receipt, open_shift, close_shift, " +
			"close_offline_session, abandon_offline_session or sync_register. status is pending (queued), " +
			"processing, succeeded (see result) or failed (see fault) — a failed task is returned as data, not as " +
			"a tool error. fault.code is a stable id with a namespace: dps.* (rejected by the DPS fiscal server), " +
			"fiscal.* (document validation), return.* (return checked against the original receipt), storno.* " +
			"(storno checked against the receipt it cancels: only the register's last document can be cancelled), " +
			"signer.* (qualified e-signature), state.* (registrar state); fault.class says what helps: resync, config, " +
			"precursor (a missing previous step such as an open shift), user_action, transient (retry), request " +
			"(fix the request), internal. result holds the documents the task issued: fiscal_number from the DPS " +
			"(computed locally for an offline document), receipt_url for the buyer, tax_url in the taxpayer " +
			"cabinet, and testing=true when the documents are test documents without fiscal effect. A close_shift " +
			"task has result.zreport.totals: the Z report totals submitted to the DPS (realiz = sales, return = " +
			"returns, each with count, sum, pay_forms and taxes by rate; service_in/service_out = cash " +
			"deposits/withdrawals). Amounts are decimal strings in UAH.",
		input: func(s *jsonschema.Schema) { requireID(s, "task_id") },
		call: func(ctx context.Context, c *prro.Client, in TaskInput) (*prro.Task, error) {
			return c.Tasks.Get(ctx, in.TaskID)
		},
		summary: summarizeTask,
	})
}

func summarizeTask(t *prro.Task) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Task %s (%s): %s", t.ID, t.Type, t.Status)
	if c, f := timePart("created", t.CreatedAt), timePart("finished", t.FinishedAt); c != "" || f != "" {
		b.WriteString("," + c)
		if c != "" && f != "" {
			b.WriteString(",")
		}
		b.WriteString(f)
	}
	b.WriteString(".")
	if f := t.Fault; f != nil {
		fmt.Fprintf(&b, "\nFault %s (class %s)", f.Code, f.Class)
		if f.Message != "" {
			b.WriteString(": " + f.Message)
		}
		b.WriteString(".")
		if f.UpstreamMessage != "" {
			b.WriteString("\nDPS said: " + f.UpstreamMessage)
		}
	} else if t.Error != "" {
		b.WriteString("\nError: " + t.Error)
	}
	if r := t.Result; r != nil {
		if r.Testing {
			b.WriteString("\nTEST documents: no fiscal effect.")
		}
		if r.ShiftNumber != 0 {
			fmt.Fprintf(&b, "\nShift #%d.", r.ShiftNumber)
		}
		for _, d := range []struct {
			name string
			doc  *prro.DocResult
		}{
			{"Receipt", r.Receipt},
			{"Shift opening", r.ShiftOpen},
			{"Z report", r.ZReport},
			{"Shift closing", r.ShiftClose},
			{"Offline session end", r.OfflineEnd},
		} {
			if d.doc == nil {
				continue
			}
			fmt.Fprintf(&b, "\n%s: fiscal number %s, local number %d", d.name, orDash(d.doc.FiscalNumber), d.doc.LocalNumber)
			if d.doc.Offline {
				b.WriteString(" (offline, reaches the DPS after the session is submitted)")
			}
			if d.doc.ReceiptURL != "" {
				b.WriteString(", " + d.doc.ReceiptURL)
			}
			b.WriteString(".")
		}
		if r.ZReport != nil && r.ZReport.Totals != nil {
			zt := r.ZReport.Totals
			fmt.Fprintf(&b, "\nZ report totals: %d sale(s) for %s UAH, %d return(s) for %s UAH, cash in %s, cash out %s.",
				zt.Sales.Count, zt.Sales.Sum, zt.Returns.Count, zt.Returns.Sum, zt.ServiceIn, zt.ServiceOut)
		}
		if a := r.OfflineAbandon; a != nil {
			fmt.Fprintf(&b, "\nOffline session %d abandoned: %d document(s) held by the DPS, %d not received.",
				a.DPSSessionID, a.HeldDocuments, len(a.Abandoned))
		}
		if rc := r.Reconcile; rc != nil && rc.Skipped != "" {
			fmt.Fprintf(&b, "\nReconciliation skipped: %s.", rc.Skipped)
		}
	}
	return b.String()
}
