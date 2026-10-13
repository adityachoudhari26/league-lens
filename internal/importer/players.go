package importer

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/adityachoudhari26/league-lens/internal/store"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

var identityProviders = [...]string{"sleeper", "espn", "gsis", "mfl"}

type playerIdentity struct {
	name, kind    string
	externalIDs   map[string]string
	mappingSource string
	sourceVersion pgtype.Text
	hasMappedName bool
}

func (s *Service) playerIdentity(sleeperID string) playerIdentity {
	identity := playerIdentity{name: "Sleeper " + sleeperID, kind: "person", externalIDs: map[string]string{"sleeper": sleeperID}, mappingSource: "provider"}
	defenses := " ARI ATL BAL BUF CAR CHI CIN CLE DAL DEN DET GB HOU IND JAX KC LAC LAR LV MIA MIN NE NO NYG NYJ PHI PIT SEA SF TB TEN WAS "
	if strings.Contains(defenses, " "+sleeperID+" ") {
		identity.kind = "team_defense"
		identity.name = sleeperID + " defense"
		return identity
	}
	mapping, found := s.Catalog[sleeperID]
	if !found {
		return identity
	}
	identity.externalIDs = mapping.IDs
	identity.mappingSource = "dynastyprocess"
	identity.sourceVersion = text(s.CatalogVersion)
	if mapping.Name != "" {
		identity.name = mapping.Name
		identity.hasMappedName = true
	}
	return identity
}

func (s *Service) resolvePlayer(ctx context.Context, queries *store.Queries, sleeperID string) (pgtype.UUID, error) {
	identity := s.playerIdentity(sleeperID)
	player, err := findMappedPlayer(ctx, queries, sleeperID, identity.externalIDs)
	if err != nil {
		return pgtype.UUID{}, err
	}
	if !player.ID.Valid {
		player, err = queries.CreateEncounteredPlayer(ctx, store.CreateEncounteredPlayerParams{DisplayName: identity.name, EntityType: identity.kind})
		if err != nil {
			return pgtype.UUID{}, err
		}
	}
	if player.EntityType != identity.kind {
		return pgtype.UUID{}, fmt.Errorf("player entity type conflict for Sleeper %s", sleeperID)
	}
	if identity.hasMappedName {
		if err = queries.UpdateMappedPlayerName(ctx, store.UpdateMappedPlayerNameParams{ID: player.ID, DisplayName: identity.name}); err != nil {
			return pgtype.UUID{}, err
		}
	}
	if err = addPlayerAliases(ctx, queries, player.ID, identity); err != nil {
		return pgtype.UUID{}, err
	}
	return player.ID, nil
}

func findMappedPlayer(ctx context.Context, queries *store.Queries, sleeperID string, externalIDs map[string]string) (store.Player, error) {
	var existing store.Player
	for _, provider := range identityProviders {
		externalID := externalIDs[provider]
		if externalID == "" {
			continue
		}
		player, err := queries.FindPlayerByExternalID(ctx, store.FindPlayerByExternalIDParams{Provider: provider, ExternalID: externalID})
		if errors.Is(err, pgx.ErrNoRows) {
			continue
		}
		if err != nil {
			return store.Player{}, err
		}
		if existing.ID.Valid && existing.ID != player.ID {
			return store.Player{}, fmt.Errorf("conflicting player mappings for Sleeper %s", sleeperID)
		}
		existing = player
	}
	return existing, nil
}

func addPlayerAliases(ctx context.Context, queries *store.Queries, playerID pgtype.UUID, identity playerIdentity) error {
	for _, provider := range identityProviders {
		externalID := identity.externalIDs[provider]
		if externalID == "" {
			continue
		}
		_, err := queries.AddPlayerExternalID(ctx, store.AddPlayerExternalIDParams{Provider: provider, ExternalID: externalID, PlayerID: playerID, MappingSource: identity.mappingSource, SourceVersion: identity.sourceVersion})
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
	}
	return nil
}
