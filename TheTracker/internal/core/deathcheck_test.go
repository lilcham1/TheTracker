package core

import (
	"strings"
	"testing"
)

func TestWhyYouDied(t *testing.T) {
	a := newTestApp(t)
	tr := a.Tracker
	item := func(name string, cd float64, castable bool, charges float64) jsonMap {
		return jsonMap{"name": "item_" + name, "cooldown": cd, "can_cast": castable, "charges": charges}
	}
	send := func(clock float64, alive bool, gold float64, items jsonMap, abilities jsonMap) {
		body := jsonMap{
			"map":    jsonMap{"matchid": "1", "clock_time": clock, "game_state": stateInProgress},
			"player": jsonMap{"activity": "playing", "gold": gold},
			"hero":   jsonMap{"name": "npc_dota_hero_axe", "alive": alive, "respawn_seconds": 20.0, "buyback_cost": 2000.0},
			"items":  items,
		}
		if abilities != nil {
			body["abilities"] = abilities
		}
		tr.HandleUpdate(body)
	}
	reasons := func() []string {
		m := tr.Status(false).Current
		return m.Deaths[len(m.Deaths)-1].Reasons
	}
	has := func(rs []string, part string) bool {
		for _, r := range rs {
			if strings.Contains(r, part) {
				return true
			}
		}
		return false
	}

	// Mid game: BKB ready, a full wand, no TP, 2,400 gold, ultimate up.
	inv := jsonMap{"slot0": item("black_king_bar", 0, true, 0), "slot1": item("magic_wand", 0, true, 14),
		"slot6":     item("force_staff", 0, true, 0), // backpack: can't be used
		"teleport0": jsonMap{"name": "empty"}}
	ult := jsonMap{"ability3": jsonMap{"name": "axe_culling_blade", "ultimate": true, "level": 1.0, "cooldown": 0.0, "can_cast": true}}
	send(900, true, 2400, inv, ult)
	send(901, false, 2100, inv, ult)
	rs := reasons()
	if len(rs) != 4 || !strings.Contains(rs[0], "Black King Bar was ready") || !has(rs, "Magic Wand had 14") || !has(rs, "ultimate was ready") || !has(rs, "No TP scroll") {
		t.Fatalf("mid-game death: %q", rs)
	}
	if m := tr.Status(false).Current; m.RespawnSeconds == nil || *m.RespawnSeconds != 20 {
		t.Fatalf("respawn timer while dead: %+v", m.RespawnSeconds)
	}

	// Back alive with a TP and the BKB on cooldown; dying again soon.
	inv2 := jsonMap{"slot0": item("black_king_bar", 40, false, 0), "teleport0": item("tpscroll", 0, true, 1)}
	send(930, true, 300, inv2, nil)
	if tr.Status(false).Current.RespawnSeconds != nil {
		t.Fatal("no respawn timer while alive")
	}
	send(931, false, 100, inv2, nil)
	if rs := reasons(); len(rs) != 1 || !strings.Contains(rs[0], "2nd death in 5 minutes") {
		t.Fatalf("a used BKB and a TP aren't mistakes; dying again soon is: %q", rs)
	}

	// Late game: short of buyback, lots of gold under the late limit.
	send(2000, true, 1800, inv2, nil)
	send(2001, false, 1500, inv2, nil)
	if rs := reasons(); !has(rs, "No buyback: 200 gold short") || has(rs, "unspent") {
		t.Fatalf("late game: %q", rs)
	}

	// Saved with the match.
	if fmtThousands(1234567) != "1,234,567" || fmtThousands(999) != "999" {
		t.Fatal("thousands")
	}
}
