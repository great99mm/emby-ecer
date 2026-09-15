package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

type identityFixture struct {
	settings        settings
	extraSeries     atomic.Bool
	newEpisodeAired atomic.Bool
	newEpisodeOwned atomic.Bool
	showCalls       atomic.Int32
	status          string
	inProduction    bool
}

func newIdentityFixture(t *testing.T, status string, inProduction bool) *identityFixture {
	t.Helper()
	setupAPITest(t, settings{})
	base := newScanFixture(t)
	f := &identityFixture{status: status, inProduction: inProduction}
	episodes := func() []embyEpisode {
		result := []embyEpisode{}
		count := 191
		if f.newEpisodeOwned.Load() {
			count = 192
		}
		for n := 1; n <= count; n++ {
			id := "a"
			if n >= 185 {
				id = "b"
			}
			result = append(result, embyEpisode{ID: fmt.Sprint(n), SeriesID: id, ParentIndexNumber: 1, IndexNumber: n})
		}
		return result
	}
	emby := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/Shows/") {
			f.showCalls.Add(1)
			_ = json.NewEncoder(w).Encode(embyEpisodesResp{Items: episodes()})
			return
		}
		if r.URL.Query().Get("IncludeItemTypes") == "Episode" {
			_ = json.NewEncoder(w).Encode(embyEpisodesResp{Items: episodes()})
			return
		}
		items := []embyItem{}
		for _, id := range []string{"a", "b", "c"} {
			items = append(items, embyItem{ID: id, Name: "凡人修仙传", Type: "Series", ProviderIDs: map[string]string{"Tmdb": "106449"}})
		}
		if f.extraSeries.Load() {
			for n := 0; n < 20; n++ {
				items = append(items, embyItem{ID: fmt.Sprint(n), Name: "其他剧集", Type: "Series"})
			}
		}
		_ = json.NewEncoder(w).Encode(embyItemsResp{Items: items, TotalRecordCount: len(items)})
	}))
	t.Cleanup(emby.Close)
	tmdb := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/tv/106449" {
			count := 191
			if f.status != "Ended" || f.newEpisodeAired.Load() {
				count = 192
			}
			_ = json.NewEncoder(w).Encode(tmdbTVDetail{ID: 106449, Name: "凡人修仙传", Status: f.status, InProduction: f.inProduction, Seasons: []tmdbSeason{{SeasonNumber: 1, EpisodeCount: count}}})
			return
		}
		eps := []tmdbEpisodeDetail{}
		for n := 1; n <= 192; n++ {
			date := "2020-01-01"
			if n == 192 && !f.newEpisodeAired.Load() {
				date = time.Now().AddDate(0, 0, 7).Format("2006-01-02")
			}
			eps = append(eps, tmdbEpisodeDetail{ID: 10000 + n, EpisodeNumber: n, AirDate: date})
		}
		_ = json.NewEncoder(w).Encode(tmdbSeasonDetail{Episodes: eps})
	}))
	t.Cleanup(tmdb.Close)
	tmdbBaseURL = tmdb.URL
	f.settings = fixtureSettings(base)
	f.settings.EmbyURL = emby.URL
	return f
}

func TestMergedCopiesAreCompleteButReturningSeriesIsNotArchived(t *testing.T) {
	f := newIdentityFixture(t, "Returning Series", true)
	result, err := scanLibrary(f.settings, true, 0, false, time.Time{}, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := missingCodes(result); len(got) != 0 {
		t.Fatalf("existing E185-E191 incorrectly missing: %v", got)
	}
	for _, id := range []string{"a", "b", "c"} {
		entry, ok := seriesScanCache.Get(id)
		if !ok || entry.Complete || entry.SeriesStatus != "Returning Series" {
			t.Fatalf("continuing show not tracked properly: %+v", entry)
		}
	}
	for _, entry := range result["diagnostics"].(map[string]any)["compared"].([]scanCompareEntry) {
		if entry.OwnedEpisodes != 191 || len(entry.SourceSeriesIDs) != 3 {
			t.Fatalf("partial copy not merged: %+v", entry)
		}
	}
	_, archived := exemptionLists()
	if len(archived) != 0 {
		t.Fatal("continuing show appeared in archive")
	}
	if f.showCalls.Load() != 0 {
		t.Fatal("bulk scan added per-show requests instead of sharing inventory")
	}
	if err = saveScanResult(result); err != nil {
		t.Fatal(err)
	}
	// Emby metadata has not changed, but a new episode is now aired on TMDB.
	f.newEpisodeAired.Store(true)
	result, err = scanLibrary(f.settings, true, 0, true, time.Now(), "", nil)
	if err != nil {
		t.Fatal(err)
	}
	missing := result["missing"].([]missingEpisode)
	if len(missing) != 1 || missing[0].Episode != 192 || !missing[0].MergedSeries {
		t.Fatalf("new aired episode must be found once: %+v", missing)
	}
	if result["summary"].(map[string]any)["seriesRescanned"] != 3 {
		t.Fatal("ongoing shows were skipped as unchanged")
	}
	// The fast bulk endpoint still partitions physical IDs. Verification uses
	// Emby's merged /Shows endpoint and notices the new episode in the other copy.
	if err = saveScanResult(result); err != nil {
		t.Fatal(err)
	}
	f.newEpisodeOwned.Store(true)
	removed, verified, err := verifyMissingEpisodes(f.settings)
	if err != nil || removed != 1 || len(anySlice(verified["missing"])) != 0 {
		t.Fatalf("merged verification failed: removed=%d err=%v", removed, err)
	}
}

func TestSingleRescanExpandsDuplicateSeriesAndRetainsUnrelatedResults(t *testing.T) {
	f := newIdentityFixture(t, "Returning Series", true)
	f.extraSeries.Store(true)
	old := map[string]any{"scannedAt": "2020-01-01T00:00:00Z", "summary": map[string]any{}, "missing": []missingEpisode{{EmbySeriesID: "a", Episode: 185}, {EmbySeriesID: "b", Episode: 185}, {EmbySeriesID: "c", Episode: 185}, {EmbySeriesID: "unrelated", Episode: 5}}}
	if err := saveScanResult(old); err != nil {
		t.Fatal(err)
	}
	fresh, err := scanLibrary(f.settings, true, 0, false, time.Time{}, "a", nil)
	if err != nil {
		t.Fatal(err)
	}
	if fresh["summary"].(map[string]any)["seriesScanned"] != 3 {
		t.Fatal("single-show rescan left sibling IDs stale")
	}
	if len(missingCodes(fresh)) != 0 {
		t.Fatal("authoritative Emby merged view not used")
	}
	if f.showCalls.Load() != 3 {
		t.Fatal("targeted scan unexpectedly read bulk library")
	}
	merged := mergeSelectedSeriesScanResult(selectedResultIDs(map[string]bool{"a": true}, fresh), fresh)
	items := anySlice(merged["missing"])
	if len(items) != 1 || missingItemSeriesID(items[0]) != "unrelated" {
		t.Fatalf("targeted merge lost unrelated result or kept stale duplicates: %v", items)
	}
}

func TestAutomaticArchiveRequiresEndedAndConsistentProductionStatus(t *testing.T) {
	for _, tc := range []struct {
		status              string
		production, archive bool
	}{
		{"Ended", false, true}, {"Ended", true, false}, {"Returning Series", true, false}, {"Canceled", false, false}, {"", false, false},
	} {
		t.Run(fmt.Sprintf("%s-%t", tc.status, tc.production), func(t *testing.T) {
			f := newIdentityFixture(t, tc.status, tc.production)
			_, err := scanLibrary(f.settings, true, 0, false, time.Time{}, "", nil)
			if err != nil {
				t.Fatal(err)
			}
			entry, ok := seriesScanCache.Get("a")
			if !ok || entry.Complete != tc.archive {
				t.Fatalf("unexpected archive state: %+v", entry)
			}
			if tc.archive {
				if !automaticArchiveValid(entry, entry.Fingerprint) || automaticArchiveValid(entry, entry.Fingerprint+"changed") {
					t.Fatal("changed library can reuse a stale complete archive")
				}
			}
		})
	}
}

func TestIdentityGroupsRespectExclusionsAndNumberingConflicts(t *testing.T) {
	items := []embyItem{{ID: "a", Type: "Series", ProviderIDs: map[string]string{"Tmdb": "101"}}, {ID: "excluded", Type: "Series", ProviderIDs: map[string]string{"Tmdb": "101"}}, {ID: "wrongOrder", Type: "Series", ProviderIDs: map[string]string{"Tmdb": "101"}}}
	groups := seriesIdentityGroups(items, map[string]bool{"excluded": true})
	ids := map[string]bool{"a": true}
	expandSelectedSeries(ids, groups)
	if ids["excluded"] || !ids["wrongOrder"] {
		t.Fatal("excluded library entered duplicate scan scope")
	}
	base := buildSeriesInventory([]embyEpisode{{ParentIndexNumber: 1, IndexNumber: 1}})
	alternate := buildSeriesInventory([]embyEpisode{{ParentIndexNumber: 1, IndexNumber: 2}, {ParentIndexNumber: 1, IndexNumber: 1000}})
	merged, sources := mergedIdentityInventory(base, "a", groups[101], map[string]*seriesInventory{"wrongOrder": alternate}, tmdbTVDetail{Seasons: []tmdbSeason{{SeasonNumber: 1, EpisodeCount: 2}}})
	if merged.has(1, 2) || len(sources) != 1 {
		t.Fatal("incompatible numbering supplied false canonical coverage")
	}
}

func TestMergedEpisodeIgnoresSurviveRescansAndCanBeRemoved(t *testing.T) {
	for _, status := range []string{"Returning Series", "Ended"} {
		for _, mode := range []string{"full", "recent", "single"} {
			t.Run(status+"/"+mode, func(t *testing.T) {
				f := newIdentityFixture(t, status, status != "Ended")
				f.newEpisodeAired.Store(true)
				token := setupAPITest(t, f.settings)
				// Neither another show nor another season may hide S01E192.
				episodeIgnores.Add([]ignoredEpisode{
					{SeriesID: "unrelated", Season: 1, Episode: 192},
					{SeriesID: "b", Season: 2, Episode: 192},
				})
				scan := func(recent bool, id string) map[string]any {
					t.Helper()
					result, err := scanLibrary(f.settings, true, 0, recent, time.Now(), id, nil)
					if err != nil {
						t.Fatal(err)
					}
					if err := saveScanResult(result); err != nil {
						t.Fatal(err)
					}
					return result
				}
				first := scan(false, "")
				missing := first["missing"].([]missingEpisode)
				if len(missing) != 1 || missing[0].Episode != 192 || !missing[0].MergedSeries {
					t.Fatalf("expected one merged missing episode: %+v", missing)
				}
				item := missing[0]
				response := apiTestRequest(t, token, http.MethodPost, "/api/episode-ignores", map[string]any{
					"items": []ignoredEpisode{{SeriesID: item.EmbySeriesID, Season: item.Season, Episode: item.Episode}},
				})
				if response.Code != http.StatusOK {
					t.Fatalf("ignore failed: %s", response.Body)
				}
				// Keep one removable rule, including after loading persisted settings.
				episodeIgnores = newEpisodeIgnoreStore(episodeIgnores.path)
				if got := len(episodeIgnores.List()); got != 3 {
					t.Fatalf("ignore rule was duplicated across copies: %d rules", got)
				}
				selectedID := ""
				if mode == "single" {
					selectedID = "b"
				}
				for attempt := 0; attempt < 2; attempt++ {
					result := scan(mode == "recent", selectedID)
					if got := missingCodes(result); len(got) != 0 {
						t.Fatalf("ignored %s on %s reappeared: %v", item.Code, item.EmbySeriesID, got)
					}
					for _, entry := range result["diagnostics"].(map[string]any)["compared"].([]scanCompareEntry) {
						if entry.TMDBEpisodes != 191 || entry.OwnedEpisodes != 191 {
							t.Fatalf("ignored episode still affects health: %+v", entry)
						}
					}
				}
				// Reload the archive as well: removing an ignore must invalidate it.
				seriesScanCache = newSeriesScanCacheStore(seriesScanCache.path)
				response = apiTestRequest(t, token, http.MethodPost, "/api/episode-ignores/delete", map[string]any{
					"keys": []string{episodeIgnoreKey(item.EmbySeriesID, item.Season, item.Episode)},
				})
				if response.Code != http.StatusOK {
					t.Fatalf("removing ignore failed: %s", response.Body)
				}
				restored := scan(mode == "recent", selectedID)["missing"].([]missingEpisode)
				if len(restored) != 1 || restored[0].Episode != 192 || restored[0].TotalEpisodes != 192 {
					t.Fatalf("removed ignore must restore the missing episode: %+v", restored)
				}
			})
		}
	}
}
