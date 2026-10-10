package main

import (
	"strings"

	"github.com/wailsapp/wails/v3/pkg/application"

	"thetracker/internal/core"
)

// placeOverlay works out where the overlay goes inside an area of the screen
// (Dota's window, or a whole monitor), in physical pixels: in the chosen
// corner, the margin and the player's offset in from the edges, all scaled
// by the monitor's scale factor. The offset never pushes it out of the area.
func placeOverlay(area application.Rect, scale float64, o core.OverlaySettings) application.Rect {
	if scale <= 0 {
		scale = 1
	}
	px := func(v float64) int { return int(v*scale + 0.5) }
	w, h := px(overlayW*o.Scale), px(overlayH*o.Scale)
	margin := px(overlayMargin)
	in := min(margin+px(float64(o.OffsetY)), max(margin, area.Height-h-margin))
	x, y := area.X+margin, area.Y+in
	if strings.HasSuffix(o.Corner, "right") {
		x = area.X + area.Width - w - margin
	}
	if strings.HasPrefix(o.Corner, "bottom") {
		y = area.Y + area.Height - h - in
	}
	return application.Rect{X: x, Y: y, Width: w, Height: h}
}
