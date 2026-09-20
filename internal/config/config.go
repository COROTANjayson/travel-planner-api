package config

import (
	"errors"
	"net/url"
	"os"
	"strconv"
	"strings"

	"github.com/joho/godotenv"
)

type Config struct {
	Port           string
	DatabaseURL    string
	Auth0IssuerURL string
	Auth0Audience  string
}

func Load() (Config, error) {
	return load(".env", os.LookupEnv)
}

func load(path string, lookupEnv func(string) (string, bool)) (Config, error) {
	values, err := godotenv.Read(path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		// Parser errors may contain credentials from the file; keep them private.
		return Config{}, errors.New("could not read .env; check its syntax and permissions")
	}
	return parse(func(key string) string {
		if value, exists := lookupEnv(key); exists {
			return value
		}
		return values[key]
	})
}

func parse(getenv func(string) string) (Config, error) {
	c := Config{
		Port:           strings.TrimSpace(getenv("PORT")),
		DatabaseURL:    strings.TrimSpace(getenv("DATABASE_URL")),
		Auth0IssuerURL: strings.TrimSpace(getenv("AUTH0_ISSUER_URL")),
		Auth0Audience:  strings.TrimSpace(getenv("AUTH0_AUDIENCE")),
	}
	if c.Port == "" {
		c.Port = "8080"
	}
	port, err := strconv.Atoi(c.Port)
	if err != nil || port < 1 || port > 65535 {
		return Config{}, errors.New("PORT must be an integer between 1 and 65535")
	}
	if c.DatabaseURL == "" {
		return Config{}, errors.New("DATABASE_URL is required")
	}
	issuer, err := url.Parse(c.Auth0IssuerURL)
	if err != nil || issuer.Scheme != "https" || issuer.Host == "" || issuer.RawQuery != "" || issuer.Fragment != "" || !strings.HasSuffix(c.Auth0IssuerURL, "/") {
		return Config{}, errors.New("AUTH0_ISSUER_URL must be an HTTPS URL ending in /")
	}
	if c.Auth0Audience == "" {
		return Config{}, errors.New("AUTH0_AUDIENCE is required")
	}
	return c, nil
}
