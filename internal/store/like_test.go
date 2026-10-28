package store

import "testing"

func TestEscapeLikeNeutralisesWildcards(t *testing.T) {
	cases := map[string]string{
		"%":         `\%`,
		"_":         `\_`,
		`a\b`:       `a\\b`,
		"50%_off":   `50\%\_off`,
		"plain/dir": "plain/dir",
		`\%`:        `\\\%`,
	}
	for in, want := range cases {
		if got := EscapeLike(in); got != want {
			t.Errorf("EscapeLike(%q) = %q, want %q", in, got, want)
		}
	}
	if got := likeUnder("%"); got != `\%/%` {
		t.Errorf("likeUnder(%%) = %q", got)
	}
	if got := likeContains("5_0"); got != `%5\_0%` {
		t.Errorf("likeContains = %q", got)
	}
}
