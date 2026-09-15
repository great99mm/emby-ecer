package main

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
)

const libraryItemFields = "ProviderIds,SortName,OriginalTitle,PremiereDate,ProductionYear,DateLastSaved,DateLastMediaAdded,RecursiveItemCount,Path"

// Ask Emby for the selected IDs and their TMDB siblings directly. A single-show
// scan must retain duplicate-copy matching without loading the entire library.
func loadSelectedLibraryItems(s settings, ids map[string]bool) ([]embyItem, error) {
	selected := make([]string, 0, len(ids))
	for id := range ids {
		selected = append(selected, id)
	}
	sort.Strings(selected)
	items, err := loadFilteredSeries(s, map[string]string{"Ids": strings.Join(selected, ",")})
	if err != nil {
		return loadLibraryItems(s)
	}
	byID := map[string]embyItem{}
	tmdbIDs := map[int]bool{}
	for _, item := range items {
		if item.Type == "Series" && ids[item.ID] {
			byID[item.ID] = item
			if id := parseInt(providerID(item.ProviderIDs, "tmdb")); id > 0 {
				tmdbIDs[id] = true
			}
		}
	}
	if len(byID) == 0 {
		return nil, fmt.Errorf("所选剧集已不存在或当前账号无法访问，请刷新媒体库")
	}
	for id := range tmdbIDs {
		related, err := loadFilteredSeries(s, map[string]string{"AnyProviderIdEquals": "tmdb." + strconv.Itoa(id)})
		if err != nil {
			return loadLibraryItems(s)
		}
		// Filter again for servers that ignore provider filters.
		for _, item := range related {
			if item.Type == "Series" && parseInt(providerID(item.ProviderIDs, "tmdb")) == id {
				byID[item.ID] = item
			}
		}
	}
	result := make([]embyItem, 0, len(byID))
	for _, item := range byID {
		result = append(result, item)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].ID < result[j].ID })
	return result, nil
}

func loadFilteredSeries(s settings, filters map[string]string) ([]embyItem, error) {
	routes := []string{embyItemsRoute(s)}
	if s.EmbyUserID != "" {
		routes = append(routes, "/Items")
	}
	var lastErr error
	for _, route := range routes {
		items := []embyItem{}
		for start := 0; ; start += 1000 {
			query := map[string]string{"Recursive": "true", "IncludeItemTypes": "Series", "Fields": libraryItemFields, "EnableImages": "false", "EnableUserData": "false", "SortBy": "SortName", "StartIndex": strconv.Itoa(start), "Limit": "1000"}
			for key, value := range filters {
				query[key] = value
			}
			var page embyItemsResp
			if err := embyGet(s, route, query, &page); err != nil {
				lastErr = err
				break
			}
			items = append(items, page.Items...)
			if len(page.Items) < 1000 {
				return items, nil
			}
		}
	}
	return nil, lastErr
}
