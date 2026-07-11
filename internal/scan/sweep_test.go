package scan

import "testing"

func TestHostIPs(t *testing.T) {
	ips, err := HostIPs("192.168.1.0/30")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"192.168.1.1", "192.168.1.2"} // network+broadcast skipped
	if len(ips) != 2 || ips[0] != want[0] || ips[1] != want[1] {
		t.Fatalf("got %v", ips)
	}
	if _, err := HostIPs("garbage"); err == nil {
		t.Fatal("want error for bad cidr")
	}
}
