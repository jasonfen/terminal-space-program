package tui

import (
	"fmt"
	"math"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// labDistance is the CIE76 colour difference between two "#RRGGBB" colours.
func labDistance(a, b lipgloss.Color) float64 {
	la, lb := toLab(a), toLab(b)
	return math.Sqrt(math.Pow(la[0]-lb[0], 2) + math.Pow(la[1]-lb[1], 2) + math.Pow(la[2]-lb[2], 2))
}

func toLab(c lipgloss.Color) [3]float64 {
	var ri, gi, bi int
	fmt.Sscanf(strings.TrimPrefix(string(c), "#"), "%02x%02x%02x", &ri, &gi, &bi)
	lin := func(v int) float64 {
		f := float64(v) / 255
		if f <= 0.04045 {
			return f / 12.92
		}
		return math.Pow((f+0.055)/1.055, 2.4)
	}
	r, g, b := lin(ri), lin(gi), lin(bi)
	x := (0.4124*r + 0.3576*g + 0.1805*b) / 0.95047
	y := 0.2126*r + 0.7152*g + 0.0722*b
	z := (0.0193*r + 0.1192*g + 0.9505*b) / 1.08883
	f := func(t float64) float64 {
		if t > 0.008856 {
			return math.Cbrt(t)
		}
		return 7.787*t + 16.0/116
	}
	return [3]float64{116*f(y) - 16, 500 * (f(x) - f(y)), 200 * (f(y) - f(z))}
}
