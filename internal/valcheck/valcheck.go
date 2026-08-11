package valcheck

import (
	"errors"
	"fmt"
	"net"
	"strconv"
	"strings"
)

// IPv4 accepts a dotted-quad IPv4 address or the A.B.C.D/len CIDR form.
func IPv4(s string) error {
	addr, prefix, hasPrefix := strings.Cut(s, "/")
	ip := net.ParseIP(addr)
	if ip == nil || ip.To4() == nil {
		return errors.New("not an IPv4 address")
	}
	if hasPrefix {
		n, err := strconv.Atoi(prefix)
		if err != nil || n < 0 || n > 32 {
			return fmt.Errorf("invalid IPv4 prefix length %q", prefix)
		}
	}
	return nil
}

// Range returns a Check that accepts a decimal integer inside lo..hi inclusive.
func Range(lo, hi uint64) func(string) error {
	return func(s string) error {
		n, err := strconv.ParseUint(s, 10, 64)
		if err != nil {
			return errors.New("not a number")
		}
		if n < lo || n > hi {
			return fmt.Errorf("out of range %d..%d", lo, hi)
		}
		return nil
	}
}
