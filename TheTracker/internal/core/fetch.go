package core

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"time"
)

// One HTTP path for every public API the app reads.

var httpClient = &http.Client{Timeout: 20 * time.Second}

func userAgent() string { return "TheTracker/" + Version + " (+desktop)" }

// apiError is a failure worth showing to a person, plus whether trying again
// could plausibly help.
type apiError struct {
	msg       string
	retryable bool
}

func (e *apiError) Error() string { return e.msg }

// service names an API in error messages and holds its base URL, which tests
// point at a local server.
type service struct {
	Name string
	Base string
	// How many times a request is attempted. The public APIs are free and
	// frequently slow; retrying turns the common blip into a short delay
	// instead of an error on the page.
	Attempts int
}

func (s *service) get(path string, out any) error {
	return s.do(http.MethodGet, path, nil, out)
}

func (s *service) do(method, path string, body any, out any) error {
	attempts := max(s.Attempts, 1)
	var last error
	for i := 0; i < attempts; i++ {
		if i > 0 {
			// 400ms, then 1.6s: short enough not to feel like a hang, long
			// enough to clear a rate-limit window.
			time.Sleep(time.Duration(400*(1<<(2*(i-1)))) * time.Millisecond)
		}
		err := s.attempt(method, path, body, out)
		if err == nil {
			return nil
		}
		last = err
		var ae *apiError
		if !errors.As(err, &ae) || !ae.retryable {
			break
		}
	}
	return last
}

func (s *service) attempt(method, path string, body any, out any) error {
	var reader io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reader = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(context.Background(), method, s.Base+path, reader)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", userAgent())
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := httpClient.Do(req)
	if err != nil {
		var ne net.Error
		if errors.As(err, &ne) && ne.Timeout() {
			return &apiError{fmt.Sprintf("%s timed out. Try again in a moment.", s.Name), true}
		}
		return &apiError{fmt.Sprintf("Couldn't reach %s. Check your connection.", s.Name), true}
	}
	defer resp.Body.Close()

	switch {
	case resp.StatusCode == http.StatusTooManyRequests:
		return &apiError{fmt.Sprintf("%s is rate-limiting right now. Try again shortly.", s.Name), true}
	case resp.StatusCode >= 500:
		return &apiError{fmt.Sprintf("%s is having problems (error %d). Try again shortly.", s.Name, resp.StatusCode), true}
	case resp.StatusCode == http.StatusNotFound:
		return &apiError{fmt.Sprintf("%s has no record of that.", s.Name), false}
	case resp.StatusCode >= 400:
		return &apiError{fmt.Sprintf("%s refused the request (error %d).", s.Name, resp.StatusCode), false}
	}
	if out == nil {
		_, _ = io.Copy(io.Discard, resp.Body)
		return nil
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 32<<20))
	if err != nil {
		return &apiError{fmt.Sprintf("%s's reply was cut off. Try again.", s.Name), true}
	}
	if err := json.Unmarshal(raw, out); err != nil {
		return &apiError{fmt.Sprintf("%s sent a reply TheTracker couldn't read.", s.Name), false}
	}
	return nil
}

// Freshness says how old a result is and whether it is a fallback.
type Freshness struct {
	// Unix seconds the data was fetched.
	FetchedAt int64 `json:"fetchedAt"`
	// True when the API could not be reached and this is the last good copy.
	Stale bool `json:"stale"`
	// Why the fresh fetch failed, when Stale is true.
	Error string `json:"error,omitempty"`
}

// cachedFetch returns a value from the disk cache while it is younger than
// ttl, and otherwise fetches it. If the fetch fails and an older copy exists,
// that copy is returned marked stale — the page keeps its numbers and says
// they are old, instead of going blank.
func cachedFetch[T any](s *Store, key string, ttl time.Duration, force bool, fetch func() (T, error)) (T, Freshness, error) {
	var v T
	if !force {
		if env, ok := s.readCache(key); ok {
			age := time.Now().Unix() - env.FetchedAt
			if age >= 0 && age < int64(ttl.Seconds()) && json.Unmarshal(env.Payload, &v) == nil {
				return v, Freshness{FetchedAt: env.FetchedAt}, nil
			}
		}
	}
	fresh, err := fetch()
	if err == nil {
		s.WriteCache(key, fresh)
		return fresh, Freshness{FetchedAt: time.Now().Unix()}, nil
	}
	var old T
	if at, ok := s.ReadCacheStale(key, &old); ok {
		return old, Freshness{FetchedAt: at.Unix(), Stale: true, Error: err.Error()}, nil
	}
	return v, Freshness{}, err
}

// ---------- Loose JSON accessors ----------

func jU64(m jsonMap, key string) uint64 {
	if v, ok := m[key].(float64); ok && v > 0 {
		return uint64(v)
	}
	return 0
}

func jI64(m jsonMap, key string) int64 {
	if v, ok := m[key].(float64); ok {
		return int64(v)
	}
	return 0
}

func jF64(m jsonMap, key string) float64 {
	v, _ := m[key].(float64)
	return v
}

func jStr(m jsonMap, key, fallback string) string {
	if v, ok := m[key].(string); ok && v != "" {
		return v
	}
	return fallback
}

func jStrPtr(m jsonMap, key string) *string {
	if v, ok := m[key].(string); ok && v != "" {
		return &v
	}
	return nil
}

func jBool(m jsonMap, key string) bool {
	v, _ := m[key].(bool)
	return v
}

func jHas(m jsonMap, key string) bool {
	v, ok := m[key]
	return ok && v != nil
}

func jList(v any) []jsonMap {
	arr, _ := v.([]any)
	out := make([]jsonMap, 0, len(arr))
	for _, e := range arr {
		if m, ok := e.(map[string]any); ok {
			out = append(out, m)
		}
	}
	return out
}

func pct(part, whole float64) float64 {
	if whole == 0 {
		return 0
	}
	return part * 100 / whole
}
