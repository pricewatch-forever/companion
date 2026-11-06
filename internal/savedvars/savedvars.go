// Package savedvars reads a SavedVariables file as World of Warcraft writes
// it: `Name = <value>` assignments of tables, strings, numbers, booleans and
// nil, with `--` comments. It parses that subset of Lua and never runs it.
package savedvars

import (
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
)

// Parse reads a SavedVariables file and returns each variable's value by its
// name. A value is nil, a bool, a float64, a string, a []any (a table whose
// keys are exactly 1 to n, and an empty table) or a map[string]any (any other
// table, its keys as strings). A file that isn't entirely in the subset the
// game writes is refused, never half-read.
func Parse(text []byte) (map[string]any, error) {
	p := &parser{src: text, line: 1}
	vars := map[string]any{}
	for {
		p.skipSpace()
		if p.done() {
			return vars, nil
		}
		name, err := p.name()
		if err != nil {
			return nil, err
		}
		p.skipSpace()
		if !p.eat('=') {
			return nil, p.errorf("expected = after %s", name)
		}
		v, err := p.value()
		if err != nil {
			return nil, err
		}
		vars[name] = v
		p.skipSpace()
		p.eat(';')
	}
}

type parser struct {
	src  []byte
	pos  int
	line int
}

func (p *parser) done() bool { return p.pos >= len(p.src) }

func (p *parser) peek() byte {
	if p.done() {
		return 0
	}
	return p.src[p.pos]
}

func (p *parser) eat(c byte) bool {
	if !p.done() && p.src[p.pos] == c {
		p.pos++
		return true
	}
	return false
}

func (p *parser) errorf(format string, args ...any) error {
	return fmt.Errorf("line %d: %s", p.line, fmt.Sprintf(format, args...))
}

// skipSpace skips white space and `--` comments to the end of their line.
func (p *parser) skipSpace() {
	for !p.done() {
		c := p.src[p.pos]
		switch {
		case c == '\n':
			p.line++
			p.pos++
		case c == ' ' || c == '\t' || c == '\r' || c == '\f' || c == '\v':
			p.pos++
		case c == '-' && p.pos+1 < len(p.src) && p.src[p.pos+1] == '-':
			for !p.done() && p.src[p.pos] != '\n' {
				p.pos++
			}
		default:
			return
		}
	}
}

func isNameStart(c byte) bool {
	return c == '_' || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
}

func isNameByte(c byte) bool { return isNameStart(c) || (c >= '0' && c <= '9') }

func (p *parser) name() (string, error) {
	if !isNameStart(p.peek()) {
		return "", p.errorf("expected a variable name")
	}
	start := p.pos
	for !p.done() && isNameByte(p.src[p.pos]) {
		p.pos++
	}
	return string(p.src[start:p.pos]), nil
}

func (p *parser) value() (any, error) {
	p.skipSpace()
	if p.done() {
		return nil, p.errorf("expected a value, found the end of the file")
	}
	c := p.peek()
	switch {
	case c == '"' || c == '\'':
		return p.str()
	case c == '{':
		return p.table()
	case c == '-' || c == '.' || (c >= '0' && c <= '9'):
		return p.number()
	case isNameStart(c):
		word, _ := p.name()
		switch word {
		case "true":
			return true, nil
		case "false":
			return false, nil
		case "nil":
			return nil, nil
		}
		return nil, p.errorf("unexpected %q where a value should be", word)
	}
	return nil, p.errorf("unexpected %q where a value should be", c)
}

func (p *parser) number() (any, error) {
	start := p.pos
	for !p.done() {
		c := p.src[p.pos]
		if (c >= '0' && c <= '9') || c == '.' || c == '-' || c == '+' || c == 'e' || c == 'E' {
			p.pos++
			continue
		}
		break
	}
	lit := string(p.src[start:p.pos])
	f, err := strconv.ParseFloat(lit, 64)
	if err != nil || math.IsInf(f, 0) || math.IsNaN(f) {
		return nil, p.errorf("%q is not a number", lit)
	}
	return f, nil
}

// str reads a quoted string, undoing Lua's escapes. Its bytes are kept as
// they are: the game writes UTF-8.
func (p *parser) str() (string, error) {
	quote := p.src[p.pos]
	startLine := p.line
	unterminated := fmt.Errorf("line %d: unterminated string", startLine)
	p.pos++
	var b strings.Builder
	for {
		if p.done() {
			return "", unterminated
		}
		c := p.src[p.pos]
		switch c {
		case quote:
			p.pos++
			return b.String(), nil
		case '\n':
			return "", unterminated
		case '\\':
			p.pos++
			if p.done() {
				return "", unterminated
			}
			if err := p.escape(&b); err != nil {
				return "", err
			}
		default:
			b.WriteByte(c)
			p.pos++
		}
	}
}

var simpleEscapes = map[byte]byte{
	'\\': '\\', '"': '"', '\'': '\'',
	'n': '\n', 't': '\t', 'r': '\r', 'a': '\a', 'b': '\b', 'f': '\f', 'v': '\v',
}

// escape undoes the escape after a backslash, the cursor on its first byte.
func (p *parser) escape(b *strings.Builder) error {
	e := p.src[p.pos]
	if r, ok := simpleEscapes[e]; ok {
		b.WriteByte(r)
		p.pos++
		return nil
	}
	switch {
	case e == '\n':
		// A backslash before a real line break is a newline.
		b.WriteByte('\n')
		p.line++
		p.pos++
	case e == '\r':
		b.WriteByte('\n')
		p.pos++
		if p.eat('\n') {
			p.line++
		}
	case e >= '0' && e <= '9':
		n := 0
		for i := 0; i < 3 && !p.done() && p.src[p.pos] >= '0' && p.src[p.pos] <= '9'; i++ {
			n = n*10 + int(p.src[p.pos]-'0')
			p.pos++
		}
		if n > 255 {
			return p.errorf("escape \\%d in a string is over 255", n)
		}
		b.WriteByte(byte(n))
	default:
		return p.errorf("unknown escape \\%c in a string", e)
	}
	return nil
}

// A table entry's key is a string, a bool, or a float64 for an entry written
// with a number key or without a key.
type entry struct {
	key   any
	value any
}

func (p *parser) table() (any, error) {
	p.pos++ // {
	var entries []entry
	next := 1.0
	for {
		p.skipSpace()
		if p.done() {
			return nil, p.errorf("unclosed table")
		}
		if p.eat('}') {
			break
		}
		var e entry
		switch {
		case p.eat('['):
			p.skipSpace()
			if p.peek() == '{' {
				return nil, p.errorf("a table used as a key")
			}
			k, err := p.value()
			if err != nil {
				return nil, err
			}
			if k == nil {
				return nil, p.errorf("nil used as a key")
			}
			p.skipSpace()
			if !p.eat(']') {
				return nil, p.errorf("expected ] after a key")
			}
			p.skipSpace()
			if !p.eat('=') {
				return nil, p.errorf("expected = after a key")
			}
			e.key = k
		case isNameStart(p.peek()) && !p.atValueWord():
			name, _ := p.name()
			p.skipSpace()
			if !p.eat('=') {
				return nil, p.errorf("expected = after %s", name)
			}
			e.key = name
		default:
			e.key = next
			next++
		}
		v, err := p.value()
		if err != nil {
			return nil, err
		}
		e.value = v
		entries = append(entries, e)
		p.skipSpace()
		if !p.eat(',') && !p.eat(';') {
			p.skipSpace()
			if !p.eat('}') {
				return nil, p.errorf("expected , or } in a table")
			}
			break
		}
	}
	return build(entries), nil
}

// atValueWord reports whether the name at the cursor is true, false or nil
// written as a value without a key, rather than a key `name = ...`.
func (p *parser) atValueWord() bool {
	end := p.pos
	for end < len(p.src) && isNameByte(p.src[end]) {
		end++
	}
	switch string(p.src[p.pos:end]) {
	case "true", "false", "nil":
		return true
	}
	return false
}

// build makes a list of a table whose keys are exactly 1 to n, and a map of
// any other. A key written twice keeps its last value, as in Lua.
func build(entries []entry) any {
	byKey := map[string]any{}
	numbers := map[float64]any{}
	allNumbers := true
	for _, e := range entries {
		byKey[keyString(e.key)] = e.value
		if f, ok := e.key.(float64); ok {
			numbers[f] = e.value
		} else {
			allNumbers = false
		}
	}
	if allNumbers {
		keys := make([]float64, 0, len(numbers))
		for k := range numbers {
			keys = append(keys, k)
		}
		sort.Float64s(keys)
		list := make([]any, len(keys))
		for i, k := range keys {
			if k != float64(i+1) {
				return byKey
			}
			list[i] = numbers[k]
		}
		return list
	}
	return byKey
}

func keyString(k any) string {
	switch k := k.(type) {
	case string:
		return k
	case float64:
		if k == math.Trunc(k) && math.Abs(k) < 1e15 {
			return strconv.FormatInt(int64(k), 10)
		}
		return strconv.FormatFloat(k, 'g', -1, 64)
	case bool:
		return strconv.FormatBool(k)
	}
	panic(fmt.Sprintf("savedvars: unexpected key %T", k))
}
