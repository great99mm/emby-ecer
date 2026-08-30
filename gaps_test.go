package main

import (
	"reflect"
	"testing"
)

func TestExtractEpisodeNumbers(t *testing.T) {
	cases := []struct {
		name string
		text string
		want []int
	}{
		{"季集号", "Westworld.S02E05.2160p.WEB-DL", []int{5}},
		{"季集号范围", "某剧 S01E01-E03 1080p", []int{1, 2, 3}},
		{"EP 前缀", "凡人修仙传 EP12 4K HEVC", []int{12}},
		{"中文范围", "狂飙 第1-5集 国语中字", []int{1, 2, 3, 4, 5}},
		{"中文单集", "三体 第08集 1080P", []int{8}},
		{"全集", "隐秘的角落 全12集 4K", []int{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12}},
		{"分辨率不当集号", "某剧 1080p x265 AAC", nil},
		{"裸集号", "某剧 [09] 1080p", []int{9}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := sortedKeys(extractEpisodeNumbers(tc.text))
			if len(got) == 0 && len(tc.want) == 0 {
				return
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("extractEpisodeNumbers(%q) = %v, want %v", tc.text, got, tc.want)
			}
		})
	}
}

func TestScoreResourcePrefersExactEpisode(t *testing.T) {
	target := scoreTarget{Season: 2, Episodes: []int{5}}
	exact := scoreResource("Westworld.S02E05.2160p.WEB-DL.DDP5.1.HDR", "", target)
	pack := scoreResource("Westworld.S02.Complete.1080p.WEB-DL", "第1-10集 中字", target)
	wrong := scoreResource("Westworld.S02E09.1080p.WEB-DL", "", target)

	if exact.Score <= pack.Score {
		t.Fatalf("精确单集(%d)应高于整季包(%d)", exact.Score, pack.Score)
	}
	if wrong.Score >= pack.Score {
		t.Fatalf("集号不符(%d)应低于整季包(%d)", wrong.Score, pack.Score)
	}
	if exact.Resolution != "4K" {
		t.Fatalf("分辨率解析错误: %q", exact.Resolution)
	}
	if !reflect.DeepEqual(exact.MatchedEpisodes, []int{5}) {
		t.Fatalf("命中集数错误: %v", exact.MatchedEpisodes)
	}
	if !pack.ChineseSubtitle {
		t.Fatal("应识别出中字")
	}
}

func TestScoreResourceRejectsWrongSeason(t *testing.T) {
	target := scoreTarget{Season: 3, Episodes: []int{2}}
	right := scoreResource("某剧 S03E02 1080p", "", target)
	wrong := scoreResource("某剧 S01E02 1080p", "", target)
	if wrong.Score >= right.Score {
		t.Fatalf("错误季(%d)不应高于正确季(%d)", wrong.Score, right.Score)
	}
}

func TestSeriesInventoryExpandsMergedEpisodes(t *testing.T) {
	inv := buildSeriesInventory([]embyEpisode{
		{ParentIndexNumber: 1, IndexNumber: 1, IndexNumberEnd: 2},
		{ParentIndexNumber: 1, IndexNumber: 3},
	})
	for _, episode := range []int{1, 2, 3} {
		if !inv.has(1, episode) {
			t.Fatalf("S01E%02d 应被视为已拥有", episode)
		}
	}
	if inv.SeasonOwned[1] != 3 {
		t.Fatalf("第一季应统计 3 集，实际 %d", inv.SeasonOwned[1])
	}
	if inv.Total != 2 {
		t.Fatalf("Emby 实际单集数应为 2，实际 %d", inv.Total)
	}
}
