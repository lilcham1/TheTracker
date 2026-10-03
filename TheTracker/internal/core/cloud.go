package core

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Cloud: optional account sign-in and sync to the app's Convex deployment,
// which backs the shared leaderboard.
//
// history.json stays the source of truth. Every match is written to disk
// first and pushed afterwards on a background worker; if the network or the
// deployment is down the tracker carries on and the failure shows up as a
// status line, not as lost data.

const defaultConvexURL = "https://calculating-seahorse-132.convex.cloud"

func convexURL() string {
	if u := os.Getenv("DOTA_TRACKER_CONVEX_URL"); u != "" {
		return strings.TrimRight(u, "/")
	}
	return defaultConvexURL
}

type AuthState struct {
	SignedIn  bool    `json:"signedIn"`
	Email     *string `json:"email"`
	UserID    *string `json:"userId"`
	LastError *string `json:"lastError"`
}

type SyncStatus struct {
	Connected bool    `json:"connected"`
	Pending   int     `json:"pending"`
	Synced    int     `json:"synced"`
	LastError *string `json:"lastError"`
	LastSync  *string `json:"lastSync"`
	// Uploads are held back only because nobody is signed in.
	NeedsSignIn bool `json:"needsSignIn"`
}

type syncJob struct {
	matches []MatchSummary
	profile *Profile
}

type Cloud struct {
	store *Store
	base  string
	http  *http.Client

	mu           sync.Mutex
	auth         AuthState
	token        string // short-lived JWT; never leaves the backend
	refreshToken string
	status       SyncStatus

	jobs chan syncJob
}

func NewCloud(store *Store) *Cloud {
	c := &Cloud{store: store, base: convexURL(), http: &http.Client{Timeout: 25 * time.Second}, jobs: make(chan syncJob, 256)}
	go c.worker()
	return c
}

// ---------- Convex over HTTP ----------

// call runs one Convex function. kind is query, mutation or action.
func (c *Cloud) call(kind, path string, args any, token string) (any, error) {
	if args == nil {
		args = map[string]any{}
	}
	body, _ := json.Marshal(map[string]any{"path": path, "args": args, "format": "json"})
	req, err := http.NewRequest(http.MethodPost, c.base+"/api/"+kind, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", userAgent())
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, errors.New("Couldn't reach the cloud service. Check your connection.")
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 8<<20))

	var out struct {
		Status       string `json:"status"`
		Value        any    `json:"value"`
		ErrorMessage string `json:"errorMessage"`
	}
	if json.Unmarshal(raw, &out) != nil || out.Status == "" {
		return nil, fmt.Errorf("The cloud service gave an unexpected reply (HTTP %d).", resp.StatusCode)
	}
	if out.Status != "success" {
		return nil, errors.New(out.ErrorMessage)
	}
	return out.Value, nil
}

// ---------- Auth ----------

type storedAuth struct {
	Email        *string `json:"email"`
	RefreshToken *string `json:"refreshToken"`
}

func (c *Cloud) Auth() AuthState {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.auth
}

func (c *Cloud) signedIn() (string, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.token, c.auth.SignedIn && c.token != ""
}

// friendlyAuthError turns Convex's long provider error strings into something
// worth showing a person.
func friendlyAuthError(raw string) string {
	lower := strings.ToLower(raw)
	switch {
	case strings.Contains(lower, "invalidsecret"), strings.Contains(lower, "invalid password"), strings.Contains(lower, "invalidaccountid"):
		return "Wrong email or password."
	case strings.Contains(lower, "already exists"), strings.Contains(lower, "account already"):
		return "An account with that email already exists. Try signing in."
	case strings.Contains(lower, "password") && strings.Contains(lower, "8"):
		return "Password must be at least 8 characters."
	case strings.Contains(lower, "invalid") && strings.Contains(lower, "email"):
		return "That doesn't look like a valid email address."
	case strings.Contains(lower, "couldn't reach"):
		return raw
	}
	return "Sign-in failed: " + strings.SplitN(raw, "\n", 2)[0]
}

func (c *Cloud) exchange(args map[string]any) (token, refresh string, err error) {
	v, err := c.call("action", "auth:signIn", args, "")
	if err != nil {
		return "", "", errors.New(friendlyAuthError(err.Error()))
	}
	m, _ := v.(map[string]any)
	tokens := sub(m, "tokens")
	token, _ = getStr(tokens, "token")
	refresh, _ = getStr(tokens, "refreshToken")
	if token == "" || refresh == "" {
		return "", "", errors.New("The cloud service didn't return a session. Check the email and password.")
	}
	return token, refresh, nil
}

func (c *Cloud) fetchUserID(token string) *string {
	v, err := c.call("query", "profiles:whoami", nil, token)
	if err != nil {
		return nil
	}
	m, _ := v.(map[string]any)
	return jStrPtr(m, "userId")
}

// SignIn creates an account (flow "signUp") or signs into one ("signIn"),
// then claims matches this install synced before it had an account and pushes
// anything waiting locally.
func (c *Cloud) SignIn(email, password, flow string) (AuthState, error) {
	email = strings.TrimSpace(email)
	if email == "" || password == "" {
		return c.Auth(), errors.New("Enter an email and a password.")
	}
	if flow != "signUp" {
		flow = "signIn"
	}
	token, refresh, err := c.exchange(map[string]any{
		"provider": "password",
		"params":   map[string]any{"email": email, "password": password, "flow": flow},
	})
	if err != nil {
		return c.Auth(), err
	}
	userID := c.fetchUserID(token)
	_ = c.store.writeJSON("auth.json", storedAuth{Email: &email, RefreshToken: &refresh})

	c.mu.Lock()
	c.auth = AuthState{SignedIn: true, Email: &email, UserID: userID}
	c.token, c.refreshToken = token, refresh
	c.status.NeedsSignIn = false
	c.mu.Unlock()

	// Best-effort: a failure here must not undo a good sign-in.
	_, _ = c.call("mutation", "matches:claimDevice", map[string]any{"deviceId": c.store.DeviceID()}, token)
	c.SyncAll()
	return c.Auth(), nil
}

// refresh trades the stored refresh token for a fresh JWT.
func (c *Cloud) refresh() error {
	c.mu.Lock()
	rt, email := c.refreshToken, c.auth.Email
	c.mu.Unlock()
	if rt == "" {
		return errors.New("Not signed in")
	}
	token, newRefresh, err := c.exchange(map[string]any{"refreshToken": rt})
	if err != nil {
		// Only a rejected token ends the session. Being offline at launch
		// must not sign the player out.
		if !strings.Contains(err.Error(), "Couldn't reach") {
			_ = os.Remove(c.store.path("auth.json"))
			c.mu.Lock()
			c.auth = AuthState{Email: email, LastError: ptr("Your session expired. Sign in again.")}
			c.token, c.refreshToken = "", ""
			c.mu.Unlock()
		}
		return err
	}
	userID := c.fetchUserID(token)
	_ = c.store.writeJSON("auth.json", storedAuth{Email: email, RefreshToken: &newRefresh})
	c.mu.Lock()
	c.auth = AuthState{SignedIn: true, Email: email, UserID: userID}
	c.token, c.refreshToken = token, newRefresh
	c.status.NeedsSignIn = false
	c.mu.Unlock()
	return nil
}

// Restore rebuilds the session from disk at startup.
func (c *Cloud) Restore() {
	var s storedAuth
	if !c.store.readJSON("auth.json", &s) || s.RefreshToken == nil || *s.RefreshToken == "" {
		return
	}
	c.mu.Lock()
	c.auth.Email = s.Email
	c.refreshToken = *s.RefreshToken
	c.mu.Unlock()
	go func() { _ = c.refresh() }()
}

func (c *Cloud) SignOut() AuthState {
	c.mu.Lock()
	token := c.token
	c.mu.Unlock()
	if token != "" {
		_, _ = c.call("action", "auth:signOut", nil, token)
	}
	_ = os.Remove(c.store.path("auth.json"))
	c.mu.Lock()
	c.auth = AuthState{}
	c.token, c.refreshToken = "", ""
	c.mu.Unlock()
	return c.Auth()
}

// DeleteCloudData removes everything this account has published.
func (c *Cloud) DeleteCloudData() (int, error) {
	token, ok := c.signedIn()
	if !ok {
		return 0, errors.New("Sign in first.")
	}
	v, err := c.call("mutation", "matches:removeMine", nil, token)
	if err != nil {
		return 0, err
	}
	m, _ := v.(map[string]any)
	return int(jI64(m, "deleted")), nil
}

// ---------- Sync ----------

func (c *Cloud) Status() SyncStatus {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.status
}

func (c *Cloud) setStatus(f func(*SyncStatus)) {
	c.mu.Lock()
	f(&c.status)
	c.mu.Unlock()
}

func (c *Cloud) enqueue(job syncJob) {
	n := max(len(job.matches), 1)
	select {
	case c.jobs <- job:
		c.setStatus(func(s *SyncStatus) { s.Pending += n })
	default: // queue full: sync is best-effort and the files hold the data
	}
}

func (c *Cloud) PushMatch(m MatchSummary) { c.enqueue(syncJob{matches: []MatchSummary{m}}) }
func (c *Cloud) PushProfile(p Profile)    { c.enqueue(syncJob{profile: &p}) }

// SyncAll pushes the whole local history. Safe to repeat: the server upserts
// on (account, matchid).
func (c *Cloud) SyncAll() int {
	h := c.store.History()
	if len(h) > 0 {
		c.enqueue(syncJob{matches: h})
	}
	c.PushProfile(c.store.LoadProfile())
	return len(h)
}

func isAuthError(msg string) bool {
	m := strings.ToLower(msg)
	return strings.Contains(m, "sign in") || strings.Contains(m, "unauthenticated") || strings.Contains(m, "unauthorized")
}

func (c *Cloud) worker() {
	for job := range c.jobs {
		queued := max(len(job.matches), 1)
		settle := func(s *SyncStatus) { s.Pending = max(s.Pending-queued, 0) }

		// Publishing needs an account. Rather than burn the job against a
		// server that will reject it, hold it back and say so.
		if _, ok := c.signedIn(); !ok {
			c.setStatus(func(s *SyncStatus) { settle(s); s.NeedsSignIn = true })
			continue
		}

		pushed, err := c.runJob(job)
		// A JWT that lapsed mid-session should not cost a match: refresh
		// once and replay.
		if err != nil && isAuthError(err.Error()) {
			if c.refresh() == nil {
				var more int
				more, err = c.runJob(syncJob{matches: job.matches[pushed:], profile: job.profile})
				pushed += more
			} else {
				c.setStatus(func(s *SyncStatus) { s.NeedsSignIn = true })
			}
		}

		c.setStatus(func(s *SyncStatus) {
			settle(s)
			s.Synced += pushed
			s.Connected = err == nil
			if err != nil {
				s.LastError = ptr(err.Error())
				return
			}
			s.LastError = nil
			s.LastSync = ptr(time.Now().Format("15:04:05"))
		})
	}
}

// runJob returns how many matches landed and the first error that stopped it.
func (c *Cloud) runJob(job syncJob) (int, error) {
	token, _ := c.signedIn()
	device := c.store.DeviceID()
	username := c.store.LoadProfile().Username
	pushed := 0
	for _, m := range job.matches {
		if _, err := c.call("mutation", "matches:upsert", matchArgs(device, username, m), token); err != nil {
			return pushed, err
		}
		pushed++
	}
	if p := job.profile; p != nil {
		args := map[string]any{"deviceId": device, "username": p.Username, "rank": p.Rank, "role": p.Role}
		if _, err := c.call("mutation", "profiles:upsert", args, token); err != nil {
			return pushed, err
		}
	}
	return pushed, nil
}

// matchArgs builds the cloud row field by field rather than serializing the
// whole summary: the server's validator rejects unknown arguments, and the
// comparison fields are local-only.
func matchArgs(device, username string, m MatchSummary) map[string]any {
	checkpoints := map[string]any{}
	for minute, cp := range m.Checkpoints {
		if cp == nil {
			checkpoints[strconv.Itoa(minute)] = nil
		} else {
			checkpoints[strconv.Itoa(minute)] = map[string]any{"lastHits": cp.LastHits, "denies": cp.Denies}
		}
	}
	deaths := []any{}
	for _, d := range m.Deaths {
		deaths = append(deaths, map[string]any{"clock": d.Clock, "goldLost": d.GoldLost})
	}
	items := []any{}
	for _, k := range m.KeyItems {
		items = append(items, map[string]any{"clock": k.Clock, "item": k.Item})
	}
	var lh25 any
	if cp := m.Checkpoints[25]; cp != nil {
		lh25 = cp.LastHits
	}
	return map[string]any{
		"deviceId": device, "username": username, "matchid": m.MatchID, "heroName": m.HeroName,
		"date": m.Date, "duration": m.Duration, "kills": m.Kills, "totalDeaths": m.TotalDeaths,
		"totalGoldLost": m.TotalGoldLost, "roshanDeaths": m.RoshanDeaths, "gameType": m.GameType,
		"lastHits25": lh25, "checkpoints": checkpoints, "deaths": deaths, "keyItems": items,
	}
}

// GlobalLeaderboard is the cross-player leaderboard. Readable signed out.
func (c *Cloud) GlobalLeaderboard(metric, gameType string, limit int) (any, error) {
	if limit <= 0 {
		limit = 10
	}
	v, err := c.call("query", "leaderboard:globalTop", map[string]any{"metric": metric, "gameType": gameType, "limit": limit}, "")
	if err != nil {
		return nil, err
	}
	if v == nil {
		v = []any{}
	}
	return v, nil
}

// Ping reports whether the cloud deployment answers, for diagnostics.
func (c *Cloud) Ping() error {
	_, err := c.call("query", "leaderboard:globalTop", map[string]any{"metric": "most_kills", "limit": 1}, "")
	return err
}
