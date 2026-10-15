package core

// Call of Duty's settings files (players\s.1.0.cod<NN>.txt0 and
// players\<id>\g.1.0.l.txt0, each with a .txt1 twin): one setting per line,
//
//	Name@index;hash;hash = value // one of a, b, c   (or: // min to max)
//
// with comment and blank lines between. Kept as lines, so a file read and
// written unchanged is byte for byte the same and an edit changes only the
// value on its own line. The game owns the list of settings: a name the file
// doesn't hold is never added, and a value outside the range or choices the
// line's own comment gives is refused.

import (
	"strconv"
	"strings"
)

type codDoc struct {
	lines    []string
	eol      string
	trailing bool
}

func codParse(text string) *codDoc {
	ini := iniParse(text)
	return &codDoc{lines: ini.lines, eol: ini.eol, trailing: ini.trailing}
}

func (d *codDoc) String() string {
	return (&iniDoc{lines: d.lines, eol: d.eol, trailing: d.trailing}).String()
}

// codLine splits a setting line: its name with index (before the first ';'),
// the part up to the value, the value, and the comment after it.
func codLine(line string) (name, head, value, comment string, ok bool) {
	if strings.HasPrefix(strings.TrimSpace(line), "//") {
		return "", "", "", "", false
	}
	h, rest, found := strings.Cut(line, " = ")
	if !found {
		return "", "", "", "", false
	}
	name, _, _ = strings.Cut(h, ";")
	if !strings.Contains(name, "@") {
		return "", "", "", "", false
	}
	value, comment = rest, ""
	if i := strings.Index(rest, " //"); i >= 0 {
		value, comment = rest[:i], rest[i:]
	}
	return name, h + " = ", value, comment, true
}

func (d *codDoc) find(key string) int {
	for i, line := range d.lines {
		if name, _, _, _, ok := codLine(line); ok && name == key {
			return i
		}
	}
	return -1
}

// Get reads one setting by its "Name@index".
func (d *codDoc) Get(path ...string) (string, bool) {
	if len(path) != 1 {
		return "", false
	}
	at := d.find(path[0])
	if at < 0 {
		return "", false
	}
	_, _, v, _, _ := codLine(d.lines[at])
	return v, true
}

// Allows says whether the line's own comment accepts value; a line without
// one accepts anything.
func (d *codDoc) Allows(value string, path ...string) bool {
	if len(path) != 1 {
		return false
	}
	at := d.find(path[0])
	if at < 0 {
		return false
	}
	_, _, _, comment, _ := codLine(d.lines[at])
	return codAllows(strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(comment), "//")), value)
}

func codAllows(rule, value string) bool {
	if list, ok := strings.CutPrefix(rule, "one of "); ok {
		for _, o := range strings.Split(list, ",") {
			if strings.TrimSpace(o) == value {
				return true
			}
		}
		return false
	}
	if lo, hi, ok := strings.Cut(rule, " to "); ok {
		l, err1 := strconv.ParseFloat(strings.TrimSpace(lo), 64)
		h, err2 := strconv.ParseFloat(strings.TrimSpace(hi), 64)
		v, err3 := strconv.ParseFloat(value, 64)
		if err1 == nil && err2 == nil {
			return err3 == nil && v >= l && v <= h
		}
	}
	return true
}

// Set changes a setting the file holds; it never adds one.
func (d *codDoc) Set(value string, path ...string) {
	if len(path) != 1 {
		return
	}
	if at := d.find(path[0]); at >= 0 {
		_, head, _, comment, _ := codLine(d.lines[at])
		d.lines[at] = head + value + comment
	}
}

// Delete is never used by the CoD presets: the game expects every line.
func (d *codDoc) Delete(path ...string) bool { return false }
