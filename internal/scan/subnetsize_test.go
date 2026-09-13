package scan

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func TestCheckSubnetSize(t *testing.T) {
	ok := []string{
		"192.168.1.0/24", "10.0.0.0/16", "10.0.0.1/32", "10.0.0.0/31",
		"2001:db8::/112", "2001:db8::1/128",
	}
	for _, cidr := range ok {
		if err := CheckSubnetSize(cidr); err != nil {
			t.Errorf("CheckSubnetSize(%q) = %v, want nil", cidr, err)
		}
	}
	// Everything here would either exhaust memory or, for the IPv6 cases,
	// never finish enumerating.
	tooBig := []string{"10.0.0.0/15", "10.0.0.0/8", "0.0.0.0/0", "2001:db8::/64", "::/0"}
	for _, cidr := range tooBig {
		err := CheckSubnetSize(cidr)
		if !errors.Is(err, ErrSubnetTooLarge) {
			t.Errorf("CheckSubnetSize(%q) = %v, want ErrSubnetTooLarge", cidr, err)
		}
		// The message has to tell the user what would work.
		if err != nil && !strings.Contains(err.Error(), "or narrower") {
			t.Errorf("CheckSubnetSize(%q) message = %q", cidr, err)
		}
	}
	if err := CheckSubnetSize("not-a-cidr"); err == nil {
		t.Error("a malformed CIDR must still be an error")
	}
}

// The limit belongs inside AllIPs too: subnets live in the database and may
// predate it, and an unbounded IPv6 prefix would hang rather than fail.
func TestAllIPsRefusesOversizedPrefix(t *testing.T) {
	done := make(chan error, 1)
	go func() {
		_, err := AllIPs("2001:db8::/64")
		done <- err
	}()
	select {
	case err := <-done:
		if !errors.Is(err, ErrSubnetTooLarge) {
			t.Errorf("AllIPs(/64) = %v, want ErrSubnetTooLarge", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("AllIPs walked an IPv6 /64 instead of refusing it")
	}

	if _, err := AllIPs("10.0.0.0/8"); !errors.Is(err, ErrSubnetTooLarge) {
		t.Errorf("AllIPs(10.0.0.0/8) = %v, want ErrSubnetTooLarge", err)
	}
	if _, err := HostIPs("10.0.0.0/8"); !errors.Is(err, ErrSubnetTooLarge) {
		t.Errorf("HostIPs(10.0.0.0/8) = %v, want ErrSubnetTooLarge", err)
	}

	// The largest accepted subnet still enumerates completely.
	ips, err := AllIPs("10.0.0.0/16")
	if err != nil {
		t.Fatal(err)
	}
	if len(ips) != MaxSubnetAddresses {
		t.Errorf("AllIPs(/16) returned %d addresses, want %d", len(ips), MaxSubnetAddresses)
	}
}
