package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestResourceOrderSearchCannotUseTMDBCoordinates(t *testing.T) {
	calls := []string{}
	mp := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls = append(calls, r.URL.Path)
		if r.URL.Query().Get("keyword") != "动画 国映TV" {
			t.Error("resource edition search keyword was changed")
		}
		_ = json.NewEncoder(w).Encode([]map[string]any{{"title": "动画 S01E015", "enclosure": "https://torrent.example/file"}})
	}))
	defer mp.Close()
	token := setupAPITest(t, settings{MPUrl: mp.URL, MPToken: "test"})
	response := apiTestRequest(t, token, http.MethodPost, "/api/mp/search", map[string]any{
		"keyword": "动画 国映TV", "numberingBasis": "resource", "tmdbId": "57911", "season": 1, "episodes": []int{15},
	})
	if response.Code != 200 {
		t.Fatalf("search failed: %s", response.Body)
	}
	if len(calls) != 1 || calls[0] != "/api/v1/search/title" {
		t.Fatalf("resource order sent canonical search: %v", calls)
	}
	if strings.Contains(response.Body.String(), `"match"`) {
		t.Fatal("unmapped resource was marked as a matched canonical episode")
	}
}
