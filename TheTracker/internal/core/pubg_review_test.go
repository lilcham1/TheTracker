package core

import (
	"encoding/json"
	"io"
	"strings"
	"testing"
)

// A small telemetry log: my squad is team 1 (me and Mate), the enemies are
// team 2. I land at Pochinki at 1:30 with two enemies close by, knock one
// enemy who my squad then finishes, take blue zone damage, get knocked with
// my mate 400 m away, and am killed by a Beryl from 85 m outside the circle.
func pgTelemetry() string {
	ch := func(name, acc string, team int, x, y float64, zone string, blue bool) map[string]any {
		c := map[string]any{"name": name, "accountId": acc, "teamId": team, "location": map[string]any{"x": x, "y": y}, "isInBlueZone": blue}
		if zone != "" {
			c["zone"] = []any{zone}
		}
		return c
	}
	t := func(s int) string { return "2026-10-12T20:00:" + pad(s) + "Z" }
	me := func(x, y float64, blue bool) map[string]any { return ch("Me", pgMe, 1, x, y, "pochinki", blue) }
	mate := ch("Mate", "account.11111111111111111111111111111111", 1, 140000, 100000, "", false)
	e1 := ch("Enemy1", "account.22222222222222222222222222222222", 2, 101000, 100500, "pochinki", false)
	e2 := ch("Enemy2", "account.33333333333333333333333333333333", 2, 100800, 99900, "pochinki", false)
	events := []map[string]any{
		{"_T": "LogMatchStart", "_D": t(0)},
		{"_T": "LogGameStatePeriodic", "_D": t(1), "gameState": map[string]any{"safetyZonePosition": map[string]any{"x": 300000, "y": 300000}, "safetyZoneRadius": 50000}},
		{"_T": "LogParachuteLanding", "_D": t(10), "character": me(100000, 100000, false)},
		{"_T": "LogParachuteLanding", "_D": t(11), "character": e1},
		{"_T": "LogParachuteLanding", "_D": t(12), "character": e2},
		{"_T": "LogParachuteLanding", "_D": t(12), "character": ch("Enemy3", "account.44444444444444444444444444444444", 2, 99500, 100200, "pochinki", false)},
		{"_T": "LogParachuteLanding", "_D": t(13), "character": mate},
		{"_T": "LogPlayerTakeDamage", "_D": t(20), "attacker": me(100000, 100000, false), "victim": e1, "damage": 120.0, "damageTypeCategory": "Damage_Gun", "damageCauserName": "WeapHK416_C"},
		{"_T": "LogPlayerMakeGroggy", "_D": t(21), "attacker": me(100000, 100000, false), "victim": e1, "dBNOId": 7, "damageTypeCategory": "Damage_Gun", "damageCauserName": "WeapHK416_C", "distance": 3000.0},
		{"_T": "LogPlayerKillV2", "_D": t(22), "victim": e1, "killer": mate, "dBNOId": 7},
		{"_T": "LogPlayerTakeDamage", "_D": t(30), "victim": me(100000, 100000, true), "damage": 55.0, "damageTypeCategory": "Damage_BlueZone"},
		{"_T": "LogPlayerPosition", "_D": t(31), "character": ch("Mate", "account.11111111111111111111111111111111", 1, 140000, 100000, "", false)},
		{"_T": "LogPlayerMakeGroggy", "_D": t(40), "attacker": e2, "victim": me(100000, 100000, false), "dBNOId": 9, "damageTypeCategory": "Damage_Gun", "damageCauserName": "WeapBerylM762_C", "distance": 8500.0},
		{"_T": "LogPlayerKillV2", "_D": t(45), "victim": me(100000, 100000, false), "killer": e2, "dBNOId": 9,
			"killerDamageInfo": map[string]any{"damageTypeCategory": "Damage_Gun", "damageCauserName": "WeapBerylM762_C", "distance": 8500.0}},
		{"_T": "LogMatchEnd", "_D": t(59)},
	}
	raw, _ := json.Marshal(events)
	return string(raw)
}

func pad(n int) string {
	if n < 10 {
		return "0" + string(rune('0'+n))
	}
	return string(rune('0'+n/10)) + string(rune('0'+n%10))
}

func TestPubgTelemetryFacts(t *testing.T) {
	f, err := pgFactsFrom(strings.NewReader(pgTelemetry()), pgMe)
	if err != nil {
		t.Fatal(err)
	}
	if f.LandedPlace != "Pochinki" || f.LandedAt != 10 || f.NearbyLand != 3 {
		t.Fatalf("landing: %+v", f)
	}
	if f.KnocksMade != 1 || f.KnocksKilled != 1 || f.BlueDamage != 55 || f.ByWeapon["M416"] != 120 {
		t.Fatalf("knocks/zone/weapons: %+v", f)
	}
	if len(f.Downs) != 2 || !f.Downs[0].Knock || f.Downs[1].Knock {
		t.Fatalf("downs: %+v", f.Downs)
	}
	d := f.Downs[1]
	if d.By != "Enemy2" || d.Weapon != "Beryl M762" || d.Distance != 85 || !d.OutSafe || d.Mate < 350 || d.Mate > 450 || d.At != 45 {
		t.Fatalf("death: %+v", d)
	}
	if !f.TeamWiped == false {
		// Mate is still standing.
	}
}

func TestPubgReviewFromTelemetry(t *testing.T) {
	a, cloud := pgTestApp(t)
	ids := []string{}
	pubg := map[string]any{"/shards/steam/seasons": map[string]any{"data": []any{}}}
	matches := []any{}
	for i := 0; i < 6; i++ {
		id := strings.Repeat(string(rune('a'+i)), 8) + "-1111-1111-1111-111111111111"
		ids = append(ids, id)
		m := pgMatch(id, "2026-10-1"+string(rune('0'+i))+"T20:00:00Z")
		inc := m["included"].([]any)
		inc[len(inc)-1] = map[string]any{"type": "asset", "attributes": map[string]any{"name": "telemetry", "URL": "https://telemetry-cdn.pubg.com/x/" + id + ".json"}}
		pubg["/shards/steam/matches/"+id] = m
		matches = append(matches, map[string]any{"type": "match", "id": id})
	}
	pubg["/shards/steam/players/"+pgMe] = map[string]any{"data": map[string]any{"id": pgMe, "relationships": map[string]any{"matches": map[string]any{"data": matches}}}}
	cloud.mu.Lock()
	cloud.pubg = pubg
	cloud.mu.Unlock()
	a.Pubg.SetLink(PubgLink{AccountID: pgMe, Name: "Me", Shard: "steam"})
	fetched := 0
	old := pubgTelemetryFetch
	pubgTelemetryFetch = func(url string) (io.ReadCloser, error) {
		fetched++
		return io.NopCloser(strings.NewReader(pgTelemetry())), nil
	}
	t.Cleanup(func() { pubgTelemetryFetch = old })

	r, err := a.PubgReview("")
	if err != nil {
		t.Fatal(err)
	}
	if r.Game != "pubg" || r.MatchKey != ids[5] || r.Deaths != 1 || len(r.DeathLog) != 2 {
		t.Fatalf("review: %+v", r)
	}
	kinds := map[string]bool{}
	for _, p := range r.Wrong {
		kinds[p.Kind] = true
	}
	for _, k := range []string{"alone", "blue_zone", "hot_drop"} {
		if !kinds[k] {
			t.Errorf("missing %s in %+v", k, r.Wrong)
		}
	}
	if !strings.Contains(r.DeathLog[1].Fight, "Beryl M762 from 85 m") || !strings.Contains(r.DeathLog[1].Note, "outside the white circle") {
		t.Fatalf("death row: %+v", r.DeathLog[1])
	}
	if r.Goals == nil || len(r.Goals.Goals) != 4 {
		t.Fatalf("goals: %+v", r.Goals)
	}
	// The telemetry is read once per match.
	if _, err := a.PubgReview(ids[5]); err != nil || fetched != 1 {
		t.Fatalf("cached facts: fetched %d %v", fetched, err)
	}
	// The coach never sees other players' names.
	facts := coachFacts(r)
	raw, _ := json.Marshal(facts)
	if strings.Contains(string(raw), "Enemy2") {
		t.Fatalf("a player's name reached the coach: %s", raw)
	}
}
