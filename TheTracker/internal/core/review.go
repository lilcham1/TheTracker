package core

// Match review: what went wrong in one match, what went well, and — across
// recent matches — what keeps going wrong, with one habit to practise for
// each. Built only from public match data (OpenDota, the Deadlock API) and
// what TheTracker recorded itself; never from other trackers' ratings.

import (
	"errors"
	"sort"
	"sync"
)

// ReviewPoint is one thing that went wrong or right.
type ReviewPoint struct {
	// Stable key, for counting the same issue across matches.
	Kind   string `json:"kind"`
	Title  string `json:"title"`
	Detail string `json:"detail"`
	// One concrete thing to practise.
	Habit string `json:"habit,omitempty"`
	// How much it mattered in this match, for ordering (0–1).
	Weight float64 `json:"weight"`
	// The figure behind it, for trends.
	Value *float64 `json:"value,omitempty"`
}

type ReviewDeath struct {
	Clock    string   `json:"clock"`
	Reasons  []string `json:"reasons,omitempty"`
	GoldLost *int64   `json:"goldLost,omitempty"`
	// Deadlock: who killed the player and how fast.
	Killer string `json:"killer,omitempty"`
	Note   string `json:"note,omitempty"`
}

type MatchReview struct {
	Game      string  `json:"game"`
	MatchID   uint64  `json:"matchId"`
	Hero      string  `json:"hero"`
	HeroImage *string `json:"heroImage,omitempty"`
	HeroSlug  string  `json:"heroSlug,omitempty"`
	Won       *bool   `json:"won"`
	Duration  int     `json:"duration"`
	StartTime int64   `json:"startTime"`
	Kills     int     `json:"kills"`
	Deaths    int     `json:"deaths"`
	Assists   int     `json:"assists"`
	// One line on the match as a whole.
	Verdict  string        `json:"verdict"`
	Wrong    []ReviewPoint `json:"wrong"`
	Good     []ReviewPoint `json:"good"`
	DeathLog []ReviewDeath `json:"deathLog"`
	// What the review couldn't use, said plainly.
	Notes []string `json:"notes"`
}

// WorkOnItem is an issue that keeps coming back.
type WorkOnItem struct {
	Kind  string `json:"kind"`
	Title string `json:"title"`
	Habit string `json:"habit"`
	// In how many of the reviewed matches it showed up.
	Count int `json:"count"`
	Of    int `json:"of"`
	// Its figure per match, oldest first (for a sparkline), when it has one.
	Series []float64 `json:"series,omitempty"`
}

type WorkOn struct {
	Game    string       `json:"game"`
	Matches int          `json:"matches"`
	Items   []WorkOnItem `json:"items"`
	// Things that went well in most matches.
	Strengths []WorkOnItem `json:"strengths"`
}

// sortPoints puts the heaviest first and keeps the top n.
func sortPoints(p []ReviewPoint, n int) []ReviewPoint {
	sort.SliceStable(p, func(i, j int) bool { return p[i].Weight > p[j].Weight })
	if len(p) > n {
		p = p[:n]
	}
	if p == nil {
		p = []ReviewPoint{}
	}
	return p
}

// tallyWorkOn counts each issue across reviews, newest first in reviews.
func tallyWorkOn(game string, reviews []MatchReview) WorkOn {
	out := WorkOn{Game: game, Matches: len(reviews), Items: []WorkOnItem{}, Strengths: []WorkOnItem{}}
	count := func(pick func(MatchReview) []ReviewPoint, min int) []WorkOnItem {
		byKind := map[string]*WorkOnItem{}
		var order []string
		for i := len(reviews) - 1; i >= 0; i-- { // oldest first, for the series
			for _, p := range pick(reviews[i]) {
				it := byKind[p.Kind]
				if it == nil {
					it = &WorkOnItem{Kind: p.Kind, Title: p.Title, Habit: p.Habit, Of: len(reviews)}
					byKind[p.Kind] = it
					order = append(order, p.Kind)
				}
				it.Count++
				// The latest wording describes it best.
				it.Title, it.Habit = p.Title, p.Habit
				if p.Value != nil {
					it.Series = append(it.Series, *p.Value)
				}
			}
		}
		items := []WorkOnItem{}
		for _, k := range order {
			if it := byKind[k]; it.Count >= min {
				items = append(items, *it)
			}
		}
		sort.SliceStable(items, func(i, j int) bool { return items[i].Count > items[j].Count })
		if len(items) > 5 {
			items = items[:5]
		}
		return items
	}
	min := 2
	if len(reviews) < 4 {
		min = 1
	}
	out.Items = count(func(r MatchReview) []ReviewPoint { return r.Wrong }, min)
	out.Strengths = count(func(r MatchReview) []ReviewPoint { return r.Good }, (len(reviews)+1)/2)
	if len(out.Strengths) > 3 {
		out.Strengths = out.Strengths[:3]
	}
	return out
}

// reviewMany reviews matches three at a time; ones that fail are skipped.
func reviewMany(ids []uint64, one func(uint64) (MatchReview, error)) []MatchReview {
	out := make([]*MatchReview, len(ids))
	var wg sync.WaitGroup
	sem := make(chan struct{}, 3)
	for i, id := range ids {
		wg.Add(1)
		go func(i int, id uint64) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			if r, err := one(id); err == nil {
				out[i] = &r
			}
		}(i, id)
	}
	wg.Wait()
	list := []MatchReview{}
	for _, r := range out {
		if r != nil {
			list = append(list, *r)
		}
	}
	return list
}

var errNoReviewMatch = errors.New("No match to review yet.")

func pctOf(v float64) *float64 { return &v }
