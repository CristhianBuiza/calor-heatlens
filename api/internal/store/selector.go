package store

import (
	"errors"
	"strconv"
	"strings"
	"unicode/utf8"
)

// Selector is a parsed selector in the CSS subset Calor supports for
// component tagging: type and universal selectors, #id, .class, [attr],
// [attr=value], :nth-of-type(n), descendant and child combinators, and
// comma-separated lists. Browsers evaluate the same strings with
// Element.closest(), so anything outside the subset is rejected up front
// rather than matched differently on the two sides.
type Selector struct {
	alts []complexSel
}

type complexSel struct {
	parts []compound
	combs []byte // combs[i] joins parts[i] and parts[i+1]: ' ' or '>'
}

type compound struct {
	tag     string // "" or "*" = any
	id      string
	classes []string
	nth     int // :nth-of-type(n); 0 = none
	attrs   []attrCond
}

type attrCond struct {
	name, value string
	hasValue    bool
}

const (
	MaxSelectorLen = 300
	maxAlts        = 10
	maxCompounds   = 8
)

var errEmpty = errors.New("selector is empty")

// ParseSelector validates and parses s.
func ParseSelector(s string) (*Selector, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil, errEmpty
	}
	if len(s) > MaxSelectorLen {
		return nil, errors.New("selector is longer than " + strconv.Itoa(MaxSelectorLen) + " characters")
	}
	p := &selParser{s: s}
	sel := &Selector{}
	for {
		cs, err := p.complex()
		if err != nil {
			return nil, err
		}
		sel.alts = append(sel.alts, cs)
		if len(sel.alts) > maxAlts {
			return nil, errors.New("too many comma-separated selectors (max " + strconv.Itoa(maxAlts) + ")")
		}
		p.spaces()
		if p.eof() {
			return sel, nil
		}
		if p.peek() != ',' {
			return nil, p.errAt("unexpected character")
		}
		p.i++
	}
}

type selParser struct {
	s string
	i int
}

func (p *selParser) eof() bool  { return p.i >= len(p.s) }
func (p *selParser) peek() byte { return p.s[p.i] }

func (p *selParser) spaces() bool {
	start := p.i
	for !p.eof() && isSpace(p.peek()) {
		p.i++
	}
	return p.i > start
}

func isSpace(c byte) bool { return c == ' ' || c == '\t' || c == '\n' || c == '\r' || c == '\f' }

func (p *selParser) errAt(msg string) error {
	if p.eof() {
		return errors.New(msg + " at end of selector")
	}
	r, _ := utf8.DecodeRuneInString(p.s[p.i:])
	switch r {
	case '+', '~':
		return errors.New("sibling combinators (+, ~) are not supported; use descendant (space) or child (>)")
	case ':':
		return errors.New("only the :nth-of-type(n) pseudo-class is supported")
	case '|':
		return errors.New("namespaces are not supported")
	}
	return errors.New(msg + " '" + string(r) + "' at position " + strconv.Itoa(p.i+1))
}

func (p *selParser) complex() (complexSel, error) {
	var cs complexSel
	p.spaces()
	for {
		c, err := p.compound()
		if err != nil {
			return cs, err
		}
		cs.parts = append(cs.parts, c)
		if len(cs.parts) > maxCompounds {
			return cs, errors.New("selector is too deep (max " + strconv.Itoa(maxCompounds) + " parts)")
		}
		sp := p.spaces()
		if p.eof() || p.peek() == ',' {
			return cs, nil
		}
		comb := byte(' ')
		if p.peek() == '>' {
			comb = '>'
			p.i++
			p.spaces()
		} else if !sp {
			return cs, p.errAt("unexpected character")
		}
		cs.combs = append(cs.combs, comb)
	}
}

func (p *selParser) compound() (compound, error) {
	var c compound
	any := false
	if !p.eof() && p.peek() == '*' {
		c.tag = "*"
		p.i++
		any = true
	} else if !p.eof() && isIdentStart(p.s[p.i:]) {
		c.tag = strings.ToLower(p.name())
		any = true
	}
	for !p.eof() {
		switch p.peek() {
		case '#':
			p.i++
			if p.eof() || !isIdentStart(p.s[p.i:]) && !isDigitStart(p.s[p.i:]) {
				return c, p.errAt("expected an id after '#'")
			}
			if c.id != "" {
				return c, errors.New("a selector part can only have one #id")
			}
			c.id = p.name()
		case '.':
			p.i++
			if p.eof() || !isIdentStart(p.s[p.i:]) {
				return c, p.errAt("expected a class name after '.'")
			}
			c.classes = append(c.classes, p.name())
		case '[':
			a, err := p.attr()
			if err != nil {
				return c, err
			}
			c.attrs = append(c.attrs, a)
		case ':':
			n, err := p.pseudo()
			if err != nil {
				return c, err
			}
			c.nth = n
		default:
			if !any {
				return c, p.errAt("unexpected character")
			}
			return c, nil
		}
		any = true
	}
	if !any {
		return c, p.errAt("expected a selector")
	}
	return c, nil
}

func (p *selParser) pseudo() (int, error) {
	const pre = ":nth-of-type("
	if !strings.HasPrefix(strings.ToLower(p.s[p.i:]), pre) {
		return 0, p.errAt("unsupported pseudo-class")
	}
	p.i += len(pre)
	p.spaces()
	start := p.i
	for !p.eof() && p.peek() >= '0' && p.peek() <= '9' {
		p.i++
	}
	n, err := strconv.Atoi(p.s[start:p.i])
	p.spaces()
	if err != nil || n < 1 || n > 10000 || p.eof() || p.peek() != ')' {
		return 0, errors.New(":nth-of-type() takes a positive whole number, e.g. :nth-of-type(2)")
	}
	p.i++
	return n, nil
}

func (p *selParser) attr() (attrCond, error) {
	p.i++ // [
	p.spaces()
	if p.eof() || !isIdentStart(p.s[p.i:]) {
		return attrCond{}, p.errAt("expected an attribute name")
	}
	a := attrCond{name: strings.ToLower(p.name())}
	p.spaces()
	if p.eof() {
		return a, errors.New("unclosed '['")
	}
	if p.peek() == ']' {
		p.i++
		return a, nil
	}
	if p.peek() != '=' {
		return a, errors.New("only [attr] and [attr=value] attribute selectors are supported")
	}
	p.i++
	p.spaces()
	if p.eof() {
		return a, errors.New("unclosed '['")
	}
	a.hasValue = true
	if q := p.peek(); q == '"' || q == '\'' {
		p.i++
		var b strings.Builder
		for {
			if p.eof() {
				return a, errors.New("unclosed string in attribute selector")
			}
			c := p.peek()
			if c == q {
				p.i++
				break
			}
			if c == '\\' {
				b.WriteString(p.escape())
				continue
			}
			b.WriteByte(c)
			p.i++
		}
		a.value = b.String()
	} else if isIdentStart(p.s[p.i:]) {
		a.value = p.name()
	} else {
		return a, errors.New("attribute values must be quoted unless they are plain identifiers")
	}
	p.spaces()
	if p.eof() || p.peek() != ']' {
		if !p.eof() && (p.peek() == 'i' || p.peek() == 's') {
			return a, errors.New("attribute case flags are not supported")
		}
		return a, errors.New("expected ']'")
	}
	p.i++
	return a, nil
}

func isIdentStart(s string) bool {
	if s == "" {
		return false
	}
	c := s[0]
	switch {
	case c == '-':
		return len(s) > 1 && (isIdentStart(s[1:]) || s[1] == '-')
	case c == '\\':
		return len(s) > 1 && s[1] != '\n'
	}
	return c == '_' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= 0x80
}

func isDigitStart(s string) bool { return s != "" && s[0] >= '0' && s[0] <= '9' }

func isNameChar(c byte) bool {
	return c == '_' || c == '-' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c >= 0x80
}

// name reads an identifier, decoding CSS escapes (\: or \31 ).
func (p *selParser) name() string {
	var b strings.Builder
	for !p.eof() {
		c := p.peek()
		if c == '\\' {
			b.WriteString(p.escape())
			continue
		}
		if !isNameChar(c) {
			break
		}
		b.WriteByte(c)
		p.i++
	}
	return b.String()
}

func (p *selParser) escape() string {
	p.i++ // backslash
	if p.eof() {
		return ""
	}
	start := p.i
	for p.i < len(p.s) && p.i-start < 6 && isHex(p.s[p.i]) {
		p.i++
	}
	if p.i > start {
		v, _ := strconv.ParseUint(p.s[start:p.i], 16, 32)
		if !p.eof() && isSpace(p.peek()) {
			p.i++
		}
		if v == 0 || v > utf8.MaxRune || v >= 0xD800 && v <= 0xDFFF {
			return "\uFFFD"
		}
		return string(rune(v))
	}
	r, n := utf8.DecodeRuneInString(p.s[p.i:])
	p.i += n
	return string(r)
}

func isHex(c byte) bool { return c >= '0' && c <= '9' || c >= 'a' && c <= 'f' || c >= 'A' && c <= 'F' }

// ---- matching against recorded clicks ----

// Click is what the server knows about a recorded click: the tracker's CSS
// path (at most five levels, two classes per ancestor, stopping at the first
// ancestor with an id or at <body>), the target's id and first six classes,
// and the value of the closest data-calor-component attribute.
type Click struct {
	PageURL   string
	Selector  string
	Tag       string
	ID        string
	Classes   string
	Component string
}

type elem struct {
	tag     string
	id      string
	classes []string
	nth     int // 0 = unknown
}

// chain rebuilds the recorded ancestry, outermost first. rooted reports that
// the path reaches <body>, so nothing above it is unknown.
func (c *Click) chain() (els []elem, rooted bool) {
	if c.Selector != "" {
		if s, err := ParseSelector(c.Selector); err == nil && len(s.alts) == 1 {
			cs := s.alts[0]
			ok := true
			for _, comb := range cs.combs {
				if comb != '>' {
					ok = false
				}
			}
			if ok {
				for i, p := range cs.parts {
					e := elem{tag: p.tag, id: p.id, classes: p.classes, nth: p.nth}
					// The tracker only writes :nth-of-type when an element has
					// same-tag siblings and never for elements with an id.
					if e.nth == 0 && e.id == "" {
						e.nth = 1
					}
					els = append(els, e)
					if i == 0 && (p.tag == "body" || p.tag == "html") {
						rooted = true
					}
				}
			}
		}
	}
	if len(els) == 0 {
		els = []elem{{tag: strings.ToLower(c.Tag)}}
	}
	t := &els[len(els)-1]
	if c.ID != "" {
		t.id = c.ID
	}
	if c.Classes != "" {
		t.classes = strings.Fields(c.Classes)
	}
	if rooted && els[0].tag == "body" {
		els = append([]elem{{tag: "html", nth: 1}}, els...)
	}
	return els, rooted
}

func (c compound) matches(e elem) bool {
	if len(c.attrs) > 0 {
		return false
	}
	if c.tag != "" && c.tag != "*" && c.tag != e.tag {
		return false
	}
	if c.id != "" && c.id != e.id {
		return false
	}
	if c.nth != 0 && c.nth != e.nth {
		return false
	}
	for _, want := range c.classes {
		found := false
		for _, have := range e.classes {
			if have == want {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

func (cs complexSel) matchAt(k int, els []elem, i int) bool {
	if !cs.parts[k].matches(els[i]) {
		return false
	}
	if k == 0 {
		return true
	}
	if cs.combs[k-1] == '>' {
		return i > 0 && cs.matchAt(k-1, els, i-1)
	}
	for j := i - 1; j >= 0; j-- {
		if cs.matchAt(k-1, els, j) {
			return true
		}
	}
	return false
}

// componentAttr returns the value of a lone [data-calor-component=value]
// selector, which the server can check against the recorded closest value.
func (cs complexSel) componentAttr() (attrCond, bool) {
	if len(cs.parts) != 1 {
		return attrCond{}, false
	}
	p := cs.parts[0]
	if (p.tag != "" && p.tag != "*") || p.id != "" || len(p.classes) > 0 || p.nth != 0 || len(p.attrs) != 1 {
		return attrCond{}, false
	}
	a := p.attrs[0]
	return a, a.name == DefaultComponentAttr
}

func (cs complexSel) serverEvaluable() bool {
	if _, ok := cs.componentAttr(); ok {
		return true
	}
	for _, p := range cs.parts {
		if len(p.attrs) > 0 {
			return false
		}
	}
	return true
}

// ServerEvaluable reports whether recorded clicks carry enough information to
// evaluate every alternative of s. Selectors on arbitrary attributes can only
// be matched by the tracker in the browser.
func (s *Selector) ServerEvaluable() bool {
	for _, cs := range s.alts {
		if !cs.serverEvaluable() {
			return false
		}
	}
	return true
}

// MatchClick reports whether the clicked element or one of its recorded
// ancestors matches s, i.e. what Element.closest(s) would find. Ancestors
// the tracker did not record are treated as non-matching.
func (s *Selector) MatchClick(c *Click) bool {
	els, _ := c.chain()
	return s.matchChain(c, els)
}

func (s *Selector) matchChain(c *Click, els []elem) bool {
	for _, cs := range s.alts {
		if a, ok := cs.componentAttr(); ok {
			if c.Component != "" && (!a.hasValue || a.value == c.Component) {
				return true
			}
			continue
		}
		k := len(cs.parts) - 1
		for i := len(els) - 1; i >= 0; i-- {
			if cs.matchAt(k, els, i) {
				return true
			}
		}
	}
	return false
}

// ---- page patterns ----

// MatchPagePattern matches the path of pageURL against a glob where '*'
// matches any run of characters (including '/'). An empty pattern matches
// every page.
func MatchPagePattern(pattern, pageURL string) bool {
	if pattern == "" {
		return true
	}
	return glob(pattern, pathOf(pageURL))
}

func glob(pat, s string) bool {
	px, sx := 0, 0
	nextPx, nextSx := -1, -1
	for px < len(pat) || sx < len(s) {
		if px < len(pat) {
			if pat[px] == '*' {
				nextPx, nextSx = px, sx+1
				px++
				continue
			}
			if sx < len(s) && pat[px] == s[sx] {
				px++
				sx++
				continue
			}
		}
		if nextPx >= 0 && nextSx <= len(s) {
			px, sx = nextPx, nextSx
			continue
		}
		return false
	}
	return true
}

// ValidatePagePattern checks a page path glob such as "/products/*".
func ValidatePagePattern(p string) error {
	if p == "" {
		return nil
	}
	if !strings.HasPrefix(p, "/") && !strings.HasPrefix(p, "*") {
		return errors.New("page pattern must start with '/' (e.g. /pricing or /blog/*)")
	}
	if len(p) > 200 {
		return errors.New("page pattern is longer than 200 characters")
	}
	if strings.ContainsAny(p, " \t\n?#") {
		return errors.New("page pattern must be a path without spaces, query string or fragment")
	}
	return nil
}
