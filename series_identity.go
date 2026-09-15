package main

import (
	"fmt"
	"sort"
	"strings"
)

// Emby can expose one logical show as several Series IDs. Both bulk inventory
// and /Shows/{id}/Episodes may contain only a physical copy, so compatible
// copies must explicitly share their owned episodes.
func seriesIdentityGroups(items []embyItem, excluded map[string]bool) map[int][]embyItem {
	groups := map[int][]embyItem{}
	for _, item := range items {
		if item.Type != "Series" || excluded[item.ID] {
			continue
		}
		if id := parseInt(providerID(item.ProviderIDs, "tmdb")); id > 0 {
			groups[id] = append(groups[id], item)
		}
	}
	for _, group := range groups {
		sort.Slice(group, func(i, j int) bool { return group[i].ID < group[j].ID })
	}
	return groups
}

func expandSelectedSeries(ids map[string]bool, groups map[int][]embyItem) {
	if len(ids) == 0 {
		return
	}
	for _, group := range groups {
		selected := false
		for _, item := range group {
			if ids[item.ID] {
				selected = true
				break
			}
		}
		if selected {
			for _, item := range group {
				ids[item.ID] = true
			}
		}
	}
}

func identityFingerprint(series embyItem, group []embyItem, airedOnly bool, ignores episodeIgnoreIndex) string {
	if len(group) <= 1 {
		group = []embyItem{series}
	}
	parts := make([]string, 0, len(group))
	ids := make([]string, 0, len(group))
	for _, item := range group {
		parts = append(parts, seriesFingerprint(item, airedOnly))
		ids = append(ids, item.ID)
	}
	sort.Strings(parts)
	ignored := []string{}
	for _, id := range ids {
		for key := range ignores[id] {
			ignored = append(ignored, id+"|"+key)
		}
	}
	sort.Strings(ignored)
	// Adding or removing an ignore changes completeness, even if Emby did not
	// change. The marker also invalidates caches created before this rule.
	return strings.Join(parts, "\n") + "\nepisode-ignores:" + strings.Join(ignored, ",")
}

func mergedIdentityInventory(base *seriesInventory, seriesID string, group []embyItem, inventory map[string]*seriesInventory, tv tmdbTVDetail) (*seriesInventory, []string) {
	result := newSeriesInventory()
	ids := []string{}
	add := func(id string, source *seriesInventory) {
		if source == nil {
			source = newSeriesInventory()
		}
		if _, reason := numberingReview(source, tv); reason != "" {
			return
		}
		ids = append(ids, id)
		result.Total += source.Total
		for key := range source.Owned {
			if result.Owned[key] {
				continue
			}
			result.Owned[key] = true
			var season, episode int
			if _, err := fmt.Sscanf(key, "%d:%d", &season, &episode); err == nil {
				result.Seasons[season] = true
				result.SeasonOwned[season]++
				if episode > result.SeasonMax[season] {
					result.SeasonMax[season] = episode
				}
			}
		}
		for id := range source.TMDBEpisodeIDs {
			result.TMDBEpisodeIDs[id] = true
		}
	}
	add(seriesID, base)
	for _, item := range group {
		if item.ID != seriesID {
			add(item.ID, inventory[item.ID])
		}
	}
	sort.Strings(ids)
	return result, ids
}

func automaticArchiveValid(entry seriesScanCacheEntry, fingerprint string) bool {
	return entry.Manual || (entry.Complete && entry.Verification == seriesScanCacheVersion && entry.SeriesStatus == "Ended" && !entry.InProduction && entry.Fingerprint == fingerprint)
}

func selectedResultIDs(requested map[string]bool, result map[string]any) map[string]bool {
	if len(requested) == 0 {
		return requested
	}
	for _, value := range anySlice(result["scannedSeriesIds"]) {
		if id := anyToString(value); id != "" {
			requested[id] = true
		}
	}
	return requested
}

func excludeSeriesEpisodes(episodes []embyEpisode, excluded map[string]bool) []embyEpisode {
	kept := episodes[:0]
	for _, episode := range episodes {
		if !excluded[episode.SeriesID] {
			kept = append(kept, episode)
		}
	}
	return kept
}
