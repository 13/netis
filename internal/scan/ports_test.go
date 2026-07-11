package scan

import (
	"context"
	"net"
	"strconv"
	"testing"
	"time"
)

func TestPortScanFindsListener(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	_, portStr, _ := net.SplitHostPort(ln.Addr().String())
	port, _ := strconv.Atoi(portStr)

	open := PortScan(context.Background(), "127.0.0.1", []int{port, port + 1}, 500*time.Millisecond)
	if len(open) != 1 || open[0] != port {
		t.Fatalf("open=%v want [%d]", open, port)
	}
}

func TestServiceGuess(t *testing.T) {
	if ServiceGuess(22) != "ssh" || ServiceGuess(8006) != "proxmox" {
		t.Fatal("guess broken")
	}
	if ServiceGuess(59999) != "" {
		t.Fatal("unknown port must be empty")
	}
}
