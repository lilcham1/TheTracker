package core

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const codSample = "1\n\n\n//\n// Display\n\n// Fullscreen mode\nDisplayMode@0;1013;1013 = Fullscreen borderless window // one of Windowed, Fullscreen, Fullscreen borderless window\n\n" +
	"// Field of view\nFov@1;5;6 = 103.000000 // 60.000000 to 120.000000\nShowBlood@0;7;8 = true\nTextChat background opacity@0;1;2 = 0.200000 // 0.000000 to 1.000000\n"

func TestCodDocRoundTripAndEdit(t *testing.T) {
	d := codParse(codSample)
	if d.String() != codSample {
		t.Fatal("round trip changed the file")
	}
	if v, ok := d.Get("DisplayMode@0"); !ok || v != "Fullscreen borderless window" {
		t.Fatalf("get: %q %v", v, ok)
	}
	if v, ok := d.Get("TextChat background opacity@0"); !ok || v != "0.200000" {
		t.Fatalf("name with spaces: %q %v", v, ok)
	}
	if !d.Allows("Fullscreen", "DisplayMode@0") || d.Allows("Exclusive", "DisplayMode@0") {
		t.Fatal("choices")
	}
	if !d.Allows("110.000000", "Fov@1") || d.Allows("130", "Fov@1") || d.Allows("wide", "Fov@1") {
		t.Fatal("range")
	}
	if !d.Allows("false", "ShowBlood@0") || d.Allows("x", "Missing@0") {
		t.Fatal("no rule / missing")
	}
	d.Set("Fullscreen", "DisplayMode@0")
	d.Set("110.000000", "Fov@1")
	d.Set("1", "Missing@0") // never added
	want := strings.Replace(strings.Replace(codSample, "= Fullscreen borderless window //", "= Fullscreen //", 1), "= 103.000000", "= 110.000000", 1)
	if d.String() != want {
		t.Fatalf("edit:\n%s", d.String())
	}
	if _, ok := d.Get("// Field of view"); ok {
		t.Fatal("a comment read as a setting")
	}
}

// With TT_COD_DIR pointing at CoD's players folder, every settings file in
// it reads back byte for byte. Read-only; no values are printed.
func TestCodDocRealFiles(t *testing.T) {
	dir := os.Getenv("TT_COD_DIR")
	if dir == "" {
		t.Skip()
	}
	n := 0
	filepath.WalkDir(dir, func(p string, e os.DirEntry, err error) error {
		if err != nil || e.IsDir() || !strings.Contains(e.Name(), ".txt") {
			return nil
		}
		raw, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		if codParse(string(raw)).String() != string(raw) {
			t.Errorf("%s doesn't round trip", e.Name())
		}
		n++
		return nil
	})
	if n == 0 {
		t.Fatal("no settings files found")
	}
	t.Logf("%d files round trip", n)
}
