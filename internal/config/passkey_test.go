package config

import "testing"

func TestParsePasskeyOrigin(t *testing.T) {
	for _, raw := range []string{"https://panel.example", "https://panel.example:8443", "http://localhost:5173"} {
		if _, _, err := ParsePasskeyOrigin(raw); err != nil {
			t.Errorf("%q: %v", raw, err)
		}
	}
	for _, raw := range []string{"", "http://panel.example", "https://panel.example/", "https://user@panel.example", "https://*.example", "https://panel.example?x=1", "https://panel.example#x", "https://PANEL.example", "https://panel.example:443", "https://panel.example:"} {
		if _, _, err := ParsePasskeyOrigin(raw); err == nil {
			t.Errorf("accepted invalid origin %q", raw)
		}
	}
}
