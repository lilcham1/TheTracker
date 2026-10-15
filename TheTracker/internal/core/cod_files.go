package core

// Where Call of Duty keeps its settings on this PC: Documents\Call of
// Duty\players. The graphics file is named for the game that wrote it
// (s.1.0.cod24.txt0 is Black Ops 6; each new CoD brings its own number), so
// the highest number present is the one in use. The gameplay file (FOV,
// mouse) sits in a folder per account. Each file has a twin (.txt0 and
// .txt1) the game alternates between; both are edited the same way.

import (
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

// codPlayersDir is CoD's settings folder; a test swaps it.
var codPlayersDir = func() string {
	return filepath.Join(documentsDir(), "Call of Duty", "players")
}

var codSystemFile = regexp.MustCompile(`^s\.1\.0\.cod(\d+)\.txt0$`)

func isCodSettingsFile(path string) bool {
	name := filepath.Base(path)
	return strings.HasSuffix(name, ".txt0") || strings.HasSuffix(name, ".txt1")
}

// codSettingsFiles finds the graphics and gameplay files and their twins;
// missing ones are left out.
func codSettingsFiles(dir string) []PresetFile {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var files []PresetFile
	add := func(kind, path string) {
		if _, err := os.Stat(path); err == nil {
			files = append(files, PresetFile{Kind: kind, Path: path, Found: true})
		}
	}
	best, bestN := "", -1
	for _, e := range entries {
		if m := codSystemFile.FindStringSubmatch(e.Name()); m != nil {
			if n, _ := strconv.Atoi(m[1]); n > bestN {
				best, bestN = e.Name(), n
			}
		}
	}
	if best != "" {
		add("system", filepath.Join(dir, best))
		add("system2", filepath.Join(dir, strings.TrimSuffix(best, "0")+"1"))
	}
	// The account folder whose gameplay file was written last.
	var gameplay string
	var newest int64
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		p := filepath.Join(dir, e.Name(), "g.1.0.l.txt0")
		if st, err := os.Stat(p); err == nil && st.ModTime().UnixNano() > newest {
			gameplay, newest = p, st.ModTime().UnixNano()
		}
	}
	if gameplay != "" {
		add("gameplay", gameplay)
		add("gameplay2", strings.TrimSuffix(gameplay, "0")+"1")
	}
	return files
}
