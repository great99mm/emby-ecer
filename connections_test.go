package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestConnectionStatusReflectsUpstream(t *testing.T) {
	emby := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/System/Info" || r.Header.Get("X-Emby-Token") != "valid-emby-key" {
			t.Error("Emby was not probed with saved credentials")
		}
		writeJSON(w, http.StatusOK, map[string]any{"ServerName": "Test Emby"})
	}))
	defer emby.Close()
	tmdb := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "invalid-tmdb-key", http.StatusUnauthorized)
	}))
	defer tmdb.Close()
	previousTMDB := tmdbBaseURL
	tmdbBaseURL = tmdb.URL
	t.Cleanup(func() { tmdbBaseURL = previousTMDB })
	token := setupAPITest(t, settings{EmbyURL: emby.URL, EmbyAPIKey: "valid-emby-key", TMDBAPIKey: "invalid-tmdb-key"})
	w := apiTestRequest(t, token, http.MethodPost, "/api/settings/test", map[string]any{"target": "all"})
	var results map[string]struct {
		OK         bool   `json:"ok"`
		Configured bool   `json:"configured"`
		CheckedAt  string `json:"checkedAt"`
		Error      string `json:"error"`
	}
	if w.Code != http.StatusOK || json.Unmarshal(w.Body.Bytes(), &results) != nil || len(results) != 3 {
		t.Fatalf("unexpected response: %d %s", w.Code, w.Body)
	}
	if !results["emby"].OK || !results["emby"].Configured || results["tmdb"].OK || !results["tmdb"].Configured || results["mp"].OK || results["mp"].Configured {
		t.Fatal("availability was inferred from configuration instead of actual responses")
	}
	if !strings.Contains(results["tmdb"].Error, "认证失败") || strings.Contains(w.Body.String(), "invalid-tmdb-key") {
		t.Fatal("authentication failure should be clear without exposing the token")
	}
	for _, result := range results {
		if _, err := time.Parse(time.RFC3339, result.CheckedAt); err != nil {
			t.Fatal("missing check timestamp")
		}
	}
}

func TestMoviePilotProbeRejectsInvalidResponses(t *testing.T) {
	for _, tc := range []struct {
		name   string
		status int
		body   string
	}{
		{"unauthorized", 401, `{"detail":"invalid-token"}`},
		{"forbidden", 403, `{"detail":"invalid-token"}`},
		{"wrong address", 404, `{"detail":"Not Found"}`},
		{"login HTML", 200, `<html>MoviePilot login</html>`},
		{"empty response", 204, ""},
		{"unrecognized JSON", 200, `{}`},
		{"application failure", 200, `{"success":false,"message":"invalid-token rejected"}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			mp := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodGet || r.URL.Path != "/api/v1/system/modulelist" {
					t.Error("probe must use only the read-only authenticated endpoint")
				}
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.body))
			}))
			defer mp.Close()
			result := probeConnection("mp", settings{MPUrl: mp.URL, MPToken: "invalid-token"})
			if result["ok"] != false || result["configured"] != true || result["error"] == "" {
				t.Fatalf("invalid response reported as connected: %v", result)
			}
			if strings.Contains(result["error"].(string), "invalid-token") {
				t.Fatal("probe exposed the saved token")
			}
		})
	}
}
