package main

import (
	"fmt"
	"sort"
)

// Episode counts do not define episode numbers: a 16-episode season can use
// E62-E77. Keep the authoritative numbers when season details are available.
func setSeasonEpisodeNumbers(tv *tmdbTVDetail, number int, detail tmdbSeasonDetail) {
	numbers := []int{}
	seen := map[int]bool{}
	for _, episode := range detail.Episodes {
		if episode.EpisodeNumber > 0 && !seen[episode.EpisodeNumber] {
			seen[episode.EpisodeNumber] = true
			numbers = append(numbers, episode.EpisodeNumber)
		}
	}
	sort.Ints(numbers)
	for i := range tv.Seasons {
		if tv.Seasons[i].SeasonNumber == number {
			tv.Seasons[i].EpisodeNumbers = numbers
		}
	}
}

// Translate season-local E1..En only when TMDB supplies a complete, contiguous,
// disjoint range. Mixed or ambiguous numbering still requires review.
func normalizeInventoryNumbering(inv *seriesInventory, tv tmdbTVDetail) *seriesInventory {
	if inv == nil {
		return newSeriesInventory()
	}
	offsets := map[int]int{}
	for _, season := range tv.Seasons {
		numbers := season.EpisodeNumbers
		count := len(numbers)
		if count == 0 || count != season.EpisodeCount || numbers[0] <= count || numbers[count-1]-numbers[0]+1 != count {
			continue
		}
		if inv.SeasonOwned[season.SeasonNumber] > 0 && inv.SeasonMax[season.SeasonNumber] <= count {
			offsets[season.SeasonNumber] = numbers[0] - 1
		}
	}
	if len(offsets) == 0 {
		return inv
	}
	result := newSeriesInventory()
	for season := range inv.Seasons {
		result.Seasons[season] = true
	}
	for key := range inv.Owned {
		var season, episode int
		if _, err := fmt.Sscanf(key, "%d:%d", &season, &episode); err == nil {
			result.add(embyEpisode{ParentIndexNumber: season, IndexNumber: episode + offsets[season]})
		}
	}
	result.Total = inv.Total
	for id := range inv.TMDBEpisodeIDs {
		result.TMDBEpisodeIDs[id] = true
	}
	return result
}

// /Shows may return either one physical copy or a merged view. Partition known
// sibling IDs so loading every copy works with both forms without double counts.
func inventoryForSeries(episodes []embyEpisode, seriesID string, group []embyItem) *seriesInventory {
	siblings := map[string]bool{}
	for _, item := range group {
		siblings[item.ID] = true
	}
	inv := newSeriesInventory()
	for _, episode := range episodes {
		if episode.SeriesID != seriesID && siblings[episode.SeriesID] {
			continue
		}
		inv.add(episode)
	}
	return inv
}
