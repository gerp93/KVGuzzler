package hostcheck

import "testing"

func TestValidate(t *testing.T) {
	good := []string{"192.168.1.50", "house-box", "refoss-em16p.lan", "fe80::1", "A1"}
	for _, h := range good {
		if err := Validate(h); err != nil {
			t.Errorf("Validate(%q) = %v, want ok", h, err)
		}
	}
	bad := []string{
		"", "http://192.168.1.50", "192.168.1.50:80", "192.168.1.50/rpc", "user@host", "host?x=1", "host#frag",
		"host name", "-host", "host-", "a..b", ".host", "host.", "fe80::1%eth0", "[::1]", "ho_st",
	}
	for _, h := range bad {
		if err := Validate(h); err == nil {
			t.Errorf("Validate(%q) = nil, want an error", h)
		}
	}
}
