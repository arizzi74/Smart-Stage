package update

import "testing"

func TestVersionPrecedence(t *testing.T) {
	ordered := []string{"v0.1.0-preview.8", "v0.1.0-preview.9", "v0.1.0-preview.10", "v0.1.0-preview.99999999999999999999", "v0.1.0-rc.1", "v0.1.0", "v0.1.1", "v0.2.0", "v1.0.0"}
	for i, raw := range ordered {
		a, err := parseVersion(raw)
		if err != nil {
			t.Fatal(err)
		}
		for j, other := range ordered {
			b, err := parseVersion(other)
			if err != nil {
				t.Fatal(err)
			}
			cmp := a.compare(b)
			if i < j && cmp >= 0 || i == j && cmp != 0 || i > j && cmp <= 0 {
				t.Fatalf("%s compare %s = %d", raw, other, cmp)
			}
		}
	}
	a, _ := parseVersion("v1.0.0+build.1")
	b, _ := parseVersion("v1.0.0+build.2")
	if a.compare(b) != 0 {
		t.Fatal("build metadata must not change release precedence")
	}
}

func TestInvalidReleaseVersion(t *testing.T) {
	for _, raw := range []string{"dev", "main", "v1", "v1.2", "v01.2.3", "v1.2.3-", "v1.2.3-preview.01", "v1.2.3+", "v1.2.3/other", "v1.2.3-foo..bar", "v18446744073709551616.0.0"} {
		if _, err := parseVersion(raw); err == nil {
			t.Errorf("accepted invalid version %q", raw)
		}
	}
}
