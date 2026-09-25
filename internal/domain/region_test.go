package domain

import "testing"

func TestRegionForCenterPoint(t *testing.T) {
	r := []RegionShape{{ID: "a", X: 0, Y: 0, W: 400, H: 300, Order: 1}}
	cases := []struct {
		x, y float64
		want string
	}{
		{0, 0, "a"},      // center (90,60) inside
		{310, 240, "a"},  // center (400,300): on the corner, edges inclusive
		{311, 0, ""},     // center x = 401
		{-90, -60, "a"},  // center exactly at (0,0)
		{-91, 0, ""},     // center x = -1
		{300, 200, "a"},  // note overhangs the region, center inside
		{1000, 1000, ""}, // far away
	}
	for _, c := range cases {
		if got := RegionFor(c.x, c.y, r); got != c.want {
			t.Errorf("note at %v,%v: region %q, want %q", c.x, c.y, got, c.want)
		}
	}
}

func TestRegionForZOrderAndTies(t *testing.T) {
	swimlane := RegionShape{ID: "lane", X: -1000, Y: 0, W: 5000, H: 400, Z: 0, Order: 1}
	box := RegionShape{ID: "box", X: 0, Y: 0, W: 400, H: 400, Z: 1, Order: 2}
	if got := RegionFor(10, 10, []RegionShape{box, swimlane}); got != "box" {
		t.Errorf("higher z should win: %q", got)
	}
	box.Z = -1
	if got := RegionFor(10, 10, []RegionShape{box, swimlane}); got != "lane" {
		t.Errorf("higher z should win: %q", got)
	}
	// Equal z: the newest region (larger Order) wins, regardless of slice order.
	older := RegionShape{ID: "older", X: 0, Y: 0, W: 400, H: 400, Order: 5}
	newer := RegionShape{ID: "newer", X: 0, Y: 0, W: 400, H: 400, Order: 9}
	for _, rs := range [][]RegionShape{{older, newer}, {newer, older}} {
		if got := RegionFor(10, 10, rs); got != "newer" {
			t.Errorf("tie should go to the newest region: %q", got)
		}
	}
}

func TestRegionValidation(t *testing.T) {
	if l, err := NormalizeRegionLabel("  Track A "); err != nil || l != "Track A" {
		t.Fatal(l, err)
	}
	if _, err := NormalizeRegionLabel(" "); err == nil {
		t.Error("empty label accepted")
	}
	for _, c := range []string{"#aabbcc", "#AABBCC"} {
		if ValidateRegionColor(c) != nil {
			t.Errorf("%s rejected", c)
		}
	}
	for _, c := range []string{"", "red", "#abc", "#aabbccdd", "aabbcc"} {
		if ValidateRegionColor(c) == nil {
			t.Errorf("%q accepted", c)
		}
	}
	if ValidateRegionGeometry(0, 0, 100, 100, 0) != nil {
		t.Error("minimum size rejected")
	}
	if ValidateRegionGeometry(0, 0, 99, 100, 0) == nil || ValidateRegionGeometry(0, 0, 100, 1e6, 0) == nil {
		t.Error("bad size accepted")
	}
	if ValidateRegionGeometry(0, 0, 100, 100, 2_000_000) == nil {
		t.Error("bad z accepted")
	}
}

func TestRegionAndStarPermissions(t *testing.T) {
	p := Actor{UserID: "p", Role: RoleParticipant}
	m := Actor{UserID: "m", Role: RoleModerator}
	o := Actor{UserID: "o", Role: RoleOrganizer}
	for _, lc := range []Lifecycle{LifecycleSetup, LifecycleActive} {
		if err := AuthorizeRegionEdit(p, lc); err == nil || err.Code != CodeForbidden {
			t.Errorf("participant region edit in %s: %v", lc, err)
		}
		for _, a := range []Actor{m, o} {
			if err := AuthorizeRegionEdit(a, lc); err != nil {
				t.Errorf("%s region edit in %s: %v", a.Role, lc, err)
			}
		}
	}
	if err := AuthorizeRegionEdit(o, LifecycleDone); err == nil || err.Code != CodeNotAllowedNow {
		t.Errorf("region edit when done: %v", err)
	}

	if err := AuthorizeStar(p, LifecycleSetup); err == nil || err.Code != CodeNotAllowedNow {
		t.Errorf("participant star in setup: %v", err)
	}
	for _, lc := range []Lifecycle{LifecycleActive, LifecycleDone} {
		for _, a := range []Actor{p, m, o} {
			if err := AuthorizeStar(a, lc); err != nil {
				t.Errorf("%s star in %s: %v", a.Role, lc, err)
			}
		}
	}
	if err := AuthorizeStar(m, LifecycleSetup); err != nil {
		t.Errorf("moderator star in setup: %v", err)
	}
}
