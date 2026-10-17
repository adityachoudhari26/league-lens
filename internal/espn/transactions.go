package espn

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/adityachoudhari26/league-lens/internal/fantasy"
)

var _ fantasy.TransactionLister = (*Client)(nil)

type transactionResponse struct {
	ID         string                    `json:"id"`
	Type       string                    `json:"type"`
	Status     string                    `json:"status"`
	ProposedAt int64                     `json:"proposedDate"`
	ProcessAt  int64                     `json:"processDate"`
	BidAmount  int                       `json:"bidAmount"`
	Items      []transactionItemResponse `json:"items"`
}

type transactionItemResponse struct {
	PlayerID   int    `json:"playerId"`
	Type       string `json:"type"`
	FromTeamID int    `json:"fromTeamId"`
	ToTeamID   int    `json:"toTeamId"`
}

// ListTransactions reads only executed adds, waivers, and accepted trades.
func (c *Client) ListTransactions(
	ctx context.Context,
	league fantasy.League,
	weeks []int,
) ([]fantasy.Transaction, error) {
	leagueID, parseErr := strconv.ParseUint(league.Ref.ExternalID, 10, 64)
	if league.Ref.Provider != fantasy.ProviderESPN || parseErr != nil || leagueID == 0 ||
		league.Ref.Season < 1900 || league.Ref.Season > 2200 {
		return nil, fmt.Errorf("valid ESPN league and season required")
	}
	if c.credentials.S2 == "" || c.credentials.SWID == "" {
		return nil, fmt.Errorf("ESPN credentials required")
	}
	if league.Ref.Season != c.season {
		return nil, fmt.Errorf("ESPN season mismatch")
	}
	if err := validateESPNTransactionWeeks(weeks); err != nil {
		return nil, err
	}

	transactions := make([]fantasy.Transaction, 0)
	for _, week := range weeks {
		response, err := c.readLeagueWithViews(
			ctx, league.Ref.ExternalID, week, []string{"mTransactions2", "mSettings"}, nil,
		)
		if err != nil {
			return nil, err
		}
		transactions = append(transactions, executedTransactions(response)...)
	}
	return transactions, nil
}

func validateESPNTransactionWeeks(weeks []int) error {
	for _, week := range weeks {
		if week < 1 || week > 18 {
			return fmt.Errorf("valid ESPN scoring week required")
		}
	}
	return nil
}

func executedTransactions(response leagueResponse) []fantasy.Transaction {
	transactions := make([]fantasy.Transaction, 0, len(response.Transactions))
	for _, entry := range response.Transactions {
		transaction, keep := executedTransaction(entry, response.Settings.AcquisitionSettings.IsUsingAcquisitionBudget)
		if keep {
			transactions = append(transactions, transaction)
		}
	}
	return transactions
}

// executedTransaction rejects incomplete rows and maps the accepted item types.
func executedTransaction(entry transactionResponse, usesAcquisitionBudget bool) (fantasy.Transaction, bool) {
	transactionType, keep := espnTransactionType(entry.Type, entry.Status)
	if !keep {
		return fantasy.Transaction{}, false
	}

	transaction := fantasy.Transaction{
		ExternalID: entry.ID,
		Type:       transactionType,
		ExecutedAt: transactionTime(entry.ProcessAt, entry.ProposedAt),
		Moves:      make([]fantasy.TransactionMove, 0, len(entry.Items)),
	}
	hasAdd := false
	for _, item := range entry.Items {
		if move, ok := transactionMove(item); ok {
			transaction.Moves = append(transaction.Moves, move)
			if entry.Type == "FREEAGENT" && item.Type == "ADD" {
				hasAdd = true
			}
		}
	}
	if len(transaction.Moves) == 0 {
		return fantasy.Transaction{}, false
	}
	if entry.Type == "FREEAGENT" && !hasAdd {
		transaction.Type = fantasy.TransactionDrop
	}
	if entry.Type == "WAIVER" && usesAcquisitionBudget {
		transaction.FAABBid = &entry.BidAmount
	}
	return transaction, true
}

func espnTransactionType(providerType, status string) (fantasy.TransactionType, bool) {
	if status != "EXECUTED" {
		return "", false
	}
	switch providerType {
	case "FREEAGENT":
		return fantasy.TransactionAdd, true
	case "WAIVER":
		return fantasy.TransactionWaiver, true
	case "TRADE_ACCEPT":
		return fantasy.TransactionTrade, true
	default:
		return "", false
	}
}

// transactionMove maps supported roster movements and ESPN's encoded defenses.
func transactionMove(item transactionItemResponse) (fantasy.TransactionMove, bool) {
	if item.Type != "ADD" && item.Type != "DROP" && item.Type != "TRADE" {
		return fantasy.TransactionMove{}, false
	}
	move := fantasy.TransactionMove{
		ExternalPlayerID: strconv.Itoa(item.PlayerID),
		FromTeamID:       espnTransactionTeamID(item.FromTeamID),
		ToTeamID:         espnTransactionTeamID(item.ToTeamID),
	}
	if item.PlayerID < 0 {
		move.DefenseTeam = proTeamAbbreviation(-item.PlayerID - 16000)
	}
	return move, true
}

func espnTransactionTeamID(teamID int) string {
	if teamID == 0 {
		return ""
	}
	return strconv.Itoa(teamID)
}

func transactionTime(processDate, proposedDate int64) time.Time {
	timestamp := processDate
	if timestamp <= 0 {
		timestamp = proposedDate
	}
	return time.UnixMilli(timestamp).UTC()
}
