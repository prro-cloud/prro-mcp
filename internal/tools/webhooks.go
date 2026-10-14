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

// DeliveriesInput — фільтр історії доставок вебхука.
type DeliveriesInput struct {
	WebhookID string `json:"webhook_id" jsonschema:"webhook id (uuid) from list_webhooks"`
	Status    string `json:"status,omitempty" jsonschema:"only deliveries in this status; omit for all statuses"`
	Limit     int    `json:"limit,omitempty" jsonschema:"how many deliveries to return, 1-200; default 50"`
}

// DeliveriesOutput — доставки вебхука.
type DeliveriesOutput struct {
	Deliveries []prro.WebhookDelivery `json:"deliveries" jsonschema:"deliveries, newest first"`
}

func registerWebhooks(s *mcp.Server, src clientsrc.Source) {
	add(s, src, tool[struct{}, *prro.WebhookList]{
		name:  "list_webhooks",
		title: "List webhooks",
		description: "Lists the client's webhook endpoints (secrets are never returned) with the limit: at most " +
			"one webhook more than the client has cash registers; used counts paused ones too. events is the " +
			"subscription, an empty list means all events; active=false means delivery is paused and events queue " +
			"up. Deliveries are POST requests with an envelope {delivery_id, event, attempt, occurred_at, data}, " +
			"signed in the X-Signature header (HMAC); delivery is at-least-once, so the receiver must deduplicate " +
			"by delivery_id and order events of a cash register by sequence. Needs a token with access to all " +
			"cash registers.",
		call: func(ctx context.Context, c *prro.Client, _ struct{}) (*prro.WebhookList, error) {
			list, err := c.Webhooks.List(ctx)
			if err != nil {
				return nil, err
			}
			list.Webhooks = nonNil(list.Webhooks)
			return list, nil
		},
		summary: func(l *prro.WebhookList) string {
			var b strings.Builder
			fmt.Fprintf(&b, "%d of %d webhook(s) used.", l.Used, l.Limit)
			for _, w := range l.Webhooks {
				state := "active"
				if !w.Active {
					state = "PAUSED"
				}
				events := "all events"
				if len(w.Events) > 0 {
					names := make([]string, len(w.Events))
					for i, e := range w.Events {
						names[i] = string(e)
					}
					events = strings.Join(names, ", ")
				}
				fmt.Fprintf(&b, "\n- %s %s (%s): %s", w.ID, w.URL, state, events)
			}
			return b.String()
		},
	})

	add(s, src, tool[DeliveriesInput, *DeliveriesOutput]{
		name:  "list_webhook_deliveries",
		title: "Webhook delivery history",
		description: "Lists deliveries of one webhook, newest first: event, status, attempts, last_error, the " +
			"event payload in data and the cash register it concerns. status is pending (queued), delivering " +
			"(attempt in progress), delivered, failed (attempt failed, will be retried at next_attempt_at) or " +
			"dead (retries exhausted). Use status=failed or status=dead with last_error to debug a receiver. " +
			"Delivered deliveries are kept 30 days, dead ones 14 days after the last attempt. Besides subscribed " +
			"events, history can contain webhook.ping (test send from the cabinet) and system.dps_* events. " +
			"Needs a token with access to all cash registers.",
		input: func(s *jsonschema.Schema) {
			requireID(s, "webhook_id")
			status := property(s, "status")
			for _, st := range []prro.DeliveryStatus{
				prro.DeliveryPending, prro.DeliveryDelivering, prro.DeliveryDelivered, prro.DeliveryFailed, prro.DeliveryDead,
			} {
				status.Enum = append(status.Enum, string(st))
			}
			limit := property(s, "limit")
			limit.Minimum, limit.Maximum = new(1.0), new(200.0)
		},
		call: func(ctx context.Context, c *prro.Client, in DeliveriesInput) (*DeliveriesOutput, error) {
			list, err := c.Webhooks.Deliveries(ctx, in.WebhookID, &prro.ListDeliveriesParams{
				Status: prro.DeliveryStatus(in.Status), Limit: in.Limit,
			})
			if err != nil {
				return nil, err
			}
			return &DeliveriesOutput{Deliveries: nonNil(list)}, nil
		},
		summary: func(out *DeliveriesOutput) string {
			if len(out.Deliveries) == 0 {
				return "No deliveries."
			}
			var b strings.Builder
			fmt.Fprintf(&b, "%d deliver(ies):", len(out.Deliveries))
			for _, d := range out.Deliveries {
				fmt.Fprintf(&b, "\n- %s %s: %s after %d attempt(s)", d.ID, d.Event, d.Status, d.Attempts)
				if created := timePart("created", d.CreatedAt); created != "" {
					b.WriteString("," + created)
				}
				if next := timePart("next attempt", d.NextAttemptAt); d.Status == prro.DeliveryFailed && next != "" {
					b.WriteString("," + next)
				}
				if d.LastError != "" {
					b.WriteString(", last error: " + d.LastError)
				}
			}
			return b.String()
		},
	})
}
