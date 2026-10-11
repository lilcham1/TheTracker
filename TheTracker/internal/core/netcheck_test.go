package core

import (
	"strings"
	"testing"
	"time"
)

func TestPingStats(t *testing.T) {
	s := statsOf([]pingSample{{10, true}, {14, true}, {0, false}, {12, true}})
	if s.Samples != 4 || s.Loss != 25 || s.Avg != 12 || s.Best != 10 || s.Jitter != 3 {
		t.Fatalf("stats: %+v", s)
	}
	if statsOf(nil) != nil {
		t.Fatal("no samples, no stats")
	}
	if s := statsOf([]pingSample{{0, false}, {0, false}}); s.Loss != 100 || s.Avg != 0 {
		t.Fatalf("all lost: %+v", s)
	}
}

func TestNetVerdict(t *testing.T) {
	good := &PingStats{Avg: 2, Best: 1, Jitter: 1, Samples: 30}
	valve := &PingStats{Avg: 84, Best: 82, Jitter: 2, Samples: 30}
	apps := []NetApp{{"OneDrive", 9}, {"chrome", 30}}
	cases := []struct {
		name           string
		router, valve  *PingStats
		down, up       float64
		want, contains string
	}{
		{"just started", &PingStats{Samples: 2}, nil, 0, 0, "measuring", ""},
		{"all fine, far away", good, valve, 1, 0.2, "ok", ""},
		{"wifi drops packets", &PingStats{Avg: 4, Jitter: 3, Loss: 8, Samples: 30}, valve, 1, 0.2, "local", "Wi-Fi"},
		{"slow wifi", &PingStats{Avg: 40, Best: 3, Jitter: 30, Samples: 30}, valve, 1, 0.2, "local", ""},
		{"provider", good, &PingStats{Avg: 90, Best: 82, Jitter: 25, Loss: 1, Samples: 30}, 1, 0.2, "internet", "provider"},
		{"queueing behind an upload", good, &PingStats{Avg: 160, Best: 82, Jitter: 10, Samples: 30}, 2, 8, "busy", "OneDrive"},
	}
	for _, c := range cases {
		got, detail := netVerdict(c.router, c.valve, c.down, c.up, apps)
		if got != c.want || !strings.Contains(detail, c.contains) {
			t.Errorf("%s: %s %q", c.name, got, detail)
		}
	}
}

type fakeProbe struct {
	rtt map[string]time.Duration
	in  uint64
	out uint64
}

func (f *fakeProbe) Gateway() (string, error) { return "192.168.1.1", nil }
func (f *fakeProbe) Ping(ip string, _ time.Duration) (time.Duration, bool) {
	d, ok := f.rtt[ip]
	return d, ok
}
func (f *fakeProbe) LineBytes() (uint64, uint64, error) {
	f.in += 1_000_000 // 8 Mbit per second
	f.out += 250_000  // 2 Mbit per second
	return f.in, f.out, nil
}
func (f *fakeProbe) Connections() map[string]int {
	return map[string]int{"OneDrive": 4, "svchost": 30, "dota2": 2, "chrome": 12}
}

func TestNetCheckMeasures(t *testing.T) {
	a := newTestApp(t)
	f := newFakeAPI(t, map[string]any{
		"/ISteamApps/GetSDRConfig/v1/": map[string]any{"pops": map[string]any{
			"fra": map[string]any{"desc": "Frankfurt (Germany)", "relays": []any{map[string]any{"ipv4": "155.133.226.68"}}},
			"iad": map[string]any{"desc": "Sterling (Virginia)", "relays": []any{map[string]any{"ipv4": "162.254.192.88"}}},
		}},
	})
	probe := &fakeProbe{rtt: map[string]time.Duration{"192.168.1.1": 2 * time.Millisecond, "155.133.226.68": 84 * time.Millisecond, "162.254.192.88": 144 * time.Millisecond}}
	n := a.Net
	n.probe, n.steam = probe, &service{Name: "Steam", Base: f.srv.URL, Attempts: 1}

	for i := 0; i < 6; i++ {
		n.measure()
		n.mu.Lock()
		n.lastAt = n.lastAt.Add(-time.Second) // a second between samples
		n.mu.Unlock()
	}
	s := n.Status()
	if s.ValveWhere != "Frankfurt (Germany)" || s.Valve == nil || s.Valve.Avg != 84 || s.Router.Avg != 2 || s.RouterIP != "192.168.1.1" {
		t.Fatalf("nearest relay and pings: %+v", s)
	}
	if s.Verdict != "ok" {
		t.Fatalf("a steady line is fine: %+v", s)
	}
	if s.DownMbps < 7 || s.DownMbps > 9 || s.UpMbps < 1.5 || s.UpMbps > 2.5 {
		t.Fatalf("line use: %v down, %v up", s.DownMbps, s.UpMbps)
	}
	if len(s.Apps) != 2 || s.Apps[0].Name != "chrome" || s.Apps[1].Name != "OneDrive" {
		t.Fatalf("programs online, Windows and the game left out: %+v", s.Apps)
	}
}
