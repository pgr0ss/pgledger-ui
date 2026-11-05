package web

import (
	"encoding/json"
	"fmt"
	"math"
	"math/big"
	"strconv"
	"strings"
	"time"

	"github.com/pgr0ss/pgledger_visualizer/internal/ledger"
)

// Chart is an account's balance over time, drawn as a step line in a
// width×height SVG with HTML axis labels positioned in percent.
type Chart struct {
	Points   string
	ZeroY    float64
	HasZero  bool
	Min, Max string
	YTicks   []YTick
	XTicks   []XTick
	// LabelWidth is the longest y label in characters, for sizing the axis.
	LabelWidth int
	// Data is JSON for the hover readout and click-through:
	// [{x, y (percent), b (balance), t (RFC3339), id (transfer, if any)}].
	Data string
}

type YTick struct {
	Label string
	Y     float64 // SVG units from the top
	Pos   float64 // percent from the top
}

type XTick struct {
	At    time.Time
	Pos   float64 // percent from the left
	Align string  // start, middle or end
}

type chartPoint struct {
	X  float64 `json:"x"`
	Y  float64 `json:"y"`
	B  string  `json:"b"`
	T  string  `json:"t"`
	ID string  `json:"id,omitempty"`
}

const xTicks = 4

// BuildChart scales balance points into a step line. The y domain is widened to
// round tick values. It returns nil for fewer than two points.
func BuildChart(points []ledger.BalancePoint, width, height float64) (*Chart, error) {
	if len(points) < 2 {
		return nil, nil
	}
	values := make([]float64, len(points))
	minI, maxI := 0, 0
	for i, p := range points {
		var r big.Rat
		if _, ok := r.SetString(p.Balance); !ok {
			return nil, fmt.Errorf("balance %q: invalid", p.Balance)
		}
		values[i], _ = r.Float64()
		if values[i] < values[minI] {
			minI = i
		}
		if values[i] > values[maxI] {
			maxI = i
		}
	}

	lo, hi, step := niceDomain(values[minI], values[maxI])
	y := func(v float64) float64 { return (hi - v) / (hi - lo) * height }

	first, last := points[0].At, points[len(points)-1].At
	span := last.Sub(first).Seconds()
	x := func(i int) float64 {
		if span > 0 {
			return points[i].At.Sub(first).Seconds() / span * width
		}
		return float64(i) / float64(len(points)-1) * width
	}

	coords := make([]string, 0, 2*len(points)-1)
	data := make([]chartPoint, len(points))
	for i, p := range points {
		xi, yi := x(i), y(values[i])
		if i > 0 {
			coords = append(coords, fmt.Sprintf("%.1f,%.1f", xi, y(values[i-1])))
		}
		coords = append(coords, fmt.Sprintf("%.1f,%.1f", xi, yi))
		data[i] = chartPoint{X: xi / width * 100, Y: yi / height * 100, B: p.Balance, T: p.At.UTC().Format(time.RFC3339Nano), ID: p.TransferID}
	}
	dataJSON, err := json.Marshal(data)
	if err != nil {
		return nil, err
	}

	c := &Chart{
		Points:  strings.Join(coords, " "),
		HasZero: lo < 0 && hi > 0,
		Min:     points[minI].Balance,
		Max:     points[maxI].Balance,
		Data:    string(dataJSON),
	}
	if c.HasZero {
		c.ZeroY = y(0)
	}

	decimals := max(0, int(math.Ceil(-math.Log10(step))))
	for i := 0; ; i++ {
		v := hi - float64(i)*step
		if v < lo-step/2 {
			break
		}
		label := strconv.FormatFloat(v, 'f', decimals, 64)
		if strings.Trim(label, "-0.") == "" {
			label = strings.TrimPrefix(label, "-")
		}
		c.YTicks = append(c.YTicks, YTick{Label: label, Y: y(v), Pos: y(v) / height * 100})
		c.LabelWidth = max(c.LabelWidth, len(label))
	}

	if span == 0 {
		c.XTicks = []XTick{{At: first, Pos: 0, Align: "start"}}
		return c, nil
	}
	for i := range xTicks {
		frac := float64(i) / (xTicks - 1)
		align := "middle"
		switch i {
		case 0:
			align = "start"
		case xTicks - 1:
			align = "end"
		}
		at := first.Add(time.Duration(frac * float64(last.Sub(first))))
		c.XTicks = append(c.XTicks, XTick{At: at, Pos: frac * 100, Align: align})
	}
	return c, nil
}

// niceDomain widens [lo, hi] to multiples of a 1/2/5×10^n step giving about
// four intervals.
func niceDomain(lo, hi float64) (float64, float64, float64) {
	if hi == lo {
		pad := max(math.Abs(lo)*0.05, 1)
		lo, hi = lo-pad, hi+pad
	}
	raw := (hi - lo) / 4
	mag := math.Pow(10, math.Floor(math.Log10(raw)))
	step := 10 * mag
	for _, m := range []float64{1, 2, 5} {
		if raw <= m*mag {
			step = m * mag
			break
		}
	}
	return math.Floor(lo/step) * step, math.Ceil(hi/step) * step, step
}
