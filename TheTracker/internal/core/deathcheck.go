package core

// Where the player died: the hero's position on the last report before a
// death, said the way a player would ("their jungle"). Only the place is
// kept; the app doesn't guess why, since it can't see the fight.

import (
	"fmt"
	"math"
	"strings"
)

// aliveSnapshot is where the hero stood on the last report before a death.
type aliveSnapshot struct {
	Clock float64
	// In Dota's world units, and which side the hero plays.
	X, Y    float64
	HasPos  bool
	Radiant bool
	HasSide bool
}

// snapshotOf reads the hero's position from one payload.
func snapshotOf(clock float64, hero, player jsonMap) *aliveSnapshot {
	s := &aliveSnapshot{Clock: clock}
	var okX, okY bool
	s.X, okX = getNum(hero, "xpos")
	s.Y, okY = getNum(hero, "ypos")
	s.HasPos = okX && okY
	if team, ok := getStr(player, "team_name"); ok {
		team = strings.ToLower(team)
		s.Radiant, s.HasSide = team == "radiant", team == "radiant" || team == "dire"
	}
	return s
}

// ---------- Where on the map ----------

// mapSpot is a place on the Dota map, in OpenDota's 64–192 grid (Radiant's
// fountain bottom-left, Dire's top-right, the river on the x + y = 256
// diagonal). GSI's world units convert with worldToGrid.
type mapSpot struct {
	Lane   string // top | mid | bottom | jungle | base | river
	Theirs bool   // past the river from the player's side
	Deep   bool   // near or in the enemy base
}

func worldToGrid(v float64) float64 { return v/128 + 128 }

func spotOf(x, y float64, radiant bool) mapSpot {
	s := x + y
	// Seen from Radiant; Dire sees the map mirrored across the river.
	ahead := s - 256
	if !radiant {
		ahead = -ahead
	}
	sp := mapSpot{Theirs: ahead > 12, Deep: ahead > 72}
	switch {
	case ahead < -72:
		sp.Lane = "base"
	case ahead > 72:
		sp.Lane = "base"
	case math.Abs(ahead) <= 12 && math.Abs(x-y) < 60:
		sp.Lane = "river"
	case x < 83 || y > 173:
		sp.Lane = "top"
	case y < 83 || x > 173:
		sp.Lane = "bottom"
	case math.Abs(x-y) < 11:
		sp.Lane = "mid"
	default:
		sp.Lane = "jungle"
	}
	return sp
}

// regionText says the spot the way a player would: "their jungle", "the
// top lane on their side", "the river", "their base".
func regionText(sp mapSpot) string {
	side := "your"
	if sp.Theirs {
		side = "their"
	}
	switch sp.Lane {
	case "base":
		return side + " base"
	case "river":
		return "the river"
	case "jungle":
		return side + " jungle"
	}
	return fmt.Sprintf("the %s lane on %s side", sp.Lane, side)
}

// deathRegion names where the snapshot's hero stood, or "" when unknown.
func deathRegion(s *aliveSnapshot) string {
	if s == nil || !s.HasPos || !s.HasSide {
		return ""
	}
	return regionText(spotOf(worldToGrid(s.X), worldToGrid(s.Y), s.Radiant))
}

func fmtThousands(n int64) string {
	s := fmt.Sprint(n)
	if n < 0 {
		return s
	}
	for i := len(s) - 3; i > 0; i -= 3 {
		s = s[:i] + "," + s[i:]
	}
	return s
}
