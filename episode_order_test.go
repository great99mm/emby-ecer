package main

import "testing"

func TestOriginalEpisodeTitleOverridesScrapedTitle(t *testing.T) {
	ep := embyEpisode{Name: "错误的刮削标题", ParentIndexNumber: 1, IndexNumber: 236, Path: "/library/动画 - S01E236 - 第 236 集.strm", MediaSources: []any{map[string]any{"Path": "https://media.example/动画.S01E236.实感帽.mp4?secret=hidden"}}}
	got := originalEpisodeInfo(ep)
	if got.Title != "实感帽" || got.Season != 1 || got.Episode != 236 {
		t.Fatalf("original filename not used: %+v", got)
	}
}

func TestSourceFilenameRangesPartsAndEscaping(t *testing.T) {
	for _, tc := range []struct {
		path, title, part string
		episode, end      int
	}{
		{"https://media.example/%E5%8A%A8%E7%94%BB.S01E0500.%E9%BB%91%E6%B4%9E%EF%BC%88%E4%B8%8A%EF%BC%89.mp4?token=private", "黑洞", "上", 500, 0},
		{`C:\media\动画.S01E001-E002.故事.mkv`, "故事", "", 1, 2},
		{"/media/动画 - S01E237 - 第 237 集.strm", "", "", 237, 0},
	} {
		got, ok := episodeInfoFromPath(tc.path)
		if !ok || got.Title != tc.title || got.Part != tc.part || got.Episode != tc.episode || got.EpisodeEnd != tc.end {
			t.Errorf("path parsing: %+v", got)
		}
	}
}

func TestLocalOrderMergedDuplicatesPartsAndUnknownEnds(t *testing.T) {
	eps := []embyEpisode{
		{ID: "a", ParentIndexNumber: 1, IndexNumber: 1, Path: "/library/动画.S01E010-E011.故事.mkv"},
		{ID: "b", ParentIndexNumber: 1, IndexNumber: 2, Path: "/library/动画.S01E010.故事.mp4"},
		{ID: "c", ParentIndexNumber: 1, IndexNumber: 3, Path: "/library/动画.S01E013.黑洞（上）.mkv"},
		{ID: "d", ParentIndexNumber: 1, IndexNumber: 4, Path: "/library/动画.S01E014.黑洞（下）.mkv"},
		{ID: "virtual", IsMissing: true, ParentIndexNumber: 1, IndexNumber: 12},
	}
	got := inspectLocalOrder(eps)
	if got.Issue != "" || got.Files != 4 || got.DuplicateSlots != 1 || got.SplitFiles != 2 || got.GapCount != 1 || got.ScrapedNumberConflicts != 4 {
		t.Fatalf("bad audit: %+v", got)
	}
	r := got.Ranges[0]
	if r.First != 10 || r.Last != 14 || r.Owned != 4 || r.Gaps[0].Episode != 12 || r.Gaps[0].After != "S01E13 · 黑洞（上）" {
		t.Fatalf("bad range: %+v", r)
	}
}

func TestLocalOrderDoesNotInventAbsencesWithUnnumberedOrConflictingFiles(t *testing.T) {
	for _, bad := range []embyEpisode{
		{ID: "unlabelled"},
		{ID: "conflict", MediaSources: []any{map[string]any{"Path": "/a/S01E02.mkv"}, map[string]any{"Path": "/b/S01E03.mkv"}}},
		{ID: "bad-range", Path: "/a/S01E01-E999999.mkv"},
	} {
		got := inspectLocalOrder([]embyEpisode{{ID: "a", ParentIndexNumber: 1, IndexNumber: 1}, {ID: "b", ParentIndexNumber: 1, IndexNumber: 3}, bad})
		if got.Issue == "" || got.GapCount != 0 || len(got.Ranges) != 0 {
			t.Fatalf("asserted absences with ambiguous files: %+v", got)
		}
	}
}

func TestSourceFilenameDoesNotReadNumericTitleAsMergedRange(t *testing.T) {
	got, ok := episodeInfoFromPath("/video/动画.S01E01.1984年的故事.mp4")
	if !ok || got.EpisodeEnd != 0 || got.Title != "1984年的故事" {
		t.Fatalf("numeric title parsed as range: %+v", got)
	}
}

func TestLocalOrderGapClosesWhenResourceArrives(t *testing.T) {
	eps := []embyEpisode{{ID: "a", Path: "/media/S01E01.mkv"}, {ID: "b", Path: "/media/S01E03.mkv"}}
	if got := inspectLocalOrder(eps); got.GapCount != 1 {
		t.Fatalf("initial gap missing: %+v", got)
	}
	eps = append(eps, embyEpisode{ID: "c", Path: "/media/S01E02.mkv"})
	if got := inspectLocalOrder(eps); got.GapCount != 0 || got.Ranges[0].Owned != 3 {
		t.Fatalf("filled gap persisted: %+v", got)
	}
}
