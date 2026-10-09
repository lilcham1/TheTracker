package core

import (
	"sync"
	"time"
)

// The overlay's reminder windows differ for a core and a support (a support
// keeps pulling and taking bounties for longer). Dota's live feed doesn't
// say which the player is, even in role queue, so it is worked out from what
// the feed does say, strongest evidence first:
//
//  1. the player chose it for this match on the live panel;
//  2. last hits at 10:00, then at 5:00, when clearly one or the other;
//  3. wards or a Blood Grenade bought before 5:00;
//  4. the player's own recorded games on this hero (last hits at 10:00);
//  5. the hero's usual position, from the cached OpenDota meta;
//  6. otherwise core.

type LiveRole struct {
	Role string `json:"role"` // core | support
	// chosen | last_hits | wards | your_games | hero | default
	Why string `json:"why"`
}

// Items a core rarely buys in the first minutes.
var supportItems = map[string]bool{"ward_observer": true, "ward_sentry": true, "ward_dispenser": true, "blood_grenade": true}

// Support items only count when bought this early: later, cores buy
// sentries too.
const supportItemsBefore = 5 * 60.0

// Last-hit lines at 5:00 and 10:00. Between them the marks prove nothing.
var roleLastHits = map[int][2]int64{
	5:  {8, 18},
	10: {20, 40},
}

type roleHint struct{ role, why string }

func guessRole(m *MatchState, hint roleHint) LiveRole {
	if m.RoleChoice == "core" || m.RoleChoice == "support" {
		return LiveRole{m.RoleChoice, "chosen"}
	}
	for _, minute := range []int{10, 5} {
		if cp := m.Checkpoints[minute]; cp != nil {
			lines := roleLastHits[minute]
			if cp.LastHits <= lines[0] {
				return LiveRole{"support", "last_hits"}
			}
			if cp.LastHits >= lines[1] {
				return LiveRole{"core", "last_hits"}
			}
		}
	}
	if m.SupportItems {
		return LiveRole{"support", "wards"}
	}
	if hint.role != "" {
		return LiveRole{hint.role, hint.why}
	}
	return LiveRole{"core", "default"}
}

// heroRoleHint is what the player's history and the hero's usual position
// say, before the match itself says anything.
func heroRoleHint(hero string, history []MatchSummary, meta *DotaMeta) roleHint {
	games, support := 0, 0
	for _, h := range history {
		if h.HeroName == nil || *h.HeroName != hero {
			continue
		}
		if cp := h.Checkpoints[10]; cp != nil {
			games++
			if cp.LastHits <= roleLastHits[10][0] {
				support++
			}
		}
	}
	if games >= 3 {
		if support*2 > games {
			return roleHint{"support", "your_games"}
		}
		return roleHint{"core", "your_games"}
	}
	if meta != nil {
		slug := heroSlug(hero)
		for _, h := range meta.Heroes {
			if h.Slug != slug {
				continue
			}
			if h.Position == "support" || h.Position == "hard_support" {
				return roleHint{"support", "hero"}
			}
			if h.Position != "" {
				return roleHint{"core", "hero"}
			}
		}
	}
	return roleHint{}
}

// The hint per hero is kept a minute: the overlay asks several times a
// second and the history file needn't be read each time.
type roleHints struct {
	mu   sync.Mutex
	hero string
	hint roleHint
	// When the kept hint runs out: a minute, or a few seconds while the
	// meta is still being fetched.
	until time.Time
	// When the meta was last asked for because none was cached.
	metaAsked time.Time
}

func (a *App) roleHintFor(hero string) roleHint {
	a.hints.mu.Lock()
	defer a.hints.mu.Unlock()
	if a.hints.hero == hero && time.Now().Before(a.hints.until) {
		return a.hints.hint
	}
	var meta DotaMeta
	var metaPtr *DotaMeta
	if _, ok := a.Store.ReadCacheStale("od_meta", &meta); ok {
		metaPtr = &meta
	} else if time.Since(a.hints.metaAsked) > 10*time.Minute {
		// Fetched in the background for the next ask; the live feed never
		// waits on the network.
		a.hints.metaAsked = time.Now()
		go a.Dota.Meta(false)
	}
	a.hints.hero, a.hints.until = hero, time.Now().Add(time.Minute)
	if metaPtr == nil {
		a.hints.until = time.Now().Add(5 * time.Second)
	}
	a.hints.hint = heroRoleHint(hero, a.Store.LoadHistory(), metaPtr)
	return a.hints.hint
}

// liveRole is the role for the match on screen, or nil without one.
func (a *App) liveRole(m *MatchState) *LiveRole {
	if m == nil {
		return nil
	}
	hint := roleHint{}
	if m.HeroName != nil && *m.HeroName != "" {
		hint = a.roleHintFor(*m.HeroName)
	}
	r := guessRole(m, hint)
	return &r
}
