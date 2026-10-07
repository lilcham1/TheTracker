package core

import (
	"bytes"
	"encoding/base64"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// Share cards: the page draws the image itself; the backend only saves it
// where the player's other exports go.

var unsafeName = regexp.MustCompile(`[^A-Za-z0-9 _.-]+`)

var pngMagic = []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'}

// SaveImage writes a PNG sent as a data URL into dir and returns its path.
// Anything that is not a PNG of a sensible size is refused.
func SaveImage(dir, name, dataURL string) (string, error) {
	raw, ok := strings.CutPrefix(dataURL, "data:image/png;base64,")
	if !ok {
		return "", errors.New("The image wasn't a PNG.")
	}
	img, err := base64.StdEncoding.DecodeString(raw)
	if err != nil || !bytes.HasPrefix(img, pngMagic) {
		return "", errors.New("The image wasn't a PNG.")
	}
	if len(img) > 8<<20 {
		return "", errors.New("The image is too large to save.")
	}
	name = strings.TrimSpace(unsafeName.ReplaceAllString(name, ""))
	if name == "" {
		name = "card"
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	path := filepath.Join(dir, "TheTracker "+name+" "+time.Now().Format("2006-01-02 150405")+".png")
	if err := os.WriteFile(path, img, 0o644); err != nil {
		return "", err
	}
	return path, nil
}
