package domain

import (
	"fmt"
	"regexp"
	"strings"
	"unicode/utf8"
)

const (
	MaxRegionLabelLen = 60
	MinRegionSize     = 100.0
	MaxRegionSize     = 100000.0
	MaxRegionZ        = 1000000
)

var hexColor = regexp.MustCompile(`^#[0-9a-fA-F]{6}$`)

// RegionShape is a region's geometry and stacking order. Order is the
// region's creation rank (larger = newer); it breaks z ties.
type RegionShape struct {
	ID         string
	X, Y, W, H float64
	Z          int
	Order      int64
}

// RegionFor returns the id of the region containing the center of a note
// whose top-left corner is (x, y), or "" if none does (SPEC §9). With
// several candidates the highest z wins, then the newest region.
func RegionFor(x, y float64, regions []RegionShape) string {
	cx, cy := x+NoteW/2, y+NoteH/2
	var best *RegionShape
	for i := range regions {
		r := &regions[i]
		if !(Rect{r.X, r.Y, r.W, r.H}).Contains(cx, cy) {
			continue
		}
		if best == nil || r.Z > best.Z || (r.Z == best.Z && r.Order > best.Order) {
			best = r
		}
	}
	if best == nil {
		return ""
	}
	return best.ID
}

// NormalizeRegionLabel trims and validates a region header (1–60 chars).
func NormalizeRegionLabel(raw string) (string, error) {
	l := strings.TrimSpace(raw)
	if n := utf8.RuneCountInString(l); n == 0 || n > MaxRegionLabelLen {
		return "", fmt.Errorf("label must be 1–%d characters", MaxRegionLabelLen)
	}
	return l, nil
}

// ValidateRegionColor accepts a #rrggbb tint.
func ValidateRegionColor(c string) error {
	if !hexColor.MatchString(c) {
		return fmt.Errorf("color must be #rrggbb")
	}
	return nil
}

// ValidateRegionGeometry checks position, size, and z.
func ValidateRegionGeometry(x, y, w, h float64, z int) error {
	if !ValidCoord(x) || !ValidCoord(y) {
		return fmt.Errorf("coordinates out of range")
	}
	if !(w >= MinRegionSize && w <= MaxRegionSize && h >= MinRegionSize && h <= MaxRegionSize) {
		return fmt.Errorf("width and height must be %g–%g", MinRegionSize, MaxRegionSize)
	}
	if z < -MaxRegionZ || z > MaxRegionZ {
		return fmt.Errorf("z out of range")
	}
	return nil
}

// AuthorizeRegionEdit covers create/update/delete_region: moderators and
// organizers, whenever the board is editable.
func AuthorizeRegionEdit(a Actor, lc Lifecycle) *CmdError {
	if !a.IsMod() {
		return Forbidden("only moderators can edit regions")
	}
	return CheckInteract(a, lc)
}

// AuthorizeStar: stars are personal bookmarks and change nothing others
// see, so they stay available in every lifecycle — except that
// participants cannot see the board during setup.
func AuthorizeStar(a Actor, lc Lifecycle) *CmdError {
	if lc == LifecycleSetup && !a.IsMod() {
		return NotAllowedNow("the event has not started yet")
	}
	return nil
}
