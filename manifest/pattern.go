package manifest

import (
	"path"
	"strings"
)

// CheckPattern returns why p cannot be an uninstall.remove pattern, or "".
// A pattern is slash-separated and relative to the install directory, and
// cannot reach outside it.
func CheckPattern(p string) string {
	switch {
	case strings.TrimSpace(p) == "":
		return "empty pattern"
	case path.IsAbs(p) || strings.Contains(p, `\`) || (len(p) > 1 && p[1] == ':'):
		return p + " is absolute; a pattern is relative to the install directory"
	}
	for _, seg := range strings.Split(p, "/") {
		switch seg {
		case "", ".":
			return p + " has an empty or \".\" segment"
		case "..":
			return p + " reaches outside the install directory"
		case "**":
			continue
		}
		if strings.Contains(seg, "**") {
			return p + `: "**" must be a whole segment, such as python/**/__pycache__`
		}
		if _, err := path.Match(seg, ""); err != nil {
			return "bad pattern " + p
		}
	}
	return ""
}

// MatchPattern reports whether the slash-separated relative path rel
// matches the pattern p. "**" matches zero or more segments; other
// segments match as path.Match does.
func MatchPattern(p, rel string) bool {
	return matchSegments(strings.Split(p, "/"), strings.Split(rel, "/"))
}

func matchSegments(pat, name []string) bool {
	for len(pat) > 0 {
		if pat[0] == "**" {
			for i := 0; i <= len(name); i++ {
				if matchSegments(pat[1:], name[i:]) {
					return true
				}
			}
			return false
		}
		if len(name) == 0 {
			return false
		}
		if ok, _ := path.Match(pat[0], name[0]); !ok {
			return false
		}
		pat, name = pat[1:], name[1:]
	}
	return len(name) == 0
}
