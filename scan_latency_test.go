package main

import (
	"compress/gzip"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestCompactScanPreservesEveryEpisodeAndDoesNotMutateSavedResult(t *testing.T) {
	items := []missingEpisode{}
	for n := 1; n <= 2000; n++ {
		items = append(items, missingEpisode{EmbySeriesID: "a", OfficialTitle: "剧集", TMDBID: 42, Season: 1, Episode: n, EpisodeName: "单集标题", TotalEpisodes: 2000, OwnedEpisodes: 0, Overview: strings.Repeat("剧情", 500)})
	}
	for _, persisted := range []bool{false, true} {
		scan := map[string]any{"missing": items, "scannedAt": "2026-09-15T00:00:00Z", "summary": map[string]any{"totalMissingEpisodes": 2000}}
		if persisted {
			raw, _ := json.Marshal(scan)
			_ = json.Unmarshal(raw, &scan)
		}
		r := httptest.NewRequest(http.MethodGet, "/api/scan/last?view=compact", nil)
		r.Header.Set("Accept-Encoding", "gzip")
		w := httptest.NewRecorder()
		writeScanJSON(w, r, 200, scanResponse(scan, r))
		if w.Header().Get("Content-Encoding") != "gzip" || w.Body.Len() > 30000 {
			t.Fatalf("compact response unexpectedly large: %d", w.Body.Len())
		}
		reader, err := gzip.NewReader(w.Body)
		if err != nil {
			t.Fatal(err)
		}
		defer reader.Close()
		var decoded struct {
			MissingGroups []struct {
				Series   map[string]any
				Episodes []struct {
					Episode     int
					EpisodeName string
				}
			}
		}
		if err := json.NewDecoder(reader).Decode(&decoded); err != nil {
			t.Fatal(err)
		}
		if len(decoded.MissingGroups) != 1 || len(decoded.MissingGroups[0].Episodes) != 2000 {
			t.Fatal("episodes lost in compact response")
		}
		g := decoded.MissingGroups[0]
		if g.Series["embySeriesId"] != "a" || g.Series["tmdbId"] != float64(42) || g.Episodes[1999].Episode != 2000 || g.Episodes[1999].EpisodeName != "单集标题" {
			t.Fatal("episode identity lost")
		}
		if len(anySlice(scan["missing"])) != 2000 || scan["missingGroups"] != nil {
			t.Fatal("saved result was mutated")
		}
	}
	for _, header := range []string{"", "br", "gzip;q=0", "gzip;q=0.000, br"} {
		if acceptsGzip(header) {
			t.Fatalf("gzip not accepted by %q", header)
		}
	}
}

func TestSelectedLibraryQueryFindsSiblingsWithoutWholeLibraryFetch(t *testing.T) {
	calls := 0
	emby := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		q := r.URL.Query()
		if q.Get("IncludeItemTypes") != "Series" || (q.Get("Ids") == "" && q.Get("AnyProviderIdEquals") == "") {
			t.Error("single scan requested whole library")
		}
		items := []embyItem{{ID: "a", Type: "Series", ProviderIDs: map[string]string{"Tmdb": "42"}}}
		if q.Get("AnyProviderIdEquals") == "tmdb.42" {
			items = append(items, embyItem{ID: "b", Type: "Series", ProviderIDs: map[string]string{"Tmdb": "42"}}, embyItem{ID: "unrelated", Type: "Series", ProviderIDs: map[string]string{"Tmdb": "99"}})
		}
		_ = json.NewEncoder(w).Encode(embyItemsResp{Items: items})
	}))
	defer emby.Close()
	items, err := loadSelectedLibraryItems(settings{EmbyURL: emby.URL, EmbyAPIKey: "test"}, map[string]bool{"a": true})
	if err != nil || len(items) != 2 || calls != 2 {
		t.Fatalf("selected query: items=%d calls=%d err=%v", len(items), calls, err)
	}
	ids := map[string]bool{"a": true}
	expandSelectedSeries(ids, seriesIdentityGroups(items, nil))
	if !ids["b"] || ids["unrelated"] {
		t.Fatal("sibling selection incorrect")
	}
}

type blockedJobResult struct{ entered, release chan struct{} }

func (b blockedJobResult) MarshalJSON() ([]byte, error) {
	close(b.entered)
	<-b.release
	return []byte(`{"scan":{}}`), nil
}

func TestJobStatusRemainsReadableDuringSlowCheckpoint(t *testing.T) {
	setupAPITest(t, settings{})
	m := newJobManager(filepath.Join(t.TempDir(), "jobs.json"))
	j := m.create("scan")
	block := blockedJobResult{make(chan struct{}), make(chan struct{})}
	done := make(chan struct{})
	go func() { m.update(j.ID, func(j *job) { j.Status = jobDone; j.Result = block }); close(done) }()
	<-block.entered
	read := make(chan *job, 1)
	go func() { read <- m.get(j.ID) }()
	select {
	case got := <-read:
		if got.Status != jobDone {
			t.Error("latest status not visible")
		}
	case <-time.After(time.Second):
		t.Error("status reader blocked behind result serialization")
	}
	close(block.release)
	<-done
}

func TestExistingSQLiteMigrationDoesNotReadOrDecodePayload(t *testing.T) {
	db, err := initSQLiteState(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.db.Close()
	// Corrupt payloads also count as existing; migration must not overwrite them.
	_, err = db.db.Exec(`INSERT INTO app_state VALUES ('scan_result', 'invalid json', '')`)
	if err != nil {
		t.Fatal(err)
	}
	var out any
	if db.ImportJSONFile("scan_result", "missing.json", &out) || out != nil {
		t.Fatal("existing payload was decoded or migrated")
	}
}

func TestSQLiteJobsMigrateAndPersistIndependently(t *testing.T) {
	setupAPITest(t, settings{})
	db, err := initSQLiteState(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	stateDB = db
	t.Cleanup(func() { stateDB = nil; _ = db.db.Close() })
	legacy := &job{ID: "old", Type: "scan", Status: jobDone, CreatedAt: time.Now(), UpdatedAt: time.Now(), Result: map[string]any{"scan": map[string]any{"missing": []string{"S01E02"}}}}
	if err := db.Save("jobs", persistedJobState{Jobs: map[string]*job{"old": legacy}}); err != nil {
		t.Fatal(err)
	}
	m := newJobManager(filepath.Join(t.TempDir(), "jobs.json"))
	var before []byte
	if err := db.db.QueryRow(`SELECT result FROM scan_jobs WHERE id='old'`).Scan(&before); err != nil {
		t.Fatal(err)
	}
	// Any attempt to rewrite the old task fails. Starting/completing another
	// task must only write that new record, even when old results are huge.
	_, err = db.db.Exec(`CREATE TRIGGER protect_old BEFORE UPDATE ON scan_jobs WHEN OLD.id='old' BEGIN SELECT RAISE(ABORT, 'old result rewritten'); END`)
	if err != nil {
		t.Fatal(err)
	}
	newJob := m.create("scan")
	m.update(newJob.ID, func(j *job) { j.Status = jobDone; j.Result = map[string]any{"scan": "new"} })
	if err := m.persist(); err != nil {
		t.Fatal(err)
	}
	if m.getSummary(newJob.ID).Result != nil || m.get(newJob.ID).Result == nil {
		t.Fatal("completed results must be evicted from memory but readable on demand")
	}
	restarted := newJobManager(m.path)
	if restarted.getSummary("old").Result != nil {
		t.Fatal("startup eagerly decoded historical results")
	}
	if restarted.get("old").Result == nil || restarted.get(newJob.ID).Result == nil {
		t.Fatal("independent job results lost after restart")
	}
	var after []byte
	if err := db.db.QueryRow(`SELECT result FROM scan_jobs WHERE id='old'`).Scan(&after); err != nil || string(before) != string(after) {
		t.Fatal("older result changed")
	}
}

func BenchmarkCompactScanResponse(b *testing.B) {
	items := make([]missingEpisode, 53000)
	for i := range items {
		items[i] = missingEpisode{EmbySeriesID: "a", Season: 1, Episode: i + 1, Overview: strings.Repeat("剧情简介", 100)}
	}
	scan := map[string]any{"missing": items}
	r := httptest.NewRequest("GET", "/api/scan/last?view=compact", nil)
	r.Header.Set("Accept-Encoding", "gzip")
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		w := httptest.NewRecorder()
		writeScanJSON(w, r, 200, scanResponse(scan, r))
		_, _ = io.Copy(io.Discard, w.Body)
	}
}
