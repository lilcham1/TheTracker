package core

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestKeyValuesRoundTripAndEdit(t *testing.T) {
	src := "\"config\"\r\n{\r\n\t\"convars\"\r\n\t{\r\n\t\t\"a\"\t\t\"1\"\r\n\t\t\"\\\"\t\t\"toggleconsole\"\r\n\t}\r\n\t\"Keys\"\r\n\t{\r\n\t\t\"Item1\"\r\n\t\t{\r\n\t\t\t\"Key\"\t\t\"Z\"\r\n\t\t\t\"Key2\"\t\t\"NONE\"\r\n\t\t}\r\n\t}\r\n}\r\n"
	doc, err := kvParse(src)
	if err != nil {
		t.Fatal(err)
	}
	if doc.String() != src {
		t.Fatalf("round trip differs:\n%q\n%q", doc.String(), src)
	}
	if v, ok := doc.Get("config", "convars", "a"); !ok || v != "1" {
		t.Fatalf("get: %q %v", v, ok)
	}
	if v, _ := doc.Get("config", "convars", `\`); v != "toggleconsole" {
		t.Fatalf("a lone backslash is a key, not an escape: %q", v)
	}
	// Edit in place, add at the end of the block, create a block.
	doc.Set("2", "config", "convars", "a")
	doc.Set("true", "config", "convars", "new_one")
	doc.Set("Q", "config", "Keys", "AbilityMelee", "Key")
	out := doc.String()
	for _, want := range []string{"\t\t\"a\"\t\t\"2\"", "\t\t\"new_one\"\t\t\"true\"", "\t\t\"AbilityMelee\"\r\n\t\t{\r\n\t\t\t\"Key\"\t\t\"Q\"\r\n\t\t}"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in\n%s", want, out)
		}
	}
	if !doc.Delete("config", "convars", "new_one") || strings.Contains(doc.String(), "new_one") {
		t.Fatal("delete")
	}
	// Order kept: "a" still before the backslash key.
	if strings.Index(out, `"a"`) > strings.Index(out, `"\"`) {
		t.Fatal("order changed")
	}
	// No trailing newline is kept too.
	d2, _ := kvParse(`"x"		"1"`)
	if d2.String() != `"x"		"1"` {
		t.Fatalf("trailing: %q", d2.String())
	}
	if _, err := kvParse(`"x" {`); err == nil {
		t.Fatal("an unclosed block is an error")
	}
}

// With TT_KV_DIR set to a folder of real settings files, every file in it
// must come back byte for byte. Read only.
func TestKeyValuesRealFilesRoundTrip(t *testing.T) {
	dir := os.Getenv("TT_KV_DIR")
	if dir == "" {
		t.Skip()
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			t.Fatal(err)
		}
		doc, err := kvParse(string(raw))
		if err != nil {
			t.Errorf("%s: %v", e.Name(), err)
			continue
		}
		if out := doc.String(); out != string(raw) {
			i := 0
			for i < len(out) && i < len(raw) && out[i] == raw[i] {
				i++
			}
			lo := max(0, i-60)
			t.Errorf("%s differs at byte %d:\n got %q\nwant %q", e.Name(), i, out[lo:min(len(out), i+60)], string(raw[lo:min(len(raw), i+60)]))
			continue
		}
		n++
		t.Logf("%s: identical (%d bytes)", e.Name(), len(raw))
	}
	if n == 0 {
		t.Fatal("no files checked")
	}
}
