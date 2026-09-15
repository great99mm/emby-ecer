package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"testing"
	"time"
)

func TestOldAutomaticArchivesAreRechecked(t *testing.T) {
	setupAPITest(t, settings{})
	path := filepath.Join(t.TempDir(), "cache.json")
	entries := map[string]seriesScanCacheEntry{
		"old":     {Complete: true},
		"manual":  {Complete: true, Manual: true},
		"current": {Complete: true, Verification: seriesScanCacheVersion, SeriesStatus: "Ended"},
		"ongoing": {Complete: true, Verification: seriesScanCacheVersion, SeriesStatus: "Returning Series"},
	}
	if err := saveStateJSON("series_scan_cache", path, entries); err != nil {
		t.Fatal(err)
	}
	cache := newSeriesScanCacheStore(path)
	if _, ok := cache.Get("old"); ok {
		t.Fatal("old count-only result bypasses numbering validation")
	}
	if _, ok := cache.Get("ongoing"); ok {
		t.Fatal("ongoing show remained archived")
	}
	for _, id := range []string{"manual", "current"} {
		if _, ok := cache.Get(id); !ok {
			t.Fatal("valid archive or manual ignore lost")
		}
	}
}

func TestEpisodeCountDoesNotProveSeasonComplete(t *testing.T) {
	inv := buildSeriesInventory([]embyEpisode{
		{ParentIndexNumber: 1, IndexNumber: 1},
		{ParentIndexNumber: 1, IndexNumber: 3},
		{ParentIndexNumber: 1, IndexNumber: 3},
		{ParentIndexNumber: 0, IndexNumber: 100},
	})
	if inv.coversSeason(1, 2) {
		t.Fatal("a gap and duplicate files must not count as a complete season")
	}
	inv.add(embyEpisode{ParentIndexNumber: 1, IndexNumber: 2})
	if !inv.coversSeason(1, 3) {
		t.Fatal("complete numeric coverage not recognized")
	}
	if _, reason := numberingReview(inv, tmdbTVDetail{Seasons: []tmdbSeason{{SeasonNumber: 1, EpisodeCount: 3}}}); reason != "" {
		t.Fatal("specials must not cause a numbering conflict")
	}
}

func TestAbsoluteNumberingNeedsReviewAndSurvivesIncrementalScan(t *testing.T) {
	setupAPITest(t, settings{})
	fixture := newScanFixture(t)
	// The real failure shape: 1,836 files, one season, numbers reaching 1,850.
	holes := map[int]bool{15: true, 195: true, 230: true, 260: true, 265: true, 294: true, 643: true, 848: true, 855: true, 865: true, 866: true, 1010: true, 1515: true, 1599: true}
	episodes := []embyEpisode{}
	for number := 1; number <= 1850; number++ {
		if !holes[number] {
			episodes = append(episodes, embyEpisode{ID: strconv.Itoa(number), SeriesID: "s1", ParentIndexNumber: 1, IndexNumber: number})
		}
	}
	emby := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/Shows/s1/Episodes" {
			start, _ := strconv.Atoi(r.URL.Query().Get("StartIndex"))
			end := start + 200
			if end > len(episodes) {
				end = len(episodes)
			}
			if start > len(episodes) {
				start = len(episodes)
			}
			_ = json.NewEncoder(w).Encode(embyEpisodesResp{Items: episodes[start:end]})
			return
		}
		fixture.emby.Config.Handler.ServeHTTP(w, r)
	}))
	defer emby.Close()
	seasonCalls := 0
	tmdb := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/tv/101" {
			seasons := []tmdbSeason{}
			for i, count := range []int{236, 264, 157, 52, 56, 53, 53, 52, 61, 50, 54, 51, 51, 51, 70, 51, 48, 48, 51, 44, 46, 40, 41, 45, 45, 38, 28} {
				seasons = append(seasons, tmdbSeason{SeasonNumber: i + 1, EpisodeCount: count})
			}
			_ = json.NewEncoder(w).Encode(tmdbTVDetail{ID: 101, Name: "连续编号动画", Seasons: seasons})
			return
		}
		seasonCalls++
		fixture.tmdb.Config.Handler.ServeHTTP(w, r)
	}))
	defer tmdb.Close()
	tmdbBaseURL = tmdb.URL
	s := fixtureSettings(fixture)
	s.EmbyURL = emby.URL
	result, err := scanLibrary(s, true, 1, false, time.Time{}, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(missingCodes(result)) != 0 {
		t.Fatal("absolute numbering emitted false missing episodes")
	}
	review := result["diagnostics"].(map[string]any)["review"].([]scanCompareEntry)
	if len(review) != 1 || !review[0].NeedsReview || review[0].EmbyEpisodes != 1836 || review[0].TMDBEpisodes != 1836 {
		t.Fatalf("expected explicit uncertain numbering: %+v", review)
	}
	local := review[0].LocalOrder
	if local == nil || local.Issue != "" || local.GapCount != 14 || len(local.Ranges) != 1 || local.Ranges[0].First != 1 || local.Ranges[0].Last != 1850 {
		t.Fatalf("resource numbering did not recover the 14 real holes: %+v", local)
	}
	for _, gap := range local.Ranges[0].Gaps {
		if !holes[gap.Episode] {
			t.Fatalf("invented resource gap: %+v", gap)
		}
	}
	if entry, ok := seriesScanCache.Get("s1"); ok && entry.Complete {
		t.Fatal("uncertain numbering was archived as complete")
	}
	if seasonCalls != 0 {
		t.Fatal("ambiguous numbering should be recognized before unnecessary season requests")
	}
	if err = saveScanResult(result); err != nil {
		t.Fatal(err)
	}
	result, err = scanLibrary(s, true, 1, true, time.Now(), "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if result["summary"].(map[string]any)["seriesNeedsReview"] != 1 {
		t.Fatal("incremental scan lost pending numbering review")
	}
	review = result["diagnostics"].(map[string]any)["review"].([]scanCompareEntry)
	if review[0].LocalOrder == nil || review[0].LocalOrder.GapCount != 14 {
		t.Fatal("incremental scan lost resource audit")
	}
}

func TestSelectedRescanReplacesOnlySelectedResults(t *testing.T) {
	setupAPITest(t, settings{})
	t.Setenv("CONFIG_PATH", store.path)
	previous := map[string]any{
		"scannedAt": "2026-01-01T00:00:00Z",
		"summary":   map[string]any{"seriesScanned": 20, "unmatchedSeries": 2},
		"missing":   []any{map[string]any{"embySeriesId": "a"}, map[string]any{"embySeriesId": "b"}, map[string]any{"embySeriesId": "c"}},
		"diagnostics": map[string]any{
			"compared": []any{map[string]any{"id": "a"}, map[string]any{"id": "b"}, map[string]any{"id": "c"}},
			"review":   []any{map[string]any{"id": "a", "needsReview": true}, map[string]any{"id": "d", "needsReview": true}},
		},
		"unmatched": map[string]any{"series": []any{map[string]any{"id": "a"}, map[string]any{"id": "e"}}},
	}
	if err := saveScanResult(previous); err != nil {
		t.Fatal(err)
	}
	fresh := map[string]any{
		"scannedAt":   "2026-02-01T00:00:00Z",
		"summary":     map[string]any{"seriesRescanned": 2, "seriesCached": 0},
		"missing":     []missingEpisode{{EmbySeriesID: "b", Code: "S01E02"}},
		"diagnostics": map[string]any{"review": []scanCompareEntry{{ID: "a", NeedsReview: true, EmbyEpisodes: 1836}}, "compared": []scanCompareEntry{{ID: "b"}}},
	}
	merged := mergeSelectedSeriesScanResult(map[string]bool{"a": true, "b": true}, fresh)
	missing := anySlice(merged["missing"])
	if len(missing) != 2 || missingItemSeriesID(missing[0]) != "c" || missingItemSeriesID(missing[1]) != "b" {
		t.Fatalf("rescan duplicated or lost other series: %v", missing)
	}
	diagnostics := merged["diagnostics"].(map[string]any)
	if len(anySlice(diagnostics["review"])) != 2 || len(anySlice(diagnostics["compared"])) != 2 {
		t.Fatal("rescan lost unrelated diagnostics")
	}
	summary := merged["summary"].(map[string]any)
	if summary["seriesNeedsReview"] != 2 || summary["unmatchedSeries"] != 1 {
		t.Fatal("review counts not updated")
	}
	if merged["scannedAt"] != previous["scannedAt"] {
		t.Fatal("targeted rescan advanced whole-library incremental cutoff")
	}
}
