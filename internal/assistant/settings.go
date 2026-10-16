package assistant

import (
	"context"
	"errors"
	"time"

	"connectrpc.com/connect/v2"
	"github.com/jackc/pgx/v5"

	"github.com/adityachoudhari26/league-lens/internal/database"
	"github.com/adityachoudhari26/league-lens/internal/fantasy"
	v1 "github.com/adityachoudhari26/league-lens/internal/gen/leaguelens/v1"
	"github.com/adityachoudhari26/league-lens/internal/leaguesync"
	"github.com/adityachoudhari26/league-lens/internal/llm"
	"github.com/adityachoudhari26/league-lens/internal/secrets"
	"github.com/adityachoudhari26/league-lens/internal/store"
)

const providerValidationTimeout = 10 * time.Second

type NewProvider func(llm.Config) (llm.Provider, error)

type resolvedKey struct {
	Plaintext  string
	ReuseSaved bool
}

type providerRequest struct {
	provider string
	baseURL  string
	key      resolvedKey
	saved    store.AssistantSetting
}

type providerRequestParams struct {
	provider v1.AssistantProvider
	baseURL  string
	apiKey   string
	model    *string
}

type settingsToSave struct {
	request providerRequest
	model   string
}

// Service stores one encrypted provider configuration for the configured owner.
type Service struct {
	DB              *database.DB
	Box             *secrets.Box
	OwnerEmail      string
	NewProvider     NewProvider
	Dashboard       assistantDashboard
	Sync            assistantSync
	ProviderSources interface {
		SourceFor(leaguesync.Target) (fantasy.Provider, error)
	}
	GameContext GameContextSource
}

// GetAssistantSettings returns the owner's safe-to-display settings, never the key.
func (s *Service) GetAssistantSettings(ctx context.Context, _ *v1.GetAssistantSettingsRequest) (*v1.GetAssistantSettingsResponse, error) {
	settings, found, err := s.loadSettings(ctx)
	if err != nil {
		return nil, internalError("assistant settings could not be loaded")
	}
	if !found {
		return &v1.GetAssistantSettingsResponse{}, nil
	}
	return &v1.GetAssistantSettingsResponse{Settings: toProtoSettings(settings)}, nil
}

// ListAssistantModels validates temporary provider credentials without saving them.
func (s *Service) ListAssistantModels(ctx context.Context, request *v1.ListAssistantModelsRequest) (*v1.ListAssistantModelsResponse, error) {
	if request == nil {
		return nil, invalidArgument("provider settings are required")
	}
	provider, _, err := s.providerForRequest(ctx, providerRequestParams{
		provider: request.GetProvider(),
		baseURL:  request.GetBaseUrl(),
		apiKey:   request.GetApiKey(),
	})
	if err != nil {
		return nil, err
	}
	models, err := s.listModels(ctx, provider)
	if err != nil {
		return nil, mapProviderError(err)
	}
	return &v1.ListAssistantModelsResponse{Models: models}, nil
}

// SaveAssistantSettings validates the effective credentials with the provider before persisting.
func (s *Service) SaveAssistantSettings(ctx context.Context, request *v1.SaveAssistantSettingsRequest) (*v1.SaveAssistantSettingsResponse, error) {
	if request == nil {
		return nil, invalidArgument("provider settings are required")
	}
	model := request.GetModel()
	provider, providerRequest, err := s.providerForRequest(ctx, providerRequestParams{
		provider: request.GetProvider(),
		baseURL:  request.GetBaseUrl(),
		apiKey:   request.GetApiKey(),
		model:    &model,
	})
	if err != nil {
		return nil, err
	}
	if _, err := s.listModels(ctx, provider); err != nil {
		return nil, mapProviderError(err)
	}

	settings, err := s.saveSettings(ctx, settingsToSave{
		request: providerRequest,
		model:   model,
	})
	if err != nil {
		return nil, internalError("assistant settings could not be saved")
	}
	return &v1.SaveAssistantSettingsResponse{Settings: toProtoSettings(settings)}, nil
}

// DeleteAssistantSettings removes only the configured owner's provider credentials.
func (s *Service) DeleteAssistantSettings(ctx context.Context, _ *v1.DeleteAssistantSettingsRequest) (*v1.DeleteAssistantSettingsResponse, error) {
	err := s.DB.InTx(ctx, func(queries *store.Queries) error {
		owner, err := queries.GetUserByEmail(ctx, s.OwnerEmail)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		return queries.DeleteAssistantSettings(ctx, owner.ID)
	})
	if err != nil {
		return nil, internalError("assistant settings could not be removed")
	}
	return &v1.DeleteAssistantSettingsResponse{}, nil
}

func (s *Service) loadSettings(ctx context.Context) (store.AssistantSetting, bool, error) {
	var settings store.AssistantSetting
	var found bool
	err := s.DB.InReadTx(ctx, func(queries *store.Queries) error {
		owner, err := queries.GetUserByEmail(ctx, s.OwnerEmail)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		settings, err = queries.GetAssistantSettings(ctx, owner.ID)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		found = true
		return nil
	})
	return settings, found, err
}

// providerForRequest validates provider settings, resolves the effective key, and builds a client.
func (s *Service) providerForRequest(
	ctx context.Context,
	params providerRequestParams,
) (llm.Provider, providerRequest, error) {
	providerName, err := providerName(params.provider)
	if err != nil {
		return nil, providerRequest{}, err
	}
	if params.model != nil && *params.model == "" {
		return nil, providerRequest{}, invalidArgument("a model is required")
	}
	if err := validateProviderSettings(providerName, params.baseURL); err != nil {
		return nil, providerRequest{}, err
	}
	saved, found, err := s.loadSettings(ctx)
	if err != nil {
		return nil, providerRequest{}, internalError("assistant settings could not be loaded")
	}
	key, err := s.resolveKey(params.apiKey, providerName, params.baseURL, saved, found)
	if err != nil {
		return nil, providerRequest{}, err
	}
	providerClient, err := s.newProvider(llm.Config{
		Provider: providerName, BaseURL: params.baseURL, APIKey: key.Plaintext,
	})
	if err != nil {
		return nil, providerRequest{}, invalidArgument("provider settings are invalid")
	}
	return providerClient, providerRequest{
		provider: providerName,
		baseURL:  params.baseURL,
		key:      key,
		saved:    saved,
	}, nil
}

// resolveKey resolves an explicit or saved API key.
// An empty key reuses the saved key only when provider and base URL both match the saved settings, so a changed base URL never receives the saved key.
func (s *Service) resolveKey(requestKey, provider, baseURL string, saved store.AssistantSetting, found bool) (resolvedKey, error) {
	if requestKey != "" {
		return resolvedKey{Plaintext: requestKey}, nil
	}
	if found && saved.Provider == provider && saved.BaseUrl == baseURL {
		resolved := resolvedKey{ReuseSaved: true}
		if len(saved.EncryptedApiKey) == 0 {
			return resolved, nil
		}
		key, err := s.Box.Open(saved.EncryptedApiKey, saved.UserID.Bytes[:])
		if err != nil {
			return resolvedKey{}, internalError("saved assistant key could not be read")
		}
		resolved.Plaintext = string(key)
		return resolved, nil
	}
	if provider == llm.ProviderAnthropic {
		return resolvedKey{}, invalidArgument("an Anthropic API key is required")
	}
	return resolvedKey{}, nil
}

func (s *Service) saveSettings(ctx context.Context, params settingsToSave) (store.AssistantSetting, error) {
	var settings store.AssistantSetting
	err := s.DB.InTx(ctx, func(queries *store.Queries) error {
		owner, err := queries.GetUserByEmail(ctx, s.OwnerEmail)
		if errors.Is(err, pgx.ErrNoRows) {
			owner, err = queries.CreateUser(ctx, s.OwnerEmail)
		}
		if err != nil {
			return err
		}
		key := params.request.key
		var encryptedKey []byte
		var hint string
		if key.ReuseSaved {
			encryptedKey = params.request.saved.EncryptedApiKey
			hint = params.request.saved.ApiKeyHint
		}
		if !key.ReuseSaved && key.Plaintext != "" {
			encryptedKey, err = s.Box.Seal([]byte(key.Plaintext), owner.ID.Bytes[:])
			if err != nil {
				return err
			}
			hint = lastCharacters(key.Plaintext, 4)
		}
		err = queries.UpsertAssistantSettings(ctx, store.UpsertAssistantSettingsParams{
			UserID: owner.ID, Provider: params.request.provider, BaseUrl: params.request.baseURL, Model: params.model,
			EncryptedApiKey: encryptedKey, ApiKeyHint: hint,
		})
		if err != nil {
			return err
		}
		settings = store.AssistantSetting{
			UserID: owner.ID, Provider: params.request.provider, BaseUrl: params.request.baseURL, Model: params.model,
			EncryptedApiKey: encryptedKey, ApiKeyHint: hint,
		}
		return nil
	})
	return settings, err
}

func (s *Service) listModels(ctx context.Context, provider llm.Provider) ([]string, error) {
	timeoutCtx, cancel := context.WithTimeout(ctx, providerValidationTimeout)
	defer cancel()
	return provider.ListModels(timeoutCtx)
}

func (s *Service) newProvider(config llm.Config) (llm.Provider, error) {
	if s.NewProvider != nil {
		return s.NewProvider(config)
	}
	return llm.New(config, nil)
}

func providerName(provider v1.AssistantProvider) (string, error) {
	switch provider {
	case v1.AssistantProvider_ASSISTANT_PROVIDER_ANTHROPIC:
		return llm.ProviderAnthropic, nil
	case v1.AssistantProvider_ASSISTANT_PROVIDER_OPENAI_COMPATIBLE:
		return llm.ProviderOpenAICompatible, nil
	default:
		return "", invalidArgument("an AI provider is required")
	}
}

func validateProviderSettings(provider, baseURL string) error {
	switch provider {
	case llm.ProviderAnthropic:
		if baseURL != "" {
			return invalidArgument("Anthropic does not use a custom base URL")
		}
	case llm.ProviderOpenAICompatible:
		if err := llm.ValidateBaseURL(baseURL); err != nil {
			return invalidArgument("OpenAI-compatible providers require an http(s) base URL without a trailing slash")
		}
	default:
		return invalidArgument("an AI provider is required")
	}
	return nil
}

func lastCharacters(value string, count int) string {
	characters := []rune(value)
	if len(characters) <= count {
		return value
	}
	return string(characters[len(characters)-count:])
}

func toProtoSettings(settings store.AssistantSetting) *v1.AssistantSettings {
	provider := v1.AssistantProvider_ASSISTANT_PROVIDER_OPENAI_COMPATIBLE
	if settings.Provider == llm.ProviderAnthropic {
		provider = v1.AssistantProvider_ASSISTANT_PROVIDER_ANTHROPIC
	}
	return &v1.AssistantSettings{
		Provider: provider, BaseUrl: settings.BaseUrl, Model: settings.Model, ApiKeyHint: settings.ApiKeyHint,
	}
}

func mapProviderError(err error) error {
	if errors.Is(err, llm.ErrUnauthorized) {
		return connect.NewError(connect.CodePermissionDenied, "provider rejected the API key")
	}
	if errors.Is(err, llm.ErrUnavailable) {
		return connect.NewError(connect.CodeUnavailable, "provider is unavailable")
	}
	return connect.NewError(connect.CodeUnavailable, "provider request failed")
}

func invalidArgument(message string) error {
	return connect.NewError(connect.CodeInvalidArgument, message)
}

func internalError(message string) error {
	return connect.NewError(connect.CodeInternal, message)
}
