package core

// Valve KeyValues text, as the games write their settings files: quoted
// keys, quoted or bare values, nested blocks in braces, tabs for indent.
// Parsed into an ordered tree and written back in the games' own style, so
// a file read and written unchanged is byte for byte the same (the tests
// prove that on the real files before anything is ever edited).
//
// No escape sequences: the games write a lone backslash as "\" and expect
// it back. Comments (// to end of line) are skipped when read.

import (
	"errors"
	"fmt"
	"strings"
)

type kvNode struct {
	Key      string
	Value    string    // for a leaf
	Children []*kvNode // for a block (non-nil, may be empty)
	Block    bool
}

type kvDoc struct {
	Roots []*kvNode
	// "\r\n" (what the games write on Windows) or "\n".
	EOL string
	// Whether the file ended with a line break.
	TrailingEOL bool
}

type kvToken struct {
	text   string
	quoted bool
	brace  byte // '{', '}' or 0
}

func kvTokens(text string) ([]kvToken, error) {
	var out []kvToken
	i := 0
	for i < len(text) {
		c := text[i]
		switch {
		case c == ' ' || c == '\t' || c == '\r' || c == '\n':
			i++
		case c == '/' && i+1 < len(text) && text[i+1] == '/':
			for i < len(text) && text[i] != '\n' {
				i++
			}
		case c == '{' || c == '}':
			out = append(out, kvToken{brace: c})
			i++
		case c == '"':
			end := strings.IndexByte(text[i+1:], '"')
			if end < 0 {
				return nil, errors.New("unterminated quote")
			}
			out = append(out, kvToken{text: text[i+1 : i+1+end], quoted: true})
			i += end + 2
		default:
			start := i
			for i < len(text) && !strings.ContainsRune(" \t\r\n{}\"", rune(text[i])) {
				i++
			}
			out = append(out, kvToken{text: text[start:i]})
		}
	}
	return out, nil
}

func kvParse(text string) (*kvDoc, error) {
	doc := &kvDoc{EOL: "\n", TrailingEOL: strings.HasSuffix(text, "\n")}
	if strings.Contains(text, "\r\n") {
		doc.EOL = "\r\n"
	}
	toks, err := kvTokens(text)
	if err != nil {
		return nil, err
	}
	pos := 0
	var parseBlock func(depth int) ([]*kvNode, error)
	parseBlock = func(depth int) ([]*kvNode, error) {
		nodes := []*kvNode{}
		for pos < len(toks) {
			t := toks[pos]
			if t.brace == '}' {
				if depth == 0 {
					return nil, errors.New("unexpected }")
				}
				pos++
				return nodes, nil
			}
			if t.brace == '{' {
				return nil, errors.New("block without a key")
			}
			pos++
			if pos >= len(toks) {
				return nil, fmt.Errorf("key %q without a value", t.text)
			}
			next := toks[pos]
			if next.brace == '{' {
				pos++
				children, err := parseBlock(depth + 1)
				if err != nil {
					return nil, err
				}
				nodes = append(nodes, &kvNode{Key: t.text, Children: children, Block: true})
				continue
			}
			if next.brace == '}' {
				return nil, fmt.Errorf("key %q without a value", t.text)
			}
			pos++
			nodes = append(nodes, &kvNode{Key: t.text, Value: next.text})
		}
		if depth != 0 {
			return nil, errors.New("missing }")
		}
		return nodes, nil
	}
	roots, err := parseBlock(0)
	if err != nil {
		return nil, err
	}
	doc.Roots = roots
	return doc, nil
}

// String writes the document the way the games do.
func (d *kvDoc) String() string {
	var b strings.Builder
	var write func(nodes []*kvNode, depth int)
	write = func(nodes []*kvNode, depth int) {
		indent := strings.Repeat("\t", depth)
		for _, n := range nodes {
			if n.Block {
				b.WriteString(indent + `"` + n.Key + `"` + d.EOL)
				b.WriteString(indent + "{" + d.EOL)
				write(n.Children, depth+1)
				b.WriteString(indent + "}" + d.EOL)
				continue
			}
			b.WriteString(indent + `"` + n.Key + `"` + "\t\t" + `"` + n.Value + `"` + d.EOL)
		}
	}
	write(d.Roots, 0)
	s := b.String()
	if !d.TrailingEOL {
		s = strings.TrimSuffix(s, d.EOL)
	}
	return s
}

func kvFind(nodes []*kvNode, key string) *kvNode {
	for _, n := range nodes {
		if n.Key == key {
			return n
		}
	}
	for _, n := range nodes {
		if strings.EqualFold(n.Key, key) {
			return n
		}
	}
	return nil
}

// node walks a path of keys; nil when any step is missing.
func (d *kvDoc) node(path ...string) *kvNode {
	nodes := d.Roots
	var cur *kvNode
	for _, key := range path {
		cur = kvFind(nodes, key)
		if cur == nil {
			return nil
		}
		nodes = cur.Children
	}
	return cur
}

// Get returns a leaf's value.
func (d *kvDoc) Get(path ...string) (string, bool) {
	n := d.node(path...)
	if n == nil || n.Block {
		return "", false
	}
	return n.Value, true
}

// Block returns a block's children (nil when missing).
func (d *kvDoc) Block(path ...string) []*kvNode {
	n := d.node(path...)
	if n == nil || !n.Block {
		return nil
	}
	return n.Children
}

// Set writes a leaf, creating the blocks on the way; a new leaf goes at
// the end of its block, the way the games append new settings.
func (d *kvDoc) Set(value string, path ...string) {
	if len(path) == 0 {
		return
	}
	nodes := &d.Roots
	for _, key := range path[:len(path)-1] {
		n := kvFind(*nodes, key)
		if n == nil {
			n = &kvNode{Key: key, Children: []*kvNode{}, Block: true}
			*nodes = append(*nodes, n)
		} else if !n.Block {
			n.Block, n.Value, n.Children = true, "", []*kvNode{}
		}
		nodes = &n.Children
	}
	last := path[len(path)-1]
	if n := kvFind(*nodes, last); n != nil && !n.Block {
		n.Value = value
		return
	}
	*nodes = append(*nodes, &kvNode{Key: last, Value: value})
}

// Delete removes a leaf or block; false when it wasn't there.
func (d *kvDoc) Delete(path ...string) bool {
	if len(path) == 0 {
		return false
	}
	nodes := &d.Roots
	for _, key := range path[:len(path)-1] {
		n := kvFind(*nodes, key)
		if n == nil || !n.Block {
			return false
		}
		nodes = &n.Children
	}
	for i, n := range *nodes {
		if n.Key == path[len(path)-1] {
			*nodes = append((*nodes)[:i], (*nodes)[i+1:]...)
			return true
		}
	}
	return false
}
