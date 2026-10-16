// Package leaguesync refreshes the local owner's leagues from their providers
// through the shared importer.
package leaguesync

import (
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/adityachoudhari26/league-lens/internal/fantasy"
	"github.com/adityachoudhari26/league-lens/internal/store"
)

// Target is one attached league season, reconstructed from its earlier import:
// the provider account that imported it and the teams selected at that time.
type Target struct {
	ID                   string
	ConnectionID         pgtype.UUID
	EncryptedCredentials []byte
	Name                 string
	ExternalAccountID    string
	Selection            fantasy.ConnectionSelection
	LastSuccessfulSyncAt time.Time
}

// TargetFromRow reconstructs a sync target from its persisted row.
func TargetFromRow(row store.ListSyncTargetsRow) Target {
	league := fantasy.LeagueRef{
		Provider:   fantasy.ProviderID(row.Provider),
		ExternalID: row.ExternalLeagueID,
		Season:     int(row.Season),
	}
	return Target{
		ID:                   uuidString(row.LeagueSeasonID),
		ConnectionID:         row.ConnectionID,
		EncryptedCredentials: row.EncryptedCredentials,
		Name:                 row.Name,
		ExternalAccountID:    row.ExternalUserID.String,
		Selection:            fantasy.ConnectionSelection{League: league, SelectedTeamIDs: row.SelectedTeamIds},
		LastSuccessfulSyncAt: timeOf(row.LastSuccessfulSyncAt),
	}
}

func uuidString(id pgtype.UUID) string {
	b := id.Bytes
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

func timeOf(value pgtype.Timestamptz) time.Time {
	if !value.Valid {
		return time.Time{}
	}
	return value.Time
}
