package scan

import (
	"bufio"
	"bytes"
	_ "embed"
	"strings"
	"sync"
)

//go:embed oui_data.txt
var ouiRaw []byte

var (
	ouiOnce sync.Once
	ouiMap  map[string]string
)

func Vendor(mac string) string {
	ouiOnce.Do(func() {
		ouiMap = make(map[string]string)
		sc := bufio.NewScanner(bytes.NewReader(ouiRaw))
		for sc.Scan() {
			parts := strings.SplitN(sc.Text(), "\t", 2)
			if len(parts) == 2 {
				ouiMap[strings.ToLower(parts[0])] = parts[1]
			}
		}
	})
	clean := strings.ToLower(strings.NewReplacer(":", "", "-", "").Replace(mac))
	if len(clean) < 6 {
		return ""
	}
	return ouiMap[clean[:6]]
}
