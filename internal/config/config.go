package config

import (
	"errors"
	"net"
	"net/url"
	"os"
	"strconv"
)

type Config struct {
	HTTPAddr        string
	DatabaseURL     string
	LocalOwnerEmail string
	MigrateOnStart  bool
}

func Load() (Config, error) {
	cfg := Config{
		LocalOwnerEmail: os.Getenv("LOCAL_OWNER_EMAIL"),
		HTTPAddr:        os.Getenv("HTTP_ADDR"),
		DatabaseURL:     os.Getenv("DATABASE_URL"),
		MigrateOnStart:  os.Getenv("MIGRATE_ON_START") == "true",
	}
	if cfg.LocalOwnerEmail == "" {
		cfg.LocalOwnerEmail = "owner@localhost"
	}
	if cfg.HTTPAddr == "" {
		cfg.HTTPAddr = "127.0.0.1:8080"
	}
	if cfg.DatabaseURL == "" {
		return Config{}, errors.New("DATABASE_URL is required")
	}
	u, err := url.Parse(cfg.DatabaseURL)
	if err != nil || (u.Scheme != "postgres" && u.Scheme != "postgresql") || u.Hostname() == "" || u.Path == "" || u.Path == "/" {
		return Config{}, errors.New("DATABASE_URL must be a postgres:// or postgresql:// URL with host and database")
	}
	_, port, err := net.SplitHostPort(cfg.HTTPAddr)
	if err != nil {
		return Config{}, errors.New("HTTP_ADDR must contain host:port")
	}
	n, err := strconv.Atoi(port)
	if err != nil || n < 1 || n > 65535 {
		return Config{}, errors.New("HTTP_ADDR port must be between 1 and 65535")
	}
	return cfg, nil
}
