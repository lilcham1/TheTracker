package core

// Goals for the role and hero: what a good game looks like — last hits and
// net worth at 10 and 20 minutes, when the key items should be done, how few
// deaths — taken from how the better players on the same hero do (OpenDota's
// public hero benchmarks) or, for Deadlock, from the player's own best games.

import (
	"fmt"
	"math"
	"sort"
	"strings"
	"sync"
	"time"
)

// Goal is one target, and how the match measured up.
type Goal struct {
	Key   string `json:"key"`
	Label string `json:"label"`
	// count | gold | time
	Format string  `json:"format"`
	Target float64 `json:"target"`
	// Checked at this game clock (seconds); 0: at the end of the match.
	At int `json:"at"`
	// The target is a most (deaths) or, for item timings, a latest time.
	AtMost bool     `json:"atMost,omitempty"`
	Why    string   `json:"why"`
	Value  *float64 `json:"value,omitempty"`
	// pending | met | missed
	State string `json:"state"`
}

type GoalSet struct {
	Game  string `json:"game"`
	Role  string `json:"role"`
	Hero  string `json:"hero"`
	Level string `json:"level"`
	Goals []Goal `json:"goals"`
	Note  string `json:"note,omitempty"`
}

// Goal levels: the percentile aimed at.
var goalLevels = map[string]struct {
	pct   float64
	label string
}{
	"steady": {0.5, "top 50%"},
	"push":   {0.7, "top 30%"},
	"pro":    {0.8, "top 20%"},
}

func goalLevel(level string) (float64, string) {
	if l, ok := goalLevels[level]; ok {
		return l.pct, l.label
	}
	return goalLevels["push"].pct, goalLevels["push"].label
}

// ---------- Dota ----------

type benchPoint struct {
	Percentile float64 `json:"percentile"`
	Value      float64 `json:"value"`
}

// heroBenchmarks: per metric, the value at each percentile, for one hero.
func (d *Dota) heroBenchmarks(heroID int) (map[string][]benchPoint, error) {
	out, _, err := cachedFetch(d.store, fmt.Sprintf("od_bench_%d", heroID), 24*time.Hour, false, func() (map[string][]benchPoint, error) {
		var v struct {
			Result map[string][]benchPoint `json:"result"`
		}
		err := d.api.get(fmt.Sprintf("/benchmarks?hero_id=%d", heroID), &v)
		return v.Result, err
	})
	return out, err
}

// benchAt reads a metric at a percentile, between the published points.
func benchAt(points []benchPoint, p float64) float64 {
	if len(points) == 0 {
		return 0
	}
	sort.Slice(points, func(i, j int) bool { return points[i].Percentile < points[j].Percentile })
	if p <= points[0].Percentile {
		return points[0].Value
	}
	for i := 1; i < len(points); i++ {
		if p <= points[i].Percentile {
			a, b := points[i-1], points[i]
			return a.Value + (b.Value-a.Value)*(p-a.Percentile)/(b.Percentile-a.Percentile)
		}
	}
	return points[len(points)-1].Value
}

// heroItemPopularity: how often each item is bought, by game phase.
func (d *Dota) heroItemPopularity(heroID int) (map[string]map[string]int, error) {
	out, _, err := cachedFetch(d.store, fmt.Sprintf("od_itempop_%d", heroID), 24*time.Hour, false, func() (map[string]map[string]int, error) {
		var v map[string]map[string]int
		err := d.api.get(fmt.Sprintf("/heroes/%d/itemPopularity", heroID), &v)
		return v, err
	})
	return out, err
}

// Items that support heroes build first, to time a support's item goal.
var dotaSupportItems = map[string]bool{"glimmer_cape": true, "force_staff": true, "ghost": true, "cyclone": true, "solar_crest": true,
	"aether_lens": true, "pavise": true, "mekansm": true, "pipe": true, "lotus_orb": true, "guardian_greaves": true, "spirit_vessel": true,
	"urn_of_shadows": true, "holy_locket": true, "boots_of_bearing": true, "blink": true, "rod_of_atos": true, "orchid": true}

// coreItems picks the hero's most-bought items that make the build: the
// big mid-game items for a core, the usual first utility item for a support.
func coreItems(pop map[string]map[string]int, items map[int]dotaItem, support bool) []dotaItem {
	type scored struct {
		it    dotaItem
		count int
	}
	var list []scored
	seen := map[string]bool{}
	phases := []string{"mid_game_items"}
	if support {
		phases = []string{"early_game_items", "mid_game_items"}
	}
	for _, phase := range phases {
		for id, n := range pop[phase] {
			var key int
			fmt.Sscan(id, &key)
			it, ok := items[key]
			if !ok || seen[it.Key] {
				continue
			}
			boots := strings.Contains(it.Key, "boots") || it.Key == "power_treads" || it.Key == "travel_boots"
			if support {
				if !dotaSupportItems[it.Key] {
					continue
				}
			} else if it.Cost < 1800 || boots || strings.HasPrefix(it.Key, "recipe") || strings.Contains(it.Key, "ward") {
				continue
			}
			seen[it.Key] = true
			list = append(list, scored{it, n})
		}
	}
	// A part of another item on the list (Yasha on the way to Manta) is
	// bought on the way, not a goal of its own.
	part := map[string]bool{}
	for _, s := range list {
		for _, c := range s.it.Components {
			part[c] = true
		}
	}
	kept := list[:0]
	for _, s := range list {
		if !part[s.it.Key] {
			kept = append(kept, s)
		}
	}
	list = kept
	sort.Slice(list, func(i, j int) bool { return list[i].count > list[j].count })
	n := 2
	if support {
		n = 1
	}
	out := []dotaItem{}
	for i := 0; i < len(list) && i < n; i++ {
		out = append(out, list[i].it)
	}
	return out
}

// How much of a match's average gold rate comes in before the midgame.
const earlyGoldShare = 0.65

// roundTo rounds a time to the nearest 15 seconds.
func roundTo(sec float64) float64 { return math.Round(sec/15) * 15 }

// DotaGoals: the goals for a hero and role (core | support), at a level.
func (a *App) DotaGoals(heroID int, role, level string) (GoalSet, error) {
	hero := a.Dota.Heroes()[heroID]
	pct, levelLabel := goalLevel(level)
	set := GoalSet{Game: "dota", Hero: hero.Name, Level: levelLabel, Goals: []Goal{}}
	bench, err := a.Dota.heroBenchmarks(heroID)
	if err != nil {
		return set, err
	}
	who := fmt.Sprintf("%s of %s players", levelLabel, hero.Name)
	gpm := benchAt(bench["gold_per_min"], pct)
	lhpm := benchAt(bench["last_hits_per_min"], pct)
	// Fewer deaths is better: the better players sit low on this scale.
	deathRate := benchAt(bench["deaths_per_min"], 1-pct)
	assistRate := benchAt(bench["assists_per_min"], pct)
	support := role == "support"
	set.Role = "Core"
	if support {
		set.Role = "Support"
	}
	if pos := a.heroPosition(hero.Slug); pos != "" {
		set.Role = map[string]string{"carry": "Carry", "mid": "Mid", "offlane": "Offlane", "support": "Support", "hard_support": "Hard support"}[pos]
		if support && pos != "support" && pos != "hard_support" {
			set.Role = "Support"
		}
		if !support && (pos == "support" || pos == "hard_support") {
			set.Role = "Core"
		}
	}

	if !support {
		set.Goals = append(set.Goals,
			Goal{Key: "lh10", Label: "Last hits by 10:00", Format: "count", Target: math.Round(lhpm * 10 * 0.8), At: 600, Why: fmt.Sprintf("%.1f last hits a minute: %s", lhpm, who)},
			Goal{Key: "nw10", Label: "Net worth at 10:00", Format: "gold", Target: math.Round((600+gpm*10*0.75)/50) * 50, At: 600, Why: fmt.Sprintf("%.0f gold a minute: %s", gpm, who)},
		)
	}
	nw20Factor := 0.95
	if support {
		nw20Factor = 0.9
	}
	set.Goals = append(set.Goals, Goal{Key: "nw20", Label: "Net worth at 20:00", Format: "gold", Target: math.Round((600+gpm*20*nw20Factor)/50) * 50, At: 1200, Why: fmt.Sprintf("%.0f gold a minute: %s", gpm, who)})
	if support {
		set.Goals = append(set.Goals, Goal{Key: "assists20", Label: "Assists by 20:00", Format: "count", Target: math.Round(assistRate * 20 * 0.85), At: 1200, Why: fmt.Sprintf("%.2f assists a minute: %s", assistRate, who)})
	}
	set.Goals = append(set.Goals, Goal{Key: "deaths", Label: "Deaths at most", Format: "count", Target: deathRate, AtMost: true, Why: fmt.Sprintf("%.2f deaths a minute: %s", deathRate, who)})

	// Item timings: the hero's usual build, at the target gold rate. Boots and
	// starting items come first (about 1,300 gold, 600 of it given at the
	// start), and gold comes in slower before the midgame than a whole
	// match's average: about two thirds of it.
	if pop, err := a.Dota.heroItemPopularity(heroID); err == nil && gpm > 0 {
		spent := 1300.0
		rate := gpm * earlyGoldShare / 60
		for _, it := range coreItems(pop, a.Dota.itemNames(), support) {
			spent += float64(it.Cost)
			set.Goals = append(set.Goals, Goal{Key: "item_" + it.Key, Label: it.Name + " by", Format: "time", Target: roundTo((spent - 600) / rate), AtMost: true,
				Why: fmt.Sprintf("the usual build, estimated from %.0f gold a minute", gpm)})
		}
	}
	return set, nil
}

// heroPosition is the hero's usual position from the cached meta.
func (a *App) heroPosition(slug string) string {
	var meta DotaMeta
	if _, ok := a.Store.ReadCacheStale("od_meta", &meta); ok {
		for _, h := range meta.Heroes {
			if h.Slug == slug {
				return h.Position
			}
		}
	}
	return ""
}

// dotaGoalFacts is what a match gave, for checking goals: by clock where
// it matters.
type dotaGoalFacts struct {
	Clock     float64 // seconds played so far (the length, once over)
	Ended     bool
	LastHits  map[int]float64 // by minute mark (5, 10, 15, 20)
	NetWorth  map[int]float64
	Assists   map[int]float64
	Deaths    int
	ItemTimes map[string]float64 // item key → first owned, game clock
	// Right now, for goals still ahead: progress so far.
	NowLastHits, NowNetWorth, NowAssists *float64
}

// checkDotaGoals fills each goal's value and state.
func checkDotaGoals(set GoalSet, f dotaGoalFacts) GoalSet {
	out := set
	out.Goals = make([]Goal, len(set.Goals))
	for i, g := range set.Goals {
		mark := g.At / 60
		var have *float64
		switch {
		case g.Key == "lh10":
			if v, ok := f.LastHits[mark]; ok {
				have = &v
			}
		case strings.HasPrefix(g.Key, "nw"):
			if v, ok := f.NetWorth[mark]; ok {
				have = &v
			}
		case g.Key == "assists20":
			if v, ok := f.Assists[mark]; ok {
				have = &v
			}
		case g.Key == "deaths":
			v := float64(f.Deaths)
			have = &v
			// A rate: so many deaths for the time played.
			g.Target = math.Max(1, math.Round(g.Target*math.Max(f.Clock, 600)/60))
		case strings.HasPrefix(g.Key, "item_"):
			if t, ok := f.ItemTimes[strings.TrimPrefix(g.Key, "item_")]; ok {
				have = &t
			}
		}
		g.Value = have
		g.State = "pending"
		// Still ahead: show the progress so far.
		if have == nil && g.At > 0 && f.Clock < float64(g.At) && !f.Ended {
			switch {
			case g.Key == "lh10":
				g.Value = f.NowLastHits
			case strings.HasPrefix(g.Key, "nw"):
				g.Value = f.NowNetWorth
			case g.Key == "assists20":
				g.Value = f.NowAssists
			}
			out.Goals[i] = g
			continue
		}
		switch {
		case g.Key == "deaths":
			if *have > g.Target {
				g.State = "missed"
			} else if f.Ended {
				g.State = "met"
			}
		case g.Format == "time":
			if have != nil {
				g.State = map[bool]string{true: "met", false: "missed"}[*have <= g.Target]
			} else if f.Clock > g.Target || f.Ended {
				g.State = "missed"
			}
		case have != nil:
			g.State = map[bool]string{true: "met", false: "missed"}[*have >= g.Target]
		case f.Clock > float64(g.At)+90 || f.Ended:
			// The mark passed without a reading (app started late).
			g.State = "unknown"
		}
		out.Goals[i] = g
	}
	return out
}

// goalPoints turns missed goals into review points.
func goalPoints(set GoalSet) []ReviewPoint {
	var out []ReviewPoint
	for _, g := range set.Goals {
		if g.State != "missed" || (g.Value == nil && g.Format != "time") {
			continue
		}
		var v float64
		var detail string
		if g.Value == nil {
			// An item goal never reached: not bought at all.
			v = g.Target * 1.5
			detail = fmt.Sprintf("not bought this match, goal %s (%s)", fmtGoal(g, g.Target), g.Why)
		} else {
			v = *g.Value
			detail = fmt.Sprintf("%s, goal %s (%s)", fmtGoal(g, v), fmtGoal(g, g.Target), g.Why)
		}
		habit := "Look at this goal at its time next match: it's the pace of the better players on this hero."
		switch {
		case g.Key == "lh10":
			habit = "Lane first: every creep counts before 10:00. Practise last hits in a demo with this hero."
		case strings.HasPrefix(g.Key, "nw"):
			habit = "Keep farming between fights: a lane or camp on the way, never walking empty."
		case g.Key == "deaths":
			habit = "Before moving forward, count the enemies on the minimap."
		case strings.HasPrefix(g.Key, "item_"), strings.HasPrefix(g.Key, "tier"):
			habit = "Farm toward your key item and don't buy anything in between that doesn't help get it."
		case g.Key == "assists20":
			habit = "Move with your cores and join their fights early."
		case g.Key == "souls":
			habit = "Between fights, clear a camp or push a lane: souls lying around are souls the enemy takes."
		case g.Key == "lane10":
			habit = "In lane, last-hit every creep you can and pop the orbs; deny theirs."
		}
		w := 0.3
		if g.Target > 0 {
			w = math.Min(0.6, math.Abs(v-g.Target)/g.Target+0.15)
		}
		out = append(out, ReviewPoint{Kind: "goal_" + g.Key, Title: "Missed goal: " + goalTitle(g), Detail: detail, Habit: habit, Weight: w, Value: &v})
	}
	return out
}

func fmtGoal(g Goal, v float64) string {
	switch g.Format {
	case "time":
		return FmtClock(v)
	case "gold":
		return fmtThousands(int64(v))
	}
	return fmt.Sprintf("%.0f", v)
}

// quantile of a set of values (0–1).
func quantile(vals []float64, q float64) float64 {
	if len(vals) == 0 {
		return 0
	}
	s := append([]float64{}, vals...)
	sort.Float64s(s)
	pos := q * float64(len(s)-1)
	lo := int(math.Floor(pos))
	hi := int(math.Ceil(pos))
	return s[lo] + (s[hi]-s[lo])*(pos-float64(lo))
}

// Item prices by key, kept in memory: the tracker asks on every payload,
// under its lock, so this never waits on the network — prices load in the
// background and count as 0 until they have.
var dotaItemCosts struct {
	sync.Mutex
	byKey   map[string]int64
	at      time.Time
	loading bool
}

// itemCost is what one item costs, by its key (no "item_" prefix).
func (d *Dota) itemCost(key string) int64 {
	dotaItemCosts.Lock()
	defer dotaItemCosts.Unlock()
	if !dotaItemCosts.loading && (dotaItemCosts.byKey == nil || time.Since(dotaItemCosts.at) > time.Hour) {
		dotaItemCosts.loading = true
		go func() {
			m := map[string]int64{}
			for _, it := range d.itemNames() {
				m[it.Key] = int64(it.Cost)
			}
			dotaItemCosts.Lock()
			if len(m) > 0 {
				dotaItemCosts.byKey = m
			}
			dotaItemCosts.at, dotaItemCosts.loading = time.Now(), false
			dotaItemCosts.Unlock()
		}()
	}
	return dotaItemCosts.byKey[key]
}

// ---------- Live goals (Dota) ----------

// The goals for the match on screen, worked out once per hero, role and
// level, in the background: the live feed must never wait on OpenDota.
var liveGoalCache struct {
	sync.Mutex
	key     string
	set     *GoalSet
	loading bool
	heroIDs map[string]int // hero internal name → id
}

// liveGoals is the goal set for a running match with its progress, or nil
// while it is being worked out (or goals are set by hand, or it's Turbo,
// whose pace no hero benchmark describes).
func (a *App) liveGoals(m *MatchState, role *LiveRole) *GoalSet {
	coach := a.Store.LoadPrefs().Coach
	if coach.DotaMode == "own" || m == nil || m.HeroName == nil || *m.HeroName == "" || m.GameType == "turbo" {
		return nil
	}
	r := "core"
	if role != nil {
		r = role.Role
	}
	hero := *m.HeroName
	key := hero + "/" + r + "/" + coach.DotaLevel
	liveGoalCache.Lock()
	if liveGoalCache.key != key {
		liveGoalCache.key, liveGoalCache.set = key, nil
	}
	set, loading := liveGoalCache.set, liveGoalCache.loading
	if set == nil && !loading {
		liveGoalCache.loading = true
		go func() {
			var out *GoalSet
			if id := a.dotaHeroID(hero); id > 0 {
				if s, err := a.DotaGoals(id, r, coach.DotaLevel); err == nil {
					out = &s
				}
			}
			liveGoalCache.Lock()
			if liveGoalCache.key == key {
				liveGoalCache.set = out
			}
			liveGoalCache.loading = false
			liveGoalCache.Unlock()
		}()
	}
	liveGoalCache.Unlock()
	if set == nil {
		return nil
	}
	checked := checkDotaGoals(*set, factsFromMatch(m))
	return &checked
}

// dotaHeroID turns "npc_dota_hero_x" into OpenDota's hero id.
func (a *App) dotaHeroID(name string) int {
	liveGoalCache.Lock()
	if id, ok := liveGoalCache.heroIDs[name]; ok {
		liveGoalCache.Unlock()
		return id
	}
	liveGoalCache.Unlock()
	slug := heroSlug(name)
	for id, h := range a.Dota.Heroes() {
		if h.Slug == slug {
			liveGoalCache.Lock()
			if liveGoalCache.heroIDs == nil {
				liveGoalCache.heroIDs = map[string]int{}
			}
			liveGoalCache.heroIDs[name] = id
			liveGoalCache.Unlock()
			return id
		}
	}
	return 0
}

// factsFromMatch reads what goals are checked against from a live match.
func factsFromMatch(m *MatchState) dotaGoalFacts {
	f := dotaGoalFacts{Clock: m.LastClockTime, Ended: m.Ended, Deaths: len(m.Deaths),
		LastHits: map[int]float64{}, NetWorth: map[int]float64{}, Assists: map[int]float64{}, ItemTimes: m.ItemTimes}
	for minute, cp := range m.Checkpoints {
		if cp == nil {
			continue
		}
		f.LastHits[minute] = float64(cp.LastHits)
		if cp.NetWorth > 0 {
			f.NetWorth[minute] = float64(cp.NetWorth)
		}
		if cp.Assists > 0 || minute <= 20 {
			f.Assists[minute] = float64(cp.Assists)
		}
	}
	now := func(v float64) *float64 { return &v }
	f.NowLastHits = now(float64(m.LastHits))
	if m.NetWorth != nil {
		f.NowNetWorth = now(float64(*m.NetWorth))
	}
	if m.Assists != nil {
		f.NowAssists = now(float64(*m.Assists))
	}
	return f
}

// goalTitle names a goal inside a sentence: item names keep their capitals.
func goalTitle(g Goal) string {
	label := strings.TrimSuffix(g.Label, " by")
	if g.Format == "time" {
		return label + " on time"
	}
	return strings.ToLower(label[:1]) + label[1:]
}
