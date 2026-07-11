package wol

import (
	"bytes"
	"testing"
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
