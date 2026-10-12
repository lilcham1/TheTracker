package core

// Unreal Engine .ini settings files (PUBG's GameUserSettings.ini): sections
// in brackets, key=value lines, some keys repeated. Kept as lines so a file
// read and written unchanged is byte for byte the same, and an edit touches
// only the one line it changes; every other line, whatever it holds, passes
// through untouched.

import "strings"

type iniDoc struct {
	lines []string // without line endings
	eol   string
	// Whether the file ended with a line break.
	trailing bool
}

func iniParse(text string) *iniDoc {
	d := &iniDoc{eol: "\n", trailing: strings.HasSuffix(text, "\n")}
	if strings.Contains(text, "\r\n") {
		d.eol = "\r\n"
	}
	body := strings.TrimSuffix(strings.TrimSuffix(text, "\n"), "\r")
	if !d.trailing {
		body = text
	}
	if body == "" && !d.trailing {
		return d
	}
	d.lines = strings.Split(body, d.eol)
	return d
}

func (d *iniDoc) String() string {
	s := strings.Join(d.lines, d.eol)
	if d.trailing {
		s += d.eol
	}
	return s
}

func iniSection(line string) (string, bool) {
	t := strings.TrimSpace(line)
	if strings.HasPrefix(t, "[") && strings.HasSuffix(t, "]") {
		return t[1 : len(t)-1], true
	}
	return "", false
}

// find returns the index of a key's first line in a section, and the index
// of the section's last non-blank line (where a new key goes); -1 when not
// found.
func (d *iniDoc) find(section, key string) (at, end int) {
	at, end = -1, -1
	in := false
	for i, line := range d.lines {
		if name, ok := iniSection(line); ok {
			if in {
				return at, end
			}
			in = name == section
			if in {
				end = i
			}
			continue
		}
		if !in {
			continue
		}
		if strings.TrimSpace(line) != "" {
			end = i
		}
		if k, _, ok := strings.Cut(line, "="); ok && at < 0 && strings.TrimSpace(k) == key {
			at = i
		}
	}
	return at, end
}

// Get reads section, key.
func (d *iniDoc) Get(path ...string) (string, bool) {
	if len(path) != 2 {
		return "", false
	}
	at, _ := d.find(path[0], path[1])
	if at < 0 {
		return "", false
	}
	_, v, _ := strings.Cut(d.lines[at], "=")
	return v, true
}

// Set writes section, key: in place when the key is there, else at the end
// of its section, else in a new section at the end of the file.
func (d *iniDoc) Set(value string, path ...string) {
	if len(path) != 2 {
		return
	}
	section, key := path[0], path[1]
	at, end := d.find(section, key)
	switch {
	case at >= 0:
		k, _, _ := strings.Cut(d.lines[at], "=")
		d.lines[at] = k + "=" + value
	case end >= 0:
		d.lines = append(d.lines[:end+1], append([]string{key + "=" + value}, d.lines[end+1:]...)...)
	default:
		if n := len(d.lines); n > 0 && strings.TrimSpace(d.lines[n-1]) != "" {
			d.lines = append(d.lines, "")
		}
		d.lines = append(d.lines, "["+section+"]", key+"="+value)
		d.trailing = true
	}
}

// Delete removes a key's first line.
func (d *iniDoc) Delete(path ...string) bool {
	if len(path) != 2 {
		return false
	}
	at, _ := d.find(path[0], path[1])
	if at < 0 {
		return false
	}
	d.lines = append(d.lines[:at], d.lines[at+1:]...)
	return true
}
