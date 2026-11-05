package web

import (
	"fmt"
	"math/big"
	"strings"

	"github.com/pgr0ss/pgledger_visualizer/internal/ledger"
)

type Sparkline struct {
	Points   string
	ZeroY    float64
	HasZero  bool
	Min, Max string
}

// BuildSparkline scales balance points into a width×height SVG polyline. It
// returns nil for fewer than two points.
func BuildSparkline(points []ledger.BalancePoint, width, height float64) (*Sparkline, error) {
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
	lo, hi := values[minI], values[maxI]
	y := func(v float64) float64 {
		if hi == lo {
			return height / 2
		}
		return (hi - v) / (hi - lo) * height
	}

	first, last := points[0].At, points[len(points)-1].At
	span := last.Sub(first).Seconds()
	coords := make([]string, len(points))
	for i, p := range points {
		var x float64
		if span > 0 {
			x = p.At.Sub(first).Seconds() / span * width
		} else {
			x = float64(i) / float64(len(points)-1) * width
		}
		coords[i] = fmt.Sprintf("%.1f,%.1f", x, y(values[i]))
	}

	s := &Sparkline{
		Points:  strings.Join(coords, " "),
		HasZero: lo < 0 && hi > 0,
		Min:     points[minI].Balance,
		Max:     points[maxI].Balance,
	}
	if s.HasZero {
		s.ZeroY = y(0)
	}
	return s, nil
}
