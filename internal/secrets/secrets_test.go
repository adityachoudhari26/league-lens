package secrets

import (
	"bytes"
	"encoding/base64"
	"os"
	"path/filepath"
	"testing"
)

func TestBoxRoundTripAndAuthentication(t *testing.T) {
	key := bytes.Repeat([]byte{7}, keySize)
	box, err := NewBox(key)
	if err != nil {
		t.Fatal(err)
	}
	plaintext := []byte(`{"s2":"synthetic-cookie","swid":"{synthetic-id}"}`)
	associatedData := []byte("synthetic-connection-id")

	sealed, err := box.Seal(plaintext, associatedData)
	if err != nil {
		t.Fatal(err)
	}
	opened, err := box.Open(sealed, associatedData)
	if err != nil || !bytes.Equal(opened, plaintext) {
		t.Fatalf("Open() = %q, %v", opened, err)
	}
	if _, err = box.Open(sealed, []byte("another-connection-id")); err == nil {
		t.Fatal("accepted ciphertext with different associated data")
	}
	sealed[len(sealed)-1] ^= 1
	if _, err = box.Open(sealed, associatedData); err == nil {
		t.Fatal("accepted tampered ciphertext")
	}
}

func TestLoadOrCreateKeyCreatesPrivateFileAndReusesKey(t *testing.T) {
	unsetSecretKeyEnv(t)
	dataDir := filepath.Join(t.TempDir(), "app-data")

	first, err := LoadOrCreateKey(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	second, err := LoadOrCreateKey(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(first) != keySize || !bytes.Equal(first, second) {
		t.Fatal("key was not reused")
	}
	dirInfo, err := os.Stat(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	if dirInfo.Mode().Perm() != 0700 {
		t.Fatalf("data directory mode = %04o", dirInfo.Mode().Perm())
	}
	fileInfo, err := os.Stat(filepath.Join(dataDir, "secret.key"))
	if err != nil {
		t.Fatal(err)
	}
	if fileInfo.Mode().Perm() != 0600 {
		t.Fatalf("key file mode = %04o", fileInfo.Mode().Perm())
	}
}

func TestLoadOrCreateKeyUsesStandardBase64Environment(t *testing.T) {
	expected := bytes.Repeat([]byte{9}, keySize)
	t.Setenv("LEAGUE_LENS_SECRET_KEY", base64.StdEncoding.EncodeToString(expected))
	got, err := LoadOrCreateKey(t.TempDir())
	if err != nil || !bytes.Equal(got, expected) {
		t.Fatalf("environment key was not loaded: %v", err)
	}
}

func TestLoadOrCreateKeyRejectsInvalidEnvironmentKey(t *testing.T) {
	for _, encoded := range []string{"!", base64.StdEncoding.EncodeToString([]byte("short"))} {
		t.Run("invalid", func(t *testing.T) {
			t.Setenv("LEAGUE_LENS_SECRET_KEY", encoded)
			if _, err := LoadOrCreateKey(t.TempDir()); err == nil {
				t.Fatal("invalid environment key accepted")
			}
		})
	}
}

func unsetSecretKeyEnv(t *testing.T) {
	t.Helper()
	value, exists := os.LookupEnv("LEAGUE_LENS_SECRET_KEY")
	if err := os.Unsetenv("LEAGUE_LENS_SECRET_KEY"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if exists {
			_ = os.Setenv("LEAGUE_LENS_SECRET_KEY", value)
		} else {
			_ = os.Unsetenv("LEAGUE_LENS_SECRET_KEY")
		}
	})
}
