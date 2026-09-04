package normalize

import "testing"

func TestURLRejectsCorruptedHost(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"normal", "https://www.example.com/path?utm_source=x#frag", "https://example.com/path"},
		{"double dot host", "https://rfc..editor.org/rfc/rfc5508", ""},
		{"multi double dot host", "https://firefox..source..docs.mozilla.org/preferences.html", ""},
		{"no host", "/relative/path", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := URL(tc.in); got != tc.want {
				t.Errorf("URL(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}
