package aiosource

import (
	"context"
	"time"

	"github.com/theo-bnts/aiosources/internal/model"
	"github.com/theo-bnts/aiosources/internal/store"
	"golang.org/x/sync/errgroup"
)

func (s *Service) drainMatching(ctx context.Context, history *operationHistory) error {
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		rows, err := s.DB.AIOMatchBatch(ctx, 100)
		if err != nil || len(rows) == 0 {
			return err
		}
		group, work := errgroup.WithContext(ctx)
		group.SetLimit(s.DB.AIOMatchWorkers())
		outcomes := make([][]model.AIOMatchResult, (len(rows)+9)/10)
		for start := 0; start < len(rows); start += 10 {
			chunk, index := rows[start:min(start+10, len(rows))], start/10
			group.Go(func() error {
				results, err := s.DB.MatchAIOBatch(work, chunk, decideAIORecord)
				outcomes[index] = results
				return err
			})
		}
		err = group.Wait()
		processed := 0
		for _, results := range outcomes {
			for _, result := range results {
				history.run.Processed++
				history.run.Results[result.Status]++
				processed++
			}
		}
		history.checkpoint(ctx, "identification")
		if err != nil {
			return err
		}
		if processed == 0 {
			// An import may own every picked row. Back off instead of spinning
			// against those locks; the durable queue remains authoritative.
			timer := time.NewTimer(100 * time.Millisecond)
			select {
			case <-ctx.Done():
				timer.Stop()
				return ctx.Err()
			case <-timer.C:
			}
		}
	}
}

func decideAIORecord(r model.AIORecord, find store.AIOCandidateSearch) (model.AIOMatchResult, error) {
	p := withSourceNumbering(ParseRelease(r.Title), SourceEpisode{r.Season, r.Episode})
	r.Normalized = p.Normalized
	if p.InvalidNumbering {
		return model.AIOMatchResult{Record: r, Status: "unmatched", Reason: "invalid or unsupported numbering range"}, nil
	}
	filter := model.AIOCandidateFilter{Title: p.Normalized, TMDB: r.TMDB, IMDb: canonicalIMDb(r.IMDb), TVDB: r.TVDB, Type: p.Type, Year: p.Year, Seasons: p.Seasons}
	if filter.Type == "" && (r.Season != nil || r.Episode != nil) {
		filter.Type = "tv"
	}
	if !p.Pack && len(p.Seasons) == 1 && len(p.Episodes) == 1 {
		filter.Episode = &p.Episodes[0]
	}
	works, overflow, err := find(filter)
	if err != nil {
		return model.AIOMatchResult{}, err
	}
	candidates := make([]Candidate, len(works))
	for i, work := range works {
		candidates[i] = WorkCandidate(work)
	}
	d := ChooseMatch(p, candidates, SourceIDs{TMDB: r.TMDB, IMDb: r.IMDb, TVDB: r.TVDB}, overflow, SourceEpisode{r.Season, r.Episode})
	result := model.AIOMatchResult{Record: r, Status: d.Status, Reason: d.Reason}
	if d.Winner != nil {
		for i := range works {
			if itoa64(works[i].ID) == d.Winner.EntryID {
				result.Work = &works[i]
				break
			}
		}
	}
	if d.Status != "matched" && p.Normalized != "" && len(p.Seasons) > 0 {
		// Unknown episode years are not evidence for accepting a candidate,
		// but the year filter must not prevent requesting that missing data.
		filter.Type, filter.Year, filter.Episode, filter.ExactOnly = "tv", nil, nil, true
		possible, _, err := find(filter)
		if err != nil {
			return model.AIOMatchResult{}, err
		}
		for _, work := range possible {
			exact := false
			for _, title := range work.Titles {
				exact = exact || Normalize(title) == p.Normalized
			}
			if !exact {
				continue
			}
			for _, season := range p.Seasons {
				missing := work.SeasonYears[season] == 0
				// A season can air across two calendar years. If its first air
				// date does not corroborate a pack's year, request its episodes
				// once; the durable season request prevents a retry loop.
				missing = missing || (p.Pack && p.Year != nil && !contains(work.Years, *p.Year) && work.SeasonYears[season] != *p.Year)
				for _, episode := range p.Episodes {
					missing = missing || work.EpisodeYears[strconvPair(season, episode)] == 0
				}
				if missing && season >= 0 && season <= 999 {
					result.Seasons = append(result.Seasons, model.AIOSeasonRequest{TMDB: work.TMDB, Season: season})
				}
			}
		}
	}
	return result, nil
}
