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
	LogLevel      string
	AdminPassword string
	AuthDisabled  bool
}

// Load resolves config: flags win over env, env wins over defaults.
func Load(args []string, getenv func(string) string) (Config, error) {
	c := Config{Port: 8090, DBPath: "./data/crate.db", LogLevel: "info"}

	if v := getenv("CRATE_PORT"); v != "" {
		if p, err := strconv.Atoi(v); err == nil {
			c.Port = p
		}
	}
	if v := getenv("CRATE_DB"); v != "" {
		c.DBPath = v
	}
	if getenv("CRATE_DEV") == "1" {
		c.Dev = true
	}
	c.AdminPassword = getenv("CRATE_ADMIN_PASSWORD")
	if v := getenv("CRATE_AUTH_DISABLED"); v == "1" || v == "true" {
		c.AuthDisabled = true
	}

	fs := flag.NewFlagSet("crate", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.IntVar(&c.Port, "port", c.Port, "HTTP port")
	fs.StringVar(&c.DBPath, "db", c.DBPath, "SQLite path")
	fs.BoolVar(&c.Dev, "dev", c.Dev, "dev mode (proxy Vite)")
	fs.StringVar(&c.LogLevel, "log-level", c.LogLevel, "log level")
	if err := fs.Parse(args); err != nil {
		return Config{}, err
	}
	return c, nil
}
