// Package importer coordinates provider reads and atomic local persistence.
package importer

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/adityachoudhari26/league-lens/internal/database"
	"github.com/adityachoudhari26/league-lens/internal/playerids"
	"github.com/adityachoudhari26/league-lens/internal/sleeper"
	"github.com/adityachoudhari26/league-lens/internal/store"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

type Source interface {
	Header(context.Context, string, string) (sleeper.Header, error)
	Fetch(context.Context, sleeper.Header) (sleeper.Data, error)
}
type Service struct {
	DB             *database.DB
	Source         Source
	Catalog        playerids.Catalog
	CatalogVersion string
}
type Request struct{ OwnerEmail, Username, LeagueID string }
type Result struct {
	Teams, Players, Weeks int
	SyncID                pgtype.UUID
}

func text(v string) pgtype.Text                { return pgtype.Text{String: v, Valid: true} }
func timestamp(v time.Time) pgtype.Timestamptz { return pgtype.Timestamptz{Time: v, Valid: true} }

func (s *Service) Import(ctx context.Context, req Request) (Result, error) {
	var result Result
	if strings.TrimSpace(req.OwnerEmail) == "" || req.Username == "" || req.LeagueID == "" {
		return result, fmt.Errorf("owner, username, and league ID required")
	}
	h, err := s.Source.Header(ctx, req.Username, req.LeagueID)
	if err != nil {
		return result, err
	}
	year, err := strconv.Atoi(h.League.Season)
	if err != nil {
		return result, err
	}
	settings, err := json.Marshal(h.League)
	if err != nil {
		return result, err
	}
	var user store.User
	var connection store.Connection
	var league store.LeagueSeason
	var run store.SyncRun
	// Establish the import target and durable run before bulk provider reads.
	err = s.DB.InTx(ctx, func(q *store.Queries) error {
		if e := q.LockImportWrites(ctx); e != nil {
			return e
		}
		var e error
		user, e = q.GetUserByEmail(ctx, strings.TrimSpace(req.OwnerEmail))
		if errors.Is(e, pgx.ErrNoRows) {
			user, e = q.CreateUser(ctx, strings.TrimSpace(req.OwnerEmail))
		}
		if e != nil {
			return e
		}
		connection, e = q.FindSleeperConnection(ctx, store.FindSleeperConnectionParams{UserID: user.ID, ExternalUserID: text(h.User.ID)})
		if errors.Is(e, pgx.ErrNoRows) {
			connection, e = q.CreateConnection(ctx, store.CreateConnectionParams{UserID: user.ID, Provider: "sleeper", ExternalUserID: text(h.User.ID)})
		}
		if e != nil {
			return e
		}
		if connection.Status == "disabled" {
			return fmt.Errorf("connection is disabled")
		}
		league, e = q.EnsureLeagueSeason(ctx, store.EnsureLeagueSeasonParams{Provider: "sleeper", ExternalLeagueID: h.League.ID, Season: int32(year), Name: h.League.Name, SettingsJson: settings})
		if e != nil {
			return e
		}
		if e = q.AttachLeague(ctx, store.AttachLeagueParams{ConnectionID: connection.ID, LeagueSeasonID: league.ID, UserID: user.ID, Provider: "sleeper"}); e != nil {
			return e
		}
		kind := "initial"
		if connection.Status == "active" {
			kind = "refresh"
		}
		run, e = q.StartSync(ctx, store.StartSyncParams{ConnectionID: connection.ID, LeagueSeasonID: league.ID, Kind: kind})
		return e
	})
	if err != nil {
		return result, err
	}
	result.SyncID = run.ID
	fail := func(cause error, code string) (Result, error) {
		cleanup, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_, e := s.DB.Queries().FailSync(cleanup, store.FailSyncParams{ID: run.ID, ErrorCode: text(code)})
		if e != nil {
			return result, errors.Join(cause, fmt.Errorf("record failed sync: %w", e))
		}
		return result, cause
	}
	data, err := s.Source.Fetch(ctx, h)
	if err != nil {
		return fail(err, "provider_fetch_failed")
	}
	err = s.DB.InTx(ctx, func(q *store.Queries) error {
		if e := q.LockImportWrites(ctx); e != nil {
			return e
		}
		newer, e := q.HasNewerSuccessfulSync(ctx, store.HasNewerSuccessfulSyncParams{LeagueSeasonID: league.ID, ID: run.ID})
		if e != nil {
			return e
		}
		if newer {
			return fmt.Errorf("a newer import already completed; retry")
		}
		if _, e = q.UpsertLeagueSeason(ctx, store.UpsertLeagueSeasonParams{Provider: "sleeper", ExternalLeagueID: h.League.ID, Season: int32(year), Name: h.League.Name, SettingsJson: settings}); e != nil {
			return e
		}
		result.Teams, result.Players, result.Weeks, e = s.persist(ctx, q, data, user, connection, league, run)
		if e != nil {
			return e
		}
		if _, e = q.UpdateConnectionForUser(ctx, store.UpdateConnectionForUserParams{ID: connection.ID, UserID: user.ID, Status: "active"}); e != nil {
			return e
		}
		done, e := q.CompleteSync(ctx, run.ID)
		if e != nil {
			return e
		}
		return q.MarkLeagueSynced(ctx, store.MarkLeagueSyncedParams{ConnectionID: connection.ID, LeagueSeasonID: league.ID, LastSuccessfulSyncAt: done.CompletedAt})
	})
	if err != nil {
		return fail(err, "import_write_failed")
	}
	return result, nil
}

type entry struct{ ID, Slot, Label string }

func rosterEntries(players, starters, reserve, taxi, positions []string) ([]entry, string, error) {
	slots := []string{}
	for _, p := range positions {
		if p != "BN" && p != "IR" && p != "TAXI" {
			slots = append(slots, p)
		}
	}
	if len(starters) > len(slots) {
		return nil, "", fmt.Errorf("more starters than configured slots")
	}
	byID := map[string]entry{}
	coverage := "full"
	if players == nil {
		coverage = "lineup"
	}
	for _, id := range players {
		if id == "" || id == "0" {
			return nil, "", fmt.Errorf("invalid roster player")
		}
		if _, ok := byID[id]; ok {
			return nil, "", fmt.Errorf("duplicate roster player")
		}
		byID[id] = entry{ID: id, Label: "BN"}
	}
	starterSeen := map[string]bool{}
	for i, id := range starters {
		if id == "0" || id == "" {
			continue
		}
		if starterSeen[id] {
			return nil, "", fmt.Errorf("duplicate starter")
		}
		starterSeen[id] = true
		if _, ok := byID[id]; !ok && players != nil {
			return nil, "", fmt.Errorf("starter missing from roster")
		}
		byID[id] = entry{ID: id, Slot: strconv.Itoa(i), Label: slots[i]}
	}
	for label, ids := range map[string][]string{"IR": reserve, "TAXI": taxi} {
		for _, id := range ids {
			e, ok := byID[id]
			if !ok || starterSeen[id] {
				return nil, "", fmt.Errorf("invalid reserve/taxi player")
			}
			e.Label = label
			byID[id] = e
		}
	}
	out := make([]entry, 0, len(byID))
	for _, e := range byID {
		out = append(out, e)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, coverage, nil
}
func (s *Service) persist(ctx context.Context, q *store.Queries, d sleeper.Data, u store.User, c store.Connection, l store.LeagueSeason, run store.SyncRun) (int, int, int, error) {
	teams := map[int]store.Team{}
	players := map[string]pgtype.UUID{}
	names := map[string]string{}
	for _, user := range d.Users {
		name := user.Metadata["team_name"]
		if name == "" {
			name = user.DisplayName
		}
		names[user.ID] = name
	}
	resolve := func(id string) (pgtype.UUID, error) {
		if p, ok := players[id]; ok {
			return p, nil
		}
		p, e := s.resolvePlayer(ctx, q, id)
		if e == nil {
			players[id] = p
		}
		return p, e
	}
	snapshot := func(team store.Team, period int, origin string, entries []entry, coverage string, record []byte) error {
		raw, e := json.Marshal(struct {
			Entries  []entry
			Coverage string
			Record   json.RawMessage
		}{entries, coverage, record})
		if e != nil {
			return e
		}
		hash := fmt.Sprintf("%x", sha256.Sum256(raw))
		key := fmt.Sprintf("week-%d:%s", period, coverage)
		if origin == "observed" {
			prev, e := q.GetLatestObservedSnapshot(ctx, store.GetLatestObservedSnapshotParams{TeamID: team.ID, Period: int32(period)})
			if e == nil && prev.ContentHash == hash {
				return nil
			}
			if e != nil && !errors.Is(e, pgx.ErrNoRows) {
				return e
			}
			key = fmt.Sprintf("%x", run.ID.Bytes)
		}
		snap, e := q.UpsertTeamSnapshot(ctx, store.UpsertTeamSnapshotParams{TeamID: team.ID, LeagueSeasonID: l.ID, SyncRunID: run.ID, Period: int32(period), Origin: origin, Coverage: coverage, SourceKey: key, ObservedAt: timestamp(d.ObservedAt), RecordJson: record, ContentHash: hash})
		if e != nil {
			return e
		}
		if e = q.DeleteSnapshotEntries(ctx, snap.ID); e != nil {
			return e
		}
		for _, item := range entries {
			p, e := resolve(item.ID)
			if e != nil {
				return e
			}
			slot := pgtype.Text{}
			if item.Slot != "" {
				slot = text(item.Slot)
			}
			if e = q.AddRosterEntry(ctx, store.AddRosterEntryParams{SnapshotID: snap.ID, PlayerID: p, ProviderSlotID: slot, SlotLabel: text(item.Label), EligiblePositionsJson: []byte("[]")}); e != nil {
				return e
			}
		}
		return nil
	}
	if err := q.ClearSelectedTeams(ctx, store.ClearSelectedTeamsParams{ConnectionID: c.ID, LeagueSeasonID: l.ID}); err != nil {
		return 0, 0, 0, err
	}
	for _, r := range d.Rosters {
		name := names[r.OwnerID]
		if name == "" {
			name = fmt.Sprintf("Team %d", r.ID)
		}
		team, e := q.UpsertTeam(ctx, store.UpsertTeamParams{LeagueSeasonID: l.ID, ExternalTeamID: strconv.Itoa(r.ID), Name: name})
		if e != nil {
			return 0, 0, 0, e
		}
		teams[r.ID] = team
		mine := r.OwnerID == d.User.ID
		for _, id := range r.CoOwners {
			mine = mine || id == d.User.ID
		}
		if mine {
			if e = q.SelectUserTeam(ctx, store.SelectUserTeamParams{UserID: u.ID, ConnectionID: c.ID, LeagueSeasonID: l.ID, TeamID: team.ID}); e != nil {
				return 0, 0, 0, e
			}
		}
		if r.Players == nil && r.Starters == nil {
			continue
		}
		entries, coverage, e := rosterEntries(r.Players, r.Starters, r.Reserve, r.Taxi, d.League.Positions)
		if e != nil {
			return 0, 0, 0, e
		}
		// Resolve even unchanged rosters so summary counts include every encountered identity.
		for _, item := range entries {
			if _, e = resolve(item.ID); e != nil {
				return 0, 0, 0, e
			}
		}
		var record []byte
		if r.Settings != nil {
			record, e = json.Marshal(r.Settings)
			if e != nil {
				return 0, 0, 0, e
			}
		}
		if e = snapshot(team, d.Period, "observed", entries, coverage, record); e != nil {
			return 0, 0, 0, e
		}
	}
	weeks := 0
	for week := 1; week <= d.Period; week++ {
		rows := d.Weeks[week]
		if len(rows) == 0 {
			continue
		}
		weeks++

		groups := map[string][]sleeper.Matchup{}
		for _, m := range rows {
			team, ok := teams[m.RosterID]
			if !ok {
				return 0, 0, 0, fmt.Errorf("unknown matchup team")
			}
			// Current week's live lineup isn't past history. Historical null player lists
			// preserve lineup-only coverage; absent lineups yield no invented snapshot.
			if week < d.Period || d.State.SeasonType == "post" {
				if m.Players != nil || m.Starters != nil {
					entries, coverage, e := rosterEntries(m.Players, m.Starters, nil, nil, d.League.Positions)
					if e != nil {
						return 0, 0, 0, e
					}
					if e = snapshot(team, week, "provider_history", entries, coverage, nil); e != nil {
						return 0, 0, 0, e
					}
				}
			}
			key := "bye-" + strconv.Itoa(m.RosterID)
			if m.MatchupID != nil && *m.MatchupID > 0 {
				key = strconv.Itoa(*m.MatchupID)
			}
			groups[key] = append(groups[key], m)
		}
		keys := make([]string, 0, len(groups))
		for key := range groups {
			keys = append(keys, key)
		}
		if e := q.DeleteStaleMatchups(ctx, store.DeleteStaleMatchupsParams{LeagueSeasonID: l.ID, Period: int32(week), Column3: keys}); e != nil {
			return 0, 0, 0, e
		}
		for key, group := range groups {
			if len(group) > 2 {
				return 0, 0, 0, fmt.Errorf("more than two teams in matchup")
			}
			sort.Slice(group, func(i, j int) bool { return group[i].RosterID < group[j].RosterID })
			status := "unknown"
			if week < d.Period || d.State.SeasonType == "post" {
				status = "final"
			}
			arg := store.UpsertMatchupParams{LeagueSeasonID: l.ID, ExternalMatchupID: key, Period: int32(week), TeamAID: teams[group[0].RosterID].ID, Status: status, FetchedAt: timestamp(d.ObservedAt)}
			var e error
			arg.ScoreA, e = score(group[0])
			if e != nil {
				return 0, 0, 0, e
			}
			if len(group) == 2 {
				arg.TeamBID = teams[group[1].RosterID].ID
				arg.ScoreB, e = score(group[1])
				if e != nil {
					return 0, 0, 0, e
				}
			}
			if _, e = q.UpsertMatchup(ctx, arg); e != nil {
				return 0, 0, 0, e
			}
		}
	}
	return len(teams), len(players), weeks, nil
}
func score(m sleeper.Matchup) (pgtype.Numeric, error) {
	p := m.Points
	if m.CustomPoints != nil {
		p = m.CustomPoints
	}
	var n pgtype.Numeric
	if p == nil {
		return n, nil
	}
	e := n.Scan(strconv.FormatFloat(*p, 'f', 4, 64))
	return n, e
}
func (s *Service) resolvePlayer(ctx context.Context, q *store.Queries, id string) (pgtype.UUID, error) {
	mapped, ok := s.Catalog[id]
	name := "Sleeper " + id
	kind := "person"
	defenses := " ARI ATL BAL BUF CAR CHI CIN CLE DAL DEN DET GB HOU IND JAX KC LAC LAR LV MIA MIN NE NO NYG NYJ PHI PIT SEA SF TB TEN WAS "
	if strings.Contains(defenses, " "+id+" ") {
		kind = "team_defense"
		name = id + " defense"
		ok = false
	}
	ids := map[string]string{"sleeper": id}
	if ok {
		ids = mapped.IDs
		if mapped.Name != "" {
			name = mapped.Name
		}
	}
	var existing store.Player
	for _, provider := range []string{"sleeper", "espn", "gsis", "mfl"} {
		external := ids[provider]
		if external == "" {
			continue
		}
		p, e := q.FindPlayerByExternalID(ctx, store.FindPlayerByExternalIDParams{Provider: provider, ExternalID: external})
		if errors.Is(e, pgx.ErrNoRows) {
			continue
		}
		if e != nil {
			return pgtype.UUID{}, e
		}
		if existing.ID.Valid && existing.ID != p.ID {
			return pgtype.UUID{}, fmt.Errorf("conflicting player mappings for Sleeper %s", id)
		}
		existing = p
	}
	if !existing.ID.Valid {
		var e error
		existing, e = q.CreateEncounteredPlayer(ctx, store.CreateEncounteredPlayerParams{DisplayName: name, EntityType: kind})
		if e != nil {
			return pgtype.UUID{}, e
		}
	}
	if existing.EntityType != kind {
		return pgtype.UUID{}, fmt.Errorf("player entity type conflict for Sleeper %s", id)
	}
	if ok && mapped.Name != "" {
		if err := q.UpdateMappedPlayerName(ctx, store.UpdateMappedPlayerNameParams{ID: existing.ID, DisplayName: name}); err != nil {
			return pgtype.UUID{}, err
		}
	}
	for _, provider := range []string{"sleeper", "espn", "gsis", "mfl"} {
		external := ids[provider]
		if external == "" {
			continue
		}
		source := "provider"
		version := pgtype.Text{}
		if ok {
			source = "dynastyprocess"
			version = text(s.CatalogVersion)
		}
		_, e := q.AddPlayerExternalID(ctx, store.AddPlayerExternalIDParams{Provider: provider, ExternalID: external, PlayerID: existing.ID, MappingSource: source, SourceVersion: version})
		if e != nil && !errors.Is(e, pgx.ErrNoRows) {
			return pgtype.UUID{}, e
		}
	}
	return existing.ID, nil
}
