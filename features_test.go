package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func setupAPITest(t *testing.T, s settings) string {
	t.Helper()
	previousStore, previousSecret, previousDB := store, jwtSecret, stateDB
	store = &settingsStore{path: filepath.Join(t.TempDir(), "config.json"), data: s}
	jwtSecret = []byte("test-secret")
	stateDB = nil
	t.Cleanup(func() { store, jwtSecret, stateDB = previousStore, previousSecret, previousDB })
	token, err := signToken("tester", time.Now().Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	return token
}

func apiTestRequest(t *testing.T, token, method, path string, body any) *httptest.ResponseRecorder {
	t.Helper()
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(method, path, bytes.NewReader(raw))
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	route(w, req)
	return w
}

func TestRemovedAPIsAreUnavailable(t *testing.T) {
	token := setupAPITest(t, settings{})
	paths := []string{
		"/api/search", "/api/search-missing", "/api/search-results", "/api/tmdb/search",
		"/api/115/transfer", "/api/hdhive/search", "/api/hdhive/login", "/api/hdhive/unlock", "/api/hdhive/checkin",
		"/api/subscriptions", "/api/subscriptions/delete", "/api/subscriptions/archive", "/api/subscriptions/run", "/api/subscriptions/webhook",
	}
	for _, path := range paths {
		for _, method := range []string{http.MethodGet, http.MethodPost} {
			t.Run(method+path, func(t *testing.T) {
				w := apiTestRequest(t, token, method, path, map[string]any{})
				if w.Code != http.StatusNotFound {
					t.Fatalf("status = %d, want 404: %s", w.Code, w.Body)
				}
			})
		}
	}
	w := apiTestRequest(t, token, http.MethodPost, "/api/jobs", map[string]any{"type": "scan-search"})
	if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), "任务类型仅支持 scan") {
		t.Fatalf("retired task must be rejected: %d %s", w.Code, w.Body)
	}
	path := filepath.Join(t.TempDir(), "jobs.json")
	state := persistedJobState{Jobs: map[string]*job{
		"retired": {ID: "retired", Type: "scan-search", Status: jobPending, UpdatedAt: time.Now()},
		"scan":    {ID: "scan", Type: "scan", Status: jobPending, UpdatedAt: time.Now()},
	}}
	if err := saveStateJSON("jobs", path, state); err != nil {
		t.Fatal(err)
	}
	manager := newJobManager(path)
	if manager.get("retired") != nil || manager.get("scan") == nil {
		t.Fatal("only scan jobs should survive an upgrade")
	}
}

func TestSettingsRetainScanAndMoviePilotAcrossUpgrade(t *testing.T) {
	setupAPITest(t, settings{})
	path := store.path
	legacy := `{"embyUrl":"http://emby.test","embyApiKey":"emby-key","tmdbApiKey":"tmdb-key","mpUrl":"http://mp.test","mpToken":"mp-key","scanAutoEnabled":true,"scanAutoInterval":12,"scanAutoRecentOnly":true,"excludedLibraries":["lib-1"],"pansouUrl":"http://retired.test","hdhiveCookie":"obsolete","subEnabled":true,"openaiApiKey":"obsolete"}`
	if err := os.WriteFile(path, []byte(legacy), 0600); err != nil {
		t.Fatal(err)
	}
	store = newSettingsStore(path)
	updated, err := store.Update(map[string]any{"scanConcurrency": 8, "pansouUrl": "ignored", "mpToken": ""})
	if err != nil {
		t.Fatal(err)
	}
	if updated.EmbyAPIKey != "emby-key" || updated.TMDBAPIKey != "tmdb-key" || updated.MPToken != "mp-key" || updated.ScanConcurrency != 8 || !updated.ScanAutoEnabled {
		t.Fatal("retained configuration was not preserved")
	}
	if !reflect.DeepEqual(updated.ExcludedLibraries, []string{"lib-1"}) {
		t.Fatal("library exclusions were not preserved")
	}
	allowed := map[string]bool{}
	for _, key := range []string{"embyUrl", "embyApiKey", "embyUserId", "tmdbApiKey", "scanConcurrency", "excludedLibraries", "scanAutoEnabled", "scanAutoInterval", "scanAutoRecentOnly", "mpUrl", "mpToken", "ready"} {
		allowed[key] = true
	}
	masked := maskSettings(updated)
	for key := range masked {
		if !allowed[key] {
			t.Errorf("unexpected setting exposed: %s", key)
		}
	}
	if masked["mpToken"] == "mp-key" || masked["embyApiKey"] == "emby-key" {
		t.Fatal("credentials were not masked")
	}
	var saved map[string]any
	if err := loadStateJSON("settings", path, &saved); err != nil {
		t.Fatal(err)
	}
	for key := range saved {
		if !allowed[key] {
			t.Errorf("unexpected setting persisted: %s", key)
		}
	}
	if got := testConnection("all", settings{}); len(got) != 3 || got["emby"] == nil || got["tmdb"] == nil || got["mp"] == nil {
		t.Fatalf("unexpected connection targets: %v", got)
	}
}

func TestMoviePilotSearchDownloadAndSubscribe(t *testing.T) {
	var downloads, subscriptions int
	mp := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/v1/search/title", "/api/v1/search/media/tmdb:101":
			if r.Header.Get("x-api-key") != "test-mp-key" {
				t.Error("search API key missing")
			}
			writeJSON(w, http.StatusOK, map[string]any{"data": []any{
				map[string]any{"torrent_info": map[string]any{"title": "Example.S02E09.1080p", "enclosure": "https://example.test/wrong.torrent"}},
				map[string]any{"torrent_info": map[string]any{"title": "Example.S02E05.1080p", "enclosure": "https://example.test/match.torrent"}},
			}})
		case "/api/v1/download/add":
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			if r.Method != http.MethodPost || r.Header.Get("x-api-key") != "test-mp-key" || body["tmdbid"] != float64(101) || body["torrent_in"] == nil {
				t.Error("download payload or authentication missing")
			}
			downloads++
			writeJSON(w, http.StatusOK, map[string]any{"success": true})
		case "/api/v1/subscribe/seerr":
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			media, _ := body["media"].(map[string]any)
			extra, _ := body["extra"].([]any)
			if r.Header.Get("Authorization") != "test-mp-key" || media["tmdbId"] != float64(101) || media["media_type"] != "tv" || len(extra) != 1 {
				t.Error("subscription payload or authentication missing")
			} else if extra[0].(map[string]any)["value"] != "2" {
				t.Error("wrong subscription season")
			}
			subscriptions++
			writeJSON(w, http.StatusOK, map[string]any{"success": true})
		case "/api/v1/subscribe/media/tmdb:101":
			if r.URL.Query().Get("token") != "test-mp-key" || r.URL.Query().Get("season") != "2" {
				t.Error("subscription status query missing")
			}
			writeJSON(w, http.StatusOK, map[string]any{"data": map[string]any{"id": 12, "name": "Example", "season": 2}})
		default:
			http.NotFound(w, r)
		}
	}))
	defer mp.Close()
	token := setupAPITest(t, settings{MPUrl: mp.URL, MPToken: "test-mp-key"})
	w := apiTestRequest(t, token, http.MethodPost, "/api/mp/search", map[string]any{"keyword": "Example", "tmdbId": "101", "season": 2, "episodes": []int{5}})
	var found struct {
		Results []map[string]any `json:"results"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &found); err != nil || w.Code != http.StatusOK || len(found.Results) != 2 {
		t.Fatalf("search did not return deduplicated results: %d %s", w.Code, w.Body)
	}
	if found.Results[0]["title"] != "Example.S02E05.1080p" {
		t.Fatal("missing episode match did not rank first")
	}
	for _, action := range []struct {
		Path string
		Body map[string]any
	}{
		{"/api/mp/download", map[string]any{"tmdbId": "101", "rawData": found.Results[0]}},
		{"/api/mp/subscribe", map[string]any{"tmdbId": "101", "mediaType": "tv", "season": 2, "title": "Example"}},
		{"/api/mp/subscribe/status", map[string]any{"tmdbId": "101", "mediaType": "tv", "season": 2}},
	} {
		w := apiTestRequest(t, token, http.MethodPost, action.Path, action.Body)
		if w.Code != http.StatusOK {
			t.Errorf("%s: %d %s", action.Path, w.Code, w.Body)
		}
	}
	if downloads != 1 || subscriptions != 1 {
		t.Fatal("MoviePilot action was not forwarded exactly once")
	}
}

func TestMoviePilotEmptySubscriptionStatus(t *testing.T) {
	for _, raw := range []string{`null`, `{}`, `[]`, `{"data":null,"success":true}`, `{"data":[],"success":true}`} {
		var decoded any
		_ = json.Unmarshal([]byte(raw), &decoded)
		if normalizeMPSubscriptionStatus(decoded)["exists"] != false {
			t.Errorf("empty response was reported as an existing subscription: %s", raw)
		}
	}
}
