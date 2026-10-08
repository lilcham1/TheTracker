package core

import (
	"fmt"
	"strings"
	"sync"
	"time"
)

// Ranks for many accounts at once, with the game's rank icons, for the Live
// page's streamers. A rank only changes after a ranked match, so each one is
// kept for ten minutes.

type rankMemo struct {
	mu   sync.Mutex
	seen map[uint64]rankSeen
}

type rankSeen struct {
	rank *DeadlockRank
	at   time.Time
}

var liveRanks = &rankMemo{seen: map[uint64]rankSeen{}}

// rankIcons maps "tier/subrank" to the icon of that rank.
func (d *Deadlock) rankIcons() map[string]string {
	rows, _, _ := cachedFetch(d.store, "dl_rank_icons", 24*time.Hour, false, func() ([]jsonMap, error) {
		var v []jsonMap
		err := d.api.get("/v1/assets/ranks", &v)
		return v, err
	})
	out := map[string]string{}
	for _, r := range rows {
		tier := int(jI64(r, "tier"))
		imgs := sub(r, "images")
		for k, v := range imgs {
			url, _ := v.(string)
			if url == "" {
				continue
			}
			if n, ok := strings.CutPrefix(k, "small_subrank"); ok && !strings.HasSuffix(n, "_webp") {
				out[fmt.Sprintf("%d/%s", tier, n)] = url
			}
		}
		if url := jStr(imgs, "large", ""); url != "" {
			out[fmt.Sprintf("%d/0", tier)] = url
		}
	}
	return out
}

// RanksFor is the current rank of each account that has one. Unranked and
// hidden accounts are simply absent.
func (d *Deadlock) RanksFor(ids []uint64) map[uint64]DeadlockRank {
	out := map[uint64]DeadlockRank{}
	want := []string{}
	liveRanks.mu.Lock()
	for _, id := range ids {
		if s, ok := liveRanks.seen[id]; ok && time.Since(s.at) < 10*time.Minute {
			if s.rank != nil {
				out[id] = *s.rank
			}
			continue
		}
		want = append(want, fmt.Sprint(id))
	}
	liveRanks.mu.Unlock()
	if len(want) == 0 {
		return out
	}

	icons := d.rankIcons()
	for start := 0; start < len(want); start += 1000 {
		end := min(start+1000, len(want))
		var rows []jsonMap
		if err := d.api.get("/v1/players/rank?account_ids="+strings.Join(want[start:end], ","), &rows); err != nil {
			continue // try again next time
		}
		now := time.Now()
		liveRanks.mu.Lock()
		got := map[uint64]bool{}
		for _, r := range rows {
			id := jU64(r, "account_id")
			got[id] = true
			badge := int(jI64(r, "badge"))
			if badge <= 0 {
				liveRanks.seen[id] = rankSeen{nil, now}
				continue
			}
			rank := decodeBadge(badge)
			if icon, ok := icons[fmt.Sprintf("%d/%d", rank.Tier, rank.Subrank)]; ok {
				rank.Icon = &icon
			} else if icon, ok := icons[fmt.Sprintf("%d/0", rank.Tier)]; ok {
				rank.Icon = &icon
			}
			liveRanks.seen[id] = rankSeen{&rank, now}
			out[id] = rank
		}
		// Accounts left out are protected: remember that too.
		for _, s := range want[start:end] {
			var id uint64
			fmt.Sscan(s, &id)
			if !got[id] {
				liveRanks.seen[id] = rankSeen{nil, now}
			}
		}
		liveRanks.mu.Unlock()
	}
	return out
}
