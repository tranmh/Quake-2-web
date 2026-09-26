// Command genconst parses Quake 2 C headers from the oracle tree and emits the
// shared protocol/game constants for Go and TypeScript, so both ports agree on
// every magic number. Run via `make gen` from the repository root.
package main

import (
	"bufio"
	"bytes"
	"flag"
	"fmt"
	"go/format"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

type constant struct {
	name    string
	isFloat bool
	isStr   bool
	i       int64
	f       float64
	s       string
	src     string
}

var (
	defineRe = regexp.MustCompile(`^\s*#define\s+([A-Za-z_][A-Za-z0-9_]*)\s+(.*)$`)
	skip     = map[string]bool{
		// platform / compiler macros that are not protocol constants
		"CPUSTRING": true, "BUILDSTRING": true, "idaxp": true, "id386": true,
		"GAME_INCLUDE": true, "NULL": true, "M_PI": true, "nanmask": true,
	}
)

type gen struct {
	consts []*constant
	byName map[string]*constant
}

func (g *gen) add(c *constant) {
	if skip[c.name] {
		return
	}
	if old, ok := g.byName[c.name]; ok {
		if old.i != c.i || old.f != c.f || old.s != c.s {
			fmt.Fprintf(os.Stderr, "genconst: %s redefined (%s vs %s), keeping first\n", c.name, old.src, c.src)
		}
		return
	}
	g.byName[c.name] = c
	g.consts = append(g.consts, c)
}

func stripComments(s string) string {
	if i := strings.Index(s, "//"); i >= 0 {
		s = s[:i]
	}
	for {
		i := strings.Index(s, "/*")
		if i < 0 {
			break
		}
		j := strings.Index(s[i:], "*/")
		if j < 0 {
			s = s[:i]
			break
		}
		s = s[:i] + " " + s[i+j+2:]
	}
	return strings.TrimSpace(s)
}

// --- tiny integer/float expression evaluator ---

type parser struct {
	toks []string
	pos  int
	g    *gen
	flt  bool
}

var tokRe = regexp.MustCompile(`\s*('.'|0[xX][0-9a-fA-F]+|[0-9]+\.[0-9]*[fF]?|\.[0-9]+[fF]?|[0-9]+[uUlL]*|[A-Za-z_][A-Za-z0-9_]*|<<|>>|[-+*/|&~()^])`)

func tokenize(s string) ([]string, bool) {
	var out []string
	for len(strings.TrimSpace(s)) > 0 {
		m := tokRe.FindStringSubmatchIndex(s)
		if m == nil || m[0] != 0 {
			return nil, false
		}
		out = append(out, s[m[2]:m[3]])
		s = s[m[1]:]
	}
	return out, true
}

func (p *parser) peek() string {
	if p.pos < len(p.toks) {
		return p.toks[p.pos]
	}
	return ""
}
func (p *parser) next() string { t := p.peek(); p.pos++; return t }

func (p *parser) expr() (float64, error) { return p.or() }
func (p *parser) or() (float64, error) {
	v, err := p.xor()
	for err == nil && p.peek() == "|" {
		p.next()
		var r float64
		r, err = p.xor()
		v = float64(int64(v) | int64(r))
	}
	return v, err
}
func (p *parser) xor() (float64, error) {
	v, err := p.and()
	for err == nil && p.peek() == "^" {
		p.next()
		var r float64
		r, err = p.and()
		v = float64(int64(v) ^ int64(r))
	}
	return v, err
}
func (p *parser) and() (float64, error) {
	v, err := p.shift()
	for err == nil && p.peek() == "&" {
		p.next()
		var r float64
		r, err = p.shift()
		v = float64(int64(v) & int64(r))
	}
	return v, err
}
func (p *parser) shift() (float64, error) {
	v, err := p.add()
	for err == nil && (p.peek() == "<<" || p.peek() == ">>") {
		op := p.next()
		var r float64
		r, err = p.add()
		if op == "<<" {
			v = float64(int64(v) << uint(r))
		} else {
			v = float64(int64(v) >> uint(r))
		}
	}
	return v, err
}
func (p *parser) add() (float64, error) {
	v, err := p.mul()
	for err == nil && (p.peek() == "+" || p.peek() == "-") {
		op := p.next()
		var r float64
		r, err = p.mul()
		if op == "+" {
			v += r
		} else {
			v -= r
		}
	}
	return v, err
}
func (p *parser) mul() (float64, error) {
	v, err := p.unary()
	for err == nil && (p.peek() == "*" || p.peek() == "/") {
		op := p.next()
		var r float64
		r, err = p.unary()
		if op == "*" {
			v *= r
		} else if p.flt {
			v /= r
		} else {
			v = float64(int64(v) / int64(r))
		}
	}
	return v, err
}
func (p *parser) unary() (float64, error) {
	switch p.peek() {
	case "-":
		p.next()
		v, err := p.unary()
		return -v, err
	case "~":
		p.next()
		v, err := p.unary()
		return float64(^int64(v)), err
	case "(":
		p.next()
		v, err := p.expr()
		if err != nil {
			return 0, err
		}
		if p.next() != ")" {
			return 0, fmt.Errorf("missing )")
		}
		return v, nil
	}
	t := p.next()
	if t == "" {
		return 0, fmt.Errorf("unexpected end")
	}
	if len(t) == 3 && t[0] == '\'' {
		return float64(t[1]), nil
	}
	if c, ok := p.g.byName[t]; ok && !c.isStr {
		if c.isFloat {
			p.flt = true
			return c.f, nil
		}
		return float64(c.i), nil
	}
	if strings.HasPrefix(t, "0x") || strings.HasPrefix(t, "0X") {
		u, err := strconv.ParseUint(t[2:], 16, 64)
		return float64(u), err
	}
	if strings.ContainsAny(t, ".") {
		p.flt = true
		f, err := strconv.ParseFloat(strings.TrimRight(t, "fF"), 64)
		return f, err
	}
	u, err := strconv.ParseInt(strings.TrimRight(t, "uUlL"), 10, 64)
	return float64(u), err
}

func (g *gen) eval(name, expr, src string) (*constant, bool) {
	expr = strings.TrimSpace(expr)
	if expr == "" {
		return nil, false
	}
	if strings.HasPrefix(expr, `"`) && strings.HasSuffix(expr, `"`) && strings.Count(expr, `"`) == 2 {
		return &constant{name: name, isStr: true, s: expr[1 : len(expr)-1], src: src}, true
	}
	toks, ok := tokenize(expr)
	if !ok {
		return nil, false
	}
	// reject unknown identifiers (macros, types, casts)
	for _, t := range toks {
		if t[0] == '\'' {
			continue
		}
		if (t[0] == '_' || (t[0] >= 'A' && t[0] <= 'Z') || (t[0] >= 'a' && t[0] <= 'z')) && !strings.HasPrefix(t, "0x") {
			if _, known := g.byName[t]; !known {
				return nil, false
			}
		}
	}
	p := &parser{toks: toks, g: g}
	v, err := p.expr()
	if err != nil || p.pos != len(toks) {
		return nil, false
	}
	if p.flt {
		return &constant{name: name, isFloat: true, f: v, src: src}, true
	}
	return &constant{name: name, i: int64(v), src: src}, true
}

// parseHeader extracts #defines and enum bodies (in order) from a C header.
func (g *gen) parseHeader(path, rel string, only *regexp.Regexp) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	text := strings.ReplaceAll(strings.ReplaceAll(string(data), "\r\n", "\n"), "\r", "\n")
	lines := strings.Split(text, "\n")
	inEnum := false
	enumVal := int64(0)
	var enumBuf strings.Builder
	flushEnum := func(lineNo int) {
		body := strings.TrimLeft(strings.TrimSpace(enumBuf.String()), "{")
		enumBuf.Reset()
		for _, item := range strings.Split(body, ",") {
			item = strings.TrimSpace(item)
			if item == "" {
				continue
			}
			name, val := item, ""
			if i := strings.Index(item, "="); i >= 0 {
				name, val = strings.TrimSpace(item[:i]), strings.TrimSpace(item[i+1:])
			}
			if val != "" {
				c, ok := g.eval(name, val, rel)
				if !ok {
					fmt.Fprintf(os.Stderr, "genconst: cannot eval enum %s = %s\n", name, val)
					continue
				}
				enumVal = c.i
			}
			if only == nil || only.MatchString(name) {
				g.add(&constant{name: name, i: enumVal, src: fmt.Sprintf("%s:%d", rel, lineNo)})
			} else {
				// still record for later expression references
				g.byName[name] = &constant{name: name, i: enumVal}
			}
			enumVal++
		}
	}
	for n, raw := range lines {
		line := stripComments(raw)
		if inEnum {
			if i := strings.Index(line, "}"); i >= 0 {
				enumBuf.WriteString(line[:i])
				flushEnum(n + 1)
				inEnum = false
				continue
			}
			if strings.HasPrefix(line, "#") {
				continue
			}
			enumBuf.WriteString(line + "\n")
			continue
		}
		if m := defineRe.FindStringSubmatch(line); m != nil {
			name := m[1]
			if only != nil && !only.MatchString(name) {
				continue
			}
			if c, ok := g.eval(name, m[2], fmt.Sprintf("%s:%d", rel, n+1)); ok {
				g.add(c)
			}
			continue
		}
		if strings.HasPrefix(line, "typedef enum") || strings.HasPrefix(line, "enum") {
			inEnum = true
			enumVal = 0
			if i := strings.Index(line, "{"); i >= 0 {
				rest := line[i+1:]
				if j := strings.Index(rest, "}"); j >= 0 {
					enumBuf.WriteString(rest[:j])
					flushEnum(n + 1)
					inEnum = false
				} else {
					enumBuf.WriteString(rest + "\n")
				}
			}
		}
	}
	return nil
}

var floatTripleRe = regexp.MustCompile(`(-?[0-9]*\.?[0-9]+(?:[eE][-+]?[0-9]+)?)`)

// parseFloatTable reads all float literals inside the first { ... } block after marker.
func parseFloatTable(path, marker string) ([]float64, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	s := string(data)
	if marker != "" {
		i := strings.Index(s, marker)
		if i < 0 {
			return nil, fmt.Errorf("%s: marker %q not found", path, marker)
		}
		s = s[i:]
	}
	var sb strings.Builder
	for _, l := range strings.Split(s, "\n") {
		sb.WriteString(stripComments(l) + "\n")
	}
	s = sb.String()
	i := strings.Index(s, "{")
	if strings.HasPrefix(marker, "{") {
		i = -1 // the marker is the first element; there is no enclosing brace
	}
	j := strings.Index(s[i+1:], "};")
	if j < 0 {
		j = len(s) - i - 1
	}
	body := s[i+1 : i+1+j]
	// Elements are separated by commas; an element may be a constant
	// product (m_flash.c: "21.1 * 1.2"), folded in double like the C compiler.
	var out []float64
	body = strings.NewReplacer("{", ",", "}", ",").Replace(body)
	for _, elem := range strings.Split(body, ",") {
		if strings.TrimSpace(elem) == "" {
			continue
		}
		v := 1.0
		for _, factor := range strings.Split(elem, "*") {
			m := floatTripleRe.FindString(factor)
			if m == "" || strings.TrimSpace(factor) != m {
				return nil, fmt.Errorf("%s: cannot parse table element %q", path, elem)
			}
			f, err := strconv.ParseFloat(m, 64)
			if err != nil {
				return nil, err
			}
			v *= f
		}
		out = append(out, v)
	}
	return out, nil
}

func fmtFloat32(f float64) string {
	return strconv.FormatFloat(float64(float32(f)), 'g', -1, 32)
}

func main() {
	oracle := flag.String("oracle", "../Quake-2", "path to the Quake-2 source tree")
	goOut := flag.String("go", "internal/q2const/const_gen.go", "Go output file")
	tsOut := flag.String("ts", "", "TypeScript output file")
	flag.Parse()

	g := &gen{byName: map[string]*constant{}}
	headers := []struct {
		rel  string
		only *regexp.Regexp
	}{
		{"game/q_shared.h", nil},
		{"qcommon/qcommon.h", nil},
		{"qcommon/qfiles.h", nil},
		{"game/game.h", nil},
		{"client/keys.h", nil},
		{"client/ref.h", regexp.MustCompile(`^(MAX_DLIGHTS|MAX_ENTITIES|MAX_PARTICLES|POWERSUIT_SCALE|SHELL_.*|ENTITY_FLAGS|API_VERSION)$`)},
		{"client/client.h", regexp.MustCompile(`^(CMD_BACKUP|MAX_PARSE_ENTITIES|MAX_CLIENTWEAPONMODELS)$`)},
		{"server/server.h", regexp.MustCompile(`^(MAX_MASTERS|LATENCY_COUNTS|RATE_MESSAGES)$`)},
	}
	for _, h := range headers {
		if err := g.parseHeader(filepath.Join(*oracle, h.rel), h.rel, h.only); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
	}
	// qboolean false/true are not useful constants
	var consts []*constant
	for _, c := range g.consts {
		if c.name == "false" || c.name == "true" {
			continue
		}
		consts = append(consts, c)
	}

	anorms, err := parseFloatTable(filepath.Join(*oracle, "client/anorms.h"), "{-0.525731")
	if err != nil || len(anorms) != 162*3 {
		fmt.Fprintf(os.Stderr, "genconst: anorms parse failed (%v, %d values)\n", err, len(anorms))
		os.Exit(1)
	}
	flash, err := parseFloatTable(filepath.Join(*oracle, "game/m_flash.c"), "monster_flash_offset")
	if err != nil || len(flash)%3 != 0 {
		fmt.Fprintf(os.Stderr, "genconst: m_flash parse failed (%v, %d values)\n", err, len(flash))
		os.Exit(1)
	}

	if *goOut != "" {
		writeGo(*goOut, consts, anorms, flash)
	}
	if *tsOut != "" {
		writeTS(*tsOut, consts, anorms, flash)
	}
}

func header(w *bufio.Writer, comment string) {
	fmt.Fprintf(w, "%s Code generated by genconst from the Quake-2 oracle headers. DO NOT EDIT.\n\n", comment)
}

// goName exports lower-case C enum names (svc_nop -> Svc_nop).
func goName(n string) string {
	if n[0] >= 'a' && n[0] <= 'z' {
		return strings.ToUpper(n[:1]) + n[1:]
	}
	return n
}

func writeGo(path string, consts []*constant, anorms, flash []float64) {
	os.MkdirAll(filepath.Dir(path), 0o755)
	var buf bytes.Buffer
	w := bufio.NewWriter(&buf)
	defer func() {
		w.Flush()
		src, err := format.Source(buf.Bytes())
		if err != nil {
			panic(err)
		}
		if err := os.WriteFile(path, src, 0o644); err != nil {
			panic(err)
		}
	}()
	header(w, "//")
	fmt.Fprintf(w, "// Package q2const holds constants shared by the Quake 2 engine, game and client.\npackage q2const\n\nconst (\n")
	for _, c := range consts {
		switch {
		case c.isStr:
			fmt.Fprintf(w, "\t%s = %q // %s\n", goName(c.name), c.s, c.src)
		case c.isFloat:
			fmt.Fprintf(w, "\t%s = %s // %s\n", goName(c.name), strconv.FormatFloat(c.f, 'g', -1, 64), c.src)
		default:
			fmt.Fprintf(w, "\t%s = %d // %s\n", goName(c.name), c.i, c.src)
		}
	}
	fmt.Fprintf(w, ")\n\n// ByteDirs is the 162-entry normal table (client/anorms.h), used by MSG_WriteDir/ReadDir and MD2 normals.\nvar ByteDirs = [%d][3]float32{\n", len(anorms)/3)
	for i := 0; i < len(anorms); i += 3 {
		fmt.Fprintf(w, "\t{%s, %s, %s},\n", fmtFloat32(anorms[i]), fmtFloat32(anorms[i+1]), fmtFloat32(anorms[i+2]))
	}
	fmt.Fprintf(w, "}\n\n// MonsterFlashOffset is game/m_flash.c monster_flash_offset, indexed by MZ2_*.\nvar MonsterFlashOffset = [%d][3]float32{\n", len(flash)/3)
	for i := 0; i < len(flash); i += 3 {
		fmt.Fprintf(w, "\t{%s, %s, %s},\n", fmtFloat32(flash[i]), fmtFloat32(flash[i+1]), fmtFloat32(flash[i+2]))
	}
	fmt.Fprintln(w, "}")
}

func tsNum(c *constant) string {
	if c.isFloat {
		return strconv.FormatFloat(c.f, 'g', -1, 64)
	}
	v := c.i
	// JS bit operations are int32: present values that fit uint32 but not int32 as signed.
	if v > math.MaxInt32 && v <= math.MaxUint32 {
		v = int64(int32(uint32(v)))
	}
	return strconv.FormatInt(v, 10)
}

func writeTS(path string, consts []*constant, anorms, flash []float64) {
	os.MkdirAll(filepath.Dir(path), 0o755)
	f, err := os.Create(path)
	if err != nil {
		panic(err)
	}
	defer f.Close()
	w := bufio.NewWriter(f)
	defer w.Flush()
	header(w, "//")
	names := make([]string, 0, len(consts))
	for _, c := range consts {
		names = append(names, c.name)
		if c.isStr {
			fmt.Fprintf(w, "export const %s = %q; // %s\n", c.name, c.s, c.src)
		} else {
			fmt.Fprintf(w, "export const %s = %s; // %s\n", c.name, tsNum(c), c.src)
		}
	}
	sort.Strings(names)
	fmt.Fprintf(w, "\n/** client/anorms.h, 162 unit normals (x,y,z interleaved, float32 values). */\nexport const BYTE_DIRS: Float32Array = new Float32Array([\n")
	for i := 0; i < len(anorms); i += 3 {
		fmt.Fprintf(w, "  %s, %s, %s,\n", fmtFloat32(anorms[i]), fmtFloat32(anorms[i+1]), fmtFloat32(anorms[i+2]))
	}
	fmt.Fprintf(w, "]);\n\n/** game/m_flash.c monster_flash_offset (x,y,z interleaved), indexed by MZ2_*. */\nexport const MONSTER_FLASH_OFFSET: Float32Array = new Float32Array([\n")
	for i := 0; i < len(flash); i += 3 {
		fmt.Fprintf(w, "  %s, %s, %s,\n", fmtFloat32(flash[i]), fmtFloat32(flash[i+1]), fmtFloat32(flash[i+2]))
	}
	fmt.Fprintln(w, "]);")
}
