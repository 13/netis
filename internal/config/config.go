package config

import "os"

type Config struct {
	Addr   string
	DBPath string
}

func Load() Config {
	c := Config{Addr: ":8080", DBPath: "netis.db"}
	if v := os.Getenv("NETIS_ADDR"); v != "" {
		c.Addr = v
	}
	if v := os.Getenv("NETIS_DB"); v != "" {
		c.DBPath = v
	}
	return c
}
