package core

import "testing"

func TestOverlayReminderWindowsDefaultsAndOldFiles(t *testing.T) {
	// A prefs file from before 1.5.1: one switch for all runes, no windows.
	p, err := parsePrefs([]byte(`{"overlay":{"dota":{"runes":true,"lotus":false,"stacks":true}}}`))
	if err != nil {
		t.Fatal(err)
	}
	d := p.Overlay.Dota
	if !d.Runes || !d.Bounty || !d.Power || !d.Wisdom || !d.Water || d.Lotus || !d.Stacks || !d.LotusLate {
		t.Fatalf("old switches kept, new ones on: %+v", d)
	}
	if d.Until["stack"] != 10 || d.Until["bounty"] != 12 || d.Until["lotus"] != 15 || d.Until["power"] != 0 {
		t.Fatalf("the default windows should apply: %+v", d.Until)
	}

	// Saved windows are kept, clamped, and unknown kinds dropped.
	o := defaultOverlay()
	o.Dota.Until = map[string]int{"stack": 20, "bounty": 500, "made-up": 3}
	o = o.sanitized()
	if o.Dota.Until["stack"] != 20 || o.Dota.Until["bounty"] != 90 || o.Dota.Until["lotus"] != 15 {
		t.Fatalf("windows not sanitized: %+v", o.Dota.Until)
	}
	if _, ok := o.Dota.Until["made-up"]; ok {
		t.Fatal("an unknown reminder kept a window")
	}
}

func TestOverlayOffsetKeptAndClamped(t *testing.T) {
	o := defaultOverlay()
	if o.sanitized().OffsetY != 0 {
		t.Fatal("no offset by default: the overlay stays where it was")
	}
	o.OffsetY = 120
	if o.sanitized().OffsetY != 120 {
		t.Fatal("a chosen offset is kept")
	}
	o.OffsetY = 5000
	if o.sanitized().OffsetY != 600 {
		t.Fatal("the offset is clamped")
	}
}
