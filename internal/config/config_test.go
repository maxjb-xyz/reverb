package config

import "testing"

func TestLoadDefaults(t *testing.T) {
	c, err := Load(nil, func(string) string { return "" })
	if err != nil {
		t.Fatal(err)
	}
	if c.Port != 8090 || c.DBPath != "./data/reverb.db" || c.Dev {
		t.Fatalf("unexpected defaults: %+v", c)
	}
}

func TestLoadFlagsOverrideDefaults(t *testing.T) {
	c, err := Load([]string{"--port", "9000", "--dev"}, func(string) string { return "" })
	if err != nil {
		t.Fatal(err)
	}
	if c.Port != 9000 || !c.Dev {
		t.Fatalf("flags not applied: %+v", c)
	}
}

func TestEnvFillsPortWhenNoFlag(t *testing.T) {
	env := map[string]string{"REVERB_PORT": "7000", "REVERB_ADMIN_PASSWORD": "secret"}
	c, err := Load(nil, func(k string) string { return env[k] })
	if err != nil {
		t.Fatal(err)
	}
	if c.Port != 7000 || c.AdminPassword != "secret" {
		t.Fatalf("env not applied: %+v", c)
	}
}
