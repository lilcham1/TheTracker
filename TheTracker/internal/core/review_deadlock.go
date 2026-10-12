package core

import (
	"fmt"
	"math"
	"sort"
	"strings"
	"time"
)

// deadlockReviewRaw is the part of a match's metadata a review needs, for
// the player only (the whole payload is about a megabyte).
type deadlockReviewRaw struct {
	MatchID   uint64          `json:"matchId"`
	Duration  int             `json:"duration"`
	StartTime int64           `json:"startTime"`
	Won       bool            `json:"won"`
	HeroID    int             `json:"heroId"`
	Kills     int             `json:"kills"`
	Deaths    int             `json:"deaths"`
	Assists   int             `json:"assists"`
	NetWorth  int64           `json:"netWorth"`
	LastHits  int             `json:"lastHits"`
	DeathLog  []deadlockDeath `json:"deathLog"`
	// Creeps last-hit out of those that could have been, by 10:00.
	LaneCreeps, LanePossible int64
	DamageDealt, DamageTaken int64
	SoulsLostToDeaths        int64
	// Each item bought, with when (for item timing goals).
	Buys []deadlockBuy `json:"buys,omitempty"`
}

type deadlockBuy struct {
	At int   `json:"at"`
	ID int64 `json:"id"`
}

type deadlockDeath struct {
	At         int     `json:"at"`
	KillerHero int     `json:"killerHero"`
	TimeToKill float64 `json:"ttk"`
}

func (d *Deadlock) reviewRaw(matchID uint64) (deadlockReviewRaw, error) {
	me := d.store.LoadLink("deadlock").AccountID
	if me == nil {
		return deadlockReviewRaw{}, fmt.Errorf("Connect your Steam account to review Deadlock matches.")
	}
	out, _, err := cachedFetch(d.store, fmt.Sprintf("dl_review2_%d", matchID), 30*24*time.Hour, false, func() (deadlockReviewRaw, error) {
		var v jsonMap
		if err := d.api.get(fmt.Sprintf("/v1/matches/%d/metadata", matchID), &v); err != nil {
			return deadlockReviewRaw{}, err
		}
		return deadlockReviewFrom(v, *me)
	})
	return out, err
}

// deadlockReviewFrom reads the player's part of a match's metadata.
func deadlockReviewFrom(v jsonMap, me uint64) (deadlockReviewRaw, error) {
	info := sub(v, "match_info")
	if info == nil {
		info = v
	}
	players := jList(info["players"])
	heroBySlot := map[int64]int{}
	for _, p := range players {
		heroBySlot[jI64(p, "player_slot")] = int(jI64(p, "hero_id"))
	}
	out := deadlockReviewRaw{MatchID: jU64(info, "match_id"), Duration: int(jI64(info, "duration_s")), StartTime: jI64(info, "start_time"), DeathLog: []deadlockDeath{}}
	for _, p := range players {
		if jU64(p, "account_id") != me {
			continue
		}
		out.Won = jI64(p, "team") == jI64(info, "winning_team")
		out.HeroID = int(jI64(p, "hero_id"))
		out.Kills, out.Deaths, out.Assists = int(jI64(p, "kills")), int(jI64(p, "deaths")), int(jI64(p, "assists"))
		out.NetWorth, out.LastHits = jI64(p, "net_worth"), int(jI64(p, "last_hits"))
		for _, it := range jList(p["items"]) {
			if t := jI64(it, "game_time_s"); t > 0 {
				out.Buys = append(out.Buys, deadlockBuy{At: int(t), ID: jI64(it, "item_id")})
			}
		}
		for _, dd := range jList(p["death_details"]) {
			out.DeathLog = append(out.DeathLog, deadlockDeath{At: int(jI64(dd, "game_time_s")), KillerHero: heroBySlot[jI64(dd, "killer_player_slot")], TimeToKill: jF64(dd, "time_to_kill_s")})
		}
		// Stats are snapshots over the match: the one at the end of the lane
		// (the last at or before 10:00) and the final one.
		var lane, last jsonMap
		for _, s := range jList(p["stats"]) {
			if jI64(s, "time_stamp_s") <= 600 {
				lane = s
			}
			last = s
		}
		if lane != nil {
			out.LaneCreeps, out.LanePossible = jI64(lane, "creep_kills"), jI64(lane, "possible_creeps")
		}
		if last != nil {
			out.DamageDealt, out.DamageTaken = jI64(last, "player_damage"), jI64(last, "player_damage_taken")
			out.SoulsLostToDeaths = jI64(last, "gold_death_loss")
		}
		return out, nil
	}
	return out, fmt.Errorf("Your account isn't in that match's data.")
}

// DeadlockReview reviews one match; 0 means the latest.
func (a *App) DeadlockReview(matchID uint64) (MatchReview, error) {
	ov, err := a.Deadlock.Overview(30, false)
	if err != nil {
		return MatchReview{}, err
	}
	if matchID == 0 {
		for _, m := range ov.Matches {
			if !m.Abandoned {
				matchID = m.MatchID
				break
			}
		}
		if matchID == 0 {
			return MatchReview{}, errNoReviewMatch
		}
	}
	raw, err := a.Deadlock.reviewRaw(matchID)
	if err != nil {
		return MatchReview{}, err
	}
	heroes := a.Deadlock.Heroes()
	hero := heroes[raw.HeroID]
	won := raw.Won
	r := MatchReview{Game: "deadlock", MatchID: matchID, Hero: hero.Name, HeroImage: hero.Image, Won: &won, Duration: raw.Duration,
		StartTime: raw.StartTime, Kills: raw.Kills, Deaths: raw.Deaths, Assists: raw.Assists,
		Wrong: []ReviewPoint{}, Good: []ReviewPoint{}, DeathLog: []ReviewDeath{}, Notes: []string{}}
	if r.Hero == "" {
		r.Hero = fmt.Sprintf("Hero %d", raw.HeroID)
	}
	heroName := func(id int) string {
		if h, ok := heroes[id]; ok {
			return h.Name
		}
		return "an enemy"
	}

	// The player's usual, from their other recent matches.
	var souls, lh, deaths []float64
	for _, m := range ov.Matches {
		if m.MatchID == matchID || m.Abandoned || m.DurationSeconds < 600 {
			continue
		}
		mins := float64(m.DurationSeconds) / 60
		souls = append(souls, float64(m.NetWorth)/mins)
		lh = append(lh, float64(m.LastHits)/mins)
		deaths = append(deaths, float64(m.Deaths))
	}
	mins := math.Max(1, float64(raw.Duration)/60)
	var wrong, good []ReviewPoint
	compare := func(kind string, v float64, usual []float64, unit, low, high, habit string, lowerIsBetter bool) {
		if len(usual) < 3 {
			return
		}
		avg := mean(usual)
		if avg <= 0 {
			return
		}
		ratio := v / avg
		val := v
		worse, better := ratio < 0.85, ratio > 1.15
		if lowerIsBetter {
			worse, better = ratio > 1.3 && v-avg >= 2, ratio < 0.7
		}
		switch {
		case worse:
			wrong = append(wrong, ReviewPoint{Kind: kind, Title: low, Detail: fmt.Sprintf("%s (you usually get %s)", fmtReviewNum(v, unit), fmtReviewNum(avg, unit)), Habit: habit, Weight: math.Min(0.6, math.Abs(1-ratio)), Value: &val})
		case better:
			good = append(good, ReviewPoint{Kind: kind, Title: high, Detail: fmt.Sprintf("%s (you usually get %s)", fmtReviewNum(v, unit), fmtReviewNum(avg, unit)), Weight: math.Min(0.6, math.Abs(1-ratio)), Value: &val})
		}
	}
	compare("souls", float64(raw.NetWorth)/mins, souls, "souls a minute", "Low souls per minute", "Strong souls per minute",
		"Between fights, clear a camp or push a lane: souls lying around are souls the enemy takes.", false)
	compare("last_hits", float64(raw.LastHits)/mins, lh, "last hits a minute", "Few last hits", "Good last hits",
		"Last-hit creeps and pop the orbs: in lane, a missed orb is a gift to the enemy.", false)
	compare("deaths", float64(raw.Deaths), deaths, "deaths", "More deaths than usual", "Fewer deaths than usual",
		"Before pushing forward, check where the enemies are; if you can't see them, assume they see you.", true)

	if raw.LanePossible >= 20 {
		eff := float64(raw.LaneCreeps) / float64(raw.LanePossible) * 100
		if eff < 50 {
			wrong = append(wrong, ReviewPoint{Kind: "lane_cs", Title: "Missed lane creeps", Detail: fmt.Sprintf("%.0f%% of the creeps in lane by 10:00", eff),
				Habit: "In lane, last-hit every creep you can and deny theirs: the first 10 minutes set up the game.", Weight: (50 - eff) / 100 * 2, Value: &eff})
		} else if eff >= 72 {
			good = append(good, ReviewPoint{Kind: "lane_cs", Title: "Strong lane farm", Detail: fmt.Sprintf("%.0f%% of the creeps in lane by 10:00", eff), Weight: eff / 100, Value: &eff})
		}
	}
	if raw.SoulsLostToDeaths >= 2500 {
		v := float64(raw.SoulsLostToDeaths)
		wrong = append(wrong, ReviewPoint{Kind: "souls_lost", Title: "Souls lost to deaths", Detail: fmt.Sprintf("%s souls lost when you died", fmtThousands(raw.SoulsLostToDeaths)),
			Habit: "Spend souls before you fight: unspent souls are what a death takes.", Weight: math.Min(0.5, v/10000), Value: &v})
	}
	if raw.DamageTaken > 0 && raw.DamageDealt > 0 {
		ratio := float64(raw.DamageDealt) / float64(raw.DamageTaken)
		if ratio < 0.6 {
			wrong = append(wrong, ReviewPoint{Kind: "trades", Title: "Lost your trades", Detail: fmt.Sprintf("%s damage dealt to heroes, %s taken", fmtThousands(raw.DamageDealt), fmtThousands(raw.DamageTaken)),
				Habit: "Fight from cover and only take trades when your shots are landing; back off when outnumbered.", Weight: 0.6 - ratio, Value: &ratio})
		} else if ratio >= 1.5 {
			good = append(good, ReviewPoint{Kind: "trades", Title: "Won your trades", Detail: fmt.Sprintf("%s damage dealt to heroes, %s taken", fmtThousands(raw.DamageDealt), fmtThousands(raw.DamageTaken)), Weight: math.Min(1, ratio/3), Value: &ratio})
		}
	}

	// The deaths themselves: when, to whom, how fast.
	early, fast := 0, 0
	byKiller := map[int]int{}
	for _, dd := range raw.DeathLog {
		note := ""
		if dd.TimeToKill > 0 && dd.TimeToKill < 2 {
			fast++
			note = fmt.Sprintf("down in %.1f s: caught out or burst", dd.TimeToKill)
		}
		if dd.At < 600 {
			early++
		}
		if dd.KillerHero != 0 {
			byKiller[dd.KillerHero]++
		}
		r.DeathLog = append(r.DeathLog, ReviewDeath{Clock: FmtClock(float64(dd.At)), Killer: heroName(dd.KillerHero), Note: note})
	}
	if early >= 2 {
		v := float64(early)
		wrong = append(wrong, ReviewPoint{Kind: "lane_deaths", Title: "Died in lane", Detail: fmt.Sprintf("%d deaths before 10:00", early),
			Habit: "In lane, stay where your support can reach you and leave when your health drops below half.", Weight: 0.2 + 0.1*float64(early), Value: &v})
	}
	if fast >= 2 {
		v := float64(fast)
		wrong = append(wrong, ReviewPoint{Kind: "caught", Title: "Caught out", Detail: fmt.Sprintf("%d of %d deaths took under 2 seconds", fast, len(raw.DeathLog)),
			Habit: "Move between cover and stay near a teammate; don't stand in the open without vision.", Weight: 0.2 + 0.08*float64(fast), Value: &v})
	}
	type killer struct{ hero, n int }
	var killers []killer
	for h, n := range byKiller {
		killers = append(killers, killer{h, n})
	}
	sort.Slice(killers, func(i, j int) bool { return killers[i].n > killers[j].n })
	if len(killers) > 0 && killers[0].n >= 3 && float64(killers[0].n) >= 0.4*float64(len(raw.DeathLog)) {
		name := heroName(killers[0].hero)
		v := float64(killers[0].n)
		wrong = append(wrong, ReviewPoint{Kind: "nemesis", Title: "One hero kept killing you", Detail: fmt.Sprintf("%s: %d of your %d deaths", name, killers[0].n, len(raw.DeathLog)),
			Habit: fmt.Sprintf("Track where %s is before you move, and keep your escape for them.", name), Weight: 0.25 + 0.05*v, Value: &v})
	}

	// Goals from the player's own best games at the chosen level.
	if set := a.deadlockGoals(matchID, raw, ov, souls, deaths); set != nil {
		r.Goals = set
		have := map[string]bool{}
		for _, p := range wrong {
			have[p.Kind] = true
		}
		same := map[string]string{"goal_souls": "souls", "goal_deaths": "deaths", "goal_lane10": "lane_cs"}
		for _, p := range goalPoints(*set) {
			if !have[same[p.Kind]] {
				wrong = append(wrong, p)
			}
		}
	}
	r.Wrong, r.Good = sortPoints(wrong, 4), sortPoints(good, 3)
	r.Verdict = reviewVerdict(won, r.Wrong, r.Good)
	return r, nil
}

func fmtReviewNum(v float64, unit string) string {
	if v >= 100 {
		return fmt.Sprintf("%.0f %s", v, unit)
	}
	if v == math.Trunc(v) {
		return fmt.Sprintf("%.0f %s", v, unit)
	}
	return fmt.Sprintf("%.1f %s", v, unit)
}

// DeadlockWorkOn tallies the issues of the last ten matches.
func (a *App) DeadlockWorkOn() (WorkOn, error) {
	ov, err := a.Deadlock.Overview(30, false)
	if err != nil {
		return WorkOn{}, err
	}
	var ids []uint64
	for _, m := range ov.Matches {
		if !m.Abandoned && len(ids) < 10 {
			ids = append(ids, m.MatchID)
		}
	}
	return tallyWorkOn("deadlock", reviewMany(ids, a.DeadlockReview)), nil
}

// itemCosts: what each Deadlock item costs, by id (public item list).
func (d *Deadlock) itemCosts() map[int64]int64 {
	m, _, _ := cachedFetch(d.store, "dl_item_costs", 24*time.Hour, false, func() (map[int64]int64, error) {
		var rows []jsonMap
		if err := d.api.get("/v1/assets/items", &rows); err != nil {
			return nil, err
		}
		out := map[int64]int64{}
		for _, r := range rows {
			if c := jI64(r, "cost"); c > 0 {
				out[jI64(r, "id")] = c
			}
		}
		return out, nil
	})
	return m
}

// tierTimes: when the first item of at least 3,200 and 6,400 souls was
// bought; 0 when never.
func tierTimes(buys []deadlockBuy, costs map[int64]int64) (t3, t4 int) {
	for _, b := range buys {
		c := costs[b.ID]
		if c >= 3200 && (t3 == 0 || b.At < t3) {
			t3 = b.At
		}
		if c >= 6400 && (t4 == 0 || b.At < t4) {
			t4 = b.At
		}
	}
	return
}

// deadlockGoals: targets from the player's own recent games — souls a
// minute and deaths from the match list, lane last hits and item timings
// from the matches already reviewed — at the chosen level.
func (a *App) deadlockGoals(matchID uint64, raw deadlockReviewRaw, ov DeadlockOverview, souls, deaths []float64) *GoalSet {
	pct, label := goalLevel(a.Store.LoadPrefs().Coach.DeadlockLevel)
	who := "your best " + strings.TrimPrefix(label, "top ") + " of recent games"
	set := GoalSet{Game: "deadlock", Hero: "", Level: label, Goals: []Goal{}}
	costs := a.Deadlock.itemCosts()
	var lanes, t3s, t4s []float64
	for _, m := range ov.Matches {
		if m.MatchID == matchID {
			continue
		}
		var o deadlockReviewRaw
		if _, ok := a.Store.ReadCacheStale(fmt.Sprintf("dl_review2_%d", m.MatchID), &o); !ok {
			continue
		}
		if o.LanePossible >= 20 {
			lanes = append(lanes, float64(o.LaneCreeps))
		}
		t3, t4 := tierTimes(o.Buys, costs)
		if t3 > 0 {
			t3s = append(t3s, float64(t3))
		}
		if t4 > 0 {
			t4s = append(t4s, float64(t4))
		}
	}
	add := func(g Goal, have *float64) {
		g.Value, g.State = have, "missed"
		switch {
		case have == nil:
		case g.AtMost && *have <= g.Target, !g.AtMost && *have >= g.Target:
			g.State = "met"
		}
		set.Goals = append(set.Goals, g)
	}
	v := func(x float64) *float64 { return &x }
	mins := math.Max(1, float64(raw.Duration)/60)
	if len(souls) >= 5 {
		add(Goal{Key: "souls", Label: "Souls a minute", Format: "count", Target: math.Round(quantile(souls, pct)), Why: who}, v(float64(raw.NetWorth)/mins))
	}
	if len(deaths) >= 5 {
		add(Goal{Key: "deaths", Label: "Deaths at most", Format: "count", Target: math.Round(quantile(deaths, 1-pct)), AtMost: true, Why: who}, v(float64(raw.Deaths)))
	}
	if len(lanes) >= 3 && raw.LanePossible >= 20 {
		add(Goal{Key: "lane10", Label: "Last hits in lane by 10:00", Format: "count", Target: math.Round(quantile(lanes, pct)), At: 600, Why: who}, v(float64(raw.LaneCreeps)))
	}
	t3, t4 := tierTimes(raw.Buys, costs)
	if len(t3s) >= 3 {
		var have *float64
		if t3 > 0 {
			have = v(float64(t3))
		}
		add(Goal{Key: "tier3", Label: "First 3,200-soul item by", Format: "time", Target: roundTo(quantile(t3s, 1-pct)), AtMost: true, Why: who}, have)
	}
	if len(t4s) >= 3 && raw.Duration > int(quantile(t4s, 1-pct)) {
		var have *float64
		if t4 > 0 {
			have = v(float64(t4))
		}
		add(Goal{Key: "tier4", Label: "First 6,400-soul item by", Format: "time", Target: roundTo(quantile(t4s, 1-pct)), AtMost: true, Why: who}, have)
	}
	if len(set.Goals) == 0 {
		return nil
	}
	if len(lanes) < 3 || len(t3s) < 3 {
		set.Note = "Lane and item goals appear once a few more of your matches have been reviewed (What to work on reviews your last ten)."
	}
	return &set
}
