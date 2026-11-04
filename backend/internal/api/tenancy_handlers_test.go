package api

import (
	"strings"
	"testing"
)

func TestSlugify(t *testing.T) {
	for _, tc := range []struct{ name, want string }{
		{"My Team", "my-team"},
		{" Équipe 東京 ", "quipe"},
		{strings.Repeat("a", 38) + " b", strings.Repeat("a", 38)},
	} {
		if got := slugify(tc.name); got != tc.want {
			t.Errorf("slugify(%q) = %q, want %q", tc.name, got, tc.want)
		}
	}
	seen := map[string]bool{}
	for _, name := range []string{"東京", "团队", "Привет", "🚀", "é"} {
		got := slugify(name)
		if !slugRe.MatchString(got) || got != slugify(name) || seen[got] {
			t.Errorf("invalid, unstable or colliding slug for %q: %q", name, got)
		}
		seen[got] = true
	}
}
