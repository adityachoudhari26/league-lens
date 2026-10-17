package assistant

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"sort"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/adityachoudhari26/league-lens/internal/fantasy"
	v1 "github.com/adityachoudhari26/league-lens/internal/gen/leaguelens/v1"
	"github.com/adityachoudhari26/league-lens/internal/leaguesync"
	"github.com/adityachoudhari26/league-lens/internal/playerids"
	"github.com/adityachoudhari26/league-lens/internal/store"
)

var (
	errTransactionsESPN        = errors.New("espn transaction lookup failed")
	errTransactionsSleeper     = errors.New("sleeper transaction lookup failed")
	errTransactionsUnavailable = errors.New("transaction target unavailable")
)

const (
	transactionDefaultWeeksBack = 1
	transactionMinWeeksBack     = 1
	transactionMaxWeeksBack     = 4
	transactionDefaultLimit     = 20
	transactionMinLimit         = 1
	transactionMaxLimit         = 50
)

const getTransactionsSchema = `{
  "type": "object",
  "properties": {
    "league_season_id": {"type": "string", "format": "uuid"},
    "weeks_back": {"type": "integer", "minimum": 1, "maximum": 4, "default": 1},
    "limit": {"type": "integer", "minimum": 1, "maximum": 50, "default": 20}
  },
  "required": ["league_season_id"],
  "additionalProperties": false
}`

type transactionToolInput struct {
	LeagueSeasonID string `json:"league_season_id"`
	WeeksBack      *int   `json:"weeks_back"`
	Limit          *int   `json:"limit"`
}

type validatedTransactionInput struct {
	leagueSeasonID string
	leagueSeason   pgtype.UUID
	weeksBack      int
	limit          int
}

type transactionToolResult struct {
	Transactions []transactionToolItem `json:"transactions"`
}

type transactionToolItem struct {
	Date    string                  `json:"date"`
	Type    string                  `json:"type"`
	Team    string                  `json:"team,omitempty"`
	Teams   []string                `json:"teams,omitempty"`
	Players []transactionToolPlayer `json:"players"`
	FAABBid *int                    `json:"faab_bid,omitempty"`
}

type transactionToolPlayer struct {
	Name     string `json:"name"`
	Position string `json:"position,omitempty"`
	Action   string `json:"action"`
	Team     string `json:"team,omitempty"`
	FromTeam string `json:"from_team,omitempty"`
}

type transactionReferenceData struct {
	teamNames   map[string]string
	playerNames map[string]string
}

// transactionTools registers the owner-scoped recent-activity lookup.
func transactionTools(reader assistantDashboard, service *Service) []assistantTool {
	return []assistantTool{newAssistantTool(
		"get_transactions",
		"Recent adds, drops, waiver claims and trades in one of the user's leagues, newest first. "+
			"Use it for 'what's happening in my league' and to see what other managers are picking up.",
		getTransactionsSchema,
		fixedToolLabel("Checking league activity"),
		func(ctx context.Context, raw json.RawMessage) (string, error) {
			input, err := decodeToolInput[transactionToolInput](raw)
			if err != nil {
				return "", errInvalidToolInput
			}
			validated, err := validateTransactionInput(input)
			if err != nil {
				return "", err
			}
			ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
			defer cancel()
			result, provider, err := service.listTransactions(ctx, reader, validated)
			if err != nil {
				return "", err
			}
			encoded, err := json.Marshal(result)
			if err != nil {
				return "", errTransactionsError(provider)
			}
			return string(encoded), nil
		},
	)}
}

// validateTransactionInput applies defaults and bounds the requested history window.
func validateTransactionInput(input transactionToolInput) (validatedTransactionInput, error) {
	validated := validatedTransactionInput{
		leagueSeasonID: input.LeagueSeasonID,
		weeksBack:      transactionDefaultWeeksBack,
		limit:          transactionDefaultLimit,
	}
	if input.LeagueSeasonID == "" || validated.leagueSeason.Scan(input.LeagueSeasonID) != nil {
		return validatedTransactionInput{}, errInvalidToolInput
	}
	if input.WeeksBack != nil {
		validated.weeksBack = *input.WeeksBack
	}
	if input.Limit != nil {
		validated.limit = *input.Limit
	}
	if validated.weeksBack < transactionMinWeeksBack || validated.weeksBack > transactionMaxWeeksBack ||
		validated.limit < transactionMinLimit || validated.limit > transactionMaxLimit {
		return validatedTransactionInput{}, errInvalidToolInput
	}
	return validated, nil
}

// listTransactions fetches recent provider activity for an attached league.
func (s *Service) listTransactions(
	ctx context.Context,
	reader assistantDashboard,
	input validatedTransactionInput,
) (transactionToolResult, fantasy.ProviderID, error) {
	target, err := s.ownerSyncTarget(ctx, input.leagueSeasonID, errTransactionsUnavailable)
	if err != nil {
		return transactionToolResult{}, "", err
	}
	provider := target.Selection.League.Provider
	leaguePage, err := reader.GetLeague(ctx, &v1.GetLeagueRequest{LeagueSeasonId: input.leagueSeasonID})
	if err != nil {
		return transactionToolResult{}, provider, errTransactionsError(provider)
	}
	currentWeek, err := latestSelectedScoringWeek(ctx, reader, leaguePage)
	if err != nil {
		return transactionToolResult{}, provider, errTransactionsError(provider)
	}
	lister, league, err := s.transactionSource(ctx, target)
	if err != nil {
		return transactionToolResult{}, provider, errTransactionsError(provider)
	}
	transactions, err := lister.ListTransactions(ctx, league, transactionWeeks(currentWeek, input.weeksBack))
	if err != nil {
		return transactionToolResult{}, provider, errTransactionsError(provider)
	}
	result, err := s.mapTransactions(ctx, input.leagueSeason, provider, transactions, input.limit)
	if err != nil {
		return transactionToolResult{}, provider, err
	}
	return result, provider, nil
}

// transactionSource resolves the attached league's provider transaction adapter.
func (s *Service) transactionSource(
	ctx context.Context,
	target leaguesync.Target,
) (fantasy.TransactionLister, fantasy.League, error) {
	source, err := s.ProviderSources.SourceFor(target)
	if err != nil {
		return nil, fantasy.League{}, err
	}
	lister, ok := source.(fantasy.TransactionLister)
	if !ok {
		return nil, fantasy.League{}, errors.New("provider does not list transactions")
	}
	league, err := source.DescribeLeague(ctx, target.Selection.League.ExternalID)
	if err != nil {
		return nil, fantasy.League{}, err
	}
	return lister, league, nil
}

// transactionWeeks returns the requested trailing range, clamped to week one.
func transactionWeeks(currentWeek, weeksBack int) []int {
	firstWeek := currentWeek - weeksBack + 1
	if firstWeek < 1 {
		firstWeek = 1
	}
	weeks := make([]int, 0, currentWeek-firstWeek+1)
	for week := firstWeek; week <= currentWeek; week++ {
		weeks = append(weeks, week)
	}
	return weeks
}

func errTransactionsError(provider fantasy.ProviderID) error {
	if provider == fantasy.ProviderESPN {
		return errTransactionsESPN
	}
	return errTransactionsSleeper
}

// mapTransactions joins provider IDs to saved names and formats the tool result.
func (s *Service) mapTransactions(
	ctx context.Context,
	leagueSeasonID pgtype.UUID,
	provider fantasy.ProviderID,
	transactions []fantasy.Transaction,
	limit int,
) (transactionToolResult, error) {
	externalIDs := transactionPlayerIDs(transactions)
	references, err := s.loadTransactionReferenceData(ctx, leagueSeasonID, provider, externalIDs)
	if err != nil {
		return transactionToolResult{}, internalError("league activity could not be loaded")
	}
	catalog, _ := s.trendingCatalog(ctx)
	items := mapTransactionItems(provider, transactions, references, catalog)
	sort.SliceStable(items, func(i, j int) bool {
		return items[i].Date > items[j].Date
	})
	if len(items) > limit {
		items = items[:limit]
	}
	return transactionToolResult{Transactions: items}, nil
}

func transactionPlayerIDs(transactions []fantasy.Transaction) []string {
	externalIDs := make([]string, 0)
	for _, transaction := range transactions {
		externalIDs = append(externalIDs, transactionMovePlayerIDs(transaction.Moves)...)
	}
	return uniqueTransactionIDs(externalIDs)
}

func transactionMovePlayerIDs(moves []fantasy.TransactionMove) []string {
	externalIDs := make([]string, 0, len(moves))
	for _, move := range moves {
		if move.ExternalPlayerID != "" {
			externalIDs = append(externalIDs, move.ExternalPlayerID)
		}
	}
	return externalIDs
}

func uniqueTransactionIDs(externalIDs []string) []string {
	slices.Sort(externalIDs)
	return slices.Compact(externalIDs)
}

// loadTransactionReferenceData reads team and player names in one database snapshot.
func (s *Service) loadTransactionReferenceData(
	ctx context.Context,
	leagueSeasonID pgtype.UUID,
	provider fantasy.ProviderID,
	externalIDs []string,
) (transactionReferenceData, error) {
	var data transactionReferenceData
	err := s.DB.InReadTx(ctx, func(q *store.Queries) error {
		teams, err := q.AssistantTransactionTeams(ctx, leagueSeasonID)
		if err != nil {
			return err
		}
		players, err := q.AssistantTransactionPlayers(ctx, store.AssistantTransactionPlayersParams{
			Provider: string(provider), ExternalIds: externalIDs,
		})
		if err != nil {
			return err
		}
		data = transactionReferenceData{
			teamNames:   transactionTeamNames(teams),
			playerNames: transactionPlayerNames(players),
		}
		return nil
	})
	return data, err
}

func transactionTeamNames(rows []store.AssistantTransactionTeamsRow) map[string]string {
	names := make(map[string]string, len(rows))
	for _, row := range rows {
		names[row.ExternalTeamID] = row.Name
	}
	return names
}

func transactionPlayerNames(rows []store.AssistantTransactionPlayersRow) map[string]string {
	names := make(map[string]string, len(rows))
	for _, row := range rows {
		names[row.ExternalID] = row.DisplayName
	}
	return names
}

// mapTransactionItems filters and maps transactions whose players have names.
func mapTransactionItems(
	provider fantasy.ProviderID,
	transactions []fantasy.Transaction,
	references transactionReferenceData,
	catalog playerids.Catalog,
) []transactionToolItem {
	items := make([]transactionToolItem, 0, len(transactions))
	for _, transaction := range transactions {
		item := mapTransactionItem(provider, transaction, references, catalog)
		if len(item.Players) > 0 {
			items = append(items, item)
		}
	}
	return items
}

// mapTransactionItem resolves players and delegates team assignment.
func mapTransactionItem(
	provider fantasy.ProviderID,
	transaction fantasy.Transaction,
	references transactionReferenceData,
	catalog playerids.Catalog,
) transactionToolItem {
	item := transactionToolItem{
		Date:    transaction.ExecutedAt.UTC().Format(time.RFC3339),
		Type:    string(transaction.Type),
		Players: make([]transactionToolPlayer, 0, len(transaction.Moves)),
		FAABBid: transaction.FAABBid,
	}
	for _, move := range transaction.Moves {
		player, found := mapTransactionPlayer(
			provider, transaction.Type, move, references.playerNames, references.teamNames, catalog,
		)
		if found {
			item.Players = append(item.Players, player)
		}
	}
	assignTransactionTeams(&item, transaction, references.teamNames)
	return item
}

// assignTransactionTeams maps transaction moves to their owning team labels.
func assignTransactionTeams(
	item *transactionToolItem,
	transaction fantasy.Transaction,
	teamNames map[string]string,
) {
	if transaction.Type != fantasy.TransactionTrade {
		if len(transaction.Moves) == 0 {
			return
		}
		move := transaction.Moves[0]
		teamID := move.ToTeamID
		if teamID == "" {
			teamID = move.FromTeamID
		}
		item.Team = teamNames[teamID]
		return
	}
	seenTeams := make(map[string]bool)
	for _, move := range transaction.Moves {
		item.Teams = appendTransactionTeam(item.Teams, seenTeams, teamNames, move.FromTeamID)
		item.Teams = appendTransactionTeam(item.Teams, seenTeams, teamNames, move.ToTeamID)
	}
}

// mapTransactionPlayer resolves saved, catalog, then defense-fallback names.
func mapTransactionPlayer(
	provider fantasy.ProviderID,
	transactionType fantasy.TransactionType,
	move fantasy.TransactionMove,
	playerNames, teamNames map[string]string,
	catalog playerids.Catalog,
) (transactionToolPlayer, bool) {
	name := playerNames[move.ExternalPlayerID]
	entry, catalogFound := catalog.Lookup(string(provider), move.ExternalPlayerID)
	if name == "" && catalogFound {
		name = entry.Name
	}
	if name == "" && move.DefenseTeam != "" {
		name = move.DefenseTeam + " D/ST"
	}
	if name == "" {
		return transactionToolPlayer{}, false
	}
	position := ""
	if move.DefenseTeam != "" {
		position = "DST"
	} else if catalogFound {
		position = trendingPosition(entry.Position)
	}
	player := transactionToolPlayer{Name: name, Position: position}
	if transactionType == fantasy.TransactionTrade {
		player.Action = "received"
		player.Team = teamNames[move.ToTeamID]
		player.FromTeam = teamNames[move.FromTeamID]
	} else if move.ToTeamID != "" {
		player.Action = "added"
	} else {
		player.Action = "dropped"
	}
	return player, true
}

func appendTransactionTeam(
	teams []string,
	seen map[string]bool,
	teamNames map[string]string,
	teamID string,
) []string {
	if teamID == "" || seen[teamID] {
		return teams
	}
	seen[teamID] = true
	if name := teamNames[teamID]; name != "" {
		return append(teams, name)
	}
	return teams
}
