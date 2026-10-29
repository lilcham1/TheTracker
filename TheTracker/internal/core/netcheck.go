package core

// The connection check: while a game is open, all the time when the player
// switched that on, or for a short test from Settings, ping the router and the nearest Valve relay once a second and
// watch how busy the line is, then say in plain words where any lag comes
// from. Nothing here changes anything; it only measures.

import (
	"math"
	"sort"
	"strings"
	"sync"
	"time"
)

type netProbe interface {
	Gateway() (string, error)
	Ping(ip string, timeout time.Duration) (time.Duration, bool)
	LineBytes() (in, out uint64, err error)
	Connections() map[string]int
}

// How much is kept: the last minute.
const netWindow = 60

type pingSample struct {
	ms float64
	ok bool
}

type PingStats struct {
	Avg     float64 `json:"avg"`
	Best    float64 `json:"best"`
	Jitter  float64 `json:"jitter"`
	Loss    float64 `json:"loss"` // percent
	Samples int     `json:"samples"`
}

type NetApp struct {
	Name        string `json:"name"`
	Connections int    `json:"connections"`
}

type NetStatus struct {
	// Measuring right now (a game is on or a test is running).
	Active bool `json:"active"`
	// The game being played, if any.
	Game string `json:"game,omitempty"`
	// Until when a test from Settings runs, unix seconds.
	TestUntil  int64      `json:"testUntil,omitempty"`
	Router     *PingStats `json:"router"`
	RouterIP   string     `json:"routerIp"`
	Valve      *PingStats `json:"valve"`
	ValveWhere string     `json:"valveWhere"`
	DownMbps   float64    `json:"downMbps"`
	UpMbps     float64    `json:"upMbps"`
	Apps       []NetApp   `json:"apps"`
	// measuring | ok | local | internet | busy
	Verdict string `json:"verdict"`
	Detail  string `json:"detail"`
}

type NetCheck struct {
	probe netProbe
	steam *service
	store *Store

	mu       sync.Mutex
	router   []pingSample
	valve    []pingSample
	routerIP string
	relayIP  string
	// A second relay at the same place: Valve relays answer pings on a
	// best-effort basis, so a miss is only counted when both miss.
	relayIP2   string
	relayWhere string
	relayAt    time.Time
	down, up   float64
	apps       []NetApp
	lastIn     uint64
	lastOut    uint64
	lastAt     time.Time
	// Measuring is on while live or always is set, or until testUntil.
	live      bool
	always    bool
	game      string
	testUntil time.Time
	running   bool
	// A relay pick is under way (it runs beside the measuring, never in it).
	picking bool
}

func NewNetCheck(store *Store) *NetCheck {
	return &NetCheck{
		probe: windowsProbe{},
		steam: &service{Name: "Steam", Base: "https://api.steampowered.com", Attempts: 2},
		store: store,
	}
}

func (n *NetCheck) wanted() bool {
	return n.live || n.always || time.Now().Before(n.testUntil)
}

// SetAlways keeps measuring whether or not a game is open.
func (n *NetCheck) SetAlways(on bool) {
	n.mu.Lock()
	changed := n.always != on
	n.always = on
	n.mu.Unlock()
	if on && changed {
		n.start()
	}
}

// SetLive starts or stops measuring with the match.
func (n *NetCheck) SetLive(on bool) {
	n.mu.Lock()
	n.live = on
	if on {
		n.resetLocked()
	}
	n.mu.Unlock()
	if on {
		n.start()
	}
}

// SetGame records which game is being played, for the status.
func (n *NetCheck) SetGame(game string) {
	n.mu.Lock()
	n.game = game
	n.mu.Unlock()
}

// Test measures for the given time, from Settings.
func (n *NetCheck) Test(d time.Duration) {
	n.mu.Lock()
	if !n.wanted() {
		n.resetLocked()
	}
	n.testUntil = time.Now().Add(d)
	n.mu.Unlock()
	n.start()
}

func (n *NetCheck) resetLocked() {
	n.router, n.valve = nil, nil
	n.lastAt = time.Time{}
}

func (n *NetCheck) start() {
	n.mu.Lock()
	if n.running {
		n.mu.Unlock()
		return
	}
	n.running = true
	n.mu.Unlock()
	go n.loop()
}

func (n *NetCheck) loop() {
	tick := time.NewTicker(time.Second)
	defer tick.Stop()
	for {
		// Stopping is decided and recorded in one step, so a start in
		// between can't find the loop "running" when it is about to end.
		n.mu.Lock()
		if !n.wanted() {
			n.running = false
			n.mu.Unlock()
			return
		}
		n.mu.Unlock()
		n.measure()
		<-tick.C
	}
}

// measure takes one sample of everything.
func (n *NetCheck) measure() {
	n.mu.Lock()
	// relayAt holds back a retry after a failed pick (15 s) and a re-pick
	// after a good one (an hour).
	needRelay := !n.picking && time.Since(n.relayAt) > time.Hour
	if needRelay {
		n.picking = true
	}
	routerIP := n.routerIP
	n.mu.Unlock()
	if routerIP == "" {
		if ip, err := n.probe.Gateway(); err == nil {
			routerIP = ip
		}
	}
	if needRelay {
		// The relay list can take seconds to fetch, or fail offline; the
		// router keeps being measured meanwhile.
		go func() {
			n.pickRelay()
			n.mu.Lock()
			n.picking = false
			n.mu.Unlock()
		}()
	}
	n.mu.Lock()
	relayIP, relayIP2 := n.relayIP, n.relayIP2
	n.routerIP = routerIP
	n.mu.Unlock()

	var wg sync.WaitGroup
	var rMs, vMs time.Duration
	var rOK, vOK bool
	if routerIP != "" {
		wg.Add(1)
		go func() { defer wg.Done(); rMs, rOK = n.probe.Ping(routerIP, 900*time.Millisecond) }()
	}
	if relayIP != "" {
		wg.Add(1)
		go func() {
			defer wg.Done()
			vMs, vOK = n.probe.Ping(relayIP, 800*time.Millisecond)
			if !vOK && relayIP2 != "" {
				vMs, vOK = n.probe.Ping(relayIP2, 800*time.Millisecond)
			}
		}()
	}
	wg.Wait()
	in, out, err := n.probe.LineBytes()
	apps := n.probe.Connections()

	n.mu.Lock()
	defer n.mu.Unlock()
	if routerIP != "" {
		n.router = pushSample(n.router, pingSample{float64(rMs.Microseconds()) / 1000, rOK})
	}
	if relayIP != "" {
		n.valve = pushSample(n.valve, pingSample{float64(vMs.Microseconds()) / 1000, vOK})
	}
	now := time.Now()
	if err == nil {
		if !n.lastAt.IsZero() && in >= n.lastIn && out >= n.lastOut {
			secs := now.Sub(n.lastAt).Seconds()
			if secs > 0 {
				n.down = float64(in-n.lastIn) * 8 / secs / 1e6
				n.up = float64(out-n.lastOut) * 8 / secs / 1e6
			}
		}
		n.lastIn, n.lastOut, n.lastAt = in, out, now
	} else {
		// No reading, or a different adapter: start over from the next one.
		n.lastAt = time.Time{}
	}
	n.apps = topApps(apps, 6)
}

func pushSample(list []pingSample, s pingSample) []pingSample {
	list = append(list, s)
	if len(list) > netWindow {
		list = list[len(list)-netWindow:]
	}
	return list
}

// Programs that are part of Windows or of the game itself are left out of
// the "using the internet" list.
var netQuietApps = map[string]bool{"svchost": true, "System": true, "dota2": true, "thetracker": true, "lsass": true, "services": true}

func topApps(conns map[string]int, max int) []NetApp {
	out := []NetApp{}
	for name, c := range conns {
		if !netQuietApps[name] {
			out = append(out, NetApp{Name: name, Connections: c})
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Connections != out[j].Connections {
			return out[i].Connections > out[j].Connections
		}
		return out[i].Name < out[j].Name
	})
	if len(out) > max {
		out = out[:max]
	}
	return out
}

// pickRelay chooses the Valve relay that answers fastest, from Valve's own
// public list of Dota's relays (cached a day).
func (n *NetCheck) pickRelay() {
	type relay struct {
		IPv4 string `json:"ipv4"`
	}
	type pop struct {
		Desc   string  `json:"desc"`
		Relays []relay `json:"relays"`
	}
	type config struct {
		Pops map[string]pop `json:"pops"`
	}
	cfg, _, err := cachedFetch(n.store, "steam_sdr_570", 24*time.Hour, false, func() (config, error) {
		var c config
		err := n.steam.get("/ISteamApps/GetSDRConfig/v1/?appid=570", &c)
		return c, err
	})
	if err != nil || len(cfg.Pops) == 0 {
		n.mu.Lock()
		n.relayAt = time.Now().Add(-time.Hour + 15*time.Second)
		n.mu.Unlock()
		return
	}
	type result struct {
		ip, ip2, where string
		ms             time.Duration
	}
	results := make(chan result, len(cfg.Pops))
	var wg sync.WaitGroup
	for _, p := range cfg.Pops {
		if len(p.Relays) == 0 || p.Relays[0].IPv4 == "" {
			continue
		}
		wg.Add(1)
		ip2 := ""
		if len(p.Relays) > 1 {
			ip2 = p.Relays[1].IPv4
		}
		go func(ip, ip2, where string) {
			defer wg.Done()
			if ms, ok := n.probe.Ping(ip, 800*time.Millisecond); ok {
				results <- result{ip, ip2, where, ms}
			}
		}(p.Relays[0].IPv4, ip2, p.Desc)
	}
	wg.Wait()
	close(results)
	var best *result
	for r := range results {
		if best == nil || r.ms < best.ms {
			r := r
			best = &r
		}
	}
	n.mu.Lock()
	defer n.mu.Unlock()
	// A failed pick is tried again in a few seconds, a good one each hour.
	n.relayAt = time.Now().Add(-time.Hour + 15*time.Second)
	if best != nil {
		n.relayAt = time.Now()
		if best.ip != n.relayIP {
			n.valve = nil
		}
		n.relayIP, n.relayIP2, n.relayWhere = best.ip, best.ip2, best.where
	}
}

func statsOf(list []pingSample) *PingStats {
	if len(list) == 0 {
		return nil
	}
	s := &PingStats{Samples: len(list)}
	var ok []float64
	for _, p := range list {
		if p.ok {
			ok = append(ok, p.ms)
		}
	}
	s.Loss = round1(100 * float64(len(list)-len(ok)) / float64(len(list)))
	if len(ok) == 0 {
		return s
	}
	sum, best := 0.0, ok[0]
	for _, v := range ok {
		sum += v
		best = math.Min(best, v)
	}
	s.Avg, s.Best = round1(sum/float64(len(ok))), round1(best)
	// Jitter: the average change from one reply to the next.
	if len(ok) > 1 {
		d := 0.0
		for i := 1; i < len(ok); i++ {
			d += math.Abs(ok[i] - ok[i-1])
		}
		s.Jitter = round1(d / float64(len(ok)-1))
	}
	return s
}

func round1(v float64) float64 { return math.Round(v*10) / 10 }

// Lines between fine and not. A home router answers in a few milliseconds
// over cable and under ~10 over good Wi-Fi; past these it is the home
// network. Toward Valve, distance sets the ping and nothing local can lower
// it, so what counts is loss, jitter, and a ping well above the best seen in
// the same minute (the line queueing up behind other traffic).
const (
	routerSlowMs    = 25.0
	routerJitterMs  = 15.0
	routerLossPct   = 5.0
	valveLossPct    = 5.0
	valveJitterMs   = 20.0
	valveQueueMs    = 40.0
	busyUpMbps      = 2.0
	busyDownMbps    = 25.0
	verdictMinCount = 5
)

func netVerdict(router, valve *PingStats, down, up float64, apps []NetApp) (string, string) {
	if (router == nil || router.Samples < verdictMinCount) && (valve == nil || valve.Samples < verdictMinCount) {
		return "measuring", "Measuring for a few seconds…"
	}
	if router != nil && router.Samples >= verdictMinCount && (router.Loss >= routerLossPct || router.Avg >= routerSlowMs || router.Jitter >= routerJitterMs) {
		return "local", "Lag is on your side: your Wi-Fi or home network is slow to reach the router. A cable, or moving closer to the router, usually fixes it."
	}
	if valve != nil && valve.Samples >= verdictMinCount && (valve.Loss >= valveLossPct || valve.Jitter >= valveJitterMs || valve.Avg-valve.Best >= valveQueueMs) {
		if up >= busyUpMbps || down >= busyDownMbps {
			names := []string{}
			for _, a := range apps {
				if len(names) < 3 {
					names = append(names, a.Name)
				}
			}
			who := ""
			if len(names) > 0 {
				who = " Programs online now: " + strings.Join(names, ", ") + "."
			}
			return "busy", "Your line is busy while you play, and the game's packets wait behind it." + who
		}
		return "internet", "Your home network is fine; the lag is past your router, at your internet provider or on the way to Valve."
	}
	return "ok", "Your connection is fine."
}

func (n *NetCheck) Status() NetStatus {
	n.mu.Lock()
	defer n.mu.Unlock()
	s := NetStatus{Active: n.wanted(), Game: n.game, RouterIP: n.routerIP, ValveWhere: n.relayWhere,
		Router: statsOf(n.router), Valve: statsOf(n.valve), DownMbps: round1(n.down), UpMbps: round1(n.up), Apps: n.apps}
	if s.Apps == nil {
		s.Apps = []NetApp{}
	}
	if time.Now().Before(n.testUntil) {
		s.TestUntil = n.testUntil.Unix()
	}
	s.Verdict, s.Detail = netVerdict(s.Router, s.Valve, n.down, n.up, n.apps)
	return s
}
