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

func yLabels(c *web.Chart) []string {
	var out []string
	for _, tick := range c.YTicks {
		out = append(out, tick.Label)
	}
	return out
}

func TestBuildChart(t *testing.T) {
	c, err := web.BuildChart(points(hours(3), "0", "50", "25"), 600, 80)
	require.NoError(t, err)
	assert.Equal(t, "0.0,80.0 300.0,80.0 300.0,13.3 600.0,13.3 600.0,46.7", c.Points)
	assert.False(t, c.HasZero)
	assert.Equal(t, "0", c.Min)
	assert.Equal(t, "50", c.Max)

	assert.Equal(t, []string{"60", "40", "20", "0"}, yLabels(c))
	assert.InDelta(t, 0.0, c.YTicks[0].Pos, 1e-9)
	assert.InDelta(t, 100.0, c.YTicks[3].Pos, 1e-9)

	want := []web.XTick{
		{At: t0, Pos: 0, Align: "start"},
		{At: t0.Add(40 * time.Minute), Pos: 100.0 / 3, Align: "middle"},
		{At: t0.Add(80 * time.Minute), Pos: 200.0 / 3, Align: "middle"},
		{At: t0.Add(2 * time.Hour), Pos: 100, Align: "end"},
	}
	require.Len(t, c.XTicks, len(want))
	for i, w := range want {
		assert.Equal(t, w.At, c.XTicks[i].At)
		assert.Equal(t, w.Align, c.XTicks[i].Align)
		assert.InDelta(t, w.Pos, c.XTicks[i].Pos, 1e-9)
	}

	assert.JSONEq(t, `[
		{"x": 0, "y": 100, "b": "0", "t": "2026-01-01T00:00:00Z"},
		{"x": 50, "y": 16.666666666666664, "b": "50", "t": "2026-01-01T01:00:00Z"},
		{"x": 100, "y": 58.333333333333336, "b": "25", "t": "2026-01-01T02:00:00Z"}
	]`, c.Data)
}

func TestBuildChartZeroLine(t *testing.T) {
	c, err := web.BuildChart(points(hours(2), "-10", "10"), 600, 80)
	require.NoError(t, err)
	assert.True(t, c.HasZero)
	assert.InDelta(t, 40.0, c.ZeroY, 1e-9)
	assert.Equal(t, []string{"10", "5", "0", "-5", "-10"}, yLabels(c))
}

func TestBuildChartFlat(t *testing.T) {
	c, err := web.BuildChart(points(hours(3), "5", "5", "5"), 600, 80)
	require.NoError(t, err)
	for p := range strings.FieldsSeq(c.Points) {
		assert.True(t, strings.HasSuffix(p, ",40.0"), p)
	}
	assert.Equal(t, []string{"6.0", "5.5", "5.0", "4.5", "4.0"}, yLabels(c))

	same := []time.Time{t0, t0, t0}
	c, err = web.BuildChart(points(same, "1", "2", "3"), 600, 80)
	require.NoError(t, err)
	assert.Equal(t, "0.0,80.0 300.0,80.0 300.0,40.0 600.0,40.0 600.0,0.0", c.Points)
	assert.Equal(t, []web.XTick{{At: t0, Pos: 0, Align: "start"}}, c.XTicks)
}

func TestBuildChartEdges(t *testing.T) {
	c, err := web.BuildChart(points(hours(1), "1"), 600, 80)
	require.NoError(t, err)
	assert.Nil(t, c)

	_, err = web.BuildChart(points(hours(2), "1", "abc"), 600, 80)
	assert.EqualError(t, err, `balance "abc": invalid`)
}
