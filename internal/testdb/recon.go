package testdb

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/pgr0ss/pgledger-visualizer/internal/db"
)

// RefundSentEventAt is the event_at of the p_123 refund_sent transfer.
var RefundSentEventAt = time.Date(2025, 7, 21, 12, 45, 54, 123_000_000, time.UTC)

// ReconData holds the ids created by Recon. Transfers are in creation order.
type ReconData struct {
	URL       string
	Admin     *pgxpool.Pool
	Accounts  map[string]string
	Transfers []ReconTransfer
}

type ReconTransfer struct {
	ID, Kind, PaymentID string
}

// Transfer returns the first transfer with the given payment id and kind.
func (r ReconData) Transfer(t *testing.T, paymentID, kind string) ReconTransfer {
	t.Helper()
	for _, tr := range r.Transfers {
		if tr.PaymentID == paymentID && tr.Kind == kind {
			return tr
		}
	}
	t.Fatalf("no transfer %s/%s", paymentID, kind)
	return ReconTransfer{}
}

// Recon reproduces pgledger's examples/reconciliation.sql in a fresh schema,
// plus transfers with non-object metadata and accounts a_b and axb.
func Recon(t *testing.T) ReconData {
	t.Helper()
	url, admin := New(t)
	ctx := context.Background()
	q := db.New(admin)
	r := ReconData{URL: url, Admin: admin, Accounts: map[string]string{}}

	for _, name := range []string{
		"user1.external", "user1.receivables", "user1.available", "user1.pending_outbound", "a_b", "axb",
	} {
		id, err := q.CreateAccount(ctx, db.CreateAccountParams{
			Name: name, Currency: "USD", AllowNegativeBalance: true, AllowPositiveBalance: true,
		})
		if err != nil {
			t.Fatalf("create account %s: %v", name, err)
		}
		r.Accounts[name] = id
	}

	type step struct {
		from, to, amount, kind, paymentID, metadata string
		eventAt                                     time.Time
	}
	steps := []step{
		{"user1.external", "user1.receivables", "50.00", "payment_created", "p_123", "", time.Time{}},
		{"user1.external", "user1.receivables", "50.00", "payment_created", "p_456", "", time.Time{}},
		{"user1.receivables", "user1.available", "50.00", "payment_received", "p_456", "", time.Time{}},
		{"user1.receivables", "user1.available", "49.50", "payment_received", "p_123", "", time.Time{}},
		{"user1.available", "user1.pending_outbound", "20.00", "refund_created", "p_123", "", time.Time{}},
		{
			"user1.pending_outbound", "user1.external", "20.00", "refund_sent", "p_123",
			`{"kind": "refund_sent", "payment_id": "p_123", "webhook_id": "webhook_123"}`, RefundSentEventAt,
		},
		{"a_b", "axb", "1.00", "", "", `[1, 2]`, time.Time{}},
		{"a_b", "axb", "2.00", "", "", `"str"`, time.Time{}},
	}
	for _, s := range steps {
		md := s.metadata
		if md == "" {
			md = `{"kind": "` + s.kind + `", "payment_id": "` + s.paymentID + `"}`
		}
		eventAt := s.eventAt
		if eventAt.IsZero() {
			eventAt = time.Now()
		}
		id, err := q.CreateTransfer(ctx, db.CreateTransferParams{
			FromAccountID: r.Accounts[s.from], ToAccountID: r.Accounts[s.to], Amount: s.amount,
			EventAt: eventAt, Metadata: []byte(md),
		})
		if err != nil {
			t.Fatalf("create transfer %s: %v", s.kind, err)
		}
		r.Transfers = append(r.Transfers, ReconTransfer{ID: id, Kind: s.kind, PaymentID: s.paymentID})
	}
	return r
}
