package update

import (
	"fmt"
	"strconv"
	"strings"
)

// releaseVersion implements SemVer precedence, including numeric prerelease
// identifiers (preview.10 is newer than preview.9).
type releaseVersion struct {
	major, minor, patch uint64
	pre                 []string
}

func parseVersion(raw string) (releaseVersion, error) {
	var v releaseVersion
	if !strings.HasPrefix(raw, "v") {
		return v, fmt.Errorf("not a release version: %q", raw)
	}
	core, build, hasBuild := strings.Cut(raw[1:], "+")
	if hasBuild && !validIdentifiers(build, false) {
		return v, fmt.Errorf("invalid version metadata")
	}
	core, pre, hasPre := strings.Cut(core, "-")
	if hasPre {
		if !validIdentifiers(pre, true) {
			return v, fmt.Errorf("invalid prerelease version")
		}
		v.pre = strings.Split(pre, ".")
	}
	parts := strings.Split(core, ".")
	if len(parts) != 3 {
		return v, fmt.Errorf("invalid release version")
	}
	values := []*uint64{&v.major, &v.minor, &v.patch}
	for i, p := range parts {
		if !numeric(p) || len(p) > 1 && p[0] == '0' {
			return v, fmt.Errorf("invalid release version")
		}
		n, err := strconv.ParseUint(p, 10, 64)
		if err != nil {
			return v, fmt.Errorf("invalid release version: %w", err)
		}
		*values[i] = n
	}
	return v, nil
}

func validIdentifiers(s string, rejectLeadingZero bool) bool {
	for _, p := range strings.Split(s, ".") {
		if p == "" || rejectLeadingZero && numeric(p) && len(p) > 1 && p[0] == '0' {
			return false
		}
		for _, c := range p {
			if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c == '-') {
				return false
			}
		}
	}
	return true
}

func numeric(s string) bool {
	if s == "" {
		return false
	}
	for _, c := range s {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}

func (v releaseVersion) compare(other releaseVersion) int {
	a := [...]uint64{v.major, v.minor, v.patch}
	b := [...]uint64{other.major, other.minor, other.patch}
	for i := range a {
		if a[i] < b[i] {
			return -1
		}
		if a[i] > b[i] {
			return 1
		}
	}
	if len(v.pre) == 0 && len(other.pre) != 0 {
		return 1
	}
	if len(v.pre) != 0 && len(other.pre) == 0 {
		return -1
	}
	for i := 0; i < len(v.pre) && i < len(other.pre); i++ {
		x, y := v.pre[i], other.pre[i]
		if x == y {
			continue
		}
		xNum, yNum := numeric(x), numeric(y)
		if xNum && !yNum {
			return -1
		}
		if !xNum && yNum {
			return 1
		}
		// Numeric identifiers have no leading zeroes and may exceed uint64.
		if xNum && len(x) != len(y) {
			if len(x) < len(y) {
				return -1
			}
			return 1
		}
		return strings.Compare(x, y)
	}
	if len(v.pre) < len(other.pre) {
		return -1
	}
	if len(v.pre) > len(other.pre) {
		return 1
	}
	return 0
}
