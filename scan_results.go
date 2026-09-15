package main

import "encoding/json"

func mergeSelectedSeriesScanResult(ids map[string]bool, fresh map[string]any) map[string]any {
	if len(ids) == 0 {
		return fresh
	}
	previous, err := loadScanResult()
	if err != nil || previous == nil {
		return fresh
	}
	// Normalize only the small rescan, so persisted and typed diagnostic entries
	// use the same representation without copying the whole library result.
	raw, err := json.Marshal(fresh)
	if err != nil {
		return fresh
	}
	var normalized map[string]any
	if json.Unmarshal(raw, &normalized) != nil {
		return fresh
	}
	merge := func(old, new any, idKey string) []any {
		result := []any{}
		for _, value := range anySlice(old) {
			item, ok := value.(map[string]any)
			if !ok || !ids[anyToString(item[idKey])] {
				result = append(result, value)
			}
		}
		for _, value := range anySlice(new) {
			if item, ok := value.(map[string]any); ok && ids[anyToString(item[idKey])] {
				result = append(result, value)
			}
		}
		return result
	}
	object := func(parent map[string]any, key string) map[string]any {
		if value, ok := parent[key].(map[string]any); ok {
			return value
		}
		value := map[string]any{}
		parent[key] = value
		return value
	}
	previous["missing"] = merge(previous["missing"], normalized["missing"], "embySeriesId")
	diagnostics, latest := object(previous, "diagnostics"), object(normalized, "diagnostics")
	for _, key := range []string{"compared", "skipped", "review"} {
		diagnostics[key] = merge(diagnostics[key], latest[key], "id")
	}
	unmatched, latestUnmatched := object(previous, "unmatched"), object(normalized, "unmatched")
	oldUnmatchedCount := len(anySlice(unmatched["series"]))
	unmatched["series"] = merge(unmatched["series"], latestUnmatched["series"], "id")
	summary, freshSummary := object(previous, "summary"), object(normalized, "summary")
	oldUnmatchedTotal, _ := anyToInt(summary["unmatchedSeries"])
	summary["unmatchedSeries"] = maxInt(0, oldUnmatchedTotal+len(anySlice(unmatched["series"]))-oldUnmatchedCount)
	summary["totalMissingEpisodes"] = len(anySlice(previous["missing"]))
	summary["seriesNeedsReview"] = len(anySlice(diagnostics["review"]))
	summary["scanMode"] = "single"
	summary["seriesRescanned"] = freshSummary["seriesRescanned"]
	summary["seriesCached"] = freshSummary["seriesCached"]
	diagnostics["rescannedSeries"] = freshSummary["seriesRescanned"]
	diagnostics["unmatchedSeries"] = summary["unmatchedSeries"]
	diagnostics["comparedCount"] = len(anySlice(diagnostics["compared"]))
	diagnostics["skippedCount"] = len(anySlice(diagnostics["skipped"]))
	// A targeted rescan must not advance the full-library incremental cutoff.
	previous["verifiedAt"] = normalized["scannedAt"]
	return previous
}
