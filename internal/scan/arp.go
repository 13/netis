package scan

import (
	"bufio"
	"io"
	"os"
	"strconv"
	"strings"
)

// atfCom is the kernel's ATF_COM flag: the entry holds a resolved MAC. Entries
// without it (incomplete or failed resolutions) are not evidence of a host.
const atfCom = 0x2

// ParseARPTable returns the resolved (ATF_COM) entries of /proc/net/arp as an
// IP to lower-case MAC map.
func ParseARPTable(r io.Reader) map[string]string {
	out := make(map[string]string)
	sc := bufio.NewScanner(r)
	first := true
	for sc.Scan() {
		if first { // header line
			first = false
			continue
		}
		f := strings.Fields(sc.Text())
		if len(f) < 4 {
			continue
		}
		ip, flags, mac := f[0], f[2], strings.ToLower(f[3])
		fl, err := strconv.ParseUint(strings.TrimPrefix(flags, "0x"), 16, 32)
		if err != nil || fl&atfCom == 0 || mac == "00:00:00:00:00:00" {
			continue
		}
		out[ip] = mac
	}
	return out
}

func ReadARPTable() (map[string]string, error) {
	f, err := os.Open("/proc/net/arp")
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return ParseARPTable(f), nil
}
