package service

import "testing"

func TestParseIDFromHost(t *testing.T) {
	const suffix = ".gw.example.com"
	cases := map[string]string{
		"abc" + suffix:     "abc",
		suffix:             "",
		"":                 "",
		"a":                "",
		"abc.gw.other.org": "",
		"short":            "",
	}
	for host, want := range cases {
		if got := parseIDFromHost(host, suffix); got != want {
			t.Errorf("parseIDFromHost(%q) = %q, want %q", host, got, want)
		}
	}
}
