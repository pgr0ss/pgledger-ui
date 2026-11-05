package ledger

import (
	"fmt"
	"math/big"
	"strings"
	"time"
)

type GridColumn struct{ AccountID, Name, Currency string }

// GridCell has Sign 0 when the transfer has no entry in the column.
type GridCell struct {
	EntryID, Amount, BalanceAfter string
	Sign                          int
}

type GridRow struct {
	TransferID         string
	CreatedAt, EventAt time.Time
	Metadata           Metadata
	Cells              []GridCell
	// Tx numbers, from 1 in order of first appearance, the transactions that
	// created more than one of the grid's rows; it is 0 for a lone row. TxSize
	// counts the transaction's rows; TxStart and TxEnd mark where a run of
	// adjacent rows from it begins and ends.
	Tx, TxSize     int
	TxStart, TxEnd bool
}

type GridTotal struct{ In, Out, Net string }

type Grid struct {
	Columns   []GridColumn
	Rows      []GridRow
	Totals    []GridTotal
	Truncated bool
}

type colSums struct {
	in, out big.Rat
	scale   int
}

// BuildGrid lays entries out with one column per account, in order of first
// appearance, and one row per transfer, in input order.
func BuildGrid(entries []GridEntry, truncated bool) (Grid, error) {
	g := Grid{Truncated: truncated}
	colIdx := map[string]int{}
	rowIdx := map[string]int{}
	var sums []*colSums
	type placed struct{ row, col int }
	var cells []placed

	for _, e := range entries {
		c, ok := colIdx[e.AccountID]
		if !ok {
			c = len(g.Columns)
			colIdx[e.AccountID] = c
			g.Columns = append(g.Columns, GridColumn{AccountID: e.AccountID, Name: e.AccountName, Currency: e.Currency})
			sums = append(sums, &colSums{})
		}
		r, ok := rowIdx[e.TransferID]
		if !ok {
			r = len(g.Rows)
			rowIdx[e.TransferID] = r
			g.Rows = append(g.Rows, GridRow{TransferID: e.TransferID, CreatedAt: e.CreatedAt, EventAt: e.EventAt, Metadata: e.Metadata})
		}

		var v big.Rat
		if _, ok := v.SetString(e.Amount); !ok {
			return Grid{}, fmt.Errorf("amount %q: invalid", e.Amount)
		}
		s := sums[c]
		if v.Sign() > 0 {
			s.in.Add(&s.in, &v)
		} else {
			s.out.Add(&s.out, &v)
		}
		s.scale = max(s.scale, scale(e.Amount))
		cells = append(cells, placed{r, c})
	}

	for i := range g.Rows {
		g.Rows[i].Cells = make([]GridCell, len(g.Columns))
	}
	for i, p := range cells {
		e := entries[i]
		g.Rows[p.row].Cells[p.col] = GridCell{EntryID: e.EntryID, Amount: e.Amount, BalanceAfter: e.BalanceAfter, Sign: sign(e.Amount)}
	}
	groupTransactions(g.Rows)

	g.Totals = make([]GridTotal, len(sums))
	for i, s := range sums {
		var net big.Rat
		net.Add(&s.in, &s.out)
		g.Totals[i] = GridTotal{In: s.in.FloatString(s.scale), Out: s.out.FloatString(s.scale), Net: net.FloatString(s.scale)}
	}
	return g, nil
}

// groupTransactions relies on pgledger stamping created_at with now(), the
// transaction's start time: rows sharing a created_at came from one
// transaction, unless two transactions began in the same microsecond.
func groupTransactions(rows []GridRow) {
	sizes := map[int64]int{}
	for _, r := range rows {
		sizes[r.CreatedAt.UnixMicro()]++
	}
	numbers := map[int64]int{}
	for i := range rows {
		r := &rows[i]
		at := r.CreatedAt.UnixMicro()
		if sizes[at] < 2 {
			continue
		}
		if numbers[at] == 0 {
			numbers[at] = len(numbers) + 1
		}
		r.Tx, r.TxSize = numbers[at], sizes[at]
		r.TxStart = i == 0 || rows[i-1].Tx != r.Tx
		r.TxEnd = i == len(rows)-1 || rows[i+1].CreatedAt.UnixMicro() != at
	}
}

func scale(amount string) int {
	if i := strings.IndexByte(amount, '.'); i >= 0 {
		return len(amount) - i - 1
	}
	return 0
}

func sign(amount string) int {
	if strings.HasPrefix(amount, "-") {
		return -1
	}
	if strings.Trim(amount, "0.") == "" {
		return 0
	}
	return 1
}
