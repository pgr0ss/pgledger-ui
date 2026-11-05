package ledger_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/pgr0ss/pgledger_visualizer/internal/ledger"
)

func entry(transfer, account, amount string) ledger.GridEntry {
	return ledger.GridEntry{EntryID: "e_" + transfer + account, TransferID: transfer, AccountID: account, AccountName: account, Currency: "USD", Amount: amount}
}

func TestBuildGridReconciliation(t *testing.T) {
	entries := []ledger.GridEntry{
		entry("t1", "user1.external", "-50.00"),
		entry("t1", "user1.receivables", "50.00"),
		entry("t2", "user1.receivables", "-49.50"),
		entry("t2", "user1.available", "49.50"),
		entry("t3", "user1.available", "-20.00"),
		entry("t3", "user1.pending_outbound", "20.00"),
		entry("t4", "user1.pending_outbound", "-20.00"),
		entry("t4", "user1.external", "20.00"),
	}

	g, err := ledger.BuildGrid(entries, false)
	require.NoError(t, err)

	var names []string
	for _, c := range g.Columns {
		names = append(names, c.Name)
	}
	assert.Equal(t, []string{"user1.external", "user1.receivables", "user1.available", "user1.pending_outbound"}, names)

	require.Len(t, g.Rows, 4)
	var row2 []string
	for _, c := range g.Rows[1].Cells {
		row2 = append(row2, c.Amount)
	}
	assert.Equal(t, []string{"", "-49.50", "49.50", ""}, row2)
	assert.Equal(t, []int{0, -1, 1, 0}, []int{g.Rows[1].Cells[0].Sign, g.Rows[1].Cells[1].Sign, g.Rows[1].Cells[2].Sign, g.Rows[1].Cells[3].Sign})

	var nets []string
	for _, tot := range g.Totals {
		nets = append(nets, tot.Net)
	}
	assert.Equal(t, []string{"-30.00", "0.50", "29.50", "0.00"}, nets)
	assert.Equal(t, "20.00", g.Totals[0].In)
	assert.Equal(t, "-50.00", g.Totals[0].Out)
}

func TestBuildGridMixedScale(t *testing.T) {
	g, err := ledger.BuildGrid([]ledger.GridEntry{entry("t1", "a", "1.5"), entry("t2", "a", "2.25")}, true)
	require.NoError(t, err)
	assert.Equal(t, "3.75", g.Totals[0].Net)
	assert.True(t, g.Truncated)
}

func TestBuildGridInvalidAmount(t *testing.T) {
	_, err := ledger.BuildGrid([]ledger.GridEntry{entry("t1", "a", "abc")}, false)
	assert.EqualError(t, err, `amount "abc": invalid`)
}
