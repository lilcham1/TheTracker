package core

// Why the player died: a few plain checks on the player's own state just
// before the death — what they carried, what was ready, how much gold they
// held — so the overlay can say it while they wait to respawn. Nothing here
// knows about the enemy: only what Dota tells the player about themselves.

import (
	"fmt"
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
func snapshotOf(clock float64, hero, player, items, abilities jsonMap) *aliveSnapshot {
	s := &aliveSnapshot{Clock: clock}
	s.Gold, s.HasGold = getInt(player, "gold")
	s.BuybackCost, s.HasBuyback = getInt(hero, "buyback_cost")
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
	// 2. A full wand is a free heal.
	for _, it := range s.Items {
		if (it.Name == "magic_wand" || it.Name == "magic_stick") && it.Charges >= 8 && it.CanCast {
			out = append(out, fmt.Sprintf("Magic Wand had %d charges unused", it.Charges))
			break
		}
	}
	// 3. Ultimate up and not used.
	if s.HasAbilities && s.UltReady {
		out = append(out, "Your ultimate was ready")
	}
	// 4. No way back or out.
	if clock >= 4*60 && (s.TP == nil || (s.TP.Charges == 0 && !s.TP.CanCast)) {
		out = append(out, "No TP scroll: buy one every time you're back in base")
	}
	// 5. Gold that should have been an item.
	if s.HasGold {
		limit := int64(1500)
		if clock >= late {
			limit = 2500
		}
		if s.Gold >= limit {
			out = append(out, fmt.Sprintf("%s gold unspent: buy before you fight", fmtThousands(s.Gold)))
		}
	}
	// 6. Dying again soon after the last time.
	recent := 1
	for _, d := range earlier {
		if d.At > 0 && clock-d.At <= 5*60 {
			recent++
		}
	}
	if recent >= 2 {
		out = append(out, fmt.Sprintf("%s death in 5 minutes: play safer until your next item", ordinal(recent)))
	}
	// 7. Late game, and no buyback.
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
