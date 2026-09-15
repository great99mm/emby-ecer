package main

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"
)

// Match the live scan's 32,000 missing episodes and include the final durable write.
func BenchmarkScanProgress(b *testing.B) {
	previousDB := stateDB
	db, err := initSQLiteState(filepath.Join(b.TempDir(), "state.db"))
	if err != nil {
		b.Fatal(err)
	}
	stateDB = db
	b.Cleanup(func() { stateDB = previousDB; _ = db.db.Close() })
	manager := newJobManager(filepath.Join(b.TempDir(), "jobs.json"))
	item := manager.create("scan")
	missing := make([]missingEpisode, 32000)
	for i := range missing {
		missing[i] = missingEpisode{ID: fmt.Sprint(i), EmbySeriesID: fmt.Sprint(i / 20), OfficialTitle: "性能测试剧集", Season: 1, Episode: i%20 + 1, Overview: strings.Repeat("剧情简介", 40)}
	}
	result := map[string]any{"scan": map[string]any{"missing": missing}}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		manager.update(item.ID, func(j *job) { j.Status = jobRunning; j.Progress = i % 99; j.Result = result })
	}
	manager.update(item.ID, func(j *job) { j.Status = jobDone; j.Progress = 100 })
}
