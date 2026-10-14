package dashboard

import (
	"context"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/adityachoudhari26/league-lens/internal/fantasy"
	v1 "github.com/adityachoudhari26/league-lens/internal/gen/leaguelens/v1"
	"github.com/adityachoudhari26/league-lens/internal/store"
	"github.com/jackc/pgx/v5/pgtype"
)

const playerNewsCoverage = "Latest RotoWire news via ESPN from the past 7 days, saved at each sync. Players ESPN can't identify have no news."

var (
	injuryHeadline        = regexp.MustCompile(`(?i)injur|\b(?:ankle|knee|hamstring|concussion|groin|shoulder|calf|foot|toe|hip|quad|bruise|rib|ribs|wrist|finger|hand|elbow|neck|chest|thumb|achilles|oblique|pectoral|abdomen|illness|ruled out|questionable|doubtful|practice|sidelined|ir|injured reserve)\b`)
	injuryParenthetical   = regexp.MustCompile(`\b[A-Z][A-Za-z'.-]+ \([a-z]+(?:[ -][a-z]+){0,2}\)`)
	practiceSquadHeadline = regexp.MustCompile(`(?i)\bpractice\s+squad\b`)
	transactionHeadline   = regexp.MustCompile(`(?i)\b(?:signed|re-signed|released|waived|traded|claimed|activated|promoted|elevated|cut|acquired|suspended|reinstated)\b`)
)

type playerSummary struct {
	id           pgtype.UUID
	name         string
	teams        []*v1.OverviewTeam
	starter      bool
	injuryStatus v1.InjuryStatus
}

type rankedPlayerUpdate struct {
	update *v1.PlayerUpdate
	tier   int
}

// GetOverviewPlayerUpdates lists the latest saved news for rostered players.
// It uses observed snapshots and stored news only; it never contacts providers.
func (s *Service) GetOverviewPlayerUpdates(ctx context.Context, _ *v1.GetOverviewPlayerUpdatesRequest) (*v1.GetOverviewPlayerUpdatesResponse, error) {
	response := &v1.GetOverviewPlayerUpdatesResponse{Coverage: coverage("empty", "Import a league to see player news.")}
	now := time.Now()
	err := s.readOverview(ctx, func(ctx context.Context, q *store.Queries, owner pgtype.UUID, teams []store.OverviewTeamsRow) error {
		if len(teams) == 0 {
			return nil
		}

		summaries := make(map[string]*playerSummary)
		playerIDs := make([]pgtype.UUID, 0)
		for _, team := range teams {
			if !team.SnapshotID.Valid {
				continue
			}
			entries, err := q.ListSnapshotRosterForUser(ctx, store.ListSnapshotRosterForUserParams{UserID: owner, SnapshotID: team.SnapshotID})
			if err != nil {
				return err
			}
			for _, entry := range entries {
				addPlayerSummary(summaries, team, entry)
			}
		}
		for _, summary := range summaries {
			playerIDs = append(playerIDs, summary.id)
		}
		newsRows, err := q.ListRecentPlayerNews(ctx, store.ListRecentPlayerNewsParams{
			PlayerIds: playerIDs,
			Since:     pgtype.Timestamptz{Time: now.Add(-7 * 24 * time.Hour), Valid: true},
		})
		if err != nil {
			return err
		}
		response.Updates = rankPlayerUpdates(newsRows, summaries)
		response.Coverage = coverage("partial", playerNewsCoverage)
		return nil
	})
	return response, err
}

func addPlayerSummary(summaries map[string]*playerSummary, team store.OverviewTeamsRow, entry store.ListSnapshotRosterForUserRow) {
	playerID := entry.PlayerID.String()
	summary := summaries[playerID]
	if summary == nil {
		summary = &playerSummary{id: entry.PlayerID, name: entry.DisplayName, injuryStatus: v1.InjuryStatus_INJURY_STATUS_UNKNOWN}
		summaries[playerID] = summary
	}

	teamID := team.ID.String()
	teamAdded := false
	for _, existing := range summary.teams {
		if existing.Id == teamID {
			teamAdded = true
			break
		}
	}
	if !teamAdded {
		summary.teams = append(summary.teams, overviewTeam(team))
	}
	if (team.Coverage == "full" || team.Coverage == "lineup") && entry.SlotLabel.Valid && isStarter(fantasy.RosterSlot(entry.SlotLabel.String)) {
		summary.starter = true
	}
	status := normalizedInjuryStatus(entry.InjuryStatus.String)
	if injuryStatusPriority(status) > injuryStatusPriority(summary.injuryStatus) {
		summary.injuryStatus = status
	}
}

func normalizedInjuryStatus(status string) v1.InjuryStatus {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case string(fantasy.InjuryReserve):
		return v1.InjuryStatus_INJURY_STATUS_INJURED_RESERVE
	case string(fantasy.InjuryOut):
		return v1.InjuryStatus_INJURY_STATUS_OUT
	case string(fantasy.InjuryDoubtful):
		return v1.InjuryStatus_INJURY_STATUS_DOUBTFUL
	case string(fantasy.InjuryQuestionable):
		return v1.InjuryStatus_INJURY_STATUS_QUESTIONABLE
	case string(fantasy.InjuryActive):
		return v1.InjuryStatus_INJURY_STATUS_ACTIVE
	default:
		return v1.InjuryStatus_INJURY_STATUS_UNKNOWN
	}
}

func injuryStatusPriority(status v1.InjuryStatus) int {
	switch status {
	case v1.InjuryStatus_INJURY_STATUS_INJURED_RESERVE:
		return 5
	case v1.InjuryStatus_INJURY_STATUS_OUT:
		return 4
	case v1.InjuryStatus_INJURY_STATUS_DOUBTFUL:
		return 3
	case v1.InjuryStatus_INJURY_STATUS_QUESTIONABLE:
		return 2
	case v1.InjuryStatus_INJURY_STATUS_ACTIVE:
		return 1
	default:
		return 0
	}
}

// rankPlayerUpdates keeps each player's newest recent item, then puts
// designations and starter injury reports ahead of other starters and bench.
func rankPlayerUpdates(rows []store.PlayerNews, summaries map[string]*playerSummary) []*v1.PlayerUpdate {
	orderedRows := append([]store.PlayerNews(nil), rows...)
	sort.Slice(orderedRows, func(i, j int) bool {
		if !orderedRows[i].PublishedAt.Time.Equal(orderedRows[j].PublishedAt.Time) {
			return orderedRows[i].PublishedAt.Time.After(orderedRows[j].PublishedAt.Time)
		}
		if orderedRows[i].SourceItemID != orderedRows[j].SourceItemID {
			return orderedRows[i].SourceItemID < orderedRows[j].SourceItemID
		}
		return orderedRows[i].Source < orderedRows[j].Source
	})

	seenPlayers := make(map[string]bool)
	var ranked []rankedPlayerUpdate
	for _, row := range orderedRows {
		playerID := row.PlayerID.String()
		summary := summaries[playerID]
		if summary == nil || seenPlayers[playerID] {
			continue
		}
		seenPlayers[playerID] = true
		category := categorize(row.Headline)
		update := &v1.PlayerUpdate{
			Id:           row.Source + ":" + row.SourceItemID + ":" + playerID,
			PlayerId:     playerID,
			Headline:     row.Headline,
			SourceUrl:    row.Url,
			PublishedAt:  timestamp(row.PublishedAt),
			PlayerName:   summary.name,
			Summary:      row.Summary,
			SourceLabel:  row.SourceLabel,
			Category:     category,
			InjuryStatus: summary.injuryStatus,
			Starter:      summary.starter,
			Teams:        append([]*v1.OverviewTeam(nil), summary.teams...),
		}
		ranked = append(ranked, rankedPlayerUpdate{update: update, tier: playerUpdateTier(summary, category)})
	}
	sort.Slice(ranked, func(i, j int) bool {
		if ranked[i].tier != ranked[j].tier {
			return ranked[i].tier < ranked[j].tier
		}
		if ranked[i].update.Starter != ranked[j].update.Starter {
			return ranked[i].update.Starter
		}
		iPublished := ranked[i].update.PublishedAt.AsTime()
		jPublished := ranked[j].update.PublishedAt.AsTime()
		if !iPublished.Equal(jPublished) {
			return iPublished.After(jPublished)
		}
		return ranked[i].update.Id < ranked[j].update.Id
	})
	updates := make([]*v1.PlayerUpdate, 0, len(ranked))
	for _, item := range ranked {
		updates = append(updates, item.update)
	}
	return updates
}

func playerUpdateTier(summary *playerSummary, category v1.NewsCategory) int {
	if injuryStatusPriority(summary.injuryStatus) >= injuryStatusPriority(v1.InjuryStatus_INJURY_STATUS_QUESTIONABLE) ||
		(summary.starter && category == v1.NewsCategory_NEWS_CATEGORY_INJURY) {
		return 0
	}
	if summary.starter {
		return 1
	}
	return 2
}

func categorize(headline string) v1.NewsCategory {
	headlineWithoutPracticeSquad := practiceSquadHeadline.ReplaceAllString(headline, " ")
	switch {
	case injuryHeadline.MatchString(headlineWithoutPracticeSquad) || injuryParenthetical.MatchString(headlineWithoutPracticeSquad):
		return v1.NewsCategory_NEWS_CATEGORY_INJURY
	case transactionHeadline.MatchString(headline):
		return v1.NewsCategory_NEWS_CATEGORY_TRANSACTION
	default:
		return v1.NewsCategory_NEWS_CATEGORY_GENERAL
	}
}
