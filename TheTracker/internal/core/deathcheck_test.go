package core

import "testing"

// A death records when, where and the gold lost; never a guessed reason.
func TestDeathRecordsPlaceNotReasons(t *testing.T) {
	a := newTestApp(t)
	tr := a.Tracker
	send := func(clock float64, alive bool, gold float64) {
		tr.HandleUpdate(jsonMap{
			"map":    jsonMap{"matchid": "1", "clock_time": clock, "game_state": stateInProgress},
			"player": jsonMap{"activity": "playing", "gold": gold, "team_name": "radiant"},
			"hero":   jsonMap{"name": "npc_dota_hero_axe", "alive": alive, "respawn_seconds": 20.0, "xpos": 2500.0, "ypos": 1000.0},
			"items":  jsonMap{"slot0": jsonMap{"name": "item_black_king_bar", "cooldown": 0.0, "can_cast": true}},
		})
	}
	send(900, true, 2400)
	send(901, false, 2100)
	m := tr.Status(false).Current
	if len(m.Deaths) != 1 {
		t.Fatalf("deaths: %+v", m.Deaths)
	}
	d := m.Deaths[0]
	if d.Clock != "15:01" || d.Region != "their jungle" || d.GoldLost == nil || *d.GoldLost != 300 {
		t.Fatalf("death: %+v", d)
	}
	if fmtThousands(1234567) != "1,234,567" || fmtThousands(999) != "999" {
		t.Fatal("thousands")
	}
}
