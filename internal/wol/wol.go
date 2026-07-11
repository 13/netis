// Package wol builds and sends IEEE 802.3 Wake-on-LAN magic packets.
package wol

import (
	"bytes"
	"fmt"
	"net"
)

// BuildMagicPacket builds the 102-byte WoL magic packet for mac: six 0xFF
// bytes followed by the 6-byte MAC address repeated sixteen times.
func BuildMagicPacket(mac string) ([]byte, error) {
	hw, err := net.ParseMAC(mac)
	if err != nil || len(hw) != 6 {
		return nil, fmt.Errorf("bad mac %q: %w", mac, err)
	}
	var b bytes.Buffer
	b.Write(bytes.Repeat([]byte{0xFF}, 6))
	for i := 0; i < 16; i++ {
		b.Write(hw)
	}
	return b.Bytes(), nil
}

// Send builds a magic packet for mac and broadcasts it via UDP to the
// local subnet broadcast address on port 9 (the discard port).
func Send(mac string) error {
	pkt, err := BuildMagicPacket(mac)
	if err != nil {
		return err
	}
	conn, err := net.Dial("udp", "255.255.255.255:9")
	if err != nil {
		return err
	}
	defer conn.Close()
	_, err = conn.Write(pkt)
	return err
}
