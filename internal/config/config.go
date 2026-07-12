package config

import (
	"flag"
	"io"
	"strconv"
)

type Config struct {
	Port          int
	DBPath        string
	Dev           bool
	AdminPassword string
}

// Load resolves config: flags win over env, env wins over defaults.
func Load(args []string, getenv func(string) string) (Config, error) {
	c := Config{Port: 8090, DBPath: "./data/reverb.db"}

	if v := getenv("REVERB_PORT"); v != "" {
		if p, err := strconv.Atoi(v); err == nil {
			c.Port = p
		}
	}
	if v := getenv("REVERB_DB"); v != "" {
		c.DBPath = v
	}
	if getenv("REVERB_DEV") == "1" {
		c.Dev = true
	}
	c.AdminPassword = getenv("REVERB_ADMIN_PASSWORD")

	fs := flag.NewFlagSet("reverb", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.IntVar(&c.Port, "port", c.Port, "HTTP port")
	fs.StringVar(&c.DBPath, "db", c.DBPath, "SQLite path")
	fs.BoolVar(&c.Dev, "dev", c.Dev, "dev mode (proxy Vite)")
	if err := fs.Parse(args); err != nil {
		return Config{}, err
	}
	return c, nil
}
