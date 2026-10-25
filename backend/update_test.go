package backend

// XW-09: сравнение версий с pre-release.

import "testing"

func TestIsNewer_Semver(t *testing.T) {
	cases := []struct {
		a, b string
		want bool
	}{
		{"1.6.2", "1.6.1", true},
		{"1.10.0", "1.9.9", true},
		{"v1.7.0", "1.6.0", true},
		{"1.6.1", "1.6.1", false},
		{"v1.6.1", "1.6.1", false},
		{"1.6.1", "1.6.1-beta", true},  // финал новее pre-release
		{"1.6.1-beta", "1.6.1", false}, // и наоборот — не «новее»
		{"1.6.1-rc1", "1.6.1-beta", true},
		{"1.6.1-beta.2", "1.6.1-beta.1", true},
		{"1.6.1-beta", "1.6.0", true},
		{"1.6.0", "1.6.1-beta", false},
		{"2.0.0+build5", "2.0.0", false}, // build-метаданные не влияют
	}
	for _, c := range cases {
		if got := isNewer(c.a, c.b); got != c.want {
			t.Errorf("isNewer(%q, %q) = %v, ожидалось %v", c.a, c.b, got, c.want)
		}
	}
}
