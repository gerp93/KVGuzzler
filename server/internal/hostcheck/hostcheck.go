// Package hostcheck accepts only a plain host name or IP address for a device. A URL, path, port, user info or
// anything else is refused, so a typed address can never change what the server requests.
package hostcheck

import (
	"errors"
	"net/netip"
	"strings"
)

var ErrNotPlainHost = errors.New("must be a plain host name or IP address, not a URL, path or port")

// Validate returns nil for an IP address or a dotted host name made of letters, digits and hyphens.
func Validate(host string) error {
	if host == "" || len(host) > 253 || strings.ContainsRune(host, '%') {
		return ErrNotPlainHost
	}
	if _, err := netip.ParseAddr(host); err == nil {
		return nil
	}
	for _, label := range strings.Split(host, ".") {
		if label == "" || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return ErrNotPlainHost
		}
		for _, r := range label {
			ok := r == '-' || (r >= '0' && r <= '9') || (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z')
			if !ok {
				return ErrNotPlainHost
			}
		}
	}
	return nil
}
