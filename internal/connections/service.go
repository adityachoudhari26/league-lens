package connections

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"connectrpc.com/connect/v2"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/adityachoudhari26/league-lens/internal/database"
	"github.com/adityachoudhari26/league-lens/internal/espn"
	"github.com/adityachoudhari26/league-lens/internal/fantasy"
	v1 "github.com/adityachoudhari26/league-lens/internal/gen/leaguelens/v1"
	"github.com/adityachoudhari26/league-lens/internal/importer"
	"github.com/adityachoudhari26/league-lens/internal/leaguesync"
	"github.com/adityachoudhari26/league-lens/internal/secrets"
	"github.com/adityachoudhari26/league-lens/internal/sleeper"
	"github.com/adityachoudhari26/league-lens/internal/store"
)

type DiscoverESPNFunc func(context.Context, espn.Credentials) ([]fantasy.DiscoveredLeague, error)
type DiscoverSleeperFunc func(context.Context, string, int) (string, []fantasy.DiscoveredLeague, error)
type NewSourceFunc func(context.Context, ImportSpec) (PreparedSource, error)

// ImportStarter finishes begun imports in the background.
type ImportStarter interface {
	StartImports([]leaguesync.ImportJob)
}

// Service manages the configured owner's provider connections and league imports.
type Service struct {
	DB              *database.DB
	Box             *secrets.Box
	OwnerEmail      string
	CatalogPath     string
	DiscoverESPN    DiscoverESPNFunc
	DiscoverSleeper DiscoverSleeperFunc
	NewSource       NewSourceFunc
	// Imports finishes begun imports in the background; nil drops the jobs, for tests that only check the quick phase.
	Imports ImportStarter
}

func (s *Service) withDefaults() Service {
	service := *s
	if service.DiscoverESPN == nil {
		service.DiscoverESPN = func(ctx context.Context, credentials espn.Credentials) ([]fantasy.DiscoveredLeague, error) {
			return espn.DiscoverLeagues(ctx, http.DefaultClient, credentials)
		}
	}
	if service.DiscoverSleeper == nil {
		service.DiscoverSleeper = func(ctx context.Context, username string, season int) (string, []fantasy.DiscoveredLeague, error) {
			return sleeper.NewClient(http.DefaultClient).DiscoverLeagues(ctx, username, season)
		}
	}
	if service.NewSource == nil {
		service.NewSource = PrepareSource
	}
	return service
}

type storedESPNCredentials struct {
	S2   string `json:"s2"`
	SWID string `json:"swid"`
}

// ConnectESPN validates cookies through account discovery before storing them encrypted.
func (s *Service) ConnectESPN(ctx context.Context, request *v1.ConnectESPNRequest) (*v1.ConnectESPNResponse, error) {
	service := s.withDefaults()
	if request == nil || request.GetS2() == "" || request.GetSwid() == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, "ESPN cookies are required")
	}
	credentials := espn.Credentials{S2: request.GetS2(), SWID: request.GetSwid()}
	discovered, err := service.DiscoverESPN(ctx, credentials)
	if errors.Is(err, espn.ErrUnauthorized) {
		return nil, connect.NewError(connect.CodePermissionDenied, "ESPN rejected these cookies")
	}
	if err != nil {
		return nil, connect.NewError(connect.CodeUnavailable, "ESPN league discovery failed")
	}
	if len(discovered) == 0 {
		return nil, connect.NewError(connect.CodeFailedPrecondition, "No ESPN football leagues found for this account")
	}
	accountID := espn.NewClient(nil, credentials, fantasy.CurrentSeason(time.Now())).AccountID()
	connection, ownerID, err := service.saveConnection(ctx, fantasy.ProviderESPN, accountID, credentials)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, "connection could not be saved")
	}
	leagues, err := service.markImported(ctx, ownerID, discovered)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, "discovered leagues could not be listed")
	}
	return &v1.ConnectESPNResponse{ConnectionId: connection.ID.String(), Leagues: leagues}, nil
}

// ConnectSleeper resolves an account's NFL leagues before persisting the account link.
func (s *Service) ConnectSleeper(ctx context.Context, request *v1.ConnectSleeperRequest) (*v1.ConnectSleeperResponse, error) {
	service := s.withDefaults()
	if request == nil || request.GetUsername() == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, "Sleeper username is required")
	}
	season := fantasy.CurrentSeason(time.Now())
	accountID, discovered, err := service.DiscoverSleeper(ctx, request.GetUsername(), season)
	if errors.Is(err, sleeper.ErrUserNotFound) {
		return nil, connect.NewError(connect.CodeNotFound, "Sleeper user not found")
	}
	if err != nil || accountID == "" {
		return nil, connect.NewError(connect.CodeUnavailable, "Sleeper league discovery failed")
	}
	connection, ownerID, err := service.saveConnection(ctx, fantasy.ProviderSleeper, accountID, espn.Credentials{})
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, "connection could not be saved")
	}
	leagues, err := service.markImported(ctx, ownerID, discovered)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, "discovered leagues could not be listed")
	}
	return &v1.ConnectSleeperResponse{ConnectionId: connection.ID.String(), Leagues: leagues}, nil
}

// saveConnection reuses the owner's provider-account row; ESPN credentials use
// the connection ID bytes as authenticated associated data.
func (s *Service) saveConnection(
	ctx context.Context,
	provider fantasy.ProviderID,
	externalID string,
	credentials espn.Credentials,
) (store.Connection, pgtype.UUID, error) {
	var connection store.Connection
	var ownerID pgtype.UUID
	err := s.DB.InTx(ctx, func(queries *store.Queries) error {
		owner, err := ensureOwner(ctx, queries, s.OwnerEmail)
		if err != nil {
			return err
		}
		ownerID = owner.ID
		connection, err = queries.FindProviderConnection(ctx, store.FindProviderConnectionParams{
			UserID: owner.ID, Provider: string(provider), ExternalUserID: pgtype.Text{String: externalID, Valid: true},
		})
		if errors.Is(err, pgx.ErrNoRows) {
			connection, err = queries.CreateConnection(ctx, store.CreateConnectionParams{
				UserID: owner.ID, Provider: string(provider), ExternalUserID: pgtype.Text{String: externalID, Valid: true},
			})
		}
		if err != nil {
			return err
		}
		var encrypted []byte
		if provider == fantasy.ProviderESPN {
			plaintext, marshalErr := json.Marshal(storedESPNCredentials{S2: credentials.S2, SWID: espn.NormalizeSWID(credentials.SWID)})
			if marshalErr != nil || s.Box == nil {
				return errors.New("credential encryption unavailable")
			}
			encrypted, err = s.Box.Seal(plaintext, connection.ID.Bytes[:])
			if err != nil {
				return err
			}
		}
		connection, err = queries.UpdateConnectionForUser(ctx, store.UpdateConnectionForUserParams{
			ID: connection.ID, UserID: owner.ID, EncryptedCredentials: encrypted, Status: "active",
		})
		return err
	})
	return connection, ownerID, err
}

// markImported reports whether each discovered league already has an attached link.
func (s *Service) markImported(ctx context.Context, ownerID pgtype.UUID, discovered []fantasy.DiscoveredLeague) ([]*v1.DiscoveredLeague, error) {
	leagues := make([]*v1.DiscoveredLeague, 0, len(discovered))
	err := s.DB.InReadTx(ctx, func(queries *store.Queries) error {
		for _, discoveredLeague := range discovered {
			imported, err := queries.IsLeagueAttachedForUser(ctx, store.IsLeagueAttachedForUserParams{
				UserID: ownerID, Provider: string(discoveredLeague.Ref.Provider),
				ExternalLeagueID: discoveredLeague.Ref.ExternalID, Season: int32(discoveredLeague.Ref.Season),
			})
			if err != nil {
				return err
			}
			leagues = append(leagues, &v1.DiscoveredLeague{
				ExternalLeagueId: discoveredLeague.Ref.ExternalID, Name: discoveredLeague.Name,
				Season: int32(discoveredLeague.Ref.Season), TeamId: discoveredLeague.TeamID,
				TeamName: discoveredLeague.TeamName, AlreadyImported: imported,
			})
		}
		return nil
	})
	return leagues, err
}

// ListConnections returns only the configured owner's connections and attached leagues.
func (s *Service) ListConnections(ctx context.Context, _ *v1.ListConnectionsRequest) (*v1.ListConnectionsResponse, error) {
	var response v1.ListConnectionsResponse
	err := s.DB.InReadTx(ctx, func(queries *store.Queries) error {
		owner, err := queries.GetUserByEmail(ctx, s.OwnerEmail)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		connections, err := queries.ListConnectionsForUser(ctx, owner.ID)
		if err != nil {
			return err
		}
		byID := make(map[string]*v1.LeagueConnection, len(connections))
		for _, connection := range connections {
			item := &v1.LeagueConnection{Id: connection.ID.String(), Provider: connection.Provider, Status: connectionStatus(connection.Status)}
			response.Connections = append(response.Connections, item)
			byID[item.Id] = item
		}
		leagues, err := queries.ListConnectionLeaguesForUser(ctx, owner.ID)
		if err != nil {
			return err
		}
		for _, league := range leagues {
			connection := byID[league.ConnectionID.String()]
			if connection == nil {
				continue
			}
			item := &v1.ConnectedLeague{
				LeagueId: league.LeagueSeasonID.String(), ExternalLeagueId: league.ExternalLeagueID,
				Name: league.Name, Season: league.Season,
			}
			if league.LastSuccessfulSyncAt.Valid {
				item.LastSuccessfulSyncAt = timestamppb.New(league.LastSuccessfulSyncAt.Time)
			}
			connection.Leagues = append(connection.Leagues, item)
		}
		return nil
	})
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, "connections could not be listed")
	}
	return &response, nil
}

// ImportLeagues imports picks sequentially and reports a stable result for each pick.
func (s *Service) ImportLeagues(ctx context.Context, request *v1.ImportLeaguesRequest) (*v1.ImportLeaguesResponse, error) {
	if request == nil || len(request.GetPicks()) > 50 {
		return nil, connect.NewError(connect.CodeInvalidArgument, "invalid import request")
	}
	connectionID, ok := parseUUID(request.GetConnectionId())
	if !ok {
		return nil, connect.NewError(connect.CodeInvalidArgument, "invalid connection ID")
	}
	var connection store.Connection
	err := s.DB.InReadTx(ctx, func(queries *store.Queries) error {
		owner, err := queries.GetUserByEmail(ctx, s.OwnerEmail)
		if errors.Is(err, pgx.ErrNoRows) {
			return pgx.ErrNoRows
		}
		if err != nil {
			return err
		}
		connection, err = queries.GetConnectionForUser(ctx, store.GetConnectionForUserParams{ID: connectionID, UserID: owner.ID})
		return err
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, connect.NewError(connect.CodeNotFound, "connection not found")
	}
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, "connection could not be loaded")
	}
	service := s.withDefaults()
	credentials, credentialErr := service.connectionCredentials(connection)
	importerService := &importer.Service{DB: s.DB}
	response := &v1.ImportLeaguesResponse{Results: make([]*v1.ImportResult, 0, len(request.GetPicks()))}
	var jobs []leaguesync.ImportJob
	for _, pick := range request.GetPicks() {
		result, job := service.importPick(ctx, importerService, connection, credentials, credentialErr, pick)
		response.Results = append(response.Results, result)
		if job != nil {
			jobs = append(jobs, *job)
		}
	}
	if len(jobs) > 0 && service.Imports != nil {
		service.Imports.StartImports(jobs)
	}
	return response, nil
}

func (s *Service) connectionCredentials(connection store.Connection) (espn.Credentials, error) {
	if connection.Provider != string(fantasy.ProviderESPN) {
		return espn.Credentials{}, nil
	}
	if s.Box == nil || len(connection.EncryptedCredentials) == 0 {
		return espn.Credentials{}, errors.New("credentials unavailable")
	}
	plaintext, err := s.Box.Open(connection.EncryptedCredentials, connection.ID.Bytes[:])
	if err != nil {
		return espn.Credentials{}, err
	}
	var credentials storedESPNCredentials
	if json.Unmarshal(plaintext, &credentials) != nil || credentials.S2 == "" || credentials.SWID == "" {
		return espn.Credentials{}, errors.New("credentials unavailable")
	}
	return espn.Credentials{S2: credentials.S2, SWID: credentials.SWID}, nil
}

// importPick maps one selection to a stable per-pick error code and marks ESPN
// connections for reconnect when stored credentials are rejected. On success it
// returns the begun import as a job for the background importer.
func (s Service) importPick(
	ctx context.Context,
	imports *importer.Service,
	connection store.Connection,
	credentials espn.Credentials,
	credentialErr error,
	pick *v1.ImportPick,
) (*v1.ImportResult, *leaguesync.ImportJob) {
	result := &v1.ImportResult{ExternalLeagueId: pick.GetExternalLeagueId(), TeamId: pick.GetTeamId()}
	if pick.GetExternalLeagueId() == "" {
		result.ErrorCode = "invalid_selection"
		return result, nil
	}
	if credentialErr != nil {
		s.markNeedsReconnect(connection)
		result.ErrorCode = "needs_reconnect"
		return result, nil
	}
	teamID, err := strconv.Atoi(pick.GetTeamId())
	if err != nil || teamID <= 0 {
		result.ErrorCode = "invalid_selection"
		return result, nil
	}
	prepared, err := s.NewSource(ctx, ImportSpec{
		Provider: connection.Provider, LeagueID: pick.GetExternalLeagueId(), Season: fantasy.CurrentSeason(time.Now()),
		TeamID: teamID, Username: connection.ExternalUserID.String, ESPN: credentials,
	})
	if err != nil {
		if errors.Is(err, espn.ErrUnauthorized) {
			s.markNeedsReconnect(connection)
			result.ErrorCode = "needs_reconnect"
			return result, nil
		}
		result.ErrorCode = "provider_fetch_failed"
		return result, nil
	}
	if prepared.AccountID != connection.ExternalUserID.String {
		result.ErrorCode = "account_mismatch"
		return result, nil
	}
	if err = AddLeagueAvailability(ctx, s.DB.Queries(), prepared); err != nil {
		result.ErrorCode = "import_failed"
		return result, nil
	}
	imports.Source = prepared.Provider
	started, err := imports.Begin(ctx, importer.Request{
		OwnerEmail: s.OwnerEmail, ExternalAccountID: prepared.AccountID, Selection: prepared.Selection, League: prepared.League,
	})
	if err != nil {
		if errors.Is(err, espn.ErrUnauthorized) {
			s.markNeedsReconnect(connection)
			result.ErrorCode = "needs_reconnect"
			return result, nil
		}
		var recorded *importer.RecordedSyncError
		if errors.As(err, &recorded) && recorded.Code != "" {
			result.ErrorCode = recorded.Code
		} else {
			result.ErrorCode = "import_failed"
		}
		return result, nil
	}
	result.Started = true
	result.LeagueId = started.LeagueSeasonID().String()
	return result, &leaguesync.ImportJob{
		Target: leaguesync.Target{
			ID: result.LeagueId, ConnectionID: connection.ID,
			ExternalAccountID: prepared.AccountID, Selection: prepared.Selection,
		},
		Source: prepared.Provider, Started: started,
	}
}

func (s Service) markNeedsReconnect(connection store.Connection) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := s.DB.Queries().MarkConnectionNeedsReconnect(ctx, connection.ID); err != nil {
		slog.Warn("could not mark provider connection for reconnect")
	}
}

// RemoveLeague detaches one of the configured owner's leagues without deleting its history.
func (s *Service) RemoveLeague(ctx context.Context, request *v1.RemoveLeagueRequest) (*v1.RemoveLeagueResponse, error) {
	if request == nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, "league ID is required")
	}
	leagueID, ok := parseUUID(request.GetLeagueId())
	if !ok {
		return nil, connect.NewError(connect.CodeInvalidArgument, "invalid league ID")
	}
	var rows int64
	err := s.DB.InTx(ctx, func(queries *store.Queries) error {
		owner, err := queries.GetUserByEmail(ctx, s.OwnerEmail)
		if err != nil {
			return err
		}
		rows, err = queries.DetachLeagueForUser(ctx, store.DetachLeagueForUserParams{UserID: owner.ID, LeagueSeasonID: leagueID})
		return err
	})
	if errors.Is(err, pgx.ErrNoRows) || err == nil && rows == 0 {
		return nil, connect.NewError(connect.CodeNotFound, "league not found")
	}
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, "league could not be removed")
	}
	return &v1.RemoveLeagueResponse{Removed: true}, nil
}

func parseUUID(value string) (pgtype.UUID, bool) {
	var id pgtype.UUID
	if err := id.Scan(value); err != nil || !id.Valid {
		return pgtype.UUID{}, false
	}
	return id, true
}

func ensureOwner(ctx context.Context, queries *store.Queries, email string) (store.User, error) {
	owner, err := queries.GetUserByEmail(ctx, email)
	if errors.Is(err, pgx.ErrNoRows) {
		return queries.CreateUser(ctx, email)
	}
	return owner, err
}

func connectionStatus(status string) v1.ConnectionStatus {
	switch status {
	case "pending":
		return v1.ConnectionStatus_CONNECTION_STATUS_PENDING
	case "active":
		return v1.ConnectionStatus_CONNECTION_STATUS_ACTIVE
	case "needs_reconnect":
		return v1.ConnectionStatus_CONNECTION_STATUS_NEEDS_RECONNECT
	case "disabled":
		return v1.ConnectionStatus_CONNECTION_STATUS_DISABLED
	default:
		return v1.ConnectionStatus_CONNECTION_STATUS_UNSPECIFIED
	}
}
