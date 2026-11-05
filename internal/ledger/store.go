package ledger

import (
	"context"
	"errors"
	"slices"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/pgr0ss/pgledger_visualizer/internal/db"
)

const (
	PageSize         = 50
	SearchLimit      = 8
	MaxGridTransfers = 500
	ChartPoints      = 500
)

var ErrNotFound = errors.New("not found")

type Store struct{ q *db.Queries }

func NewStore(pool *pgxpool.Pool) *Store { return &Store{q: db.New(pool)} }

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
}

type GridEntry struct {
	EntryID, TransferID, AccountID, AccountName, Currency, Amount, BalanceAfter string
	CreatedAt, EventAt                                                          time.Time
	Metadata                                                                    Metadata
}

type Order string

const (
	OrderCreated Order = "created"
	OrderEvent   Order = "event"
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
	Limit                int
}

type GridFilter struct {
	MetaKey, MetaValue, AccountID string
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
		EventAt: r.EventAt, Metadata: ParseMetadata(r.Metadata),
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
		RowOffset: int32(f.Offset), RowLimit: int32(f.Limit + 1),
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
	rows, err := s.q.ListTransfers(ctx, db.ListTransfersParams{
		Prefix: prefix, Contains: contains, Before: f.Before, RowLimit: int32(f.Limit + 1),
	})
	if err != nil {
		return nil, false, err
	}
	rows, more := trim(rows, f.Limit)
	out := make([]Transfer, len(rows))
	for i, r := range rows {
		out[i] = toTransfer(r)
	}
	return out, more, nil
}

func (s *Store) GetTransfer(ctx context.Context, id string) (Transfer, error) {
	r, err := s.q.GetTransfer(ctx, id)
	if err != nil {
		return Transfer{}, notFound(err)
	}
	return toTransfer(db.ListTransfersRow(r)), nil
}

func (s *Store) ListEntries(ctx context.Context, f EntryFilter) ([]Entry, bool, error) {
	prefix, contains := patterns(f.Q)
	rows, err := s.q.ListEntries(ctx, db.ListEntriesParams{
		Prefix: prefix, Contains: contains, AccountID: f.AccountID, Before: f.Before,
		RowLimit: int32(f.Limit + 1),
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
	rows, err := s.q.MetadataPairs(ctx, db.MetadataPairsParams{Pattern: contains, RowLimit: int32(limit)})
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
	rows, err := s.q.TraceKeys(ctx, db.TraceKeysParams{Pattern: contains, RowLimit: int32(limit)})
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
	rows, err := s.q.TraceValues(ctx, db.TraceValuesParams{MetaKey: key, Pattern: contains, RowLimit: int32(limit)})
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

// BalanceHistory returns up to limit of the most recent balances, oldest first.
func (s *Store) BalanceHistory(ctx context.Context, accountID string, limit int) ([]BalancePoint, error) {
	rows, err := s.q.BalanceHistory(ctx, db.BalanceHistoryParams{AccountID: accountID, RowLimit: int32(limit)})
	if err != nil {
		return nil, err
	}
	out := make([]BalancePoint, len(rows))
	for i, r := range rows {
		out[i] = BalancePoint{At: r.CreatedAt, Balance: r.Balance}
	}
	slices.Reverse(out)
	return out, nil
}

// GridEntries returns the entries of the most recent maxTransfers matching
// transfers, oldest first; truncated reports that older transfers exist.
func (s *Store) GridEntries(ctx context.Context, f GridFilter, order Order, maxTransfers int) ([]GridEntry, bool, error) {
	rows, err := s.q.GridEntries(ctx, db.GridEntriesParams{
		ByEvent: order == OrderEvent, AccountID: f.AccountID, MetaKey: f.MetaKey, MetaValue: f.MetaValue,
		MaxTransfers: int32(maxTransfers + 1),
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
