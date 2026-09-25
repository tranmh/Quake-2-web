package shared

import (
	"bufio"
	"fmt"
	"math"
	"os"
	"strconv"
	"strings"
	"testing"
)

// TestAgainstC replays testdata/q_shared_c.txt, produced by compiling the
// real game/q_shared.c (see testdata/q_shared_c.c.txt), bit for bit.
func TestAgainstC(t *testing.T) {
	f, err := os.Open("testdata/q_shared_c.txt")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	n := 0
	for sc.Scan() {
		n++
		fields := strings.Fields(sc.Text())
		fl := func(i int) float32 {
			u, err := strconv.ParseUint(fields[i], 16, 32)
			if err != nil {
				t.Fatalf("line %d: %v", n, err)
			}
			return math.Float32frombits(uint32(u))
		}
		v3 := func(i int) Vec3 { return Vec3{fl(i), fl(i + 1), fl(i + 2)} }
		same := func(a, b Vec3) bool {
			for k := 0; k < 3; k++ {
				if math.Float32bits(a[k]) != math.Float32bits(b[k]) {
					return false
				}
			}
			return true
		}
		switch fields[0] {
		case "AV":
			var fw, rt, up Vec3
			AngleVectors(v3(1), &fw, &rt, &up)
			if !same(fw, v3(4)) || !same(rt, v3(7)) || !same(up, v3(10)) {
				t.Fatalf("line %d AngleVectors(%v): got %v %v %v want %v %v %v", n, v3(1), fw, rt, up, v3(4), v3(7), v3(10))
			}
		case "AM":
			if got := Anglemod(fl(1)); math.Float32bits(got) != math.Float32bits(fl(2)) {
				t.Fatalf("line %d anglemod(%v) = %v want %v", n, fl(1), got, fl(2))
			}
		case "LA":
			if got := LerpAngle(fl(1), fl(2), fl(3)); math.Float32bits(got) != math.Float32bits(fl(4)) {
				t.Fatalf("line %d LerpAngle = %v want %v", n, got, fl(4))
			}
		case "A2S":
			want, _ := strconv.Atoi(fields[2])
			if got := ANGLE2SHORT(fl(1)); got != int32(want) {
				t.Fatalf("line %d ANGLE2SHORT(%v) = %d want %d", n, fl(1), got, want)
			}
		case "VN":
			o := v3(1)
			l := VectorNormalize(&o)
			if math.Float32bits(l) != math.Float32bits(fl(4)) || !same(o, v3(5)) {
				t.Fatalf("line %d VectorNormalize(%v) = %v %v want %v %v", n, v3(1), l, o, fl(4), v3(5))
			}
			var o2 Vec3
			o2 = v3(1)
			if l2 := VectorNormalize2(v3(1), &o2); l2 != l || !same(o2, o) {
				t.Fatalf("line %d VectorNormalize2 mismatch", n)
			}
		case "RP":
			if got := RotatePointAroundVector(v3(1), v3(4), fl(7)); !same(got, v3(8)) {
				t.Fatalf("line %d RotatePointAroundVector = %v want %v", n, got, v3(8))
			}
		case "BS":
			mi, ma := v3(1), v3(4)
			typ, _ := strconv.Atoi(fields[11])
			sb, _ := strconv.Atoi(fields[12])
			want, _ := strconv.Atoi(fields[13])
			p := CPlane{Normal: v3(7), Dist: fl(10), Type: uint8(typ), SignBits: uint8(sb)}
			if SignbitsForPlane(&p) != p.SignBits {
				t.Fatalf("line %d signbits", n)
			}
			if got := BoxOnPlaneSide(&mi, &ma, &p); got != want {
				t.Fatalf("line %d BoxOnPlaneSide = %d want %d", n, got, want)
			}
			if got := BOX_ON_PLANE_SIDE(&mi, &ma, &p); got != want {
				t.Fatalf("line %d BOX_ON_PLANE_SIDE = %d want %d", n, got, want)
			}
			if p.Type >= 3 {
				if got := BoxOnPlaneSide2(mi, ma, &p); got == 0 {
					t.Fatalf("line %d BoxOnPlaneSide2 = 0", n)
				}
			}
		default:
			t.Fatalf("line %d: unknown record %q", n, fields[0])
		}
	}
	if n < 1000 {
		t.Fatalf("only %d records", n)
	}
}

func TestCOMParse(t *testing.T) {
	long := strings.Repeat("x", 200)
	for _, tc := range []struct {
		in, tok, rest string
		more          bool
	}{
		{"  hello world", "hello", " world", true},
		{"\"quoted string\" x", "quoted string", " x", true},
		{"// comment\nfoo bar", "foo", " bar", true},
		{"// only a comment", "", "", false},
		{"a//b c", "a//b", " c", true},
		{"\"unterminated", "unterminated", "", true},
		{"", "", "", false},
		{" \t\n ", "", "", false},
		{"{\"a\" \"b\"}", "{\"a\"", " \"b\"}", true},
		{"ab\xe9cd", "ab", "\xe9cd", true},     // signed char: 0xe9 < ' ' ends the word
		{"\xe9\xe9cd", "cd", "", true},         // ... and is skipped as whitespace
		{"\"a\xe9b\" z", "a\xe9b", " z", true}, // but kept inside quotes
		{"\"\" x", "", " x", true},
		{strings.Repeat("y", 127) + " z", strings.Repeat("y", 127), " z", true},
		{strings.Repeat("y", 128) + " z", "", " z", true}, // exactly MAX_TOKEN_CHARS: discarded
		{long + " z", "", " z", true},
		{"\"" + long + "\"", long[:128], "", true},
	} {
		tok, rest, more := COM_Parse(tc.in)
		if tok != tc.tok || rest != tc.rest || more != tc.more {
			t.Errorf("COM_Parse(%q) = %q, %q, %v; want %q, %q, %v", tc.in, tok, rest, more, tc.tok, tc.rest, tc.more)
		}
	}
	// token sequence
	data := "{\n\"classname\" \"worldspawn\" // c\n\"message\" \"a b\"\n}"
	var toks []string
	for {
		tok, rest, more := COM_Parse(data)
		if !more {
			break
		}
		toks = append(toks, tok)
		data = rest
	}
	if got := strings.Join(toks, "|"); got != "{|classname|worldspawn|message|a b|}" {
		t.Errorf("sequence %q", got)
	}
}

func TestPathHelpers(t *testing.T) {
	for _, tc := range []struct{ fn, in, want string }{
		{"skip", "maps/demo1.bsp", "demo1.bsp"},
		{"skip", "demo1.bsp", "demo1.bsp"},
		{"strip", "maps/demo1.bsp", "maps/demo1"},
		{"strip", "./maps/x.bsp", ""},
		{"ext", "a.tar.gz", "tar.gz"},
		{"ext", "abc", ""},
		{"ext", "x.abcdefghij", "abcdefg"},
		{"base", "maps/demo1.bsp", "demo1"},
		{"base", "demo1.bsp", "emo1"}, // C quirk
		{"base", "maps/demo1", ""},
		{"base", "a/b.c", "b"},
		{"base", "a/.c", ""},
		{"base", "", ""},
		{"path", "maps/demo1.bsp", "maps"},
		{"path", "demo1.bsp", ""},
		{"path", "a/b/c", "a/b"},
	} {
		var got string
		switch tc.fn {
		case "skip":
			got = COM_SkipPath(tc.in)
		case "strip":
			got = COM_StripExtension(tc.in)
		case "ext":
			got = COM_FileExtension(tc.in)
		case "base":
			got = COM_FileBase(tc.in)
		case "path":
			got = COM_FilePath(tc.in)
		}
		if got != tc.want {
			t.Errorf("%s(%q) = %q, want %q", tc.fn, tc.in, got, tc.want)
		}
	}
	for _, tc := range []struct{ in, ext, want string }{
		{"maps/demo1", ".bsp", "maps/demo1.bsp"},
		{"maps/demo1.bsp", ".bsp", "maps/demo1.bsp"},
		{"a.b/c", ".x", "a.b/c.x"},
		{".cfg", ".cfg", ".cfg.cfg"}, // index 0 is never examined
	} {
		if got := COM_DefaultExtension(tc.in, tc.ext); got != tc.want {
			t.Errorf("COM_DefaultExtension(%q, %q) = %q, want %q", tc.in, tc.ext, got, tc.want)
		}
	}
}

func TestStringCompare(t *testing.T) {
	if Q_strncasecmp("ABC", "abd", 2) != 0 || Q_strncasecmp("ABC", "abd", 3) != -1 || Q_strcasecmp("Hello", "hELLO") != 0 {
		t.Error("Q_strncasecmp")
	}
	if Q_strcasecmp("abc", "abcd") != -1 || Q_strcasecmp("", "") != 0 {
		t.Error("Q_strcasecmp lengths")
	}
	if Q_stricmp("Hello", "hELLO") != 0 || Q_stricmp("a", "b") >= 0 || Q_stricmp("b", "A") <= 0 || Q_stricmp("ab", "a") <= 0 {
		t.Error("Q_stricmp")
	}
	if Q_log2(1) != 0 || Q_log2(8) != 3 || Q_log2(9) != 3 || Q_log2(0) != 0 {
		t.Error("Q_log2")
	}
}

func TestInfoStrings(t *testing.T) {
	s := "\\name\\player\\skin\\male/grunt\\hand\\0"
	for key, want := range map[string]string{"name": "player", "skin": "male/grunt", "hand": "0", "fov": "", "Name": ""} {
		if got := Info_ValueForKey(s, key); got != want {
			t.Errorf("Info_ValueForKey(%q) = %q, want %q", key, got, want)
		}
	}
	if got := Info_RemoveKey(s, "skin"); got != "\\name\\player\\hand\\0" {
		t.Errorf("RemoveKey = %q", got)
	}
	if got := Info_RemoveKey(s, "nope"); got != s {
		t.Errorf("RemoveKey missing = %q", got)
	}
	steps := []struct{ k, v, want, warn string }{
		{"a", "1", "\\a\\1", ""},
		{"b", "2", "\\a\\1\\b\\2", ""},
		{"a", "3", "\\b\\2\\a\\3", ""},
		{"b", "", "\\a\\3", ""},
		{"c", "h\xe9llo", "\\a\\3\\c\\hillo", ""}, // high bits stripped
		{"x\\y", "1", "\\a\\3\\c\\hillo", "Can't use keys or values with a \\\n"},
		{"k;", "1", "\\a\\3\\c\\hillo", "Can't use keys or values with a semicolon\n"},
		{"k", "\"", "\\a\\3\\c\\hillo", "Can't use keys or values with a \"\n"},
		{"k", strings.Repeat("v", 64), "\\a\\3\\c\\hillo", "Keys and values must be < 64 characters.\n"},
	}
	info := ""
	for _, st := range steps {
		got, warn := Info_SetValueForKey(info, st.k, st.v)
		if got != st.want || warn != st.warn {
			t.Fatalf("Set(%q,%q) on %q = %q (%q); want %q (%q)", st.k, st.v, info, got, warn, st.want, st.warn)
		}
		info = got
	}
	// length limit: the old key is removed even though the new pair does not fit
	big := "\\k\\" + strings.Repeat("v", 497)
	got, warn := Info_SetValueForKey(big+"\\zz\\1", "zz", "0123456789")
	if warn != "Info string length exceeded\n" || strings.Contains(got, "\\zz\\") {
		t.Errorf("length limit: %q %q", got[len(got)-20:], warn)
	}
	if !Info_Validate("\\a\\b") || Info_Validate("a\"b") || Info_Validate("a;b") {
		t.Error("Info_Validate")
	}
}

func TestVectorHelpers(t *testing.T) {
	a, b := Vec3{1, 2, 3}, Vec3{4, 5, 6}
	if DotProduct(a, b) != 32 || CrossProduct(a, b) != (Vec3{-3, 6, -3}) || VectorMA(a, 2, b) != (Vec3{9, 12, 15}) {
		t.Error("dot/cross/ma")
	}
	if VectorAdd(a, b) != (Vec3{5, 7, 9}) || VectorSubtract(b, a) != (Vec3{3, 3, 3}) || VectorScale(a, 2) != (Vec3{2, 4, 6}) {
		t.Error("add/sub/scale")
	}
	if VectorCompare(a, a) != 1 || VectorCompare(a, b) != 0 || VectorLength(Vec3{3, 4, 0}) != 5 {
		t.Error("compare/length")
	}
	v := a
	VectorInverse(&v)
	if v != VectorNegate(a) {
		t.Error("inverse")
	}
	var mins, maxs Vec3
	ClearBounds(&mins, &maxs)
	AddPointToBounds(a, &mins, &maxs)
	AddPointToBounds(Vec3{-1, 7, 0}, &mins, &maxs)
	if mins != (Vec3{-1, 2, 0}) || maxs != (Vec3{1, 7, 3}) {
		t.Errorf("bounds %v %v", mins, maxs)
	}
	if Q_fabs(-2.5) != 2.5 || math.Signbit(float64(Q_fabs(float32(math.Copysign(0, -1))))) {
		t.Error("Q_fabs")
	}
	if SHORT2ANGLE(16384) != 90 || ANGLE2SHORT(90) != 16384 || ANGLE2SHORT(-90) != 49152 {
		t.Error("angle shorts")
	}
	p := PerpendicularVector(Vec3{0, 0, 1})
	if DotProduct(p, Vec3{0, 0, 1}) != 0 || math.Abs(float64(VectorLength(p))-1) > 1e-6 {
		t.Errorf("PerpendicularVector %v", p)
	}
	var in1 = [3][3]float32{{1, 0, 0}, {0, 1, 0}, {0, 0, 1}}
	var out [3][3]float32
	m := [3][3]float32{{1, 2, 3}, {4, 5, 6}, {7, 8, 9}}
	R_ConcatRotations(&in1, &m, &out)
	if out != m {
		t.Error("R_ConcatRotations identity")
	}
	var t1 = [3][4]float32{{1, 0, 0, 1}, {0, 1, 0, 2}, {0, 0, 1, 3}}
	var t2 = [3][4]float32{{1, 0, 0, 10}, {0, 1, 0, 20}, {0, 0, 1, 30}}
	var to [3][4]float32
	R_ConcatTransforms(&t1, &t2, &to)
	if to[0][3] != 11 || to[1][3] != 22 || to[2][3] != 33 {
		t.Errorf("R_ConcatTransforms %v", to)
	}
}

func TestComError(t *testing.T) {
	defer func() {
		r := recover()
		e, ok := r.(ComError)
		if !ok || e.Code != 1 || e.Error() != "bad 7" {
			t.Errorf("recovered %v", r)
		}
	}()
	Error(1, "bad %d", 7)
	t.Error(fmt.Sprint("unreachable"))
}

func BenchmarkAngleVectors(b *testing.B) {
	var f, r, u Vec3
	for i := 0; i < b.N; i++ {
		AngleVectors(Vec3{float32(i & 255), 30, 5}, &f, &r, &u)
	}
}
