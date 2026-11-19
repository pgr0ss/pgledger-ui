package ledger

import (
	"context"
	"errors"
	"slices"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/pgr0ss/pgledger-ui/internal/db"
)

const (
	PageSize          = 50
	SearchLimit       = 8
	MaxGridTransfers  = 500
	ChartPoints       = 500
	TopCounterparties = 20
)

var ErrNotFound = errors.New("not found")

type Store struct {
	// pool is nil for a Store bound to a View transaction.
	pool                       *pgxpool.Pool
	q                          *db.Queries
	idempotencyKeysTableExists bool
}

// NewStore detects whether pgledger_idempotency_keys_view exists; pgledger
// versions before idempotency keys lack it.
func NewStore(ctx context.Context, pool *pgxpool.Pool) (*Store, error) {
	q := db.New(pool)
	keys, err := q.IdempotencyKeysInstalled(ctx)
	if err != nil {
		return nil, err
	}
	return &Store{pool: pool, q: q, idempotencyKeysTableExists: keys}, nil
}

// View runs fn against one REPEATABLE READ, READ ONLY snapshot. Inside fn,
// View reuses the same snapshot.
func (s *Store) View(ctx context.Context, fn func(*Store) error) error {
	if s.pool == nil {
		return fn(s)
	}
	opts := pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly}
	return pgx.BeginTxFunc(ctx, s.pool, opts, func(tx pgx.Tx) error {
		return fn(&Store{q: s.q.WithTx(tx), idempotencyKeysTableExists: s.idempotencyKeysTableExists})
	})
}

// IdempotencyKeysTableExists reports whether transfers carry idempotency keys.
func (s *Store) IdempotencyKeysTableExists() bool { return s.idempotencyKeysTableExists }

type Account struct {
	ID, Name, Currency, Balance                string
	Version                                    int64
	AllowNegativeBalance, AllowPositiveBalance bool
	Metadata                                   Metadata
	CreatedAt, UpdatedAt                       time.Time
}

type Transfer struct {
	ID, FromAccountID, FromName, ToAccountID, ToName, Currency, Amount string
	CreatedAt, EventAt                                                 time.Time
	Metadata                                                           Metadata
	// IdempotencyKey is empty for transfers created without one. Transfers
	// created by one call share its key.
	IdempotencyKey string
}

type Entry struct {
	ID, AccountID, AccountName, Currency, TransferID, Amount, PreviousBalance, CurrentBalance string
	AccountVersion                                                                            int64
	CreatedAt, EventAt                                                                        time.Time
	Metadata                                                                                  Metadata
	CounterpartyID, CounterpartyName                                                          string
}

type MetaPair struct {
	Key, Value string
	Transfers  int64
}

type KeyCount struct {
	Key       string
	Transfers int64
}

type TraceValue struct {
	Value       string
	Transfers   int64
	First, Last time.Time
}

type Balance struct {
	Amount, EntryID string
	Entries         int64
}

type BalancePoint struct {
	At      time.Time
	Balance string
	// EntryID and TransferID are the entry that produced Balance and its
	// transfer; empty for the synthetic start and end points added by Series.
	EntryID, TransferID string
}

type Counterparty struct {
	AccountID, Name, Amount string
}

// Flows are an account's all-time transfer totals in each direction, with the
// counterparties that moved the most.
type Flows struct {
	In, Out       string
	TopIn, TopOut []Counterparty
}

type GridEntry struct {
	EntryID, TransferID, AccountID, AccountName, Currency, Amount, BalanceAfter string
	CreatedAt, EventAt                                                          time.Time
	Metadata                                                                    Metadata
}

type Order string

const (
	OrderCreated Order = "created_at"
	OrderEvent   Order = "event_at"
)

func ParseOrder(s string) Order {
	if Order(s) == OrderEvent {
		return OrderEvent
	}
	return OrderCreated
}

type AccountFilter struct {
	Q, MetaKey, MetaValue string
	Offset, Limit         int
}

type TransferFilter struct {
	Q, Before string
	Limit     int
}

type EntryFilter struct {
	Q, AccountID, Before string
	Created              TimeRange
	Limit                int
}

type GridFilter struct {
	MetaKey, MetaValue, AccountID string
	Created                       TimeRange
}

// TimeRange bounds created_at inclusively; a zero bound is open.
type TimeRange struct {
	From, To time.Time
}

func timestamptz(t time.Time) pgtype.Timestamptz {
	return pgtype.Timestamptz{Time: t, Valid: !t.IsZero()}
}

var likeEscaper = strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)

func escapeLike(s string) string { return likeEscaper.Replace(s) }

func patterns(q string) (prefix, contains string) {
	e := escapeLike(q)
	return e + "%", "%" + e + "%"
}

func notFound(err error) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	return err
}

func trim[T any](rows []T, limit int) ([]T, bool) {
	if len(rows) > limit {
		return rows[:limit], true
	}
	return rows, false
}

func toAccount(r db.ListAccountsRow) Account {
	return Account{
		ID: r.ID, Name: r.Name, Currency: r.Currency, Balance: r.Balance, Version: r.Version,
		AllowNegativeBalance: r.AllowNegativeBalance, AllowPositiveBalance: r.AllowPositiveBalance,
		Metadata: ParseMetadata(r.Metadata), CreatedAt: r.CreatedAt, UpdatedAt: r.UpdatedAt,
	}
}

func toTransfer(r db.ListTransfersRow) Transfer {
	return Transfer{
		ID: r.ID, FromAccountID: r.FromAccountID, FromName: r.FromName, ToAccountID: r.ToAccountID,
		ToName: r.ToName, Currency: r.Currency, Amount: r.Amount, CreatedAt: r.CreatedAt,
		EventAt: r.EventAt, Metadata: ParseMetadata(r.Metadata), IdempotencyKey: r.IdempotencyKey,
	}
}

func toEntry(r db.ListEntriesRow) Entry {
	return Entry{
		ID: r.ID, AccountID: r.AccountID, AccountName: r.AccountName, Currency: r.Currency,
		TransferID: r.TransferID, Amount: r.Amount, PreviousBalance: r.PreviousBalance,
		CurrentBalance: r.CurrentBalance, AccountVersion: r.AccountVersion, CreatedAt: r.CreatedAt,
		EventAt: r.EventAt, Metadata: ParseMetadata(r.Metadata), CounterpartyID: r.CounterpartyID,
		CounterpartyName: r.CounterpartyName,
	}
}

func (s *Store) ListAccounts(ctx context.Context, f AccountFilter) ([]Account, bool, error) {
	prefix, contains := patterns(f.Q)
	rows, err := s.q.ListAccounts(ctx, db.ListAccountsParams{
		Prefix: prefix, Contains: contains, MetaKey: f.MetaKey, MetaValue: f.MetaValue,
		RowOffset: int64(f.Offset), RowLimit: int64(f.Limit + 1),
	})
	if err != nil {
		return nil, false, err
	}
	rows, more := trim(rows, f.Limit)
	out := make([]Account, len(rows))
	for i, r := range rows {
		out[i] = toAccount(r)
	}
	return out, more, nil
}

func (s *Store) GetAccount(ctx context.Context, id string) (Account, error) {
	r, err := s.q.GetAccount(ctx, id)
	if err != nil {
		return Account{}, notFound(err)
	}
	return toAccount(db.ListAccountsRow(r)), nil
}

func (s *Store) ListTransfers(ctx context.Context, f TransferFilter) ([]Transfer, bool, error) {
	prefix, contains := patterns(f.Q)
	p := db.ListTransfersParams{Prefix: prefix, Contains: contains, Before: f.Before, RowLimit: int64(f.Limit + 1)}
	var out []Transfer
	if s.idempotencyKeysTableExists {
		rows, err := s.q.ListTransfers(ctx, p)
		if err != nil {
			return nil, false, err
		}
		out = make([]Transfer, len(rows))
		for i, r := range rows {
			out[i] = toTransfer(r)
		}
	} else {
		rows, err := s.q.ListTransfersNoKeys(ctx, db.ListTransfersNoKeysParams(p))
		if err != nil {
			return nil, false, err
		}
		out = make([]Transfer, len(rows))
		for i, r := range rows {
			out[i] = toTransfer(db.ListTransfersRow(r))
		}
	}
	out, more := trim(out, f.Limit)
	return out, more, nil
}

func (s *Store) GetTransfer(ctx context.Context, id string) (Transfer, error) {
	var r db.ListTransfersRow
	if s.idempotencyKeysTableExists {
		row, err := s.q.GetTransfer(ctx, id)
		if err != nil {
			return Transfer{}, notFound(err)
		}
		r = db.ListTransfersRow(row)
	} else {
		row, err := s.q.GetTransferNoKeys(ctx, id)
		if err != nil {
			return Transfer{}, notFound(err)
		}
		r = db.ListTransfersRow(row)
	}
	return toTransfer(r), nil
}

func (s *Store) ListEntries(ctx context.Context, f EntryFilter) ([]Entry, bool, error) {
	prefix, contains := patterns(f.Q)
	rows, err := s.q.ListEntries(ctx, db.ListEntriesParams{
		Prefix: prefix, Contains: contains, AccountID: f.AccountID, Before: f.Before,
		CreatedFrom: timestamptz(f.Created.From), CreatedTo: timestamptz(f.Created.To),
		RowLimit: int64(f.Limit + 1),
	})
	if err != nil {
		return nil, false, err
	}
	rows, more := trim(rows, f.Limit)
	out := make([]Entry, len(rows))
	for i, r := range rows {
		out[i] = toEntry(r)
	}
	return out, more, nil
}

func (s *Store) TransferEntries(ctx context.Context, transferID string) ([]Entry, error) {
	rows, err := s.q.ListTransferEntries(ctx, transferID)
	if err != nil {
		return nil, err
	}
	out := make([]Entry, len(rows))
	for i, r := range rows {
		out[i] = toEntry(db.ListEntriesRow(r))
	}
	return out, nil
}

func (s *Store) GetEntry(ctx context.Context, id string) (Entry, error) {
	r, err := s.q.GetEntry(ctx, id)
	if err != nil {
		return Entry{}, notFound(err)
	}
	return toEntry(db.ListEntriesRow(r)), nil
}

func (s *Store) MetadataPairs(ctx context.Context, q string, limit int) ([]MetaPair, error) {
	_, contains := patterns(q)
	rows, err := s.q.MetadataPairs(ctx, db.MetadataPairsParams{Pattern: contains, RowLimit: int64(limit)})
	if err != nil {
		return nil, err
	}
	out := make([]MetaPair, len(rows))
	for i, r := range rows {
		out[i] = MetaPair{Key: r.MetaKey, Value: r.MetaValue, Transfers: r.Transfers}
	}
	return out, nil
}

func (s *Store) TraceKeys(ctx context.Context, q string, limit int) ([]KeyCount, error) {
	_, contains := patterns(q)
	rows, err := s.q.TraceKeys(ctx, db.TraceKeysParams{Pattern: contains, RowLimit: int64(limit)})
	if err != nil {
		return nil, err
	}
	out := make([]KeyCount, len(rows))
	for i, r := range rows {
		out[i] = KeyCount{Key: r.MetaKey, Transfers: r.Transfers}
	}
	return out, nil
}

func (s *Store) TraceValues(ctx context.Context, key, q string, limit int) ([]TraceValue, error) {
	_, contains := patterns(q)
	rows, err := s.q.TraceValues(ctx, db.TraceValuesParams{MetaKey: key, Pattern: contains, RowLimit: int64(limit)})
	if err != nil {
		return nil, err
	}
	out := make([]TraceValue, len(rows))
	for i, r := range rows {
		out[i] = TraceValue{Value: r.MetaValue, Transfers: r.Transfers, First: r.FirstAt, Last: r.LastAt}
	}
	return out, nil
}

// BalanceAt answers with the stored running balance in created order. Event
// order sums entries, because account_current_balance follows created order.
func (s *Store) BalanceAt(ctx context.Context, accountID string, at time.Time, order Order) (Balance, error) {
	if order == OrderEvent {
		r, err := s.q.BalanceAtEvent(ctx, db.BalanceAtEventParams{AccountID: accountID, At: at})
		if err != nil {
			return Balance{}, err
		}
		return Balance{Amount: r.Amount, Entries: r.Entries}, nil
	}
	r, err := s.q.BalanceAtCreated(ctx, db.BalanceAtCreatedParams{AccountID: accountID, At: at})
	if errors.Is(err, pgx.ErrNoRows) {
		return Balance{Amount: "0"}, nil
	}
	if err != nil {
		return Balance{}, err
	}
	return Balance{Amount: r.Amount, EntryID: r.EntryID, Entries: r.Entries}, nil
}

// BalanceHistory is up to limit of an account's most recent balances.
type BalanceHistory struct {
	// Points are balances after each entry, oldest first.
	Points []BalancePoint
	// Start is the balance before the oldest point.
	Start string
	// Complete reports that Points begin with the first entry in the range.
	Complete bool
}

// Series returns the points preceded by the starting balance and, when end is
// set, followed by the last balance held until end. A complete history starts
// at start; a truncated one at its oldest point.
func (h BalanceHistory) Series(start, end time.Time) []BalancePoint {
	if len(h.Points) == 0 {
		return nil
	}
	first := BalancePoint{At: h.Points[0].At, Balance: h.Start}
	if h.Complete {
		first.At = start
	}
	out := append([]BalancePoint{first}, h.Points...)
	if !end.IsZero() {
		out = append(out, BalancePoint{At: end, Balance: h.Points[len(h.Points)-1].Balance})
	}
	return out
}

func (s *Store) BalanceHistory(ctx context.Context, accountID string, r TimeRange, limit int) (BalanceHistory, error) {
	rows, err := s.q.BalanceHistory(ctx, db.BalanceHistoryParams{
		AccountID: accountID, CreatedFrom: timestamptz(r.From), CreatedTo: timestamptz(r.To),
		RowLimit: int64(limit + 1),
	})
	if err != nil || len(rows) == 0 {
		return BalanceHistory{}, err
	}
	rows, truncated := trim(rows, limit)
	h := BalanceHistory{Points: make([]BalancePoint, len(rows)), Complete: !truncated}
	for i, row := range rows {
		h.Points[i] = BalancePoint{At: row.CreatedAt, Balance: row.Balance, EntryID: row.ID, TransferID: row.TransferID}
	}
	slices.Reverse(h.Points)
	h.Start = rows[len(rows)-1].PreviousBalance
	return h, nil
}

func (s *Store) AccountFlows(ctx context.Context, accountID string, r TimeRange, top int) (Flows, error) {
	rows, err := s.q.AccountFlows(ctx, db.AccountFlowsParams{
		AccountID: accountID, CreatedFrom: timestamptz(r.From), CreatedTo: timestamptz(r.To), Top: int64(top),
	})
	if err != nil {
		return Flows{}, err
	}
	f := Flows{In: "0", Out: "0"}
	for _, row := range rows {
		c := Counterparty{AccountID: row.CounterpartyID, Name: row.CounterpartyName, Amount: row.Amount}
		if row.Incoming {
			f.In, f.TopIn = row.Total, append(f.TopIn, c)
		} else {
			f.Out, f.TopOut = row.Total, append(f.TopOut, c)
		}
	}
	return f, nil
}

// GridEntries returns the entries of the most recent maxTransfers matching
// transfers, oldest first; truncated reports that older transfers exist.
func (s *Store) GridEntries(ctx context.Context, f GridFilter, order Order, maxTransfers int) ([]GridEntry, bool, error) {
	rows, err := s.q.GridEntries(ctx, db.GridEntriesParams{
		ByEvent: order == OrderEvent, AccountID: f.AccountID, MetaKey: f.MetaKey, MetaValue: f.MetaValue,
		CreatedFrom: timestamptz(f.Created.From), CreatedTo: timestamptz(f.Created.To),
		MaxTransfers: int64(maxTransfers + 1),
	})
	if err != nil {
		return nil, false, err
	}
	transfers := map[string]bool{}
	for _, r := range rows {
		transfers[r.TransferID] = true
	}
	truncated := len(transfers) > maxTransfers
	if truncated {
		first := rows[0].TransferID
		for len(rows) > 0 && rows[0].TransferID == first {
			rows = rows[1:]
		}
	}
	out := make([]GridEntry, len(rows))
	for i, r := range rows {
		out[i] = GridEntry{
			EntryID: r.EntryID, TransferID: r.TransferID, AccountID: r.AccountID, AccountName: r.AccountName,
			Currency: r.Currency, Amount: r.Amount, BalanceAfter: r.BalanceAfter, CreatedAt: r.CreatedAt,
			EventAt: r.EventAt, Metadata: ParseMetadata(r.Metadata),
		}
	}
	return out, truncated, nil
}
