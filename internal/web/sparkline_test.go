package web_test

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/pgr0ss/pgledger_visualizer/internal/ledger"
	"github.com/pgr0ss/pgledger_visualizer/internal/web"
)

var t0 = time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

func points(at []time.Time, balances ...string) []ledger.BalancePoint {
	out := make([]ledger.BalancePoint, len(balances))
	for i, b := range balances {
		out[i] = ledger.BalancePoint{At: at[i], Balance: b}
	}
	return out
}

func hours(n int) []time.Time {
	out := make([]time.Time, n)
	for i := range out {
		out[i] = t0.Add(time.Duration(i) * time.Hour)
	}
	return out
}

func TestBuildSparkline(t *testing.T) {
	s, err := web.BuildSparkline(points(hours(3), "0", "50", "25"), 600, 80)
	require.NoError(t, err)
	assert.Equal(t, "0.0,80.0 300.0,0.0 600.0,40.0", s.Points)
	assert.False(t, s.HasZero)
	assert.Equal(t, "0", s.Min)
	assert.Equal(t, "50", s.Max)
}

func TestBuildSparklineZeroLine(t *testing.T) {
	s, err := web.BuildSparkline(points(hours(2), "-10", "10"), 600, 80)
	require.NoError(t, err)
	assert.True(t, s.HasZero)
	assert.InDelta(t, 40.0, s.ZeroY, 1e-9)
}

func TestBuildSparklineFlat(t *testing.T) {
	s, err := web.BuildSparkline(points(hours(3), "5", "5", "5"), 600, 80)
	require.NoError(t, err)
	for p := range strings.FieldsSeq(s.Points) {
		assert.True(t, strings.HasSuffix(p, ",40.0"), p)
	}

	same := []time.Time{t0, t0, t0}
	s, err = web.BuildSparkline(points(same, "1", "2", "3"), 600, 80)
	require.NoError(t, err)
	assert.Equal(t, "0.0,80.0 300.0,40.0 600.0,0.0", s.Points)
}

func TestBuildSparklineEdges(t *testing.T) {
	s, err := web.BuildSparkline(points(hours(1), "1"), 600, 80)
	require.NoError(t, err)
	assert.Nil(t, s)

	_, err = web.BuildSparkline(points(hours(2), "1", "abc"), 600, 80)
	assert.EqualError(t, err, `balance "abc": invalid`)
}
