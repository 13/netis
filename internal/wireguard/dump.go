package wireguard

import (
	"bufio"
	"bytes"
	"strconv"
	"strings"
	"time"
)

type Peer struct {
	PubKey        string
	Endpoint      string
	AllowedIPs    []string
	LastHandshake time.Time
}

func ParseDump(b []byte) ([]Peer, error) {
	var peers []Peer
	sc := bufio.NewScanner(bytes.NewReader(b))
	first := true
	for sc.Scan() {
		line := sc.Text()
		if first { // interface line
			first = false
			continue
		}
		f := strings.Split(line, "\t")
		if len(f) < 5 {
			continue
		}
		p := Peer{PubKey: f[0]}
		if f[2] != "(none)" {
			p.Endpoint = f[2]
		}
		if f[3] != "(none)" && f[3] != "" {
			p.AllowedIPs = strings.Split(f[3], ",")
		}
		if secs, err := strconv.ParseInt(f[4], 10, 64); err == nil && secs > 0 {
			p.LastHandshake = time.Unix(secs, 0)
		}
		peers = append(peers, p)
	}
	return peers, sc.Err()
}
