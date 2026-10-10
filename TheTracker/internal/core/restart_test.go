package core

import (
	"strings"
	"testing"
	"time"
)

func TestMatchKeptAcrossAnUpdate(t *testing.T) {
	a := newTestApp(t)
	send := func(tr *Tracker, clock, lastHits float64, alive bool) {
		tr.HandleUpdate(jsonMap{
			"map":    jsonMap{"matchid": "777", "clock_time": clock, "game_state": stateInProgress},
			"player": jsonMap{"activity": "playing", "last_hits": lastHits, "gold": 600.0},
			"hero":   jsonMap{"name": "npc_dota_hero_axe", "alive": alive},
		})
	}
	send(a.Tracker, 290, 20, true)
	send(a.Tracker, 305, 22, true) // the 5:00 mark
	send(a.Tracker, 320, 23, false)

	// Without the player's go-ahead, an update waits for the match.
	if err := a.InstallUpdate(false); err == nil || !strings.Contains(err.Error(), "Confirm") {
		t.Fatalf("an update mid-match needs confirming: %v", err)
	}

	a.Tracker.SaveForRestart()

	// The new version starts and Dota carries on reporting the same match.
	b := NewTracker(a.Store)
	b.RestoreAfterRestart()
	send(b, 340, 25, true)
	m := b.Status(false).Current
	if m == nil || m.MatchID != "777" || len(m.Deaths) != 1 || m.Checkpoints[5] == nil || m.Checkpoints[5].LastHits != 22 || m.LastHits != 25 {
		t.Fatalf("the match should carry on where it was: %+v", m)
	}
	// The kept copy is used once.
	c := NewTracker(a.Store)
	c.RestoreAfterRestart()
	if c.Status(false).Current != nil {
		t.Fatal("a kept match must not be picked up twice")
	}
}

func TestOldKeptMatchIgnored(t *testing.T) {
	a := newTestApp(t)
	m := newMatchState("9", nil, "")
	_ = a.Store.writeJSON(restartFile, restartState{SavedAt: time.Now().Add(-time.Hour), Match: m})
	tr := NewTracker(a.Store)
	tr.RestoreAfterRestart()
	if tr.Status(false).Current != nil {
		t.Fatal("a match kept an hour ago is not picked up")
	}
}
