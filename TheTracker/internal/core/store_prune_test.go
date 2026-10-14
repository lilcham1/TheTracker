package core

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestPruneCache(t *testing.T) {
	s := NewStore(t.TempDir())
	now := time.Now()
	files := map[string]time.Duration{
		"od_review_1.json":         50 * 24 * time.Hour, // old review: goes
		"od_review_2.json":         2 * 24 * time.Hour,  // recent: stays
		"coach_dota_1.json":        50 * 24 * time.Hour, // coach notes stay longer
		"coach_dota_2.json":        200 * 24 * time.Hour,
		"pubg_facts_abc.json":      time.Hour, // an earlier version's digest: goes
		"pubg_facts2_abc.json":     time.Hour,
		"pubg_match2_abc_acc.json": 46 * 24 * time.Hour,
		"notes.txt":                400 * 24 * time.Hour, // not a cache file
	}
	dir := filepath.Join(s.Dir, "cache")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	for name, age := range files {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte("{}"), 0o644); err != nil {
			t.Fatal(err)
		}
		os.Chtimes(p, now.Add(-age), now.Add(-age))
	}
	if n := s.PruneCache(now); n != 4 {
		t.Fatalf("pruned %d", n)
	}
	for name, want := range map[string]bool{"od_review_1.json": false, "od_review_2.json": true, "coach_dota_1.json": true, "coach_dota_2.json": false,
		"pubg_facts_abc.json": false, "pubg_facts2_abc.json": true, "pubg_match2_abc_acc.json": false, "notes.txt": true} {
		_, err := os.Stat(filepath.Join(dir, name))
		if (err == nil) != want {
			t.Errorf("%s kept=%v", name, err == nil)
		}
	}
}
