package main

import (
	"testing"

	"github.com/wailsapp/wails/v3/pkg/application"

	"thetracker/internal/core"
)

func TestPlaceOverlay(t *testing.T) {
	screen := application.Rect{X: 0, Y: 0, Width: 2560, Height: 1440}
	o := core.OverlaySettings{Scale: 1, Corner: "top-right"}
	at := func(area application.Rect, scale float64, o core.OverlaySettings) application.Rect {
		return placeOverlay(area, scale, o)
	}

	if r := at(screen, 1, o); r != (application.Rect{X: 2560 - 340 - 24, Y: 24, Width: 340, Height: 300}) {
		t.Fatalf("top right: %+v", r)
	}
	// 4 cm lower on a 2560 x 1440, 60 x 34 cm screen is 170 px.
	o.OffsetY = 170
	if r := at(screen, 1, o); r.Y != 24+170 {
		t.Fatalf("lowered: %+v", r)
	}
	o.Corner = "bottom-left"
	if r := at(screen, 1, o); r.X != 24 || r.Y != 1440-300-24-170 {
		t.Fatalf("bottom left, raised: %+v", r)
	}
	// A windowed game away from the screen's corner: placed inside it.
	game := application.Rect{X: 300, Y: 200, Width: 1600, Height: 900}
	o.Corner, o.OffsetY = "top-left", 0
	if r := at(game, 1, o); r.X != 324 || r.Y != 224 {
		t.Fatalf("inside a windowed game: %+v", r)
	}
	// On a second monitor to the right, at 150 %: everything scales.
	second := application.Rect{X: 2560, Y: 0, Width: 2880, Height: 1620}
	o.Corner, o.OffsetY = "top-right", 100
	if r := at(second, 1.5, o); r != (application.Rect{X: 2560 + 2880 - 510 - 36, Y: 36 + 150, Width: 510, Height: 450}) {
		t.Fatalf("scaled: %+v", r)
	}
	// An offset bigger than the room left stops at the bottom edge.
	o.OffsetY = 5000
	if r := at(game, 1, o); r.Y+r.Height != game.Y+game.Height-24 {
		t.Fatalf("kept inside: %+v", r)
	}
}
