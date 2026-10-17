package sleeper

import (
	"context"
	"fmt"
	"net/url"
	"slices"
	"strconv"
	"time"

	"github.com/adityachoudhari26/league-lens/internal/fantasy"
)

var _ fantasy.TransactionLister = (*Client)(nil)

type transactionResponse struct {
	Type          string                       `json:"type"`
	Status        string                       `json:"status"`
	Adds          map[string]int               `json:"adds"`
	Drops         map[string]int               `json:"drops"`
	Created       int64                        `json:"created"`
	StatusUpdated int64                        `json:"status_updated"`
	Settings      *transactionSettingsResponse `json:"settings"`
	TransactionID string                       `json:"transaction_id"`
}

type transactionSettingsResponse struct {
	WaiverBid *int `json:"waiver_bid"`
}

// ListTransactions returns completed Sleeper transactions for scoring weeks.
func (c *Client) ListTransactions(
	ctx context.Context,
	league fantasy.League,
	weeks []int,
) ([]fantasy.Transaction, error) {
	if league.Ref.Provider != fantasy.ProviderSleeper || league.Ref.ExternalID == "" ||
		league.Ref.Season < 1900 || league.Ref.Season > 2200 {
		return nil, fmt.Errorf("valid Sleeper league and season required")
	}
	if err := validateSleeperTransactionWeeks(weeks); err != nil {
		return nil, err
	}

	transactions := make([]fantasy.Transaction, 0)
	for _, week := range weeks {
		path := "/league/" + url.PathEscape(league.Ref.ExternalID) + "/transactions/" + strconv.Itoa(week)
		var entries []transactionResponse
		if err := c.getURLAllowNull(ctx, c.BaseURL+path, path, &entries); err != nil {
			return nil, err
		}
		transactions = append(transactions, completedSleeperTransactions(entries)...)
	}
	return transactions, nil
}

func validateSleeperTransactionWeeks(weeks []int) error {
	for _, week := range weeks {
		if week < 1 || week > 18 {
			return fmt.Errorf("valid Sleeper scoring week required")
		}
	}
	return nil
}

func completedSleeperTransactions(entries []transactionResponse) []fantasy.Transaction {
	transactions := make([]fantasy.Transaction, 0, len(entries))
	for _, entry := range entries {
		transaction, keep := completedSleeperTransaction(entry)
		if keep {
			transactions = append(transactions, transaction)
		}
	}
	return transactions
}

// completedSleeperTransaction maps one completed waiver, free-agent move, or trade.
func completedSleeperTransaction(entry transactionResponse) (fantasy.Transaction, bool) {
	if entry.Status != "complete" {
		return fantasy.Transaction{}, false
	}
	transactionType, keep := sleeperTransactionType(entry.Type, len(entry.Adds) > 0)
	if !keep {
		return fantasy.Transaction{}, false
	}

	transaction := fantasy.Transaction{
		ExternalID: entry.TransactionID,
		Type:       transactionType,
		ExecutedAt: sleeperTransactionTime(entry.StatusUpdated, entry.Created),
		Moves:      make([]fantasy.TransactionMove, 0, len(entry.Adds)+len(entry.Drops)),
	}
	playerIDs := sleeperTransactionPlayerIDs(entry)
	for _, playerID := range playerIDs {
		move := sleeperTransactionMove(entry, playerID)
		transaction.Moves = append(transaction.Moves, move)
	}
	if len(transaction.Moves) == 0 {
		return fantasy.Transaction{}, false
	}
	if transaction.Type == fantasy.TransactionWaiver && entry.Settings != nil {
		transaction.FAABBid = entry.Settings.WaiverBid
	}
	return transaction, true
}

func sleeperTransactionType(providerType string, hasAdds bool) (fantasy.TransactionType, bool) {
	switch providerType {
	case "waiver":
		return fantasy.TransactionWaiver, true
	case "free_agent":
		if hasAdds {
			return fantasy.TransactionAdd, true
		}
		return fantasy.TransactionDrop, true
	case "trade":
		return fantasy.TransactionTrade, true
	default:
		return "", false
	}
}

// sleeperTransactionPlayerIDs follows added-player direction for trades and includes drops otherwise.
func sleeperTransactionPlayerIDs(entry transactionResponse) []string {
	playerIDs := sleeperMapPlayerIDs(entry.Adds)
	if entry.Type == "trade" {
		slices.Sort(playerIDs)
		return playerIDs
	}
	playerIDs = append(playerIDs, sleeperMapPlayerIDs(entry.Drops)...)
	slices.Sort(playerIDs)
	return slices.Compact(playerIDs)
}

func sleeperMapPlayerIDs(players map[string]int) []string {
	playerIDs := make([]string, 0, len(players))
	for playerID := range players {
		playerIDs = append(playerIDs, playerID)
	}
	return playerIDs
}

// sleeperTransactionMove maps a player ID to its outgoing and incoming roster IDs.
func sleeperTransactionMove(entry transactionResponse, playerID string) fantasy.TransactionMove {
	move := fantasy.TransactionMove{
		ExternalPlayerID: playerID,
	}
	switch entry.Type {
	case "trade":
		move.FromTeamID = sleeperTransactionTeamID(entry.Drops[playerID])
		move.ToTeamID = sleeperTransactionTeamID(entry.Adds[playerID])
	case "free_agent", "waiver":
		if rosterID, added := entry.Adds[playerID]; added {
			move.ToTeamID = sleeperTransactionTeamID(rosterID)
		} else if rosterID, dropped := entry.Drops[playerID]; dropped {
			move.FromTeamID = sleeperTransactionTeamID(rosterID)
		}
	}
	if _, err := strconv.Atoi(playerID); err != nil {
		move.DefenseTeam = playerID
	}
	return move
}

func sleeperTransactionTeamID(teamID int) string {
	if teamID == 0 {
		return ""
	}
	return strconv.Itoa(teamID)
}

func sleeperTransactionTime(statusUpdated, created int64) time.Time {
	timestamp := statusUpdated
	if timestamp <= 0 {
		timestamp = created
	}
	return time.UnixMilli(timestamp).UTC()
}
