package wol

import (
	"bytes"
	"net"
	"testing"
	"time"
)

func TestBuildMagicPacket(t *testing.T) {
	p, err := BuildMagicPacket("aa:bb:cc:dd:ee:ff")
	if err != nil {
		t.Fatal(err)
	}
	if len(p) != 102 {
		t.Fatalf("len=%d", len(p))
	}
	if !bytes.Equal(p[:6], bytes.Repeat([]byte{0xFF}, 6)) {
		t.Fatal("missing FF header")
	}
	mac := []byte{0xaa, 0xbb, 0xcc, 0xdd, 0xee, 0xff}
	for i := 0; i < 16; i++ {
		if !bytes.Equal(p[6+i*6:12+i*6], mac) {
			t.Fatalf("mac repeat %d wrong", i)
		}
	}
	if _, err := BuildMagicPacket("garbage"); err == nil {
		t.Fatal("want error")
	}
}

// SendTo is the part of Send that can be observed: it must put the exact
// 102-byte magic packet on the wire.
func TestSendToDeliversTheMagicPacket(t *testing.T) {
	conn, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	const mac = "01:02:03:04:05:06"
	if err := SendTo(mac, conn.LocalAddr().String()); err != nil {
		t.Fatal(err)
	}

	buf := make([]byte, 256)
	if err := conn.SetReadDeadline(time.Now().Add(2 * time.Second)); err != nil {
		t.Fatal(err)
	}
	n, _, err := conn.ReadFrom(buf)
	if err != nil {
		t.Fatal(err)
	}
	want, err := BuildMagicPacket(mac)
	if err != nil {
		t.Fatal(err)
	}
	if got := buf[:n]; !bytes.Equal(got, want) {
		t.Errorf("received %d bytes, want the %d-byte magic packet", len(got), len(want))
	}
}

// A bad MAC fails before anything is sent, so a typo cannot put a malformed
// packet on the network.
func TestSendToRejectsBadMACBeforeDialing(t *testing.T) {
	if err := SendTo("not-a-mac", "127.0.0.1:1"); err == nil {
		t.Fatal("a malformed MAC must be an error")
	}
}

func TestBroadcastAddrIsTheDiscardPort(t *testing.T) {
	if BroadcastAddr != "255.255.255.255:9" {
		t.Errorf("BroadcastAddr = %q", BroadcastAddr)
	}
}
