// Package oui names the maker of a network interface from its MAC address,
// using the IEEE MA-L, MA-M and MA-S registries (refresh with make oui).
package oui

import (
	"bufio"
	"bytes"
	"compress/gzip"
	_ "embed"
	"log/slog"
	"strings"
	"sync"

	"netis/internal/macaddr"
)

//go:embed oui.txt.gz
var registryGz []byte

// extras names prefixes the IEEE registry does not: virtual NICs whose
// locally administered prefixes are stable (see macaddr.IsPrivate).
var extras = map[string]string{
	"525400": "QEMU",
	"00155D": "Microsoft Hyper-V",
}

var (
	once     sync.Once
	registry map[string]string
)

func load() {
	registry = make(map[string]string, 60000)
	zr, err := gzip.NewReader(bytes.NewReader(registryGz))
	if err != nil {
		slog.Error("oui registry unreadable", "err", err)
		return
	}
	sc := bufio.NewScanner(zr)
	for sc.Scan() {
		k, v, ok := strings.Cut(sc.Text(), "\t")
		if ok {
			registry[k] = v
		}
	}
	for k, v := range extras {
		registry[k] = v
	}
}

// Vendor returns the short name of the maker the MAC's prefix is registered
// to, or "" when it is unknown or the MAC is randomized.
func Vendor(mac string) string {
	once.Do(load)
	if macaddr.IsPrivate(mac) {
		return ""
	}
	return lookup(registry, mac)
}

// lookup finds the longest registered prefix of mac in table: 36 bits
// (MA-S), then 28 (MA-M), then 24 (MA-L).
func lookup(table map[string]string, mac string) string {
	hex := strings.ToUpper(strings.NewReplacer(":", "", "-", "", ".", "").Replace(mac))
	for _, n := range []int{9, 7, 6} {
		if len(hex) < n {
			continue
		}
		if v, ok := table[hex[:n]]; ok {
			return v
		}
	}
	return ""
}
