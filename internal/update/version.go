package update

import (
	"strconv"
	"strings"
)

// Newer reports whether version a is newer than b, by semantic-versioning
// precedence (1.0.0-rc.2 < 1.0.0-rc.10 < 1.0.0). A leading "v" is ignored.
func Newer(a, b string) bool { return compare(a, b) > 0 }

// Prerelease reports whether v has a pre-release part (1.0.0-rc.3).
func Prerelease(v string) bool {
	_, pre, ok := split(v)
	return ok && pre != ""
}

func split(v string) (core []int, pre string, ok bool) {
	v = strings.TrimPrefix(strings.TrimSpace(v), "v")
	if i := strings.IndexByte(v, '+'); i >= 0 {
		v = v[:i]
	}
	if i := strings.IndexByte(v, '-'); i >= 0 {
		v, pre = v[:i], v[i+1:]
	}
	parts := strings.Split(v, ".")
	if len(parts) != 3 {
		return nil, "", false
	}
	for _, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil || n < 0 {
			return nil, "", false
		}
		core = append(core, n)
	}
	return core, pre, true
}

func compare(a, b string) int {
	ca, pa, oka := split(a)
	cb, pb, okb := split(b)
	if !oka || !okb {
		return 0
	}
	for i := range 3 {
		if ca[i] != cb[i] {
			if ca[i] > cb[i] {
				return 1
			}
			return -1
		}
	}
	switch {
	case pa == pb:
		return 0
	case pa == "":
		return 1
	case pb == "":
		return -1
	}
	xa, xb := strings.Split(pa, "."), strings.Split(pb, ".")
	for i := 0; i < len(xa) && i < len(xb); i++ {
		na, ea := strconv.Atoi(xa[i])
		nb, eb := strconv.Atoi(xb[i])
		switch {
		case ea == nil && eb == nil && na != nb:
			if na > nb {
				return 1
			}
			return -1
		case ea == nil && eb != nil:
			return -1
		case ea != nil && eb == nil:
			return 1
		case ea != nil && eb != nil && xa[i] != xb[i]:
			if xa[i] > xb[i] {
				return 1
			}
			return -1
		}
	}
	switch {
	case len(xa) > len(xb):
		return 1
	case len(xa) < len(xb):
		return -1
	}
	return 0
}
