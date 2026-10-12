package core

// Why the player died: plain checks on the player's own state just before
// the death — where they stood, what they carried, what was ready, how much
// gold they held, how the game was going — so the overlay can say it while
// they wait to respawn. Nothing here knows about the enemy beyond the
// scoreboard: only what Dota tells the player about themselves.

import (
	"fmt"
	"math"
	"strings"
)

// aliveSnapshot is the player's state on the last report before a death.
type aliveSnapshot struct {
	Clock       float64
	Gold        int64
	HasGold     bool
	BuybackCost int64
	HasBuyback  bool
	Items       []snapItem // inventory and neutral slot
	TP          *snapItem  // the teleport slot; nil when empty
	// Abilities are only reported when the GSI config asks for them.
	HasAbilities bool
	UltReady     bool
	// Where the hero stood, in Dota's world units, and which side it plays.
	X, Y    float64
	HasPos  bool
	Radiant bool
	HasSide bool
	// Day or night, and the kill score.
	Daytime  bool
	HasDay   bool
	TeamK    int64
	EnemyK   int64
	HasScore bool
	Level    int64
}

type snapItem struct {
	Name     string
	Cooldown float64
	CanCast  bool
	Charges  int64
}

// Items that save a life when used in time, with how to say them.
var savingItems = map[string]string{
	"black_king_bar": "Black King Bar", "glimmer_cape": "Glimmer Cape", "force_staff": "Force Staff",
	"hurricane_pike": "Hurricane Pike", "cyclone": "Eul's Scepter", "wind_waker": "Wind Waker",
	"ghost": "Ghost Scepter", "lotus_orb": "Lotus Orb", "manta": "Manta Style", "satanic": "Satanic",
	"blade_mail": "Blade Mail", "blink": "Blink Dagger", "overwhelming_blink": "Overwhelming Blink",
	"swift_blink": "Swift Blink", "arcane_blink": "Arcane Blink", "pipe": "Pipe of Insight",
	"shadow_amulet": "Shadow Amulet", "invis_sword": "Shadow Blade", "silver_edge": "Silver Edge",
	"sphere": "", "aeon_disk": "", // passive: nothing to press
	"mjollnir": "", "solar_crest": "Solar Crest", "crimson_guard": "Crimson Guard", "disperser": "Disperser",
	"heavens_halberd": "Heaven's Halberd", "sheepstick": "Scythe of Vyse", "orchid": "Orchid Malevolence",
	"bloodthorn": "Bloodthorn", "abyssal_blade": "Abyssal Blade",
}

// snapshotOf reads the player's state from one payload.
func snapshotOf(clock float64, gmap, hero, player, items, abilities jsonMap) *aliveSnapshot {
	s := &aliveSnapshot{Clock: clock}
	s.Gold, s.HasGold = getInt(player, "gold")
	s.BuybackCost, s.HasBuyback = getInt(hero, "buyback_cost")
	s.Level, _ = getInt(hero, "level")
	var okX, okY bool
	s.X, okX = getNum(hero, "xpos")
	s.Y, okY = getNum(hero, "ypos")
	s.HasPos = okX && okY
	if team, ok := getStr(player, "team_name"); ok {
		team = strings.ToLower(team)
		s.Radiant, s.HasSide = team == "radiant", team == "radiant" || team == "dire"
	}
	s.Daytime, s.HasDay = getBool(gmap, "daytime")
	if s.HasSide {
		rad, okR := getInt(gmap, "radiant_score")
		dire, okD := getInt(gmap, "dire_score")
		if okR && okD {
			s.HasScore = true
			s.TeamK, s.EnemyK = rad, dire
			if !s.Radiant {
				s.TeamK, s.EnemyK = dire, rad
			}
		}
	}
	for slot, raw := range items {
		data, _ := raw.(map[string]any)
		name, _ := getStr(data, "name")
		if name == "" || name == "empty" {
			continue
		}
		it := snapItem{Name: strings.TrimPrefix(name, "item_")}
		it.Cooldown, _ = getNum(data, "cooldown")
		it.CanCast, _ = getBool(data, "can_cast")
		it.Charges, _ = getInt(data, "charges")
		switch {
		case strings.HasPrefix(slot, "teleport"):
			tp := it
			s.TP = &tp
		case strings.HasPrefix(slot, "slot") || strings.HasPrefix(slot, "neutral"):
			// Backpack slots are slot6–8: nothing there can be used.
			if n := strings.TrimPrefix(slot, "slot"); n == "6" || n == "7" || n == "8" {
				continue
			}
			s.Items = append(s.Items, it)
		}
	}
	if abilities != nil {
		s.HasAbilities = true
		for _, raw := range abilities {
			a, _ := raw.(map[string]any)
			ult, _ := getBool(a, "ultimate")
			level, _ := getInt(a, "level")
			cd, _ := getNum(a, "cooldown")
			castable, _ := getBool(a, "can_cast")
			if ult && level > 0 && cd == 0 && castable {
				s.UltReady = true
			}
		}
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

// deathReasons says what went wrong, most useful first. earlier holds the
// match's earlier deaths (their clock in seconds).
func deathReasons(s *aliveSnapshot, earlier []Death, clock float64, turbo bool) []string {
	if s == nil {
		return nil
	}
	var out []string
	late := 30 * 60.0
	if turbo {
		late = 15 * 60
	}
	// 1. Something that would have saved the hero was ready.
	for _, it := range s.Items {
		if label := savingItems[it.Name]; label != "" && it.CanCast && it.Cooldown == 0 {
			out = append(out, fmt.Sprintf("Your %s was ready: use it before the fight turns", label))
			break
		}
	}
	// 2. Where it happened: the enemy's side of the map is where most
	// avoidable deaths are.
	var sp mapSpot
	hasSpot := s.HasPos && s.HasSide
	if hasSpot {
		sp = spotOf(worldToGrid(s.X), worldToGrid(s.Y), s.Radiant)
		theirs := 0
		for _, d := range earlier {
			if strings.Contains(d.Region, "their") {
				theirs++
			}
		}
		switch {
		case sp.Deep:
			out = append(out, "Died in their base: without your whole team there, that's a free kill for them")
		case sp.Theirs && sp.Lane == "jungle":
			out = append(out, "Died in their jungle: go there only with vision and a way out")
		case sp.Theirs:
			msg := fmt.Sprintf("Died on their side of the map (%s)", regionText(sp))
			if theirs >= 1 {
				msg = fmt.Sprintf("%s death on their side of the map", ordinal(theirs+1))
			}
			if s.HasScore && s.EnemyK-s.TeamK >= 4 {
				msg += fmt.Sprintf(": your team is %d kills behind, fight near your own towers", s.EnemyK-s.TeamK)
			} else {
				msg += ": the missing heroes on the minimap were coming for you"
			}
			out = append(out, msg)
		}
	}
	// 3. A full wand is a free heal.
	for _, it := range s.Items {
		if (it.Name == "magic_wand" || it.Name == "magic_stick") && it.Charges >= 8 && it.CanCast {
			out = append(out, fmt.Sprintf("Magic Wand had %d charges unused", it.Charges))
			break
		}
	}
	// 4. Ultimate up and not used.
	if s.HasAbilities && s.UltReady {
		out = append(out, "Your ultimate was ready")
	}
	// 5. No way back or out.
	if clock >= 4*60 && (s.TP == nil || (s.TP.Charges == 0 && !s.TP.CanCast)) {
		out = append(out, "No TP scroll: buy one every time you're back in base")
	}
	// 6. Gold that should have been an item.
	if s.HasGold {
		limit := int64(1500)
		if clock >= late {
			limit = 2500
		}
		if s.Gold >= limit {
			out = append(out, fmt.Sprintf("%s gold unspent: buy before you fight", fmtThousands(s.Gold)))
		}
	}
	// 7. Dying again soon after the last time.
	recent := 1
	for _, d := range earlier {
		if d.At > 0 && clock-d.At <= 5*60 {
			recent++
		}
	}
	if recent >= 2 {
		out = append(out, fmt.Sprintf("%s death in 5 minutes: play safer until your next item", ordinal(recent)))
	}
	// 8. Night on their side: vision is half as long.
	if hasSpot && s.HasDay && !s.Daytime && (sp.Theirs || sp.Lane == "river") {
		out = append(out, "At night vision is short: they were closer than the minimap showed")
	}
	// 9. Late game, and no buyback.
	if clock >= late && s.HasBuyback && s.HasGold && s.Gold < s.BuybackCost {
		out = append(out, fmt.Sprintf("No buyback: %s gold short", fmtThousands(s.BuybackCost-s.Gold)))
	}
	if len(out) > 4 {
		out = out[:4]
	}
	return out
}

func ordinal(n int) string {
	switch n {
	case 2:
		return "2nd"
	case 3:
		return "3rd"
	}
	return fmt.Sprintf("%dth", n)
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
