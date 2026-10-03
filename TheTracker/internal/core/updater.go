package core

import (
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"golang.org/x/crypto/blake2b"
)

// In-app updates, from the project's GitHub Releases.
//
// The feed is releases/latest/download/latest.json, which GitHub always
// resolves to the newest release. Every download is checked against the
// public key below before it is run; an unsigned or altered installer is
// refused. The key and the signature format are the ones the app has used
// since its first release, so installs on any earlier version update into
// this one and this one updates onward the same way.

const (
	updateFeedURL = "https://github.com/lilcham1/TheTracker/releases/latest/download/latest.json"
	// A minisign public key, base64 of the key file text.
	updatePubKey = "dW50cnVzdGVkIGNvbW1lbnQ6IG1pbmlzaWduIHB1YmxpYyBrZXk6IDk5MEE1M0QwRDc0MzIwMjAKUldRZ0lFUFgwRk1LbWJUazJIcW55cENpYjc0d1Q5bGdYMVBDVDZ1ZHhYTDdPMXpSckdhUjBIZ2wK"
)

type UpdateInfo struct {
	Available bool    `json:"available"`
	Version   *string `json:"version"`
	Notes     *string `json:"notes"`
	Date      *string `json:"date"`
	Current   string  `json:"current"`
}

type updateManifest struct {
	Version   string `json:"version"`
	Notes     string `json:"notes"`
	PubDate   string `json:"pub_date"`
	Platforms map[string]struct {
		Signature string `json:"signature"`
		URL       string `json:"url"`
	} `json:"platforms"`
}

type Updater struct {
	FeedURL string
	PubKey  string
	client  *http.Client
}

func NewUpdater() *Updater {
	return &Updater{FeedURL: updateFeedURL, PubKey: updatePubKey, client: &http.Client{Timeout: 5 * time.Minute}}
}

// versionLess compares dotted numeric versions; anything after a '-' or '+'
// is ignored.
func versionLess(a, b string) bool {
	parse := func(v string) []int {
		v = strings.TrimPrefix(strings.TrimSpace(v), "v")
		if i := strings.IndexAny(v, "-+"); i >= 0 {
			v = v[:i]
		}
		var out []int
		for _, p := range strings.Split(v, ".") {
			n, _ := strconv.Atoi(p)
			out = append(out, n)
		}
		return out
	}
	x, y := parse(a), parse(b)
	for i := 0; i < len(x) || i < len(y); i++ {
		var xi, yi int
		if i < len(x) {
			xi = x[i]
		}
		if i < len(y) {
			yi = y[i]
		}
		if xi != yi {
			return xi < yi
		}
	}
	return false
}

func (u *Updater) manifest() (*updateManifest, error) {
	req, _ := http.NewRequest(http.MethodGet, u.FeedURL, nil)
	req.Header.Set("User-Agent", userAgent())
	resp, err := u.client.Do(req)
	if err != nil {
		return nil, errors.New("Couldn't reach the update server. Check your connection.")
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return nil, errors.New("No releases are published yet.")
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("The update server returned an error (%d).", resp.StatusCode)
	}
	var m updateManifest
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if json.Unmarshal(raw, &m) != nil || m.Version == "" {
		return nil, errors.New("The update feed couldn't be read.")
	}
	return &m, nil
}

// Check asks the release feed whether anything newer exists. "Couldn't reach
// the server" and "you're up to date" are different answers, and the caller
// gets to know which.
func (u *Updater) Check() (UpdateInfo, error) {
	info := UpdateInfo{Current: Version}
	m, err := u.manifest()
	if err != nil {
		return info, err
	}
	if versionLess(Version, m.Version) {
		info.Available = true
		info.Version = &m.Version
		if m.Notes != "" {
			info.Notes = &m.Notes
		}
		if m.PubDate != "" {
			info.Date = &m.PubDate
		}
	}
	return info, nil
}

// Download fetches the pending update's installer, verifies its signature and
// returns its path. Nothing is run here; the shell launches it and quits.
func (u *Updater) Download() (string, error) {
	m, err := u.manifest()
	if err != nil {
		return "", err
	}
	if !versionLess(Version, m.Version) {
		return "", errors.New("You're already on the latest version.")
	}
	p, ok := m.Platforms["windows-x86_64"]
	if !ok || p.URL == "" || p.Signature == "" {
		return "", errors.New("That release has no Windows installer.")
	}
	if !strings.HasPrefix(p.URL, "https://github.com/lilcham1/TheTracker/") {
		return "", errors.New("The update points somewhere unexpected and was not downloaded.")
	}

	req, _ := http.NewRequest(http.MethodGet, p.URL, nil)
	req.Header.Set("User-Agent", userAgent())
	resp, err := u.client.Do(req)
	if err != nil {
		return "", errors.New("The download failed. Check your connection and try again.")
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("The download failed (error %d).", resp.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, 300<<20))
	if err != nil {
		return "", errors.New("The download was cut off. Try again.")
	}
	if err := VerifySignature(u.PubKey, p.Signature, data); err != nil {
		return "", errors.New("That update failed its signature check and was not installed.")
	}

	dir := filepath.Join(os.TempDir(), "TheTracker-update")
	_ = os.MkdirAll(dir, 0o755)
	path := filepath.Join(dir, fmt.Sprintf("TheTracker_%s_x64-setup.exe", m.Version))
	if err := os.WriteFile(path, data, 0o755); err != nil {
		return "", fmt.Errorf("Couldn't save the update: %v", err)
	}
	return path, nil
}

// VerifySignature checks data against a minisign signature. Both the key and
// the signature arrive as base64 of their minisign text files, which is how
// the release feed has always carried them.
func VerifySignature(pubKeyB64, sigB64 string, data []byte) error {
	lastLine := func(b64 string, n int) ([]string, error) {
		text, err := base64.StdEncoding.DecodeString(strings.TrimSpace(b64))
		if err != nil {
			return nil, err
		}
		lines := strings.Split(strings.ReplaceAll(strings.TrimSpace(string(text)), "\r\n", "\n"), "\n")
		if len(lines) < n {
			return nil, errors.New("malformed")
		}
		return lines, nil
	}

	keyLines, err := lastLine(pubKeyB64, 2)
	if err != nil {
		return err
	}
	key, err := base64.StdEncoding.DecodeString(keyLines[len(keyLines)-1])
	if err != nil || len(key) != 42 || string(key[:2]) != "Ed" {
		return errors.New("bad public key")
	}
	keyID, pub := key[2:10], ed25519.PublicKey(key[10:])

	sigLines, err := lastLine(sigB64, 4)
	if err != nil {
		return err
	}
	sig, err := base64.StdEncoding.DecodeString(sigLines[1])
	if err != nil || len(sig) != 74 {
		return errors.New("bad signature")
	}
	if string(sig[2:10]) != string(keyID) {
		return errors.New("signed with a different key")
	}

	var message []byte
	switch string(sig[:2]) {
	case "ED": // prehashed
		h := blake2b.Sum512(data)
		message = h[:]
	case "Ed":
		message = data
	default:
		return errors.New("unknown signature algorithm")
	}
	if !ed25519.Verify(pub, message, sig[10:]) {
		return errors.New("signature does not match")
	}

	// The trusted comment is covered by a second signature.
	const prefix = "trusted comment: "
	if !strings.HasPrefix(sigLines[2], prefix) {
		return errors.New("missing trusted comment")
	}
	global, err := base64.StdEncoding.DecodeString(sigLines[3])
	if err != nil || len(global) != ed25519.SignatureSize {
		return errors.New("bad global signature")
	}
	if !ed25519.Verify(pub, append(append([]byte{}, sig[10:]...), []byte(strings.TrimPrefix(sigLines[2], prefix))...), global) {
		return errors.New("trusted comment does not match")
	}
	return nil
}
