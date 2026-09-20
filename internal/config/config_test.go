package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadDotEnv(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".env")
	if err := os.WriteFile(path, []byte("PORT=9090\nDATABASE_URL='postgres://localhost/local_database'\n"), 0600); err != nil {
		t.Fatal(err)
	}
	lookup := func(string) (string, bool) { return "", false }
	cfg, err := load(path, lookup)
	if err != nil || cfg.Port != "9090" || cfg.DatabaseURL != "postgres://localhost/local_database" {
		t.Fatalf("file configuration was not loaded: %v", err)
	}

	cfg, err = load(path, func(key string) (string, bool) {
		if key == "DATABASE_URL" {
			return "postgres://localhost/environment_database", true
		}
		return "", false
	})
	if err != nil || cfg.DatabaseURL != "postgres://localhost/environment_database" || cfg.Port != "9090" {
		t.Fatalf("environment override failed: %v", err)
	}

	_, err = load(path, func(key string) (string, bool) { return "", key == "DATABASE_URL" })
	if err == nil {
		t.Fatal("explicit empty DATABASE_URL must not silently fall back to the file")
	}

	missing := filepath.Join(t.TempDir(), "missing.env")
	cfg, err = load(missing, func(key string) (string, bool) {
		return "postgres://localhost/environment_database", key == "DATABASE_URL"
	})
	if err != nil || cfg.Port != "8080" {
		t.Fatalf("environment-only startup failed: %v", err)
	}
	if _, err := load(missing, lookup); err == nil {
		t.Fatal("missing DATABASE_URL should fail")
	}

	if err := os.WriteFile(path, []byte("DATABASE_URL=\"secret-unclosed-value"), 0600); err != nil {
		t.Fatal(err)
	}
	_, err = load(path, lookup)
	if err == nil || strings.Contains(err.Error(), "secret") {
		t.Fatal("invalid dotenv must fail without exposing its contents")
	}
}

func TestParse(t *testing.T) {
	for _, tc := range []struct {
		name, port, database string
		wantErr              bool
	}{
		{"default port", "", "postgres://localhost/travel_planner", false},
		{"explicit port", "9090", "postgres://localhost/travel_planner", false},
		{"missing database", "8080", "", true},
		{"invalid port", "abc", "postgres://localhost/travel_planner", true},
		{"zero port", "0", "postgres://localhost/travel_planner", true},
		{"out of range", "65536", "postgres://localhost/travel_planner", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg, err := parse(func(key string) string {
				if key == "PORT" {
					return tc.port
				}
				return tc.database
			})
			if (err != nil) != tc.wantErr {
				t.Fatalf("unexpected error: %v", err)
			}
			if !tc.wantErr && tc.port == "" && cfg.Port != "8080" {
				t.Fatalf("default port: %s", cfg.Port)
			}
		})
	}
}
