package domain

import (
	"math"
	"sort"
)

// Sticky notes are a fixed size in board units (SPEC §9).
const (
	NoteW = 180.0
	NoteH = 120.0

	// SpiralStep is the distance between candidate positions when resolving
	// overlaps. It equals the client's snap grid so resolved positions stay
	// on-grid for grid users.
	SpiralStep = 20.0

	// maxSpiralRings bounds the search. With a few hundred notes a free spot
	// is always found long before this (ring k covers ±k·20 units).
	maxSpiralRings = 2000

	// MaxCoord bounds board coordinates. The board is "unbounded" in
	// practice; this only keeps floats sane.
	MaxCoord = 1e6
)

// Rect is an axis-aligned rectangle with its top-left corner at (X, Y).
type Rect struct{ X, Y, W, H float64 }

// NoteRect is the rectangle of a note whose top-left corner is at (x, y).
func NoteRect(x, y float64) Rect { return Rect{x, y, NoteW, NoteH} }

// Intersects reports whether the interiors of r and o overlap. Rectangles
// that only share an edge do not intersect.
func (r Rect) Intersects(o Rect) bool {
	return r.X < o.X+o.W && o.X < r.X+r.W && r.Y < o.Y+o.H && o.Y < r.Y+r.H
}

// Contains reports whether point (px, py) lies inside r (edges inclusive).
func (r Rect) Contains(px, py float64) bool {
	return px >= r.X && px <= r.X+r.W && py >= r.Y && py <= r.Y+r.H
}

// ValidCoord reports whether v is a usable board coordinate.
func ValidCoord(v float64) bool {
	return !math.IsNaN(v) && !math.IsInf(v, 0) && math.Abs(v) <= MaxCoord
}

type offset struct{ dx, dy int }

// spiralRings[k] lists the offsets (in steps) on the square ring at
// Chebyshev distance k, nearest first; ties are broken by angle, starting
// east and turning clockwise in screen coordinates (y grows downward).
// The first rings are precomputed (read-only, so safe for concurrent use);
// farther rings are rare and built on demand.
var spiralRings = func() [][]offset {
	rings := make([][]offset, 64)
	for k := range rings {
		rings[k] = buildRing(k)
	}
	return rings
}()

func ring(k int) []offset {
	if k < len(spiralRings) {
		return spiralRings[k]
	}
	return buildRing(k)
}

func buildRing(k int) []offset {
	if k == 0 {
		return []offset{{0, 0}}
	}
	out := make([]offset, 0, 8*k)
	for dx := -k; dx <= k; dx++ {
		for dy := -k; dy <= k; dy++ {
			if max(abs(dx), abs(dy)) == k {
				out = append(out, offset{dx, dy})
			}
		}
	}
	sort.Slice(out, func(i, j int) bool {
		di := out[i].dx*out[i].dx + out[i].dy*out[i].dy
		dj := out[j].dx*out[j].dx + out[j].dy*out[j].dy
		if di != dj {
			return di < dj
		}
		return angle(out[i]) < angle(out[j])
	})
	return out
}

// angle maps an offset to [0, 2π), 0 = east, increasing clockwise on screen.
func angle(o offset) float64 {
	a := math.Atan2(float64(o.dy), float64(o.dx))
	if a < 0 {
		a += 2 * math.Pi
	}
	return a
}

func abs(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

// ResolvePosition returns the position closest to the requested (x, y), in
// square-spiral order, at which a note does not intersect any of others.
// Rings are scanned outward 20 units at a time; within a ring the nearest
// candidate wins, ties broken by a fixed angular order. The result is
// deterministic for a given input. If the requested spot is free it is
// returned unchanged.
func ResolvePosition(x, y float64, others []Rect) (float64, float64) {
	free := func(cx, cy float64) bool {
		r := NoteRect(cx, cy)
		for _, o := range others {
			if r.Intersects(o) {
				return false
			}
		}
		return true
	}
	for k := 0; k <= maxSpiralRings; k++ {
		for _, o := range ring(k) {
			cx, cy := x+float64(o.dx)*SpiralStep, y+float64(o.dy)*SpiralStep
			if free(cx, cy) {
				return cx, cy
			}
		}
	}
	return x, y // unreachable with a finite, modest number of notes
}
