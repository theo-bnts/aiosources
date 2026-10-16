package aiosource

import (
	"slices"

	"github.com/theo-bnts/aiosources/internal/model"
)

// Torznab cannot inspect the recipient's downloaded files, unlike Stremio
// playback. Episode searches therefore require numbering or a season pack;
// missing evidence must not imply that a torrent contains every episode.
func MatchesTorznabEpisode(rec model.AIORecord, season, episode *int) bool {
	if season == nil {
		return true
	}
	p := ParseRelease(rec.Title)
	if rec.ReviewedEpisode {
		return rec.Season != nil && *rec.Season == *season && (episode == nil || rec.Episode == nil || *rec.Episode == *episode)
	}
	if p.InvalidNumbering || (rec.Season == nil && len(p.Seasons) == 0) {
		return false
	}
	if len(p.Seasons) > 0 && !slices.Contains(p.Seasons, *season) {
		return false
	}
	if rec.Season != nil && *rec.Season != *season && !(p.Pack && len(p.Seasons) > 1 && slices.Contains(p.Seasons, *rec.Season)) {
		return false
	}
	if episode == nil {
		return true
	}
	if rec.Episode == nil && len(p.Episodes) == 0 && !p.Pack {
		return false
	}
	return MatchesEpisode(rec, p, Resource{Type: "series", Season: season, Episode: episode})
}
