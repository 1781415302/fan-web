package services

import "testing"

func TestIsNewerVersion(t *testing.T) {
	cases := []struct {
		current string
		latest  string
		want    bool
	}{
		{"v1.0.0", "v1.0.1", true},
		{"v1.0.0", "v1.1.0", true},
		{"v1.0.0", "v2.0.0", true},
		{"v1.1.0", "v1.1.0", false},
		{"v1.2.0", "v1.1.0", false},
		{"v1.10.0", "v1.9.0", false},
		{"v1.9.0", "v1.10.0", true},
		{"v1.0.0", "v1.0.0", false},
		{"dev", "v1.0.0", true},
		{"v1.0.0", "dev", false},
		{"1.0.0", "1.0.1", true},
		{"v1.0", "v1.0.1", true},
	}
	for _, c := range cases {
		got := IsNewerVersion(c.current, c.latest)
		if got != c.want {
			t.Errorf("IsNewerVersion(%q, %q) = %v, want %v", c.current, c.latest, got, c.want)
		}
	}
}
