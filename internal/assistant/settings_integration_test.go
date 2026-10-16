//go:build integration

package assistant

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"connectrpc.com/connect/v2"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"
	"google.golang.org/protobuf/encoding/protojson"

	migrations "github.com/adityachoudhari26/league-lens/db"
	"github.com/adityachoudhari26/league-lens/internal/database"
	v1 "github.com/adityachoudhari26/league-lens/internal/gen/leaguelens/v1"
	"github.com/adityachoudhari26/league-lens/internal/llm"
	"github.com/adityachoudhari26/league-lens/internal/secrets"
)

type settingsTestProvider struct {
	models []string
	err    error
}

func (p settingsTestProvider) ListModels(ctx context.Context) ([]string, error) {
	if _, ok := ctx.Deadline(); !ok {
		return nil, errors.New("model listing has no timeout")
	}
	return p.models, p.err
}

func (settingsTestProvider) Complete(context.Context, llm.Request, func(string)) (llm.Response, error) {
	return llm.Response{}, nil
}

func openSettingsTestDatabase(t *testing.T) *database.DB {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Fatal("TEST_DATABASE_URL required")
	}
	cfg, err := pgx.ParseConfig(dsn)
	if err != nil {
		t.Fatal("invalid test database URL")
	}
	admin := stdlib.OpenDB(*cfg)
	t.Cleanup(func() { _ = admin.Close() })
	schema := fmt.Sprintf("assistant_settings_%d", time.Now().UnixNano())
	if _, err := admin.Exec("CREATE SCHEMA " + schema); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = admin.Exec("DROP SCHEMA " + schema + " CASCADE") })
	cfg.RuntimeParams["search_path"] = schema
	migrationDB := stdlib.OpenDB(*cfg)
	t.Cleanup(func() { _ = migrationDB.Close() })
	if err := migrations.Migrate(context.Background(), migrationDB, "up"); err != nil {
		t.Fatal(err)
	}
	parsed, err := url.Parse(dsn)
	if err != nil {
		t.Fatal("invalid test database URL")
	}
	query := parsed.Query()
	query.Set("search_path", schema)
	parsed.RawQuery = query.Encode()
	db, err := database.Open(context.Background(), parsed.String())
	if err != nil {
		t.Fatal("could not open test database")
	}
	t.Cleanup(db.Close)
	return db
}

func TestAssistantSettingsEncryptionReuseAndRemoval(t *testing.T) {
	ctx := context.Background()
	db := openSettingsTestDatabase(t)
	box, err := secrets.NewBox(bytes.Repeat([]byte{7}, 32))
	if err != nil {
		t.Fatal(err)
	}
	var configurations []llm.Config
	service := &Service{
		DB: db, Box: box, OwnerEmail: "assistant@example.test",
		NewProvider: func(config llm.Config) (llm.Provider, error) {
			configurations = append(configurations, config)
			return settingsTestProvider{models: []string{"fake-model"}}, nil
		},
	}

	saved, err := service.SaveAssistantSettings(ctx, &v1.SaveAssistantSettingsRequest{
		Provider: v1.AssistantProvider_ASSISTANT_PROVIDER_ANTHROPIC,
		Model:    "model-not-listed", ApiKey: "sk-test-secret1234",
	})
	if err != nil {
		t.Fatal(err)
	}
	if saved.Settings.GetApiKeyHint() != "1234" || saved.Settings.GetModel() != "model-not-listed" {
		t.Fatalf("saved settings = %+v", saved.Settings)
	}
	owner, err := db.Queries().GetUserByEmail(ctx, service.OwnerEmail)
	if err != nil {
		t.Fatal(err)
	}
	row, err := db.Queries().GetAssistantSettings(ctx, owner.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(row.EncryptedApiKey) == 0 || bytes.Contains(row.EncryptedApiKey, []byte("sk-test-secret1234")) {
		t.Fatal("API key was not encrypted")
	}
	decrypted, err := box.Open(row.EncryptedApiKey, owner.ID.Bytes[:])
	if err != nil || string(decrypted) != "sk-test-secret1234" {
		t.Fatalf("decrypted key = %q, %v", decrypted, err)
	}
	response, err := service.GetAssistantSettings(ctx, &v1.GetAssistantSettingsRequest{})
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := protojson.Marshal(response)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), "sk-test-secret1234") || response.Settings.GetApiKeyHint() != "1234" {
		t.Fatalf("safe settings response = %s", encoded)
	}

	_, err = service.SaveAssistantSettings(ctx, &v1.SaveAssistantSettingsRequest{
		Provider: v1.AssistantProvider_ASSISTANT_PROVIDER_ANTHROPIC,
		Model:    "another-model",
	})
	if err != nil {
		t.Fatal(err)
	}
	if configurations[1].APIKey != "sk-test-secret1234" {
		t.Fatal("same-provider save did not reuse the saved key")
	}

	openAI := v1.AssistantProvider_ASSISTANT_PROVIDER_OPENAI_COMPATIBLE
	baseURL := "http://localhost:11434/v1"
	_, err = service.SaveAssistantSettings(ctx, &v1.SaveAssistantSettingsRequest{
		Provider: openAI, BaseUrl: baseURL, Model: "local-model", ApiKey: "sk-test-openai5678",
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = service.SaveAssistantSettings(ctx, &v1.SaveAssistantSettingsRequest{
		Provider: openAI, BaseUrl: baseURL, Model: "local-model",
	})
	if err != nil {
		t.Fatal(err)
	}
	if configurations[3].APIKey != "sk-test-openai5678" {
		t.Fatal("same base URL did not reuse the saved OpenAI-compatible key")
	}
	_, err = service.SaveAssistantSettings(ctx, &v1.SaveAssistantSettingsRequest{
		Provider: openAI, BaseUrl: "http://localhost:11434/other", Model: "local-model",
	})
	if err != nil {
		t.Fatal(err)
	}
	if configurations[4].APIKey != "" {
		t.Fatal("changed base URL received the previously saved key")
	}
	row, err = db.Queries().GetAssistantSettings(ctx, owner.ID)
	if err != nil || row.ApiKeyHint != "" || row.EncryptedApiKey != nil {
		t.Fatalf("changed base URL retained credentials: %+v, %v", row, err)
	}
	if _, err = service.DeleteAssistantSettings(ctx, &v1.DeleteAssistantSettingsRequest{}); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Queries().GetAssistantSettings(ctx, owner.ID); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("settings after deletion: %v", err)
	}
}

func TestUnauthorizedProviderDoesNotSaveSettings(t *testing.T) {
	ctx := context.Background()
	db := openSettingsTestDatabase(t)
	box, err := secrets.NewBox(bytes.Repeat([]byte{8}, 32))
	if err != nil {
		t.Fatal(err)
	}
	service := &Service{
		DB: db, Box: box, OwnerEmail: "unauthorized@example.test",
		NewProvider: func(llm.Config) (llm.Provider, error) {
			return settingsTestProvider{err: llm.ErrUnauthorized}, nil
		},
	}
	_, err = service.SaveAssistantSettings(ctx, &v1.SaveAssistantSettingsRequest{
		Provider: v1.AssistantProvider_ASSISTANT_PROVIDER_ANTHROPIC,
		Model:    "claude-test", ApiKey: "sk-test-invalid",
	})
	var connectErr *connect.Error
	if !errors.As(err, &connectErr) || connectErr.Code() != connect.CodePermissionDenied {
		t.Fatalf("save error = %v", err)
	}
	if _, err := db.Queries().GetUserByEmail(ctx, service.OwnerEmail); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("unauthorized save created an owner: %v", err)
	}
}
