package manifest

import (
	"strconv"
	"strings"
)

// CompareVersions orders two app versions of the form X.Y.Z with an
// optional -prerelease and +build: -1 when a is older, 0 when they are the
// same, 1 when a is newer. A prerelease is older than its release; build
// metadata does not count. A part that is not a number compares as text.
func CompareVersions(a, b string) int {
	core := func(v string) (nums []string, pre string) {
		v, _, _ = strings.Cut(v, "+")
		v, pre, _ = strings.Cut(v, "-")
		return strings.Split(v, "."), pre
	}
	an, ap := core(a)
	bn, bp := core(b)
	for i := 0; i < len(an) || i < len(bn); i++ {
		x, y := "0", "0"
		if i < len(an) {
			x = an[i]
		}
		if i < len(bn) {
			y = bn[i]
		}
		if c := comparePart(x, y); c != 0 {
			return c
		}
	}
	switch {
	case ap == bp:
		return 0
	case ap == "":
		return 1
	case bp == "":
		return -1
	}
	ax, bx := strings.Split(ap, "."), strings.Split(bp, ".")
	for i := 0; i < len(ax) && i < len(bx); i++ {
		if c := comparePart(ax[i], bx[i]); c != 0 {
			return c
		}
	}
	return compareInt(len(ax), len(bx))
}

func comparePart(x, y string) int {
	xi, xerr := strconv.Atoi(x)
	yi, yerr := strconv.Atoi(y)
	switch {
	case xerr == nil && yerr == nil:
		return compareInt(xi, yi)
	case xerr == nil:
		return -1 // numbers before text, as semver says
	case yerr == nil:
		return 1
	}
	return strings.Compare(x, y)
}

func compareInt(x, y int) int {
	switch {
	case x < y:
		return -1
	case x > y:
		return 1
	}
	return 0
}
