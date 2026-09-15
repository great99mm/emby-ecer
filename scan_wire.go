package main

import (
	"compress/gzip"
	"encoding/json"
	"net/http"
	"strings"
)

// Browser results carry show metadata once, not once per missing episode.
// The stored scan and the default API response keep their full representation.
func scanResponse(scan map[string]any, r *http.Request) map[string]any {
	if scan == nil || r.URL.Query().Get("view") != "compact" {
		return scan
	}
	response := make(map[string]any, len(scan))
	for key, value := range scan {
		if key != "missing" {
			response[key] = value
		}
	}
	type group struct {
		Series   map[string]any   `json:"series"`
		Episodes []map[string]any `json:"episodes"`
	}
	groups := []*group{}
	byID := map[string]*group{}
	for _, value := range anySlice(scan["missing"]) {
		var item map[string]any
		switch v := value.(type) {
		case map[string]any:
			item = v
		case missingEpisode:
			item = map[string]any{
				"embySeriesId": v.EmbySeriesID, "embyTitle": v.EmbyTitle, "tmdbId": v.TMDBID,
				"officialTitle": v.OfficialTitle, "tmdbMatchYear": v.TMDBMatchYear,
				"posterPath": v.PosterPath, "totalEpisodes": v.TotalEpisodes, "ownedEpisodes": v.OwnedEpisodes,
				"season": v.Season, "episode": v.Episode, "code": v.Code, "episodeName": v.EpisodeName,
			}
		default:
			continue
		}
		key := anyToString(item["embySeriesId"]) + ":" + anyToString(item["tmdbId"])
		g := byID[key]
		if g == nil {
			g = &group{Series: map[string]any{}, Episodes: []map[string]any{}}
			for _, field := range []string{"embySeriesId", "embyTitle", "tmdbId", "officialTitle", "tmdbMatchYear", "posterPath", "totalEpisodes", "ownedEpisodes"} {
				g.Series[field] = item[field]
			}
			byID[key] = g
			groups = append(groups, g)
		}
		episode := map[string]any{}
		for _, field := range []string{"season", "episode", "code", "episodeName"} {
			episode[field] = item[field]
		}
		g.Episodes = append(g.Episodes, episode)
	}
	response["missingGroups"] = groups
	return response
}

func writeScanJSON(w http.ResponseWriter, r *http.Request, status int, payload any) {
	w.Header().Add("Vary", "Accept-Encoding")
	// Small progress responses need no compression; never compress credentials.
	if r.URL.Query().Get("summary") == "1" || !acceptsGzip(r.Header.Get("Accept-Encoding")) {
		writeJSON(w, status, payload)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Content-Encoding", "gzip")
	w.WriteHeader(status)
	writer, _ := gzip.NewWriterLevel(w, gzip.BestSpeed)
	defer writer.Close()
	_ = json.NewEncoder(writer).Encode(payload)
}

func acceptsGzip(header string) bool {
	for _, encoding := range strings.Split(header, ",") {
		parts := strings.Split(encoding, ";")
		if !strings.EqualFold(strings.TrimSpace(parts[0]), "gzip") {
			continue
		}
		for _, param := range parts[1:] {
			key, value, _ := strings.Cut(strings.TrimSpace(param), "=")
			if key == "q" && strings.Trim(value, "0.") == "" {
				return false
			}
		}
		return true
	}
	return false
}
