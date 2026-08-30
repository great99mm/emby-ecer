package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

type scanFixture struct {
	emby              *httptest.Server
	tmdb              *httptest.Server
	seasonDetailCalls int32
	episodeListCalls  int32
	showEpisodeCalls  int32
}

// newScanFixture 起一套假的 Emby / TMDB：
// 剧 s1《合并集剧》第一季 TMDB 有 4 集，Emby 有 E01-E02 合并文件 + E03，缺 E04
// 剧 s2《完整剧》第一季 TMDB 有 2 集，Emby 两集都有 —— 用来验证季级短路
// 剧 s3《被屏蔽剧》属于被屏蔽媒体库，不应出现在结果里
func newScanFixture(t *testing.T) *scanFixture {
	t.Helper()
	fixture := &scanFixture{}

	fixture.emby = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		query := r.URL.Query()
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/Library/VirtualFolders":
			_ = json.NewEncoder(w).Encode([]map[string]any{
				{"Name": "剧集", "ItemId": "lib-tv", "CollectionType": "tvshows"},
				{"Name": "短剧", "ItemId": "lib-short", "CollectionType": "tvshows"},
			})
		case strings.HasSuffix(r.URL.Path, "/Items") && query.Get("ParentId") == "lib-short":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"Items":            []map[string]any{{"Id": "s3", "Name": "被屏蔽剧", "Type": "Series"}},
				"TotalRecordCount": 1,
			})
		case strings.HasSuffix(r.URL.Path, "/Items") && query.Get("IncludeItemTypes") == "Episode":
			atomic.AddInt32(&fixture.episodeListCalls, 1)
			_ = json.NewEncoder(w).Encode(map[string]any{"Items": []map[string]any{
				{"Id": "e1", "SeriesId": "s1", "ParentIndexNumber": 1, "IndexNumber": 1, "IndexNumberEnd": 2},
				{"Id": "e2", "SeriesId": "s1", "ParentIndexNumber": 1, "IndexNumber": 3},
				{"Id": "e3", "SeriesId": "s2", "ParentIndexNumber": 1, "IndexNumber": 1},
				{"Id": "e4", "SeriesId": "s2", "ParentIndexNumber": 1, "IndexNumber": 2},
			}})
		case strings.HasSuffix(r.URL.Path, "/Items"):
			_ = json.NewEncoder(w).Encode(map[string]any{"Items": []map[string]any{
				{"Id": "s1", "Name": "合并集剧", "Type": "Series", "ProviderIds": map[string]string{"Tmdb": "101"}},
				{"Id": "s2", "Name": "完整剧", "Type": "Series", "ProviderIds": map[string]string{"Tmdb": "102"}},
				{"Id": "s3", "Name": "被屏蔽剧", "Type": "Series", "ProviderIds": map[string]string{"Tmdb": "103"}},
			}, "TotalRecordCount": 3})
		case strings.Contains(r.URL.Path, "/Shows/"):
			atomic.AddInt32(&fixture.showEpisodeCalls, 1)
			_ = json.NewEncoder(w).Encode(map[string]any{"Items": []map[string]any{}})
		default:
			_ = json.NewEncoder(w).Encode(map[string]any{"Items": []map[string]any{}})
		}
	}))
	t.Cleanup(fixture.emby.Close)

	fixture.tmdb = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/tv/101":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id": 101, "name": "合并集剧", "first_air_date": "2020-01-01",
				"seasons": []map[string]any{{"season_number": 1, "episode_count": 4}},
			})
		case "/tv/102":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id": 102, "name": "完整剧", "first_air_date": "2021-01-01",
				"seasons": []map[string]any{{"season_number": 1, "episode_count": 2}},
			})
		case "/tv/103":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id": 103, "name": "被屏蔽剧", "first_air_date": "2022-01-01",
				"seasons": []map[string]any{{"season_number": 1, "episode_count": 3}},
			})
		default:
			atomic.AddInt32(&fixture.seasonDetailCalls, 1)
			episodes := []map[string]any{}
			count := 4
			if strings.HasPrefix(r.URL.Path, "/tv/103") {
				count = 3
			}
			for i := 1; i <= count; i++ {
				episodes = append(episodes, map[string]any{
					"id": 1000 + i, "episode_number": i, "name": "第 " + string(rune('0'+i)) + " 集", "air_date": "2020-01-0" + string(rune('0'+i)),
				})
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"episodes": episodes})
		}
	}))
	t.Cleanup(fixture.tmdb.Close)

	previousTMDB := tmdbBaseURL
	tmdbBaseURL = fixture.tmdb.URL
	t.Cleanup(func() { tmdbBaseURL = previousTMDB })

	dir := t.TempDir()
	t.Setenv("CONFIG_PATH", filepath.Join(dir, "config.json"))
	seriesScanCache = newSeriesScanCacheStore(filepath.Join(dir, "series-scan-cache.json"))
	episodeIgnores = newEpisodeIgnoreStore(filepath.Join(dir, "episode-ignores.json"))
	return fixture
}

func fixtureSettings(fixture *scanFixture) settings {
	return settings{EmbyURL: fixture.emby.URL, EmbyAPIKey: "key", TMDBAPIKey: "key", ScanConcurrency: 2}
}

func missingCodes(result map[string]any) []string {
	codes := make([]string, 0)
	for _, item := range result["missing"].([]missingEpisode) {
		codes = append(codes, item.EmbyTitle+" "+item.Code)
	}
	return codes
}

func TestScanLibraryUsesInventoryAndMergedEpisodes(t *testing.T) {
	fixture := newScanFixture(t)
	result, err := scanLibrary(fixtureSettings(fixture), true, 0, false, time.Time{}, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	codes := missingCodes(result)
	merged := make([]string, 0)
	for _, code := range codes {
		if strings.HasPrefix(code, "完整剧") {
			t.Fatalf("《完整剧》已齐，不应有缺集: %v", codes)
		}
		if strings.HasPrefix(code, "合并集剧") {
			merged = append(merged, code)
		}
	}
	// E01-E02 是一个合并文件，只有 E04 真的缺
	if len(merged) != 1 || merged[0] != "合并集剧 S01E04" {
		t.Fatalf("《合并集剧》应只缺 S01E04，实际 %v", merged)
	}
	if fixture.showEpisodeCalls != 0 {
		t.Fatalf("走全库单集缓存时不应再逐剧请求 /Shows，实际 %d 次", fixture.showEpisodeCalls)
	}
	// 只有《合并集剧》和《被屏蔽剧》需要季详情，《完整剧》被季级短路省掉
	if fixture.seasonDetailCalls != 2 {
		t.Fatalf("季详情请求次数应为 2（完整剧被短路），实际 %d", fixture.seasonDetailCalls)
	}
}

func TestScanLibrarySkipsExcludedLibraries(t *testing.T) {
	fixture := newScanFixture(t)
	s := fixtureSettings(fixture)
	s.ExcludedLibraries = []string{"lib-short"}
	result, err := scanLibrary(s, true, 0, false, time.Time{}, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, code := range missingCodes(result) {
		if strings.HasPrefix(code, "被屏蔽剧") {
			t.Fatalf("被屏蔽媒体库的剧集不应出现在结果里: %v", code)
		}
	}
	summary := result["summary"].(map[string]any)
	if summary["seriesScanned"].(int) != 2 {
		t.Fatalf("屏蔽后应只扫 2 部剧，实际 %v", summary["seriesScanned"])
	}
}

func TestScanLibraryRecentOnlyReusesUnchangedSeries(t *testing.T) {
	fixture := newScanFixture(t)
	s := fixtureSettings(fixture)
	first, err := scanLibrary(s, true, 0, false, time.Time{}, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := saveScanResult(first); err != nil {
		t.Fatal(err)
	}
	before := atomic.LoadInt32(&fixture.seasonDetailCalls)

	// 假数据没有任何变更时间，最近变更模式下应全部沿用上次结果
	second, err := scanLibrary(s, true, 0, true, time.Now(), "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := atomic.LoadInt32(&fixture.seasonDetailCalls); got != before {
		t.Fatalf("未变更的剧集不应再请求 TMDB 季详情，多了 %d 次", got-before)
	}
	if len(missingCodes(second)) != len(missingCodes(first)) {
		t.Fatalf("沿用结果的缺集数量应与上次一致：%v vs %v", missingCodes(second), missingCodes(first))
	}
}

func TestVerifyMissingEpisodesDropsFilledOnes(t *testing.T) {
	fixture := newScanFixture(t)
	s := fixtureSettings(fixture)
	// S01E02 在 Emby 里由 E01-E02 合并文件提供，校验后应被摘掉
	stale := map[string]any{
		"scannedAt": time.Now().Format(time.RFC3339),
		"summary":   map[string]any{"totalMissingEpisodes": 2},
		"missing": []any{
			map[string]any{"embySeriesId": "s1", "season": 1, "episode": 2, "code": "S01E02"},
			map[string]any{"embySeriesId": "s1", "season": 1, "episode": 4, "code": "S01E04"},
		},
	}
	if err := saveScanResult(stale); err != nil {
		t.Fatal(err)
	}
	removed, result, err := verifyMissingEpisodes(s)
	if err != nil {
		t.Fatal(err)
	}
	if removed != 1 {
		t.Fatalf("应摘掉 1 集已补齐的记录，实际 %d", removed)
	}
	if left := len(anySlice(result["missing"])); left != 1 {
		t.Fatalf("校验后应还剩 1 集，实际 %d", left)
	}
}

func TestScanLibraryHonorsEpisodeIgnores(t *testing.T) {
	fixture := newScanFixture(t)
	episodeIgnores.Add([]ignoredEpisode{{SeriesID: "s1", SeriesName: "合并集剧", Season: 1, Episode: 4}})
	result, err := scanLibrary(fixtureSettings(fixture), true, 0, false, time.Time{}, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, code := range missingCodes(result) {
		if code == "合并集剧 S01E04" {
			t.Fatal("已忽略的单集不应再出现在缺集列表里")
		}
	}
}
