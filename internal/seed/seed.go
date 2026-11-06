// Package seed loads deterministic sample payment flows into pgledger.
package seed

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"math/rand/v2"
	"slices"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/pgr0ss/pgledger_visualizer/internal/db"
	"github.com/pgr0ss/pgledger_visualizer/internal/pgschema"
)

type Options struct {
	Users         int
	Seed          uint64
	Now           time.Time
	InstallSchema bool
}

type Summary struct {
	Accounts, Transfers int
}

// step is one planned transfer. For FX steps, the second leg is from2 → to2.
type step struct {
	at, eventAt    time.Time
	from, to       string
	cents          int64
	from2, to2     string
	cents2         int64
	metadata       map[string]string
	idempotencyKey string
	dependsOn      int
	debitsBalance  bool
}

type user struct {
	external, receivables, available, pendingOutbound, eurAvailable string
}

const day = 24 * time.Hour

// accountsCreatedAt predates every planned step, which start at Now-30d.
func accountsCreatedAt(now time.Time) time.Time { return now.Add(-30*day - time.Hour) }

func Run(ctx context.Context, conn *pgx.Conn, opts Options) (Summary, error) {
	var sum Summary
	err := pgx.BeginFunc(ctx, conn, func(tx pgx.Tx) error {
		q := db.New(tx)
		installed, err := q.SchemaInstalled(ctx)
		if err != nil {
			return err
		}
		if !installed {
			if !opts.InstallSchema {
				return errors.New("pgledger schema not installed; rerun with --install-schema")
			}
			if err := pgschema.Install(ctx, tx.Conn().PgConn()); err != nil {
				return err
			}
		}
		present, err := q.SampleDataPresent(ctx)
		if err != nil {
			return err
		}
		if present {
			return errors.New("sample data already present (account liquidity.usd exists); use an empty database (mise run dbreset)")
		}

		var accountIDs []string
		create := func(name, currency string, allowNegative bool, metadata map[string]string) (string, error) {
			md, err := json.Marshal(metadata)
			if err != nil {
				return "", err
			}
			id, err := q.CreateAccount(ctx, db.CreateAccountParams{
				Name: name, Currency: currency, AllowNegativeBalance: allowNegative, AllowPositiveBalance: true, Metadata: md,
			})
			if err != nil {
				return "", fmt.Errorf("create account %s: %w", name, err)
			}
			sum.Accounts++
			accountIDs = append(accountIDs, id)
			return id, nil
		}

		system := map[string]string{"type": "system"}
		liquidityUSD, err := create("liquidity.usd", "USD", true, system)
		if err != nil {
			return err
		}
		liquidityEUR, err := create("liquidity.eur", "EUR", true, system)
		if err != nil {
			return err
		}
		fees, err := create("revenue.fees.usd", "USD", true, system)
		if err != nil {
			return err
		}

		users := make([]user, opts.Users)
		for i := range users {
			n := i + 1
			uid, prefix := fmt.Sprintf("u_%03d", n), fmt.Sprintf("user%03d", n)
			acct := func(suffix, currency string, allowNegative bool) (string, error) {
				return create(prefix+"."+suffix, currency, allowNegative, map[string]string{"user_id": uid, "role": suffix})
			}
			u := &users[i]
			for _, a := range []struct {
				dst           *string
				suffix        string
				allowNegative bool
			}{
				{&u.external, "external", true},
				{&u.receivables, "receivables", true},
				{&u.available, "available", false},
				{&u.pendingOutbound, "pending_outbound", true},
			} {
				if *a.dst, err = acct(a.suffix, "USD", a.allowNegative); err != nil {
					return err
				}
			}
			if n%4 == 0 {
				if u.eurAvailable, err = acct("eur.available", "EUR", false); err != nil {
					return err
				}
			}
		}

		steps := plan(users, opts, liquidityUSD, liquidityEUR, fees)
		order := make([]int, len(steps))
		for i := range order {
			order[i] = i
		}
		slices.SortStableFunc(order, func(a, b int) int { return steps[a].at.Compare(steps[b].at) })

		balances := map[string]int64{}
		skipped := make([]bool, len(steps))
		var lastAt time.Time
		for _, i := range order {
			s := steps[i]
			// Steps after Now haven't happened yet: their payments stay in flight.
			if s.at.After(opts.Now) || (s.dependsOn >= 0 && skipped[s.dependsOn]) {
				skipped[i] = true
				continue
			}
			if s.debitsBalance {
				if balances[s.from] < s.cents {
					skipped[i] = true
					continue
				}
				balances[s.from] -= s.cents
			}
			balances[s.to] += s.cents
			if s.to2 != "" {
				balances[s.to2] += s.cents2
			}
			// Each step stands for its own transaction, and pgledger's created_at is the transaction's
			// start time, so steps must not share one at Postgres's microsecond precision.
			at := s.at.Truncate(time.Microsecond)
			if !at.After(lastAt) {
				at = lastAt.Add(time.Microsecond)
			}
			lastAt = at

			md, err := json.Marshal(s.metadata)
			if err != nil {
				return err
			}
			var ids []string
			key := pgtype.Text{String: s.idempotencyKey, Valid: s.idempotencyKey != ""}
			if s.from2 != "" {
				if ids, err = q.CreateFxTransfers(ctx, db.CreateFxTransfersParams{
					From1: s.from, To1: s.to, Amount1: money(s.cents),
					From2: s.from2, To2: s.to2, Amount2: money(s.cents2),
					EventAt: s.eventAt, Metadata: md, IdempotencyKey: key,
				}); err != nil {
					return fmt.Errorf("create fx transfers %s: %w", s.metadata["payment_id"], err)
				}
			} else {
				id, err := q.CreateTransfer(ctx, db.CreateTransferParams{
					FromAccountID: s.from, ToAccountID: s.to, Amount: money(s.cents), EventAt: s.eventAt, Metadata: md,
					IdempotencyKey: key,
				})
				if err != nil {
					return fmt.Errorf("create transfer %s %s: %w", s.metadata["kind"], s.metadata["payment_id"], err)
				}
				ids = []string{id}
			}
			if err := q.BackdateTransfers(ctx, db.BackdateTransfersParams{CreatedAt: at, Ids: ids}); err != nil {
				return err
			}
			sum.Transfers += len(ids)
		}
		return q.BackdateAccounts(ctx, db.BackdateAccountsParams{CreatedAt: accountsCreatedAt(opts.Now), Ids: accountIDs})
	})
	return sum, err
}

func plan(users []user, opts Options, liquidityUSD, liquidityEUR, fees string) []step {
	rng := rand.New(rand.NewPCG(opts.Seed, opts.Seed)) //nolint:gosec // sample data must be reproducible from --seed
	hours := func(lo, hi int) time.Duration { return time.Duration(lo+rng.IntN(hi-lo+1)) * time.Hour }
	chance := func(pct int) bool { return rng.IntN(100) < pct }
	// Client-initiated steps usually carry an idempotency key. Keys draw from their own source so the
	// steps planned for a --seed don't depend on them.
	keyRNG := rand.New(rand.NewPCG(opts.Seed, ^opts.Seed)) //nolint:gosec // reproducible from --seed
	key := func() string {
		if keyRNG.IntN(100) >= 75 {
			return ""
		}
		return uuidV4(keyRNG)
	}

	var steps []step
	add := func(s step) int {
		if s.eventAt.IsZero() {
			s.eventAt = s.at
		}
		steps = append(steps, s)
		return len(steps) - 1
	}
	var payments, webhooks, fxs int

	for _, u := range users {
		for range 1 + rng.IntN(4) {
			payments++
			pid := fmt.Sprintf("p_%05d", payments)
			meta := func(kind string) map[string]string { return map[string]string{"payment_id": pid, "kind": kind} }
			t0 := opts.Now.Add(-30*day + time.Duration(rng.Int64N(int64(29*day))))
			amount := int64(1000 + rng.IntN(49001))

			add(step{
				at: t0, from: u.external, to: u.receivables, cents: amount, metadata: meta("payment_created"),
				idempotencyKey: key(), dependsOn: -1,
			})
			if !chance(85) {
				continue
			}
			receivedAt := t0.Add(hours(1, 48))
			received := amount
			if chance(10) {
				received -= int64(1 + rng.IntN(200))
			}
			add(step{at: receivedAt, from: u.receivables, to: u.available, cents: received, metadata: meta("payment_received"), dependsOn: -1})

			if chance(30) {
				refundAt := receivedAt.Add(hours(1, 72))
				refund := int64(1 + rng.Int64N(received))
				created := add(step{
					at: refundAt, from: u.available, to: u.pendingOutbound, cents: refund,
					metadata: meta("refund_created"), idempotencyKey: key(), dependsOn: -1, debitsBalance: true,
				})
				if chance(80) {
					sentAt := refundAt.Add(hours(1, 48))
					webhooks++
					md := meta("refund_sent")
					md["webhook_id"] = fmt.Sprintf("wh_%05d", webhooks)
					add(step{
						at: sentAt, eventAt: sentAt.Add(-hours(0, 6)), from: u.pendingOutbound, to: u.external,
						cents: refund, metadata: md, dependsOn: created,
					})
				}
			}
			if chance(20) {
				fee := max((received*15+500)/1000, 1)
				add(step{
					at: receivedAt.Add(hours(1, 24)), from: u.available, to: fees, cents: fee,
					metadata: meta("fee"), dependsOn: -1, debitsBalance: true,
				})
			}
			if u.eurAvailable != "" && chance(50) {
				fxMeta := func(from, to string) map[string]string {
					fxs++
					md := meta("fx")
					md["fx_id"] = fmt.Sprintf("fx_%05d", fxs)
					md["from_currency"], md["to_currency"] = from, to
					return md
				}
				fxAt := receivedAt.Add(hours(1, 24))
				usd := received / 2
				eur := usd * 92 / 100
				toEUR := add(step{
					at: fxAt, from: u.available, to: liquidityUSD, cents: usd,
					from2: liquidityEUR, to2: u.eurAvailable, cents2: eur,
					metadata: fxMeta("USD", "EUR"), idempotencyKey: key(), dependsOn: -1, debitsBalance: true,
				})
				if chance(50) {
					back := 1 + rng.Int64N(eur)
					add(step{
						at: fxAt.Add(hours(1, 72)), from: u.eurAvailable, to: liquidityEUR, cents: back,
						from2: liquidityUSD, to2: u.available, cents2: back * 100 / 92,
						metadata: fxMeta("EUR", "USD"), idempotencyKey: key(), dependsOn: toEUR, debitsBalance: true,
					})
				}
			}
		}
	}
	return steps
}

func money(cents int64) string {
	return fmt.Sprintf("%d.%02d", cents/100, cents%100)
}

// uuidV4 formats 16 bytes from r as a version 4 UUID.
func uuidV4(r *rand.Rand) string {
	var b [16]byte
	binary.BigEndian.PutUint64(b[:8], r.Uint64())
	binary.BigEndian.PutUint64(b[8:], r.Uint64())
	b[6] = b[6]&0x0f | 0x40
	b[8] = b[8]&0x3f | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[:4], b[4:6], b[6:8], b[8:10], b[10:])
}
