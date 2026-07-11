package wireguard

import (
	"testing"
	"time"
)

const dumpFixture = "privkeyhidden\tpubSERVER\t51820\toff\n" +
	"pubPEER1=\t(none)\t203.0.113.9:51820\t10.6.0.2/32\t1752220000\t1024\t2048\toff\n" +
	"pubPEER2=\t(none)\t(none)\t10.6.0.3/32,fd00::3/128\t0\t0\t0\toff\n"

func TestParseDump(t *testing.T) {
	peers, err := ParseDump([]byte(dumpFixture))
	if err != nil || len(peers) != 2 {
		t.Fatalf("peers=%+v err=%v", peers, err)
	}
	p1 := peers[0]
	if p1.PubKey != "pubPEER1=" || p1.Endpoint != "203.0.113.9:51820" {
		t.Fatalf("p1=%+v", p1)
	}
	if len(p1.AllowedIPs) != 1 || p1.AllowedIPs[0] != "10.6.0.2/32" {
		t.Fatalf("p1 ips=%v", p1.AllowedIPs)
	}
	if p1.LastHandshake != time.Unix(1752220000, 0) {
		t.Fatalf("p1 hs=%v", p1.LastHandshake)
	}
	if !peers[1].LastHandshake.IsZero() {
		t.Fatalf("p2 should have zero handshake: %v", peers[1].LastHandshake)
	}
	if len(peers[1].AllowedIPs) != 2 {
		t.Fatalf("p2 ips=%v", peers[1].AllowedIPs)
	}
}
