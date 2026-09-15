package main

import (
	"fmt"
	"net/url"
	"path"
	"regexp"
	"sort"
	"strings"
)

type episodeSourceInfo struct {
	Title        string
	Part         string
	Season       int
	Episode      int
	EpisodeEnd   int
	Conflict     bool
	FromFilename bool
}

type localOrderRange struct {
	Season int             `json:"season"`
	First  int             `json:"first"`
	Last   int             `json:"last"`
	Owned  int             `json:"owned"`
	Gaps   []localOrderGap `json:"gaps"`
}
type localOrderGap struct {
	Episode int    `json:"episode"`
	Code    string `json:"code"`
	Before  string `json:"before,omitempty"`
	After   string `json:"after,omitempty"`
}

// Observed resource numbering is not a TMDB mapping or proof of completeness.
// The beginning, ending and absent seasons cannot be inferred from this audit.
type localOrderReport struct {
	Files                  int               `json:"files"`
	NumberedFiles          int               `json:"numberedFiles"`
	FilenameNumbers        int               `json:"filenameNumbers"`
	SplitFiles             int               `json:"splitFiles"`
	DuplicateSlots         int               `json:"duplicateSlots"`
	UnnumberedFiles        int               `json:"unnumberedFiles"`
	ConflictingFiles       int               `json:"conflictingFiles"`
	ScrapedNumberConflicts int               `json:"scrapedNumberConflicts"`
	GapCount               int               `json:"gapCount"`
	Ranges                 []localOrderRange `json:"ranges"`
	Issue                  string            `json:"issue,omitempty"`
}

var sourceEpisodeCode = regexp.MustCompile(`(?i)S(\d{1,4})[ ._-]*E(\d{1,6})(?:[ _]*-[ _]*E?(\d{1,6})|[ _]*E(\d{1,6}))?`)
var sourceSplitPart = regexp.MustCompile(`[（(](上|中|下|前篇|后篇|前集|后集)[）)]$`)
var genericEpisodeTitle = regexp.MustCompile(`(?i)^(?:第\s*\d+\s*[集话話]|episode\s*\d+|ep?\s*\d+|\d+)$`)
var windowsDrivePath = regexp.MustCompile(`^[a-zA-Z]:/`)

// Read basenames only. Never fetch media or copy source URLs into scan results.
func originalEpisodeInfo(ep embyEpisode) episodeSourceInfo {
	var found *episodeSourceInfo
	for _, value := range ep.MediaSources {
		if source, ok := value.(map[string]any); ok {
			if info, ok := episodeInfoFromPath(anyToString(source["Path"])); ok {
				if found == nil {
					found = &info
					continue
				}
				if info.Season != found.Season || info.Episode != found.Episode || info.EpisodeEnd != found.EpisodeEnd {
					return episodeSourceInfo{Conflict: true}
				}
				if found.Title == "" {
					found.Title, found.Part = info.Title, info.Part
				}
			}
		}
	}
	if found != nil {
		return *found
	}
	if info, ok := episodeInfoFromPath(ep.Path); ok {
		return info
	}
	return episodeSourceInfo{Season: ep.ParentIndexNumber, Episode: ep.IndexNumber, EpisodeEnd: ep.IndexNumberEnd}
}

func episodeInfoFromPath(value string) (episodeSourceInfo, bool) {
	value = strings.ReplaceAll(value, `\`, "/")
	filename := ""
	if windowsDrivePath.MatchString(value) || !strings.Contains(value, "://") {
		filename = path.Base(value)
	} else {
		u, err := url.Parse(value)
		if err != nil {
			return episodeSourceInfo{}, false
		}
		filename = path.Base(u.Path)
	}
	filename = strings.TrimSuffix(filename, path.Ext(filename))
	match := sourceEpisodeCode.FindStringSubmatchIndex(filename)
	if match == nil {
		return episodeSourceInfo{}, false
	}
	info := episodeSourceInfo{
		Season:       parseInt(filename[match[2]:match[3]]),
		Episode:      parseInt(filename[match[4]:match[5]]),
		FromFilename: true,
	}
	for _, index := range []int{6, 8} {
		if match[index] >= 0 {
			info.EpisodeEnd = parseInt(filename[match[index]:match[index+1]])
		}
	}
	title := strings.Trim(filename[match[1]:], " ._-\t")
	if genericEpisodeTitle.MatchString(title) {
		title = ""
	}
	if part := sourceSplitPart.FindStringSubmatch(title); len(part) > 1 {
		info.Part = part[1]
		title = strings.TrimSpace(strings.TrimSuffix(title, part[0]))
	}
	info.Title = title
	return info, info.Season >= 0 && info.Episode > 0
}

func inspectLocalOrder(episodes []embyEpisode) *localOrderReport {
	report := &localOrderReport{Ranges: []localOrderRange{}}
	owned := map[int]map[int]bool{}
	titles := map[string]string{}
	for _, ep := range episodes {
		if !isActualEmbyEpisode(ep) {
			continue
		}
		report.Files++
		info := originalEpisodeInfo(ep)
		if info.Conflict {
			report.ConflictingFiles++
			continue
		}
		if info.Episode <= 0 || info.Season < 0 {
			report.UnnumberedFiles++
			continue
		}
		if info.Season == 0 {
			continue
		}
		end := info.EpisodeEnd
		if end == 0 {
			end = info.Episode
		}
		if end < info.Episode || end-info.Episode > 200 || end > 100000 {
			report.UnnumberedFiles++
			continue
		}
		report.NumberedFiles++
		if info.FromFilename {
			report.FilenameNumbers++
		}
		if info.Part != "" {
			report.SplitFiles++
		}
		if info.Season != ep.ParentIndexNumber || info.Episode != ep.IndexNumber {
			report.ScrapedNumberConflicts++
		}
		if owned[info.Season] == nil {
			owned[info.Season] = map[int]bool{}
		}
		for number := info.Episode; number <= end; number++ {
			if owned[info.Season][number] {
				report.DuplicateSlots++
			}
			owned[info.Season][number] = true
			if info.Title != "" {
				title := info.Title
				if info.Part != "" {
					title += "（" + info.Part + "）"
				}
				titles[fmt.Sprintf("%d:%d", info.Season, number)] = title
			}
		}
	}
	if report.UnnumberedFiles > 0 || report.ConflictingFiles > 0 {
		report.Issue = fmt.Sprintf("有 %d 个视频无法读取编号，%d 个视频的不同来源编号冲突，暂不能确定断号。", report.UnnumberedFiles, report.ConflictingFiles)
		return report
	}
	seasons := make([]int, 0, len(owned))
	for season := range owned {
		seasons = append(seasons, season)
	}
	sort.Ints(seasons)
	for _, season := range seasons {
		numbers := make([]int, 0, len(owned[season]))
		for number := range owned[season] {
			numbers = append(numbers, number)
		}
		sort.Ints(numbers)
		r := localOrderRange{Season: season, First: numbers[0], Last: numbers[len(numbers)-1], Owned: len(numbers), Gaps: []localOrderGap{}}
		if r.Last-r.First > 10000 {
			report.Issue = "编号跨度异常，请核对资源命名后重扫。"
			report.Ranges = []localOrderRange{}
			report.GapCount = 0
			return report
		}
		for i := 1; i < len(numbers); i++ {
			previous, next := numbers[i-1], numbers[i]
			neighbor := func(number int) string {
				code := fmt.Sprintf("S%02dE%02d", season, number)
				if title := titles[fmt.Sprintf("%d:%d", season, number)]; title != "" {
					return code + " · " + title
				}
				return code
			}
			for number := previous + 1; number < next; number++ {
				r.Gaps = append(r.Gaps, localOrderGap{Episode: number, Code: fmt.Sprintf("S%02dE%02d", season, number), Before: neighbor(previous), After: neighbor(next)})
			}
		}
		report.GapCount += len(r.Gaps)
		report.Ranges = append(report.Ranges, r)
	}
	if len(report.Ranges) == 0 {
		report.Issue = "没有可检查的正片编号。"
	}
	return report
}
