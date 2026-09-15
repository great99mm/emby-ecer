package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestAbsoluteAndSeasonLocalCopiesShareTheirActualEpisodes(t *testing.T) {
	for _, tc := range []struct {
		name, selected string
		recent, merged bool
	}{
		{name: "full"}, {name: "recent", recent: true},
		{name: "single-split", selected: "local"}, {name: "single-merged", selected: "absolute", merged: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			setupAPITest(t, settings{})
			base := newScanFixture(t)
			// The reported shape: 23 season-local episodes and 1,083 files in
			// TMDB order, including 34 specials. There are 106 missing regular episodes.
			counts := []int{61, 16, 14, 39, 13, 52, 33, 35, 73, 45, 26, 14, 101, 58, 62, 50, 56, 55, 74, 14, 197, 67, 26}
			holes := map[int]bool{50: true, 59: true, 355: true, 522: true, 806: true, 1106: true, 1107: true, 1110: true, 1111: true}
			for n := 992; n <= 1088; n++ {
				holes[n] = true
			}
			series := []embyItem{
				{ID: "local", Name: "长篇动画", Type: "Series", ProviderIDs: map[string]string{"Tmdb": "42"}},
				{ID: "absolute", Name: "长篇动画", Type: "Series", ProviderIDs: map[string]string{"Tmdb": "42"}},
			}
			tv := tmdbTVDetail{ID: 42, Name: "长篇动画", Status: "Returning Series", InProduction: true}
			details := map[int]tmdbSeasonDetail{}
			episodes := map[string][]embyEpisode{}
			number := 0
			for index, count := range counts {
				season := index + 1
				tv.Seasons = append(tv.Seasons, tmdbSeason{SeasonNumber: season, EpisodeCount: count})
				for n := 0; n < count; n++ {
					number++
					date := "2020-01-01"
					if number > 1178 {
						date = time.Now().AddDate(0, 0, 7).Format("2006-01-02")
					}
					detail := details[season]
					detail.Episodes = append(detail.Episodes, tmdbEpisodeDetail{ID: 10000 + number, EpisodeNumber: number, AirDate: date})
					details[season] = detail
					if season < 23 && !holes[number] {
						episodes["absolute"] = append(episodes["absolute"], embyEpisode{ID: fmt.Sprint(number), SeriesID: "absolute", ParentIndexNumber: season, IndexNumber: number, ProviderIDs: map[string]string{"Tmdb": fmt.Sprint(10000 + number)}})
					}
				}
			}
			for n := 1; n <= 34; n++ {
				episodes["absolute"] = append(episodes["absolute"], embyEpisode{ID: fmt.Sprintf("special-%d", n), SeriesID: "absolute", IndexNumber: n})
			}
			for n := 1; n <= 23; n++ {
				episodes["local"] = append(episodes["local"], embyEpisode{ID: fmt.Sprintf("local-%d", n), SeriesID: "local", ParentIndexNumber: 23, IndexNumber: n})
			}
			if len(episodes["absolute"]) != 1083 {
				t.Fatal("fixture does not match the reported inventory")
			}
			var showCalls atomic.Int32
			var seasonCalls [24]atomic.Int32
			emby := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				query := r.URL.Query()
				if strings.HasPrefix(r.URL.Path, "/Shows/") || query.Get("IncludeItemTypes") == "Episode" {
					items := append(append([]embyEpisode{}, episodes["absolute"]...), episodes["local"]...)
					if strings.HasPrefix(r.URL.Path, "/Shows/") {
						showCalls.Add(1)
						if !tc.merged {
							items = episodes[strings.Split(r.URL.Path, "/")[2]]
						}
					}
					start, _ := strconv.Atoi(query.Get("StartIndex"))
					limit, _ := strconv.Atoi(query.Get("Limit"))
					end := minInt(start+limit, len(items))
					_ = json.NewEncoder(w).Encode(embyEpisodesResp{Items: items[start:end]})
					return
				}
				items := []embyItem{}
				for _, item := range series {
					if ids := query.Get("Ids"); ids == "" || strings.Contains(ids, item.ID) {
						items = append(items, item)
					}
				}
				_ = json.NewEncoder(w).Encode(embyItemsResp{Items: items})
			}))
			t.Cleanup(emby.Close)
			tmdb := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/tv/42" {
					_ = json.NewEncoder(w).Encode(tv)
					return
				}
				var season int
				_, _ = fmt.Sscanf(r.URL.Path, "/tv/42/season/%d", &season)
				seasonCalls[season].Add(1)
				_ = json.NewEncoder(w).Encode(details[season])
			}))
			t.Cleanup(tmdb.Close)
			tmdbBaseURL = tmdb.URL
			s := fixtureSettings(base)
			s.EmbyURL = emby.URL
			result, err := scanLibrary(s, true, 0, tc.recent, time.Now(), tc.selected, nil)
			if err != nil {
				t.Fatal(err)
			}
			missing := result["missing"].([]missingEpisode)
			if len(missing) != 106 || result["summary"].(map[string]any)["seriesNeedsReview"] != 0 {
				t.Fatalf("expected 106 real gaps, got %d missing; summary=%v", len(missing), result["summary"])
			}
			for _, item := range missing {
				if !holes[item.Episode] || !item.MergedSeries || item.TotalEpisodes != 1178 || item.OwnedEpisodes != 1072 {
					t.Fatalf("incorrect missing episode or health: %+v", item)
				}
			}
			for _, entry := range result["diagnostics"].(map[string]any)["compared"].([]scanCompareEntry) {
				if entry.EmbyEpisodes != 1106 || len(entry.SourceSeriesIDs) != 2 {
					t.Fatalf("copies were not merged exactly once: %+v", entry)
				}
			}
			for season := 1; season <= 23; season++ {
				if got := seasonCalls[season].Load(); got > 1 {
					t.Fatalf("same season requested repeatedly for sibling copies: S%d, %d calls", season, got)
				}
			}
			if tc.selected == "" && showCalls.Load() != 0 {
				t.Fatal("bulk scanning fell back to per-show requests")
			}
		})
	}
}

func TestSeasonLocalNumbersRequireUnambiguousTMDBRange(t *testing.T) {
	for _, tc := range []struct {
		name          string
		actual, owned []int
		want          int
		review        bool
	}{
		{"local", []int{62, 63, 64}, []int{1, 2}, 62, false},
		{"absolute", []int{62, 63, 64}, []int{62, 63}, 62, false},
		{"mixed", []int{62, 63, 64}, []int{1, 62}, 0, true},
		{"noncontiguous", []int{62, 64}, []int{1}, 0, true},
		{"overlapping", []int{2, 3, 4}, []int{1}, 0, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tv := tmdbTVDetail{Seasons: []tmdbSeason{{SeasonNumber: 2, EpisodeCount: len(tc.actual), EpisodeNumbers: tc.actual}}}
			original := newSeriesInventory()
			for _, number := range tc.owned {
				original.add(embyEpisode{ParentIndexNumber: 2, IndexNumber: number})
			}
			inv := normalizeInventoryNumbering(original, tv)
			_, reason := numberingReview(inv, tv)
			if (reason != "") != tc.review || (!tc.review && !inv.has(2, tc.want)) {
				t.Fatalf("unexpected numbering interpretation: %v, %s", inv.Owned, reason)
			}
			if !original.has(2, tc.owned[0]) {
				t.Fatal("shared raw inventory was mutated")
			}
		})
	}
}
