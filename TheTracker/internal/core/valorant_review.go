package core

// Reviews of Valorant matches, from HenrikDev's full match data: every kill
// with the players' positions, round by round. Points compare the match
// with the player's own other matches of the same kind (ranked or not), and
// the death log says where each death was, whether it was traded, and how
// far the nearest teammate stood.

import (
	"fmt"
	"sort"
	"strings"
)

// ValorantReview reviews one match, the newest when id is empty.
func (a *App) ValorantReview(id string) (MatchReview, error) {
	ov, err := a.Valorant.Overview(false)
	if err != nil && len(ov.Matches) == 0 {
		return MatchReview{}, err
	}
	var m *ValMatch
	for i := range ov.Matches {
		if id == "" || ov.Matches[i].ID == id {
			m = &ov.Matches[i]
			break
		}
	}
	if m == nil {
		return MatchReview{}, errNoReviewMatch
	}
	r := MatchReview{Game: "valorant", MatchKey: m.ID, Hero: m.Agent + " · " + m.Map, Won: m.Won, Duration: m.Duration, StartTime: m.StartTime,
		Kills: m.Kills, Deaths: m.Deaths, Assists: m.Assists, Wrong: []ReviewPoint{}, Good: []ReviewPoint{}, DeathLog: []ReviewDeath{}, Notes: []string{}}
	if ag, ok := ov.Agents[strings.ToLower(m.Agent)]; ok && ag.Icon != "" {
		icon := ag.Icon
		r.HeroImage = &icon
	}
	var wrong, good []ReviewPoint
	add := func(kind, title, detail, habit string, w, v float64) {
		wrong = append(wrong, ReviewPoint{Kind: kind, Title: title, Detail: detail, Habit: habit, Weight: w, Value: &v})
	}
	r.Facts = append(r.Facts,
		fmt.Sprintf("%s, %d–%d, %s", m.Mode, m.TeamRounds, m.EnemyRounds, m.Map),
		fmt.Sprintf("ACS %.0f (#%d in your team, #%d in the match), ADR %.0f, headshots %.0f%%, KAST %.0f%%", m.ACS, m.TeamPlace, m.MatchPlace, m.ADR, m.HSPct, m.KAST),
		fmt.Sprintf("First bloods %d, first deaths %d, clutches %d, rounds with 3+ kills %d", m.FirstBloods, m.FirstDeaths, m.Clutches, m.MultiKills))
	if m.Plants+m.Defuses > 0 {
		r.Facts = append(r.Facts, fmt.Sprintf("Spike: %d planted, %d defused", m.Plants, m.Defuses))
	}

	// The same kind of match (ranked or not, no deathmatch), against this one.
	var others []ValMatch
	for _, o := range ov.Matches {
		if o.ID != m.ID && o.Ranked == m.Ranked && o.Rounds >= 8 {
			others = append(others, o)
		}
	}
	if len(others) >= 5 && m.Rounds >= 8 {
		var acs, hs, fd, util []float64
		for _, o := range others {
			acs, hs = append(acs, o.ACS), append(hs, o.HSPct)
			fd = append(fd, float64(o.FirstDeaths)/float64(o.Rounds))
			util = append(util, float64(o.Utility)/float64(o.Rounds))
		}
		for _, l := range [][]float64{acs, hs, fd, util} {
			sort.Float64s(l)
		}
		r.Facts = append(r.Facts, fmt.Sprintf("Your usual: ACS %.0f, headshots %.0f%%, from %d matches", quantile(acs, 0.5), quantile(hs, 0.5), len(others)))
		if m.ACS < quantile(acs, 0.25) {
			add("low_acs", "Low impact", fmt.Sprintf("ACS %.0f; your usual is %.0f", m.ACS, quantile(acs, 0.5)),
				"Play for the first two kills of a round with your team: peek with a teammate ready to trade, not alone into a crossfire.", 0.45, m.ACS)
		} else if m.ACS >= quantile(acs, 0.75) {
			good = append(good, ReviewPoint{Kind: "acs", Title: "Big impact", Detail: fmt.Sprintf("ACS %.0f, better than three quarters of your matches", m.ACS), Weight: 0.6})
		}
		if m.HSPct > 0 && m.HSPct < quantile(hs, 0.25) {
			add("low_hs", "Few headshots", fmt.Sprintf("%.0f%% headshots; you usually hit %.0f%%", m.HSPct, quantile(hs, 0.5)),
				"Keep your crosshair at head height while you move, and stop before you shoot: running shots go anywhere.", 0.3, m.HSPct)
		}
		if rate := float64(m.FirstDeaths) / float64(m.Rounds); m.FirstDeaths >= 3 && rate > quantile(fd, 0.75) {
			add("first_deaths", "Died first too often", fmt.Sprintf("First death in %d of %d rounds", m.FirstDeaths, m.Rounds),
				"Unless you're the entry, don't take the first duel: let utility or your duelist go first and trade them.", 0.5, float64(m.FirstDeaths))
		}
		if u := float64(m.Utility) / float64(m.Rounds); u < quantile(util, 0.25) && m.Utility > 0 {
			add("utility", "Little utility used", fmt.Sprintf("%d abilities over %d rounds; you usually use more", m.Utility, m.Rounds),
				"Use at least one ability every round before a fight: a flash, smoke or recon wins the duel before it starts.", 0.25, u)
		}
	} else {
		r.Notes = append(r.Notes, "Comparisons with your usual start after five matches of this kind have been read.")
	}
	// From this match alone.
	untraded, alone := 0, 0
	for _, d := range m.DeathLog {
		if !d.Traded {
			untraded++
		}
		if d.Mate < 0 || d.Mate > 20 {
			alone++
		}
	}
	if m.Deaths >= 8 && untraded*10 >= m.Deaths*7 {
		add("untraded", "Deaths nobody traded", fmt.Sprintf("%d of %d deaths without a trade within 5 s", untraded, m.Deaths),
			"Fight near a teammate who can see your enemy: a death that's traded is an even round, one that isn't is a 4v5.", 0.4, float64(untraded))
	}
	if alone >= 4 {
		add("alone", "Caught alone", fmt.Sprintf("%d deaths with no teammate within 20 m", alone),
			"Play off a teammate: hold angles close enough to trade, and rotate with someone, not on your own.", 0.35+0.03*float64(alone), float64(alone))
	}
	if m.KAST > 0 && m.KAST < 60 && m.Rounds >= 12 {
		add("kast", "Low round contribution", fmt.Sprintf("KAST %.0f%%: in many rounds you got no kill, no assist, didn't survive and weren't traded", m.KAST),
			"Each round, aim to do one thing: an assist with utility, staying alive to the end, or dying where your team can trade.", 0.35, m.KAST)
	} else if m.KAST >= 80 {
		good = append(good, ReviewPoint{Kind: "kast", Title: "In every round", Detail: fmt.Sprintf("KAST %.0f%%", m.KAST), Weight: 0.5})
	}
	if m.FirstBloods >= 4 && m.FirstBloods > m.FirstDeaths {
		good = append(good, ReviewPoint{Kind: "first_bloods", Title: "Opened rounds", Detail: fmt.Sprintf("%d first bloods, %d first deaths", m.FirstBloods, m.FirstDeaths), Weight: 0.55})
	}
	if m.Clutches > 0 {
		good = append(good, ReviewPoint{Kind: "clutch", Title: "Clutched", Detail: fmt.Sprintf("Won %d round%s as the last one standing", m.Clutches, plural(m.Clutches)), Weight: 0.6})
	}
	if m.MultiKills >= 2 {
		good = append(good, ReviewPoint{Kind: "multikill", Title: "Multi-kills", Detail: fmt.Sprintf("%d rounds with 3 or more kills", m.MultiKills), Weight: 0.5})
	}
	for _, d := range m.DeathLog {
		note := []string{}
		if d.First {
			note = append(note, "first death of the round")
		}
		if d.Traded {
			note = append(note, "traded")
		} else {
			note = append(note, "not traded")
		}
		if d.Mate < 0 {
			note = append(note, "no teammate alive")
		} else if d.Mate > 0 {
			note = append(note, fmt.Sprintf("nearest teammate %.0f m", d.Mate))
		}
		fight := d.Agent
		if d.Weapon != "" {
			fight += " with " + d.Weapon
		}
		r.DeathLog = append(r.DeathLog, ReviewDeath{Clock: fmt.Sprintf("R%d %s", d.Round, d.Clock), Killer: d.Killer, Region: d.Place, Fight: fight, Note: strings.Join(note, ", ")})
	}
	if set := a.ValorantGoals(m.Ranked); len(set.Goals) > 0 {
		checked := checkValGoals(set, *m)
		r.Goals = &checked
	}
	r.Wrong, r.Good = sortPoints(wrong, 4), sortPoints(good, 3)
	r.Verdict = valVerdict(*m, r.Wrong, r.Good)
	return r, nil
}

func valVerdict(m ValMatch, wrong, good []ReviewPoint) string {
	res := fmt.Sprintf("%d–%d", m.TeamRounds, m.EnemyRounds)
	switch {
	case m.Won == nil:
		res = "Draw " + res
	case *m.Won:
		res = "Won " + res
	default:
		res = "Lost " + res
	}
	lower := func(s string) string {
		if s == "" {
			return s
		}
		return strings.ToLower(s[:1]) + s[1:]
	}
	switch {
	case len(wrong) > 0 && len(good) > 0:
		return fmt.Sprintf("%s. Biggest issue: %s. Best part: %s.", res, lower(wrong[0].Title), lower(good[0].Title))
	case len(wrong) > 0:
		return fmt.Sprintf("%s. Biggest issue: %s.", res, lower(wrong[0].Title))
	case len(good) > 0:
		return fmt.Sprintf("%s. Nothing stood out as a mistake. Best part: %s.", res, lower(good[0].Title))
	}
	return res + ". Nothing stood out either way."
}

// ValorantGoals sets goals from the player's own matches of one kind.
func (a *App) ValorantGoals(ranked bool) GoalSet {
	ov, _ := a.Valorant.Overview(false)
	pct, label := goalLevel(a.Store.LoadPrefs().Coach.ValLevel)
	kind := "unrated"
	if ranked {
		kind = "competitive"
	}
	set := GoalSet{Game: "valorant", Hero: kind, Level: label, Goals: []Goal{}}
	var acs, adr, hs, deaths []float64
	for _, m := range ov.Matches {
		if m.Ranked != ranked || m.Rounds < 8 {
			continue
		}
		acs, adr, hs = append(acs, m.ACS), append(adr, m.ADR), append(hs, m.HSPct)
		deaths = append(deaths, float64(m.Deaths)/float64(m.Rounds)*20)
	}
	if len(acs) < 5 {
		set.Note = "Goals start after five matches of this kind have been read."
		return set
	}
	for _, l := range [][]float64{acs, adr, hs, deaths} {
		sort.Float64s(l)
	}
	set.Goals = append(set.Goals,
		Goal{Key: "acs", Label: "ACS", Format: "count", Target: roundStep(quantile(acs, pct), 10), Why: "Combat score your best matches reach.", State: "pending"},
		Goal{Key: "adr", Label: "Damage a round", Format: "count", Target: roundStep(quantile(adr, pct), 5), Why: "Damage a round your best matches reach.", State: "pending"},
		Goal{Key: "hs", Label: "Headshot %", Format: "count", Target: roundStep(quantile(hs, pct), 1), Why: "Headshot share of your best matches.", State: "pending"},
		Goal{Key: "deaths", Label: "Deaths", Format: "count", Target: float64(int(quantile(deaths, 1-pct) + 0.5)), AtMost: true, Why: "Deaths over 20 rounds in your best matches.", State: "pending"},
	)
	set.Note = fmt.Sprintf("From your last %d %s matches read here.", len(acs), kind)
	return set
}

func checkValGoals(set GoalSet, m ValMatch) GoalSet {
	out := set
	out.Goals = make([]Goal, len(set.Goals))
	for i, g := range set.Goals {
		var v float64
		switch g.Key {
		case "acs":
			v = m.ACS
		case "adr":
			v = m.ADR
		case "hs":
			v = m.HSPct
		case "deaths":
			v = float64(int(float64(m.Deaths)/float64(max(m.Rounds, 1))*20 + 0.5))
		}
		g.Value = &v
		met := v >= g.Target
		if g.AtMost {
			met = v <= g.Target
		}
		g.State = "missed"
		if met {
			g.State = "met"
		}
		out.Goals[i] = g
	}
	return out
}

// ValorantWorkOn tallies the issues of the last ten matches.
func (a *App) ValorantWorkOn() (WorkOn, error) {
	ov, err := a.Valorant.Overview(false)
	if err != nil && len(ov.Matches) == 0 {
		return WorkOn{}, err
	}
	var reviews []MatchReview
	n := 0
	for _, m := range ov.Matches {
		if n >= 10 || m.Rounds < 8 {
			continue
		}
		n++
		if r, err := a.ValorantReview(m.ID); err == nil {
			reviews = append(reviews, r)
		}
	}
	return tallyWorkOn("valorant", reviews), nil
}
