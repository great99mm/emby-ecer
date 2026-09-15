package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"strings"
	"testing"
)

func TestConnectionDraftDoesNotSave(t *testing.T) {
	for _, target := range []string{"emby", "tmdb", "mp"} {
		for _, tc := range []struct {
			name     string
			savedKey string
			draftKey string
			wantKey  string
			wantOK   bool
			legacy   bool
			clearURL bool
		}{
			{name: "first configuration", draftKey: "draft-key", wantKey: "draft-key", wantOK: true},
			{name: "replace stored values", savedKey: "saved-key", draftKey: "  draft-key  ", wantKey: "draft-key", wantOK: true},
			{name: "blank secret reuses saved key", savedKey: "saved-key", draftKey: "  ", wantKey: "saved-key", wantOK: true},
			{name: "failed draft", savedKey: "saved-key", draftKey: "invalid-key", wantKey: "invalid-key"},
			{name: "missing secret"},
			{name: "cleared address", savedKey: "saved-key", draftKey: "draft-key", clearURL: true},
			{name: "stored settings remain supported", savedKey: "saved-key", wantKey: "saved-key", wantOK: true, legacy: true},
		} {
			if target == "tmdb" && tc.clearURL {
				continue
			}
			t.Run(target+"/"+tc.name, func(t *testing.T) {
				requests := make(chan string, 4)
				mock := func(source string) *httptest.Server {
					server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
						requests <- source
						if r.Method != http.MethodGet || (target != "mp" && r.URL.Query().Get("api_key") != tc.wantKey) {
							t.Error("test did not use the expected draft credential")
						}
						if target == "emby" && (r.URL.Path != "/System/Info" || r.Header.Get("X-Emby-Token") != tc.wantKey) {
							t.Error("unexpected Emby request")
						}
						if target == "tmdb" && r.URL.Path != "/configuration" {
							t.Error("unexpected TMDB request")
						}
						if target == "mp" && (r.URL.Path != "/api/v1/system/modulelist" || r.Header.Get("X-API-KEY") != tc.wantKey) {
							t.Error("unexpected MoviePilot request")
						}
						if tc.wantKey == "invalid-key" {
							writeJSON(w, http.StatusUnauthorized, map[string]any{"error": "invalid credential"})
							return
						}
						writeJSON(w, http.StatusOK, map[string]any{"ServerName": source, "success": true})
					}))
					t.Cleanup(server.Close)
					return server
				}
				savedServer, draftServer := mock("saved"), mock("draft")
				previousTMDB := tmdbBaseURL
				tmdbBaseURL = draftServer.URL
				t.Cleanup(func() { tmdbBaseURL = previousTMDB })
				initial := settings{ScanAutoEnabled: true, MPToken: "unrelated-mp-token"}
				if target == "mp" {
					initial.MPToken = ""
				}
				if tc.savedKey != "" {
					initial.EmbyURL = savedServer.URL
					initial.EmbyAPIKey = tc.savedKey
					initial.TMDBAPIKey = tc.savedKey
					initial.MPUrl = savedServer.URL
					initial.MPToken = tc.savedKey
				}
				token := setupAPITest(t, initial)
				before, err := store.Update(nil)
				if err != nil {
					t.Fatal(err)
				}
				diskBefore, err := os.ReadFile(store.path)
				if err != nil {
					t.Fatal(err)
				}
				draft := map[string]any{"tmdbApiKey": tc.draftKey}
				if target == "emby" {
					draft = map[string]any{"embyUrl": "  " + draftServer.URL + "/  ", "embyApiKey": tc.draftKey, "embyUserId": ""}
					if tc.clearURL {
						draft["embyUrl"] = ""
					}
				}
				if target == "mp" {
					draft = map[string]any{"mpUrl": "  " + draftServer.URL + "/  ", "mpToken": tc.draftKey}
					if tc.clearURL {
						draft["mpUrl"] = ""
					}
				}
				body := map[string]any{"target": target}
				if !tc.legacy {
					body["settings"] = draft
				}
				w := apiTestRequest(t, token, http.MethodPost, "/api/settings/test", body)
				var result map[string]struct {
					OK bool `json:"ok"`
				}
				if w.Code != http.StatusOK || json.Unmarshal(w.Body.Bytes(), &result) != nil || len(result) != 1 || result[target].OK != tc.wantOK {
					t.Fatalf("unexpected test result: %d %s", w.Code, w.Body)
				}
				wantCalls := 1
				if tc.wantKey == "" || tc.clearURL {
					wantCalls = 0
				}
				if len(requests) != wantCalls {
					t.Fatalf("upstream requests = %d, want %d", len(requests), wantCalls)
				}
				if wantCalls > 0 {
					wantSource := "draft"
					if target != "tmdb" && tc.legacy {
						wantSource = "saved"
					}
					if source := <-requests; source != wantSource {
						t.Fatalf("used %s address, want %s", source, wantSource)
					}
				}
				if !reflect.DeepEqual(store.Get(), before) {
					t.Fatal("connection test changed active settings")
				}
				diskAfter, err := os.ReadFile(store.path)
				if err != nil || !bytes.Equal(diskBefore, diskAfter) {
					t.Fatal("connection test changed saved settings")
				}
			})
		}
	}
}

func TestConnectionRejectsMalformedDraft(t *testing.T) {
	token := setupAPITest(t, settings{})
	for _, body := range []string{`{"target":"emby","settings":`, `{"target":"emby","settings":"invalid"}`} {
		req := httptest.NewRequest(http.MethodPost, "/api/settings/test", strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+token)
		w := httptest.NewRecorder()
		route(w, req)
		if w.Code != http.StatusBadRequest {
			t.Fatalf("malformed draft status = %d, want 400", w.Code)
		}
	}
}
