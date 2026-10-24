// Package spice parses the supported SPICE subset and normalizes values.
//
// Normalization rule (per design §6): only bare numeric literals are
// rewritten; in SPICE "1M" means milli and "1MEG" means mega, while
// Qucs-S writes "1m"/"1M". Expressions, "{...}" blocks and .param
// algebra are returned untouched.
package spice

import "strings"

// NormalizeValue converts a SPICE-style bare numeric literal into the
// form Qucs-S understands. Anything that is not a bare literal (e.g.
// "V(T)*2", "{R1*2}", "") is returned unchanged.
func NormalizeValue(s string) string {
	t := strings.TrimSpace(s)
	if !bareLiteral(t) {
		return s
	}
	body, next := scanNumber(t)
	rest := t[next:]
	if len(rest) >= 3 && strings.EqualFold(rest[:3], "meg") {
		return body + "M"
	}
	if len(rest) >= 1 {
		if letter, ok := siLetter(rest[0]); ok {
			return body + letter
		}
	}
	return body
}

// bareLiteral reports whether s is a plain numeric literal, optionally
// followed by an SI multiplier and junk units ("10uF", "1kOhm").
// Rejects expressions ("V(T)*2", "1e-6*2") and {...} templates.
func bareLiteral(s string) bool {
	if s == "" {
		return false
	}
	seenDigit := false
	i := 0
	// optional sign
	if s[i] == '+' || s[i] == '-' {
		i++
	}
	for ; i < len(s); i++ {
		c := s[i]
		switch {
		case c >= '0' && c <= '9':
			seenDigit = true
		case c == '.':
		case c == 'e' || c == 'E':
			// exponent only when directly after a digit/dot and followed
			// by digits (with optional sign); otherwise it's a letter.
			exp := i > 0 && (s[i-1] >= '0' && s[i-1] <= '9' || s[i-1] == '.')
			if exp {
				j := i + 1
				if j < len(s) && (s[j] == '+' || s[j] == '-') {
					j++
				}
				if j >= len(s) || s[j] < '0' || s[j] > '9' {
					return false // stray numeric exponent "1e"
				}
				for j < len(s) && s[j] >= '0' && s[j] <= '9' {
					j++
				}
				i = j - 1
			}
			// not an exponent: fall through as a letter (e.g. "MEG")
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z':
			if !seenDigit {
				return false
			}
			// suffix part must stay letter-only
		default:
			return false
		}
	}
	return seenDigit
}

// scanNumber consumes the leading numeric part (sign, digits, dot,
// exponent) and returns the digits text with the index right after it.
func scanNumber(s string) (body string, next int) {
	i := 0
	if i < len(s) && (s[i] == '+' || s[i] == '-') {
		i++
	}
	for i < len(s) && (s[i] >= '0' && s[i] <= '9' || s[i] == '.') {
		i++
	}
	if i < len(s) && (s[i] == 'e' || s[i] == 'E') {
		j := i + 1
		if j < len(s) && (s[j] == '+' || s[j] == '-') {
			j++
		}
		k := j
		for k < len(s) && s[k] >= '0' && s[k] <= '9' {
			k++
		}
		if k > j {
			i = k
		}
	}
	return s[:i], i
}

// siLetter maps a SPICE multiplier byte to the Qucs-S SI letter.
func siLetter(c byte) (string, bool) {
	switch c {
	case 'T', 't':
		return "T", true
	case 'G', 'g':
		return "G", true
	case 'K', 'k':
		return "k", true
	case 'M', 'm':
		return "m", true // SPICE M == milli; MEG handled separately
	case 'U', 'u':
		return "u", true
	case 'N', 'n':
		return "n", true
	case 'P', 'p':
		return "p", true
	case 'F', 'f':
		return "f", true
	}
	return "", false
}
