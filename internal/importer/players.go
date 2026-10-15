package importer

import (
	"context"
	"errors"
	"fmt"

	"github.com/adityachoudhari26/league-lens/internal/fantasy"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/adityachoudhari26/league-lens/internal/store"
)

var identityProviders = [...]string{"sleeper", "espn", "gsis", "mfl"}

type playerIdentity struct {
	name, kind    string
	externalIDs   map[string]string
	mappingSource string
	sourceVersion pgtype.Text
	hasMappedName bool
}

func (s *Service) playerIdentity(provider fantasy.ProviderID, player fantasy.Player) (playerIdentity, error) {
	identity := playerIdentity{
		name:          player.Name,
		kind:          string(player.Kind),
		externalIDs:   map[string]string{string(provider): player.ExternalID},
		mappingSource: "provider",
	}
	for _, alias := range player.KnownIDs {
		identity.externalIDs[string(alias.Namespace)] = alias.ExternalID
	}
	if player.Kind == fantasy.PlayerTeamDefense {
		return identity, nil
	}
	mapping, found := s.Catalog.Lookup(string(provider), player.ExternalID)
	if !found {
		return identity, nil
	}
	for namespace, id := range mapping.IDs {
		if existing, known := identity.externalIDs[namespace]; known && existing != id {
			return identity, fmt.Errorf("conflicting provider and dataset player aliases")
		}
		identity.externalIDs[namespace] = id
	}
	identity.mappingSource = "dynastyprocess"
	identity.sourceVersion = text(s.CatalogVersion)
	if mapping.Name != "" {
		identity.name = mapping.Name
		identity.hasMappedName = true
	}
	return identity, nil
}

func (s *Service) resolvePlayer(ctx context.Context, queries *store.Queries, provider fantasy.ProviderID, input fantasy.Player) (pgtype.UUID, error) {
	identity, err := s.playerIdentity(provider, input)
	if err != nil {
		return pgtype.UUID{}, err
	}
	player, err := findMappedPlayer(ctx, queries, input.ExternalID, identity.externalIDs)
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
		return pgtype.UUID{}, fmt.Errorf("player entity type conflict for %s", input.ExternalID)
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

func findMappedPlayer(ctx context.Context, queries *store.Queries, externalID string, externalIDs map[string]string) (store.Player, error) {
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
			return store.Player{}, fmt.Errorf("conflicting player mappings for %s", externalID)
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
		_, err := queries.AddPlayerExternalID(ctx, store.AddPlayerExternalIDParams{
			Provider:      provider,
			ExternalID:    externalID,
			PlayerID:      playerID,
			MappingSource: identity.mappingSource,
			SourceVersion: identity.sourceVersion,
		})
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
	}
	return nil
}
