package main

import (
	"encoding/json"
	"time"
)

// Jobs are independent records. Saving one task must not rewrite the full
// results of every earlier scan. Metadata can be read without loading results.
func (s *sqliteStateStore) loadJobs() (persistedJobState, error) {
	state := persistedJobState{Jobs: map[string]*job{}}
	rows, err := s.db.Query(`SELECT metadata, result IS NOT NULL FROM scan_jobs`)
	if err != nil {
		return state, err
	}
	defer rows.Close()
	for rows.Next() {
		var raw []byte
		var stored bool
		if err := rows.Scan(&raw, &stored); err != nil {
			return state, err
		}
		var item job
		if err := json.Unmarshal(raw, &item); err != nil {
			return state, err
		}
		item.resultStored = stored
		state.Jobs[item.ID] = &item
	}
	return state, rows.Err()
}

func (s *sqliteStateStore) loadJobResult(id string) (any, error) {
	var raw []byte
	if err := s.db.QueryRow(`SELECT result FROM scan_jobs WHERE id = ?`, id).Scan(&raw); err != nil {
		return nil, err
	}
	var result any
	if len(raw) == 0 {
		return nil, nil
	}
	if err := json.Unmarshal(raw, &result); err != nil {
		return nil, err
	}
	return result, nil
}

func (s *sqliteStateStore) saveJobs(state persistedJobState, savedAt map[string]time.Time) error {
	type record struct {
		id               string
		metadata, result []byte
		updated          time.Time
	}
	records := []record{}
	for id, item := range state.Jobs {
		if savedAt[id].Equal(item.UpdatedAt) {
			continue
		}
		metadata := cloneJob(item)
		metadata.Result = nil
		raw, err := json.Marshal(metadata)
		if err != nil {
			return err
		}
		var result []byte
		if item.Result != nil {
			result, err = json.Marshal(item.Result)
			if err != nil {
				return err
			}
		}
		records = append(records, record{id, raw, result, item.UpdatedAt})
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, row := range records {
		_, err = tx.Exec(`INSERT INTO scan_jobs(id, metadata, result, updated_at) VALUES (?, ?, ?, ?)
ON CONFLICT(id) DO UPDATE SET metadata=excluded.metadata, result=COALESCE(excluded.result, scan_jobs.result), updated_at=excluded.updated_at`, row.id, row.metadata, row.result, row.updated.Format(time.RFC3339Nano))
		if err != nil {
			return err
		}
	}
	_, err = tx.Exec(`DELETE FROM scan_jobs WHERE updated_at < ? AND json_extract(CAST(metadata AS TEXT), '$.status') IN ('done', 'error')`, time.Now().Add(-time.Hour).Format(time.RFC3339Nano))
	if err != nil {
		return err
	}
	// Legacy records have now been migrated atomically to scan_jobs.
	if _, err = tx.Exec(`DELETE FROM app_state WHERE key = 'jobs'`); err != nil {
		return err
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	for _, row := range records {
		savedAt[row.id] = row.updated
	}
	for id := range savedAt {
		if state.Jobs[id] == nil {
			delete(savedAt, id)
		}
	}
	return nil
}
