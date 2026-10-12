package core

// The deeper half of a Dota review, from OpenDota's parsed match: each
// death placed in its fight (who was there, who died, where, how the game
// stood), time spent dead, wards, buybacks, and the farm race against the
// enemy hero in the same role. Everything here is public match data.

import (
	"fmt"
	"math"
	"sort"
	"strings"
)

// dotaFight is the teamfight a death was part of, from the player's side.
type dotaFight struct {
	AlliesIn    int `json:"alliesIn"` // allies who did anything in it, the player included
	EnemiesIn   int `json:"enemiesIn"`
	AlliesDead  int `json:"alliesDead"`
	EnemiesDead int `json:"enemiesDead"`
}

// dotaDeathCtx is one death with what the parsed match knows about it.
type dotaDeathCtx struct {
	Time   float64    `json:"time"`
	Killer string     `json:"killer,omitempty"`
	Region string     `json:"region,omitempty"`
	Theirs bool       `json:"theirs,omitempty"`
	Fight  *dotaFight `json:"fight,omitempty"`
	// Team gold lead at that minute, from the player's side (negative: behind).
	GoldAdv int  `json:"goldAdv"`
	BuyBack bool `json:"buyBack,omitempty"` // bought back within two minutes after
	Early   bool `json:"early,omitempty"`   // before 10:00
}

// dotaMatchFacts is what the review keeps from a parsed match beyond the
// per-player benchmarks.
type dotaMatchFacts struct {
	HeroNPC       string         `json:"heroNpc,omitempty"`
	LaneRole      int            `json:"laneRole,omitempty"`
	Participation float64        `json:"participation,omitempty"`
	DeadSeconds   int            `json:"deadSeconds,omitempty"`
	Obs           int            `json:"obs,omitempty"`
	Sen           int            `json:"sen,omitempty"`
	Buybacks      int            `json:"buybacks,omitempty"`
	Deaths        []dotaDeathCtx `json:"deaths,omitempty"`
	// Net worth by minute, the player's and the enemy hero in the same role.
	NetWorth      []int  `json:"netWorth,omitempty"`
	RivalNetWorth []int  `json:"rivalNetWorth,omitempty"`
	RivalHero     string `json:"rivalHero,omitempty"`
	// The team's gold lead by minute, from the player's side.
	GoldAdv []int `json:"goldAdv,omitempty"`
}

// dotaFactsFrom reads the parsed-match facts for the player at index me.
func dotaFactsFrom(m jsonMap, players []jsonMap, me int, heroes map[int]DotaHero) dotaMatchFacts {
	p := players[me]
	radiant := jI64(p, "player_slot") < 128
	f := dotaMatchFacts{}
	if h, ok := heroes[int(jI64(p, "hero_id"))]; ok {
		f.HeroNPC = "npc_dota_hero_" + h.Slug
	}
	f.LaneRole = int(jI64(p, "lane_role"))
	f.Participation = jF64(p, "teamfight_participation")
	// Source 2 life states: 0 alive, 1 dying, 2 dead.
	if ls := sub(p, "life_state"); ls != nil {
		f.DeadSeconds = int(jI64(ls, "1") + jI64(ls, "2"))
	}
	f.Obs, f.Sen = len(jList(p["obs_log"])), len(jList(p["sen_log"]))
	buybacks := jList(p["buyback_log"])
	f.Buybacks = len(buybacks)
	for _, v := range ints(p["networth_t"]) {
		f.NetWorth = append(f.NetWorth, v)
	}
	for _, v := range ints(m["radiant_gold_adv"]) {
		if !radiant {
			v = -v
		}
		f.GoldAdv = append(f.GoldAdv, v)
	}
	// The enemy in the same role, for the farm race.
	for _, q := range players {
		if (jI64(q, "player_slot") < 128) == radiant || int(jI64(q, "lane_role")) != f.LaneRole {
			continue
		}
		f.RivalNetWorth = ints(q["networth_t"])
		if h, ok := heroes[int(jI64(q, "hero_id"))]; ok {
			f.RivalHero = h.Name
		}
		break
	}
	// Deaths: OpenDota lists the player's own, with the killer; older parses
	// only have them in the killers' logs.
	type death struct {
		t      float64
		killer string
	}
	var deaths []death
	for _, d := range jList(p["deaths_log"]) {
		deaths = append(deaths, death{jF64(d, "time"), jStr(d, "key", "")})
	}
	if len(deaths) == 0 && f.HeroNPC != "" {
		for _, q := range players {
			if (jI64(q, "player_slot") < 128) == radiant {
				continue
			}
			killer := ""
			if h, ok := heroes[int(jI64(q, "hero_id"))]; ok {
				killer = "npc_dota_hero_" + h.Slug
			}
			for _, k := range jList(q["kills_log"]) {
				if jStr(k, "key", "") == f.HeroNPC {
					deaths = append(deaths, death{jF64(k, "time"), killer})
				}
			}
		}
		sort.Slice(deaths, func(i, j int) bool { return deaths[i].t < deaths[j].t })
	}
	fights := jList(m["teamfights"])
	for _, d := range deaths {
		ctx := dotaDeathCtx{Time: d.t, Early: d.t < 600}
		if h := heroByNPC(heroes, d.killer); h != "" {
			ctx.Killer = h
		}
		if minute := int(d.t / 60); minute >= 0 && minute < len(f.GoldAdv) {
			ctx.GoldAdv = f.GoldAdv[minute]
		}
		for _, b := range buybacks {
			if bt := jF64(b, "time"); bt > d.t && bt-d.t <= 120 {
				ctx.BuyBack = true
			}
		}
		for _, tf := range fights {
			start, end := jF64(tf, "start"), jF64(tf, "end")
			if d.t < start-2 || d.t > end+2 {
				continue
			}
			fp := jList(tf["players"])
			if len(fp) != len(players) {
				break
			}
			fight := &dotaFight{}
			for i, q := range fp {
				ally := (jI64(players[i], "player_slot") < 128) == radiant
				in := jF64(q, "damage") > 0 || jI64(q, "deaths") > 0 || len(sub(q, "killed")) > 0
				dead := int(jI64(q, "deaths"))
				if ally {
					if in {
						fight.AlliesIn++
					}
					fight.AlliesDead += dead
				} else {
					if in {
						fight.EnemiesIn++
					}
					fight.EnemiesDead += dead
				}
				if i == me {
					if x, y, ok := firstPos(sub(q, "deaths_pos")); ok {
						sp := spotOf(x, y, radiant)
						ctx.Region, ctx.Theirs = regionText(sp), sp.Theirs
					}
				}
			}
			ctx.Fight = fight
			break
		}
		f.Deaths = append(f.Deaths, ctx)
	}
	return f
}

func ints(v any) []int {
	var out []int
	list, _ := v.([]any)
	for _, x := range list {
		if n, ok := x.(float64); ok {
			out = append(out, int(n))
		}
	}
	return out
}

func heroByNPC(heroes map[int]DotaHero, npc string) string {
	slug := strings.TrimPrefix(npc, "npc_dota_hero_")
	if slug == "" || slug == npc {
		return ""
	}
	for _, h := range heroes {
		if h.Slug == slug {
			return h.Name
		}
	}
	return ""
}

// firstPos reads OpenDota's {"x": {"y": count}} death positions.
func firstPos(pos jsonMap) (x, y float64, ok bool) {
	for xs, raw := range pos {
		inner, _ := raw.(map[string]any)
		for ys := range inner {
			var xv, yv float64
			if _, err := fmt.Sscanf(xs, "%g", &xv); err != nil {
				continue
			}
			if _, err := fmt.Sscanf(ys, "%g", &yv); err != nil {
				continue
			}
			return xv, yv, true
		}
	}
	return 0, 0, false
}

// fightText says a death's fight in a few words.
func fightText(d dotaDeathCtx) string {
	if d.Fight == nil {
		if d.Killer != "" {
			return "picked off by " + d.Killer + " outside a fight"
		}
		return "picked off outside a fight"
	}
	f := d.Fight
	if f.AlliesIn <= 1 && f.EnemiesIn >= 2 {
		return fmt.Sprintf("alone against %d", f.EnemiesIn)
	}
	switch {
	case f.EnemiesDead == 0:
		return fmt.Sprintf("lost the fight %d for 0", f.AlliesDead)
	case f.AlliesDead > f.EnemiesDead:
		return fmt.Sprintf("lost the fight %d for %d", f.AlliesDead, f.EnemiesDead)
	case f.AlliesDead < f.EnemiesDead:
		return fmt.Sprintf("won the fight %d for %d", f.EnemiesDead, f.AlliesDead)
	}
	return fmt.Sprintf("traded %d for %d", f.AlliesDead, f.EnemiesDead)
}

// dotaFactPoints turns the facts into review points and plain facts.
func dotaFactPoints(f dotaMatchFacts, duration int, support bool) (wrong, good []ReviewPoint, facts []string) {
	minutes := math.Max(1, float64(duration)/60)
	alone, theirs, lost, behindTheirs, wastedBB := 0, 0, 0, 0, 0
	for _, d := range f.Deaths {
		if d.Fight != nil && d.Fight.AlliesIn <= 1 && d.Fight.EnemiesIn >= 2 {
			alone++
		}
		if d.Theirs {
			theirs++
			if d.GoldAdv <= -4000 {
				behindTheirs++
			}
		}
		if d.Fight != nil && d.Fight.AlliesDead >= d.Fight.EnemiesDead+2 {
			lost++
		}
		if d.BuyBack {
			// Bought back, then died again within two minutes of the buyback.
			for _, e := range f.Deaths {
				if e.Time > d.Time && e.Time-d.Time <= 180 {
					wastedBB++
					break
				}
			}
		}
	}
	n := len(f.Deaths)
	add := func(kind, title, detail, habit string, w float64, v float64) {
		wrong = append(wrong, ReviewPoint{Kind: kind, Title: title, Detail: detail, Habit: habit, Weight: w, Value: &v})
	}
	if alone >= 2 {
		add("alone_deaths", "Caught alone", fmt.Sprintf("%d of %d deaths with no ally in the fight", alone, n),
			"Before you leave your team, look at the minimap: if you can't see three enemies, don't go.", 0.35+0.08*float64(alone), float64(alone))
	}
	if theirs >= 2 {
		add("enemy_side", "Died on their side of the map", fmt.Sprintf("%d of %d deaths past the river", theirs, n),
			"Cross the river only with vision and a reason: a tower, Roshan, or your team already there.", 0.3+0.08*float64(theirs), float64(theirs))
	}
	if behindTheirs >= 1 && theirs >= 2 {
		add("fought_behind", "Took fights while behind", fmt.Sprintf("%d deaths on their side with your team 4k+ gold down", behindTheirs),
			"When behind, fight only at your own towers: let them come to you and farm what's safe until your next item.", 0.4, float64(behindTheirs))
	}
	if lost >= 2 {
		add("lost_fights", "Took losing fights", fmt.Sprintf("%d fights lost by two or more", lost),
			"Count before you commit: if they have more heroes nearby or a key ultimate up, back off and fight later.", 0.3+0.08*float64(lost), float64(lost))
	}
	if wastedBB >= 1 {
		add("wasted_buyback", "Wasted buyback", fmt.Sprintf("Bought back and died again within three minutes, %d time%s", wastedBB, plural(wastedBB)),
			"Buy back only to save the base or finish a won fight; walking back into a lost one doubles the loss.", 0.45, float64(wastedBB))
	}
	if f.DeadSeconds > 0 {
		pct := float64(f.DeadSeconds) / float64(duration) * 100
		facts = append(facts, fmt.Sprintf("Dead for %s, %.0f%% of the game", FmtClock(float64(f.DeadSeconds)), pct))
		if pct >= 12 {
			add("dead_time", "Too long dead", fmt.Sprintf("%.0f%% of the game spent dead", pct),
				"Each death costs the timer plus the walk back: in a close game, one fewer death is worth an item.", 0.3+pct/100, pct)
		}
	}
	if f.Participation > 0 {
		facts = append(facts, fmt.Sprintf("In %.0f%% of the team's fights", f.Participation*100))
		if f.Participation < 0.5 && !support {
			add("participation", "Missed half the fights", fmt.Sprintf("In %.0f%% of the team's fights", f.Participation*100),
				"When your team starts a fight, TP or walk to it: a 4v5 lost is worse than the creeps you skipped.", 0.35, f.Participation*100)
		} else if f.Participation >= 0.8 {
			good = append(good, ReviewPoint{Kind: "participation", Title: "In the fights", Detail: fmt.Sprintf("In %.0f%% of the team's fights", f.Participation*100), Weight: f.Participation})
		}
	}
	if support {
		facts = append(facts, fmt.Sprintf("%d observer and %d sentry wards placed", f.Obs, f.Sen))
		if float64(f.Obs) < minutes/4 && minutes >= 20 {
			add("vision", "Little vision", fmt.Sprintf("%d observer wards in %.0f minutes", f.Obs, minutes),
				"Buy both wards every time they're available and place them where the next fight is, not where the last one was.", 0.35, float64(f.Obs))
		}
	}
	// The farm race against the same role on the other side.
	if f.RivalHero != "" && len(f.NetWorth) > 10 && len(f.RivalNetWorth) > 10 {
		at := 20
		if len(f.NetWorth) <= at || len(f.RivalNetWorth) <= at {
			at = min(len(f.NetWorth), len(f.RivalNetWorth)) - 1
		}
		gap := f.NetWorth[at] - f.RivalNetWorth[at]
		facts = append(facts, fmt.Sprintf("Net worth at %d:00: %s vs %s for %s", at, fmtThousands(int64(f.NetWorth[at])), fmtThousands(int64(f.RivalNetWorth[at])), f.RivalHero))
		if !support {
			if gap <= -3000 {
				add("outfarmed", "Out-farmed by "+f.RivalHero, fmt.Sprintf("%s gold behind the enemy %s at %d:00", fmtThousands(int64(-gap)), f.RivalHero, at),
					"Between fights, always have the next camp or wave in mind; walking without a target is where the gap opens.", 0.4, float64(-gap))
			} else if gap >= 3000 {
				good = append(good, ReviewPoint{Kind: "outfarmed", Title: "Out-farmed " + f.RivalHero, Detail: fmt.Sprintf("%s gold ahead of the enemy %s at %d:00", fmtThousands(int64(gap)), f.RivalHero, at), Weight: 0.6})
			}
		}
	}
	if len(f.GoldAdv) > 0 {
		last := f.GoldAdv[len(f.GoldAdv)-1]
		worst, worstAt := 0, 0
		for i, v := range f.GoldAdv {
			if v < worst {
				worst, worstAt = v, i
			}
		}
		if worst <= -5000 && worstAt < len(f.GoldAdv)-1 && last-worst >= 3000 {
			facts = append(facts, fmt.Sprintf("Team gold: %s down at %d:00, %s at the end", fmtThousands(int64(-worst)), worstAt, signed(last)))
		} else {
			facts = append(facts, fmt.Sprintf("Team gold at the end: %s", signed(last)))
		}
	}
	return wrong, good, facts
}

func signed(n int) string {
	if n >= 0 {
		return "+" + fmtThousands(int64(n))
	}
	return "-" + fmtThousands(int64(-n))
}

func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}
