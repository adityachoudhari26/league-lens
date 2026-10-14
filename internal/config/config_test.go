package config

import (
	"strings"
	"testing"
)

func TestLoadRequiresDatabaseWithoutEchoingSecrets(t *testing.T) {
	t.Setenv("DATABASE_URL", "")
	if _, err := Load(); err == nil {
		t.Fatal("missing database URL accepted")
	}
	t.Setenv("DATABASE_URL", "https://owner:private-cookie@host/db")
	_, err := Load()
	if err == nil {
		t.Fatal("non-Postgres URL accepted")
	}
	if strings.Contains(err.Error(), "private-cookie") {
		t.Fatal("error exposed secret")
	}
}

func TestLoadValidatesListenAddress(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://owner:secret@localhost/league_lens")
	for _, addr := range []string{"localhost", "localhost:0", "localhost:65536", "localhost:notaport"} {
		t.Setenv("HTTP_ADDR", addr)
		if _, err := Load(); err == nil {
			t.Errorf("accepted invalid address %q", addr)
		}
	}
	t.Setenv("HTTP_ADDR", "")
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.HTTPAddr != "127.0.0.1:8080" {
		t.Fatalf("unexpected default %q", cfg.HTTPAddr)
	}
}

func TestLocalOwnerSelection(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://owner:secret@localhost/league_lens")
	t.Setenv("HTTP_ADDR", "")
	t.Setenv("LOCAL_OWNER_EMAIL", "")
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.LocalOwnerEmail != "owner@localhost" {
		t.Fatal("wrong default import owner")
	}
	t.Setenv("LOCAL_OWNER_EMAIL", "custom@example.test")
	cfg, err = Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.LocalOwnerEmail != "custom@example.test" {
		t.Fatal("ignored configured owner")
	}
}
