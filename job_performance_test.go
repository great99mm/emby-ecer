package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestProgressCheckpointsAndTerminalResults(t *testing.T) {
	setupAPITest(t, settings{})
	for _, status := range []jobStatus{jobDone, jobError} {
		m := newJobManager(filepath.Join(t.TempDir(), "jobs.json"))
		j := m.create("scan")
		for i := 0; i < 100; i++ {
			m.update(j.ID, func(j *job) { j.Status = jobRunning; j.Progress = i; j.Result = map[string]any{"missing": i} })
		}
		if m.get(j.ID).Progress != 99 {
			t.Fatal("live progress lost")
		}
		var saved persistedJobState
		if err := loadStateJSON("jobs", m.path, &saved); err != nil {
			t.Fatal(err)
		}
		if saved.Jobs[j.ID].Progress != 0 {
			t.Fatal("progress wrote a full result before checkpoint interval")
		}
		m.lastPersist = time.Now().Add(-31 * time.Second)
		m.update(j.ID, func(j *job) { j.Progress = 90 })
		if err := loadStateJSON("jobs", m.path, &saved); err != nil {
			t.Fatal(err)
		}
		if saved.Jobs[j.ID].Progress != 90 || saved.Jobs[j.ID].Result == nil {
			t.Fatal("partial result checkpoint missing")
		}
		m.update(j.ID, func(j *job) { j.Status = status; j.Progress = 100 })
		restarted := newJobManager(m.path)
		got := restarted.get(j.ID)
		if got.Status != status || got.Progress != 100 || got.Result == nil {
			t.Fatal("terminal result not durable")
		}
	}
}

func TestJobSummaryLeavesFullResultAvailable(t *testing.T) {
	token := setupAPITest(t, settings{})
	previous, previousID := jobMgr, activeScanJobID
	jobMgr = newJobManager(filepath.Join(t.TempDir(), "jobs.json"))
	t.Cleanup(func() { jobMgr, activeScanJobID = previous, previousID })
	j := jobMgr.create("scan")
	activeScanJobID = j.ID
	jobMgr.update(j.ID, func(j *job) {
		j.Status = jobRunning
		j.Progress = 42
		j.Result = map[string]any{"scan": map[string]any{"missing": []string{"S01E02"}}}
	})
	for _, path := range []string{"/api/jobs/" + j.ID, "/api/jobs/active"} {
		w := apiTestRequest(t, token, http.MethodGet, path+"?summary=1", nil)
		var response map[string]any
		if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
			t.Fatal(err)
		}
		if path == "/api/jobs/active" {
			response = response["job"].(map[string]any)
		}
		if w.Code != 200 || response["result"] != nil || response["progress"] != float64(42) {
			t.Fatal("summary must only return progress")
		}
	}
	w := apiTestRequest(t, token, http.MethodGet, "/api/jobs/"+j.ID, nil)
	var full job
	if err := json.Unmarshal(w.Body.Bytes(), &full); err != nil {
		t.Fatal(err)
	}
	if full.Result == nil {
		t.Fatal("summary mutated original results")
	}
}

func TestConcurrentScanStartsReuseOneJob(t *testing.T) {
	setupAPITest(t, settings{})
	previous, previousID := jobMgr, activeScanJobID
	jobMgr = newJobManager(filepath.Join(t.TempDir(), "jobs.json"))
	activeScanJobID = ""
	t.Cleanup(func() { jobMgr, activeScanJobID = previous, previousID })
	var wg sync.WaitGroup
	var started atomic.Int32
	ids := make(chan string, 20)
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			j, created := startScanJob()
			if created {
				started.Add(1)
			}
			ids <- j.ID
		}()
	}
	wg.Wait()
	close(ids)
	if started.Load() != 1 {
		t.Fatal("duplicate scans started")
	}
	for id := range ids {
		if id != activeScanJobID {
			t.Fatal("callers did not receive the same active job")
		}
	}
}

func TestScanProgressFinalResultAndMovieRequestBudget(t *testing.T) {
	fixture := newScanFixture(t)
	items := []embyItem{{ID: "s1", Name: "合并集剧", Type: "Series", ProviderIDs: map[string]string{"Tmdb": "101"}}, {ID: "s2", Type: "Series", ProviderIDs: map[string]string{"Tmdb": "102"}}}
	for i := 0; i < 998; i++ {
		items = append(items, embyItem{Type: "Movie", ProviderIDs: map[string]string{"Tmdb": "999"}})
	}
	emby := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("IncludeItemTypes") == "Series,Movie" {
			if r.URL.Query().Get("StartIndex") == "0" {
				_ = json.NewEncoder(w).Encode(embyItemsResp{Items: items})
			} else {
				_ = json.NewEncoder(w).Encode(embyItemsResp{})
			}
			return
		}
		fixture.emby.Config.Handler.ServeHTTP(w, r)
	}))
	defer emby.Close()
	s := fixtureSettings(fixture)
	s.EmbyURL = emby.URL
	callbacks := 0
	var final map[string]any
	result, err := scanLibrary(s, true, 0, false, time.Time{}, "", func(done, total int, _, _ string, snapshot map[string]any) { callbacks++; final = snapshot })
	if err != nil {
		t.Fatal(err)
	}
	if callbacks > 2 {
		t.Fatalf("per-step full snapshots returned: %d", callbacks)
	}
	if len(final["missing"].([]missingEpisode)) != 1 || len(result["missing"].([]missingEpisode)) != 1 {
		t.Fatal("throttling lost final missing episodes")
	}
	if result["summary"].(map[string]any)["movieTotal"] != 998 {
		t.Fatal("movie inventory count lost")
	}
	if fixture.seasonDetailCalls != 1 {
		t.Fatalf("unexpected movie requests during episode scan: %d", fixture.seasonDetailCalls)
	}
}
