package shared

import (
	"math"
	"strconv"
	"strings"
)

// C library replacements with glibc semantics (C locale), used wherever the
// C code calls atof/atoi/strtod/strtol on game or network strings.

func cIsSpace(c byte) bool {
	return c == ' ' || c == '\t' || c == '\n' || c == '\v' || c == '\f' || c == '\r'
}

func isDigit(c byte) bool { return c >= '0' && c <= '9' }

func isHexDigit(c byte) bool {
	return isDigit(c) || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')
}

func lower(c byte) byte {
	if c >= 'A' && c <= 'Z' {
		return c + ('a' - 'A')
	}
	return c
}

// hasPrefixFold reports whether s[i:] starts with p (p lowercase), ignoring case.
func hasPrefixFold(s string, i int, p string) bool {
	if len(s)-i < len(p) {
		return false
	}
	for k := 0; k < len(p); k++ {
		if lower(s[i+k]) != p[k] {
			return false
		}
	}
	return true
}

// Strtod is glibc strtod: it returns the correctly rounded double of the
// longest valid prefix of s and the number of bytes consumed (0 when no
// conversion was performed, in which case the value is 0). Handles leading
// whitespace, sign, decimal and hexadecimal floats, inf/infinity and
// nan/nan(chars). s is treated as a C string (stops at NUL).
func Strtod(s string) (float64, int) {
	s = cstr(s)
	i := 0
	for i < len(s) && cIsSpace(s[i]) {
		i++
	}
	neg := false
	if i < len(s) && (s[i] == '+' || s[i] == '-') {
		neg = s[i] == '-'
		i++
	}
	sign := func(v float64) float64 {
		if neg {
			return -v
		}
		return v
	}

	// inf / infinity
	if hasPrefixFold(s, i, "inf") {
		n := i + 3
		if hasPrefixFold(s, n, "inity") {
			n += 5
		}
		return sign(math.Inf(1)), n
	}
	// nan / nan(n-char-sequence)
	if hasPrefixFold(s, i, "nan") {
		n := i + 3
		if n < len(s) && s[n] == '(' {
			k := n + 1
			for k < len(s) && (isDigit(s[k]) || (lower(s[k]) >= 'a' && lower(s[k]) <= 'z') || s[k] == '_') {
				k++
			}
			payload := uint64(0)
			if k < len(s) && s[k] == ')' {
				payload = nanPayload(s[n+1 : k])
				n = k + 1
			}
			v := math.Float64frombits(0x7ff8000000000000 | payload)
			if neg {
				v = math.Float64frombits(math.Float64bits(v) | 1<<63)
			}
			return v, n
		}
		v := math.Float64frombits(0x7ff8000000000000)
		if neg {
			v = math.Float64frombits(math.Float64bits(v) | 1<<63)
		}
		return v, n
	}

	// hexadecimal
	if i+1 < len(s) && s[i] == '0' && lower(s[i+1]) == 'x' {
		k := i + 2
		mant := k
		digits := 0
		for k < len(s) && isHexDigit(s[k]) {
			k++
			digits++
		}
		if k < len(s) && s[k] == '.' {
			k++
			for k < len(s) && isHexDigit(s[k]) {
				k++
				digits++
			}
		}
		if digits == 0 {
			// only the "0" is converted
			return sign(0), i + 1
		}
		body := s[mant:k]
		exp := "p0"
		if k < len(s) && lower(s[k]) == 'p' {
			e := k + 1
			if e < len(s) && (s[e] == '+' || s[e] == '-') {
				e++
			}
			if e < len(s) && isDigit(s[e]) {
				for e < len(s) && isDigit(s[e]) {
					e++
				}
				exp = "p" + s[k+1:e]
				k = e
			}
		}
		if strings.HasPrefix(body, ".") {
			body = "0" + body
		}
		v, _ := strconv.ParseFloat("0x"+body+exp, 64)
		return sign(v), k
	}

	// decimal
	k := i
	digits := 0
	for k < len(s) && isDigit(s[k]) {
		k++
		digits++
	}
	if k < len(s) && s[k] == '.' {
		k++
		for k < len(s) && isDigit(s[k]) {
			k++
			digits++
		}
	}
	if digits == 0 {
		return 0, 0
	}
	end := k
	if k < len(s) && lower(s[k]) == 'e' {
		e := k + 1
		if e < len(s) && (s[e] == '+' || s[e] == '-') {
			e++
		}
		if e < len(s) && isDigit(s[e]) {
			for e < len(s) && isDigit(s[e]) {
				e++
			}
			end = e
		}
	}
	num := s[i:end]
	if strings.HasPrefix(num, ".") {
		num = "0" + num
	}
	v, err := strconv.ParseFloat(num, 64)
	if err != nil {
		// ParseFloat reports range errors but still returns +-Inf or the
		// correctly rounded (possibly zero/denormal) value, like strtod.
		if ne, ok := err.(*strconv.NumError); !ok || ne.Err != strconv.ErrRange {
			v = 0
		}
	}
	return sign(v), end
}

// nanPayload mirrors glibc's handling of nan(n-char-sequence): the sequence
// is read with strtoull base 0 and, when fully consumed, becomes the low
// mantissa bits of the quiet NaN.
func nanPayload(seq string) uint64 {
	if seq == "" || strings.ContainsRune(seq, '_') ||
		(len(seq) > 1 && seq[0] == '0' && (lower(seq[1]) == 'b' || lower(seq[1]) == 'o')) {
		return 0 // Go-only base prefixes / separators; strtoull rejects them
	}
	v, err := strconv.ParseUint(seq, 0, 64)
	if err != nil {
		return 0
	}
	return v & (1<<51 - 1)
}

// Atof is C atof (strtod(s, NULL)); the result is a double.
func Atof(s string) float64 {
	v, _ := Strtod(s)
	return v
}

// Strtol is glibc strtol with base 10: whitespace, sign, digits; the value
// saturates at the 64-bit LONG_MIN/LONG_MAX (LP64 oracle). Returns the value
// and the number of bytes consumed.
func Strtol(s string) (int64, int) {
	s = cstr(s)
	i := 0
	for i < len(s) && cIsSpace(s[i]) {
		i++
	}
	neg := false
	if i < len(s) && (s[i] == '+' || s[i] == '-') {
		neg = s[i] == '-'
		i++
	}
	start := i
	var acc uint64
	overflow := false
	for i < len(s) && isDigit(s[i]) {
		d := uint64(s[i] - '0')
		if !overflow {
			if acc > (math.MaxUint64-d)/10 {
				overflow = true
			} else {
				acc = acc*10 + d
			}
		}
		i++
	}
	if i == start {
		return 0, 0
	}
	if neg {
		if overflow || acc > 1<<63 {
			return math.MinInt64, i
		}
		return -int64(acc), i
	}
	if overflow || acc > math.MaxInt64 {
		return math.MaxInt64, i
	}
	return int64(acc), i
}

// Atoi is glibc atoi: (int)strtol(s, NULL, 10), truncated to 32 bits.
func Atoi(s string) int32 {
	v, _ := Strtol(s)
	return int32(v)
}
