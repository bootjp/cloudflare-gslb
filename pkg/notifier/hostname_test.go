package notifier

import "testing"

func TestDisplayOrigin(t *testing.T) {
	for _, tc := range []struct{ name, zone, want string }{
		{"www", "example.com", "www.example.com"},
		{"misskey.bootjp.me", "bootjp.me", "misskey.bootjp.me"},
		{"example.com", "example.com", "example.com"},
	} {
		if got := displayOrigin(tc.name, tc.zone); got != tc.want {
			t.Errorf("displayOrigin(%q, %q) = %q, want %q", tc.name, tc.zone, got, tc.want)
		}
	}
}
