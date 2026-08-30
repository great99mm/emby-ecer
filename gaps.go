package main

import (
	"fmt"
	"log"
	"net/http"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// ---------------------------------------------------------------------------
// 全库单集库存：一次性拉取所有 Episode，替代逐剧 /Shows/{id}/Episodes 请求
// ---------------------------------------------------------------------------

type seriesInventory struct {
	Owned          map[string]bool // "季:集"
	Seasons        map[int]bool
	SeasonOwned    map[int]int
	TMDBEpisodeIDs map[int]bool
	Total          int
}

func newSeriesInventory() *seriesInventory {
	return &seriesInventory{
		Owned:          map[string]bool{},
		Seasons:        map[int]bool{},
		SeasonOwned:    map[int]int{},
		TMDBEpisodeIDs: map[int]bool{},
	}
}

// add 记录一个真实存在的单集。IndexNumberEnd 用于展开 E01-E02 这类合并文件，
// 否则合并集里除第一集之外的集号会被永远误判为缺失。
func (inv *seriesInventory) add(ep embyEpisode) {
	if inv == nil {
		return
	}
	inv.Total++
	season := ep.ParentIndexNumber
	if season > 0 {
		inv.Seasons[season] = true
	}
	if season > 0 && ep.IndexNumber > 0 {
		end := ep.IndexNumberEnd
		if end < ep.IndexNumber {
			end = ep.IndexNumber
		}
		if end-ep.IndexNumber > 200 {
			end = ep.IndexNumber
		}
		for number := ep.IndexNumber; number <= end; number++ {
			key := fmt.Sprintf("%d:%d", season, number)
			if !inv.Owned[key] {
				inv.Owned[key] = true
				inv.SeasonOwned[season]++
			}
		}
	}
	if id := parseInt(providerID(ep.ProviderIDs, "tmdb")); id > 0 {
		inv.TMDBEpisodeIDs[id] = true
	}
}

func (inv *seriesInventory) has(season, episode int) bool {
	if inv == nil {
		return false
	}
	return inv.Owned[fmt.Sprintf("%d:%d", season, episode)]
}

func buildSeriesInventory(episodes []embyEpisode) *seriesInventory {
	inv := newSeriesInventory()
	for _, ep := range episodes {
		inv.add(ep)
	}
	return inv
}

const episodeInventoryFields = "SeriesId,ProviderIds,ParentIndexNumber,IndexNumber,IndexNumberEnd,LocationType,IsMissing"

// loadEpisodeInventory 分页拉取全库单集并按剧集聚合。
// 1000 部剧的媒体库原本需要 1000+ 次逐剧请求，这里压缩成十几次分页请求。
func loadEpisodeInventory(s settings, onPage func(page, seriesCount int)) (map[string]*seriesInventory, error) {
	routes := []string{embyItemsRoute(s)}
	if strings.TrimSpace(s.EmbyUserID) != "" {
		routes = append(routes, "/Items")
	}
	var firstErr error
	for _, route := range routes {
		inventory, err := loadEpisodeInventoryFromRoute(s, route, onPage)
		if err == nil {
			return inventory, nil
		}
		if firstErr == nil {
			firstErr = err
		}
	}
	return nil, firstErr
}

func loadEpisodeInventoryFromRoute(s settings, route string, onPage func(page, seriesCount int)) (map[string]*seriesInventory, error) {
	inventory := map[string]*seriesInventory{}
	startIndex := 0
	pageLimit := 1000
	pageNum := 0
	for {
		var page embyEpisodesResp
		if err := embyGet(s, route, map[string]string{
			"Recursive":        "true",
			"IncludeItemTypes": "Episode",
			"IsMissing":        "false",
			"Fields":           episodeInventoryFields,
			"SortBy":           "SeriesSortName,ParentIndexNumber,IndexNumber",
			"StartIndex":       strconv.Itoa(startIndex),
			"Limit":            strconv.Itoa(pageLimit),
		}, &page); err != nil {
			return nil, err
		}
		pageNum++
		for _, ep := range page.Items {
			seriesID := strings.TrimSpace(ep.SeriesID)
			if seriesID == "" || !isActualEmbyEpisode(ep) {
				continue
			}
			inv, ok := inventory[seriesID]
			if !ok {
				inv = newSeriesInventory()
				inventory[seriesID] = inv
			}
			inv.add(ep)
		}
		if onPage != nil {
			onPage(pageNum, len(inventory))
		}
		if len(page.Items) < pageLimit {
			break
		}
		startIndex += pageLimit
	}
	return inventory, nil
}

// ---------------------------------------------------------------------------
// 媒体库屏蔽
// ---------------------------------------------------------------------------

type embyVirtualFolder struct {
	Name           string   `json:"Name"`
	ItemID         string   `json:"ItemId"`
	Guid           string   `json:"Guid"`
	ID             string   `json:"Id"`
	CollectionType string   `json:"CollectionType"`
	Locations      []string `json:"Locations"`
}

func (f embyVirtualFolder) libraryID() string {
	return firstNonEmpty(strings.TrimSpace(f.ItemID), strings.TrimSpace(f.Guid), strings.TrimSpace(f.ID))
}

func loadEmbyLibraries(s settings) ([]embyVirtualFolder, error) {
	var folders []embyVirtualFolder
	if err := embyGet(s, "/Library/VirtualFolders", nil, &folders); err != nil {
		return nil, err
	}
	return folders, nil
}

func excludedLibrarySet(s settings) map[string]bool {
	set := map[string]bool{}
	for _, value := range s.ExcludedLibraries {
		if value = strings.TrimSpace(value); value != "" {
			set[value] = true
		}
	}
	return set
}

// excludedLibraryItems 返回被屏蔽媒体库下所有剧集/电影的 ID。
// 只有配置了屏蔽项时才会产生额外请求。
func excludedLibraryItems(s settings) map[string]bool {
	excluded := map[string]bool{}
	wanted := excludedLibrarySet(s)
	if len(wanted) == 0 {
		return excluded
	}
	folders, err := loadEmbyLibraries(s)
	if err != nil {
		log.Printf("读取 Emby 媒体库列表失败，本次不做屏蔽: %v", err)
		return excluded
	}
	for _, folder := range folders {
		id := folder.libraryID()
		if id == "" {
			continue
		}
		if !wanted[id] && !wanted[strings.TrimSpace(folder.Name)] {
			continue
		}
		ids, err := loadLibraryItemIDs(s, id)
		if err != nil {
			log.Printf("读取媒体库《%s》条目失败，本次不屏蔽该库: %v", folder.Name, err)
			continue
		}
		for _, itemID := range ids {
			excluded[itemID] = true
		}
		log.Printf("缺集扫描屏蔽媒体库《%s》，跳过 %d 个条目", folder.Name, len(ids))
	}
	return excluded
}

func loadLibraryItemIDs(s settings, parentID string) ([]string, error) {
	ids := make([]string, 0)
	startIndex := 0
	pageLimit := 1000
	for {
		var page embyItemsResp
		if err := embyGet(s, embyItemsRoute(s), map[string]string{
			"ParentId":         parentID,
			"Recursive":        "true",
			"IncludeItemTypes": "Series,Movie",
			"StartIndex":       strconv.Itoa(startIndex),
			"Limit":            strconv.Itoa(pageLimit),
		}, &page); err != nil {
			return nil, err
		}
		for _, item := range page.Items {
			if strings.TrimSpace(item.ID) != "" {
				ids = append(ids, item.ID)
			}
		}
		if len(page.Items) < pageLimit {
			break
		}
		startIndex += pageLimit
	}
	return ids, nil
}

func handleEmbyLibraries(w http.ResponseWriter, r *http.Request) {
	s := store.Get()
	if err := requireFields(s, "embyUrl", "embyApiKey"); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	folders, err := loadEmbyLibraries(s)
	if err != nil {
		writeError(w, statusFromError(err), err)
		return
	}
	excluded := excludedLibrarySet(s)
	items := make([]map[string]any, 0, len(folders))
	for _, folder := range folders {
		id := folder.libraryID()
		count := 0
		var page embyItemsResp
		if err := embyGet(s, embyItemsRoute(s), map[string]string{
			"ParentId":         id,
			"Recursive":        "true",
			"IncludeItemTypes": "Series",
			"Limit":            "1",
		}, &page); err == nil {
			count = page.TotalRecordCount
		}
		items = append(items, map[string]any{
			"id":             id,
			"name":           folder.Name,
			"collectionType": folder.CollectionType,
			"seriesCount":    count,
			"excluded":       excluded[id] || excluded[strings.TrimSpace(folder.Name)],
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"libraries": items})
}

// ---------------------------------------------------------------------------
// 单集忽略名单
// ---------------------------------------------------------------------------

type ignoredEpisode struct {
	Key        string `json:"key"`
	SeriesID   string `json:"seriesId"`
	SeriesName string `json:"seriesName"`
	Season     int    `json:"season"`
	Episode    int    `json:"episode"`
	Code       string `json:"code"`
	Title      string `json:"title"`
	CreatedAt  int64  `json:"createdAt"`
}

type episodeIgnoreStore struct {
	mu   sync.RWMutex
	path string
	data map[string]ignoredEpisode
}

var episodeIgnores *episodeIgnoreStore

func episodeIgnoreKey(seriesID string, season, episode int) string {
	return fmt.Sprintf("%s|%d|%d", strings.TrimSpace(seriesID), season, episode)
}

func newEpisodeIgnoreStore(path string) *episodeIgnoreStore {
	s := &episodeIgnoreStore{path: path, data: map[string]ignoredEpisode{}}
	if stateDB != nil {
		stateDB.ImportJSONFile("episode_ignores", path, &s.data)
	}
	if err := loadStateJSON("episode_ignores", path, &s.data); err != nil || s.data == nil {
		s.data = map[string]ignoredEpisode{}
	}
	return s
}

func (s *episodeIgnoreStore) Has(seriesID string, season, episode int) bool {
	if s == nil || strings.TrimSpace(seriesID) == "" {
		return false
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	_, ok := s.data[episodeIgnoreKey(seriesID, season, episode)]
	return ok
}

func (s *episodeIgnoreStore) Add(items []ignoredEpisode) int {
	if s == nil {
		return 0
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	count := 0
	for _, item := range items {
		item.SeriesID = strings.TrimSpace(item.SeriesID)
		if item.SeriesID == "" || item.Season <= 0 || item.Episode <= 0 {
			continue
		}
		item.Key = episodeIgnoreKey(item.SeriesID, item.Season, item.Episode)
		if item.Code == "" {
			item.Code = fmt.Sprintf("S%02dE%02d", item.Season, item.Episode)
		}
		item.CreatedAt = time.Now().Unix()
		s.data[item.Key] = item
		count++
	}
	if count > 0 {
		_ = saveStateJSON("episode_ignores", s.path, s.data)
	}
	return count
}

func (s *episodeIgnoreStore) Delete(keys []string) int {
	if s == nil {
		return 0
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	count := 0
	for _, key := range keys {
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		if _, ok := s.data[key]; ok {
			delete(s.data, key)
			count++
		}
	}
	if count > 0 {
		_ = saveStateJSON("episode_ignores", s.path, s.data)
	}
	return count
}

func (s *episodeIgnoreStore) List() []ignoredEpisode {
	items := make([]ignoredEpisode, 0)
	if s == nil {
		return items
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, item := range s.data {
		items = append(items, item)
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].SeriesName == items[j].SeriesName {
			if items[i].Season == items[j].Season {
				return items[i].Episode < items[j].Episode
			}
			return items[i].Season < items[j].Season
		}
		return items[i].SeriesName < items[j].SeriesName
	})
	return items
}

func handleGetEpisodeIgnores(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"items": episodeIgnores.List()})
}

func handleAddEpisodeIgnores(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Items []ignoredEpisode `json:"items"`
	}
	if err := readJSON(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	count := episodeIgnores.Add(body.Items)
	if count > 0 {
		removeEpisodesFromSavedScanResult(body.Items)
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "count": count, "items": episodeIgnores.List()})
}

func handleDeleteEpisodeIgnores(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Keys []string `json:"keys"`
	}
	if err := readJSON(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	count := episodeIgnores.Delete(body.Keys)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "count": count, "items": episodeIgnores.List()})
}

// removeEpisodesFromSavedScanResult 把刚忽略的单集从已保存的扫描结果里摘掉，
// 免得用户还要重扫一次才能看到列表变化。
func removeEpisodesFromSavedScanResult(items []ignoredEpisode) {
	if len(items) == 0 {
		return
	}
	drop := map[string]bool{}
	for _, item := range items {
		if strings.TrimSpace(item.SeriesID) == "" {
			continue
		}
		drop[episodeIgnoreKey(item.SeriesID, item.Season, item.Episode)] = true
	}
	if len(drop) == 0 {
		return
	}
	result, err := loadScanResult()
	if err != nil || result == nil {
		return
	}
	kept := make([]any, 0)
	for _, entry := range anySlice(result["missing"]) {
		seriesID, season, episode := missingItemKey(entry)
		if drop[episodeIgnoreKey(seriesID, season, episode)] {
			continue
		}
		kept = append(kept, entry)
	}
	result["missing"] = kept
	if summary, ok := result["summary"].(map[string]any); ok {
		summary["totalMissingEpisodes"] = len(kept)
	}
	_ = saveScanResult(result)
}

// previousMissingBySeries 读取上次扫描结果并按剧集分组，
// 供最近变更模式沿用未变化剧集的缺集记录。
func previousMissingBySeries() map[string][]missingEpisode {
	out := map[string][]missingEpisode{}
	result, err := loadScanResult()
	if err != nil || result == nil {
		return out
	}
	for _, item := range anySlice(result["missing"]) {
		var entry missingEpisode
		switch value := item.(type) {
		case missingEpisode:
			entry = value
		case map[string]any:
			entry = bodyToMissing(value)
		default:
			continue
		}
		seriesID := strings.TrimSpace(entry.EmbySeriesID)
		if seriesID == "" {
			continue
		}
		if episodeIgnores.Has(seriesID, entry.Season, entry.Episode) {
			continue
		}
		out[seriesID] = append(out[seriesID], entry)
	}
	return out
}

func missingItemKey(item any) (string, int, int) {
	switch v := item.(type) {
	case missingEpisode:
		return v.EmbySeriesID, v.Season, v.Episode
	case map[string]any:
		seriesID := strings.TrimSpace(anyToString(v["embySeriesId"]))
		season, _ := anyToInt(v["season"])
		episode, _ := anyToInt(v["episode"])
		return seriesID, season, episode
	default:
		return "", 0, 0
	}
}

// ---------------------------------------------------------------------------
// 缺集校验：只核对当前列表里的缺集是否已经补齐，不做全量重扫
// ---------------------------------------------------------------------------

func handleVerifyScan(w http.ResponseWriter, r *http.Request) {
	s := store.Get()
	if err := requireFields(s, "embyUrl", "embyApiKey"); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	removed, result, err := verifyMissingEpisodes(s)
	if err != nil {
		writeError(w, statusFromError(err), err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "removed": removed, "scan": result})
}

func verifyMissingEpisodes(s settings) (int, map[string]any, error) {
	result, err := loadScanResult()
	if err != nil || result == nil {
		return 0, nil, badRequest("还没有扫描结果，请先扫描一次媒体库")
	}
	items := anySlice(result["missing"])
	if len(items) == 0 {
		return 0, result, nil
	}
	inventory, err := loadEpisodeInventory(s, nil)
	if err != nil {
		return 0, nil, err
	}
	kept := make([]any, 0, len(items))
	removed := 0
	touched := map[string]bool{}
	for _, item := range items {
		seriesID, season, episode := missingItemKey(item)
		if seriesID != "" && season > 0 && episode > 0 {
			if inventory[seriesID].has(season, episode) || episodeIgnores.Has(seriesID, season, episode) {
				removed++
				touched[seriesID] = true
				continue
			}
		}
		kept = append(kept, item)
	}
	if removed == 0 {
		result["verifiedAt"] = time.Now().Format(time.RFC3339)
		_ = saveScanResult(result)
		return 0, result, nil
	}

	// 补齐后仍留在列表里的剧集不动缓存；整部补全的剧集删掉缓存，
	// 让下一次扫描重新确认能否进入完结归档。
	stillMissing := map[string]bool{}
	for _, item := range kept {
		if seriesID, _, _ := missingItemKey(item); seriesID != "" {
			stillMissing[seriesID] = true
		}
	}
	for seriesID := range touched {
		if !stillMissing[seriesID] {
			seriesScanCache.Delete(seriesID)
		}
	}
	_ = seriesScanCache.Flush()

	result["missing"] = kept
	result["verifiedAt"] = time.Now().Format(time.RFC3339)
	if summary, ok := result["summary"].(map[string]any); ok {
		summary["totalMissingEpisodes"] = len(kept)
	}
	_ = saveScanResult(result)
	return removed, result, nil
}

// ---------------------------------------------------------------------------
// 定时自动扫描
// ---------------------------------------------------------------------------

var appStartedAt = time.Now()

func scanJobIsActive() bool {
	id := currentActiveScanJobID()
	if id == "" {
		return false
	}
	j := jobMgr.get(id)
	if j == nil {
		// 任务刚创建还没落盘，视为进行中
		return true
	}
	return j.Status == jobRunning || j.Status == jobPending
}

func startScanScheduler() {
	go func() {
		ticker := time.NewTicker(time.Minute)
		defer ticker.Stop()
		for range ticker.C {
			s := store.Get()
			if !s.ScanAutoEnabled {
				continue
			}
			if requireFields(s, "embyUrl", "embyApiKey", "tmdbApiKey") != nil {
				continue
			}
			if scanJobIsActive() {
				continue
			}
			interval := time.Duration(clampIntervalHours(s.ScanAutoInterval)) * time.Hour
			last := lastScanTime()
			if last.IsZero() {
				// 从没扫过：等服务跑稳一点再开第一次全量
				if time.Since(appStartedAt) < 3*time.Minute {
					continue
				}
			} else if time.Since(last) < interval {
				continue
			}
			j := jobMgr.create("scan")
			activateScanJob(j.ID)
			log.Printf("自动缺集扫描启动（间隔 %d 小时，模式 %s）", clampIntervalHours(s.ScanAutoInterval), scanModeLabel(s.ScanAutoRecentOnly))
			go runJob(j.ID, s, "scan", true, 0, s.ScanAutoRecentOnly, "")
		}
	}()
}

// ---------------------------------------------------------------------------
// 资源标题解析与打分
// ---------------------------------------------------------------------------

var (
	reSeasonEpisode  = regexp.MustCompile(`(?i)S\d{1,2}\s*E(\d{1,3})(?:\s*-\s*E?(\d{1,3}))?`)
	reEpisodeToken   = regexp.MustCompile(`(?i)\b(?:EPISODE|EP|E)[\s._\-]*(\d{1,3})(?:\s*-\s*E?(\d{1,3}))?`)
	reChineseRange   = regexp.MustCompile(`第\s*(\d{1,3})\s*(?:-|~|～|至|到)\s*(\d{1,3})\s*[集話话]`)
	reChineseSingle  = regexp.MustCompile(`第\s*(\d{1,3})\s*[集話话]`)
	reChineseAll     = regexp.MustCompile(`全\s*(\d{1,3})\s*[集話话]`)
	reNakedEpisode   = regexp.MustCompile(`(?:\[|\s-\s|\s|\.)(\d{2,3})(?:\]|\s|\.|$)`)
	reSeasonNumber   = regexp.MustCompile(`(?i)S(\d{1,2})(?:\D|$)`)
	nakedNumberDenyL = map[int]bool{264: true, 265: true, 360: true, 480: true, 540: true, 576: true, 720: true}
)

// extractEpisodeNumbers 从资源标题/描述里解析集号，支持 S01E01-E03、EP01、第1-12集 等写法。
func extractEpisodeNumbers(text string) map[int]bool {
	eps := map[int]bool{}
	if strings.TrimSpace(text) == "" {
		return eps
	}
	addRange := func(start, end int) {
		if start <= 0 || start > 999 {
			return
		}
		if end < start || end > 999 || end-start > 200 {
			end = start
		}
		for i := start; i <= end; i++ {
			eps[i] = true
		}
	}
	for _, m := range reSeasonEpisode.FindAllStringSubmatch(text, -1) {
		addRange(parseInt(m[1]), parseInt(m[2]))
	}
	for _, m := range reEpisodeToken.FindAllStringSubmatch(text, -1) {
		addRange(parseInt(m[1]), parseInt(m[2]))
	}
	for _, m := range reChineseRange.FindAllStringSubmatch(text, -1) {
		addRange(parseInt(m[1]), parseInt(m[2]))
	}
	for _, m := range reChineseSingle.FindAllStringSubmatch(text, -1) {
		addRange(parseInt(m[1]), parseInt(m[1]))
	}
	if len(eps) == 0 {
		for _, m := range reChineseAll.FindAllStringSubmatch(text, -1) {
			addRange(1, parseInt(m[1]))
		}
	}
	if len(eps) == 0 {
		for _, m := range reNakedEpisode.FindAllStringSubmatch(text, -1) {
			number := parseInt(m[1])
			if number <= 0 || nakedNumberDenyL[number] {
				continue
			}
			eps[number] = true
		}
	}
	return eps
}

func extractSeasonNumbers(text string) map[int]bool {
	seasons := map[int]bool{}
	for _, m := range reSeasonNumber.FindAllStringSubmatch(text, -1) {
		if n := parseInt(m[1]); n > 0 {
			seasons[n] = true
		}
	}
	for _, m := range regexp.MustCompile(`第\s*([0-9一二三四五六七八九十]{1,3})\s*季`).FindAllStringSubmatch(text, -1) {
		if n := parseChineseNumber(m[1]); n > 0 {
			seasons[n] = true
		}
	}
	return seasons
}

func parseChineseNumber(value string) int {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0
	}
	if n := parseInt(value); n > 0 {
		return n
	}
	digits := map[rune]int{'一': 1, '二': 2, '三': 3, '四': 4, '五': 5, '六': 6, '七': 7, '八': 8, '九': 9}
	runes := []rune(value)
	if len(runes) == 1 {
		if runes[0] == '十' {
			return 10
		}
		return digits[runes[0]]
	}
	total := 0
	for i, r := range runes {
		if r == '十' {
			if i == 0 {
				total = 10
			} else {
				total = digits[runes[i-1]] * 10
			}
			if i == len(runes)-1 {
				return total
			}
			continue
		}
		if i == len(runes)-1 && total > 0 {
			return total + digits[r]
		}
	}
	return total
}

type resourceScore struct {
	Score           int      `json:"score"`
	Episodes        []int    `json:"episodes,omitempty"`
	MatchedEpisodes []int    `json:"matchedEpisodes,omitempty"`
	MatchRatio      int      `json:"matchRatio"`
	MatchLabel      string   `json:"matchLabel,omitempty"`
	Resolution      string   `json:"resolution,omitempty"`
	Quality         string   `json:"quality,omitempty"`
	Tags            []string `json:"tags,omitempty"`
	ChineseAudio    bool     `json:"chineseAudio,omitempty"`
	ChineseSubtitle bool     `json:"chineseSubtitle,omitempty"`
	SeasonPack      bool     `json:"seasonPack,omitempty"`
	SeasonMatched   bool     `json:"seasonMatched,omitempty"`
	Free            bool     `json:"free,omitempty"`
	Discount        string   `json:"discount,omitempty"`
	HitAndRun       bool     `json:"hitAndRun,omitempty"`
}

type scoreTarget struct {
	Season   int
	Episodes []int
}

func (t scoreTarget) set() map[int]bool {
	out := map[int]bool{}
	for _, e := range t.Episodes {
		if e > 0 {
			out[e] = true
		}
	}
	return out
}

func sortedKeys(set map[int]bool) []int {
	out := make([]int, 0, len(set))
	for k := range set {
		out = append(out, k)
	}
	sort.Ints(out)
	return out
}

// scoreResource 按“是否命中目标集数”为主、画质/促销/中文为辅给资源打分。
func scoreResource(title, description string, target scoreTarget) resourceScore {
	full := strings.TrimSpace(title + " " + description)
	upper := strings.ToUpper(full)
	score := resourceScore{}

	episodes := extractEpisodeNumbers(full)
	wanted := target.set()
	score.Episodes = sortedKeys(episodes)

	if target.Season > 0 {
		seasons := extractSeasonNumbers(full)
		if len(seasons) == 0 || seasons[target.Season] {
			score.SeasonMatched = true
		}
		if len(seasons) > 0 && !seasons[target.Season] {
			score.Score -= 40
		}
	}

	switch {
	case len(wanted) == 0 || len(episodes) == 0:
		// 没法判断集数：季命中给一点分，当作整季包
		if score.SeasonMatched && target.Season > 0 {
			score.Score += 20
			score.SeasonPack = len(episodes) == 0
			score.MatchLabel = "整季"
		}
	default:
		matched := map[int]bool{}
		for episode := range episodes {
			if wanted[episode] {
				matched[episode] = true
			}
		}
		score.MatchedEpisodes = sortedKeys(matched)
		ratio := float64(len(matched)) / float64(len(wanted))
		score.MatchRatio = int(ratio*100 + 0.5)
		switch {
		case ratio >= 1 && len(episodes) == len(wanted):
			score.Score += 120
			score.MatchLabel = "精确"
		case ratio >= 1:
			score.Score += 100
			score.MatchLabel = "全覆盖"
		case ratio >= 0.5:
			score.Score += int(ratio * 80)
			score.MatchLabel = "部分"
		case ratio > 0:
			score.Score += int(ratio * 40)
			score.MatchLabel = "部分"
		default:
			score.Score -= 50
		}
		score.SeasonPack = len(episodes) > len(wanted)
	}

	switch {
	case strings.Contains(upper, "2160P"), strings.Contains(upper, "4K"), strings.Contains(upper, "UHD"):
		score.Resolution = "4K"
		score.Score += 30
	case strings.Contains(upper, "1080P"), strings.Contains(upper, "1080I"):
		score.Resolution = "1080P"
		score.Score += 25
	case strings.Contains(upper, "720P"):
		score.Resolution = "720P"
		score.Score += 10
	case strings.Contains(upper, "480P"):
		score.Resolution = "480P"
	}

	switch {
	case strings.Contains(upper, "REMUX"):
		score.Quality = "REMUX"
		score.Score += 10
	case strings.Contains(upper, "BLURAY"), strings.Contains(upper, "BLU-RAY"):
		score.Quality = "BluRay"
	case strings.Contains(upper, "WEB-DL"), strings.Contains(upper, "WEBDL"):
		score.Quality = "WEB-DL"
	case strings.Contains(upper, "WEBRIP"):
		score.Quality = "WEBRip"
	case strings.Contains(upper, "HDTV"):
		score.Quality = "HDTV"
	}

	tags := make([]string, 0, 4)
	if score.Resolution != "" {
		tags = append(tags, score.Resolution)
	}
	if score.Quality != "" {
		tags = append(tags, score.Quality)
	}
	if strings.Contains(upper, "DOVI") || strings.Contains(upper, "DOLBY VISION") || strings.Contains(upper, "DV ") {
		score.Score += 20
		tags = append(tags, "DoVi")
	} else if strings.Contains(upper, "HDR") {
		score.Score += 15
		tags = append(tags, "HDR")
	}
	switch {
	case strings.Contains(upper, "H.265"), strings.Contains(upper, "HEVC"), strings.Contains(upper, "X265"):
		tags = append(tags, "HEVC")
	case strings.Contains(upper, "H.264"), strings.Contains(upper, "AVC"), strings.Contains(upper, "X264"):
		tags = append(tags, "AVC")
	}
	for _, keyword := range []string{"国语", "普通话", "国配", "中文配音"} {
		if strings.Contains(full, keyword) {
			score.ChineseAudio = true
			score.Score += 5
			tags = append(tags, "国语")
			break
		}
	}
	for _, keyword := range []string{"中字", "中英", "简体", "繁体", "简繁", "CHS", "CHT"} {
		if strings.Contains(full, keyword) || strings.Contains(upper, keyword) {
			score.ChineseSubtitle = true
			score.Score += 5
			tags = append(tags, "中字")
			break
		}
	}
	score.Tags = tags
	return score
}

// applyTorrentDiscount 读取 MoviePilot 返回的促销/HR 字段，补进评分。
func applyTorrentDiscount(item map[string]any, score *resourceScore) {
	raw := deepExtract(item, "volume_factor", "free", "is_free", "free_status")
	factor := 1.0
	if raw != nil {
		text := strings.ToLower(strings.TrimSpace(anyToString(raw)))
		switch text {
		case "free", "freebie", "true", "yes":
			factor = 0
		default:
			if parsed, err := strconv.ParseFloat(text, 64); err == nil {
				factor = parsed
			}
		}
	}
	switch {
	case factor <= 0:
		score.Free = true
		score.Discount = "免费"
		score.Score += 15
	case factor < 1:
		score.Discount = fmt.Sprintf("%d%%", int(factor*100))
		score.Score += int((1 - factor) * 20)
	default:
		if freeDate := deepExtract(item, "freedate", "free_time"); freeDate != nil && strings.TrimSpace(anyToString(freeDate)) != "" {
			score.Discount = "限免"
			score.Score += 5
		}
	}
	if hr := deepExtract(item, "hit_and_run", "hr"); hr != nil {
		switch strings.ToLower(strings.TrimSpace(anyToString(hr))) {
		case "1", "true", "yes", "hr", "1.0":
			score.HitAndRun = true
			score.Score -= 10
		}
	}
}

// deepExtract 依次在顶层和常见嵌套层里找第一个非空字段。
func deepExtract(item map[string]any, keys ...string) any {
	if item == nil {
		return nil
	}
	pick := func(m map[string]any) any {
		for _, key := range keys {
			if value, ok := m[key]; ok && value != nil && strings.TrimSpace(anyToString(value)) != "" {
				return value
			}
		}
		return nil
	}
	if value := pick(item); value != nil {
		return value
	}
	for _, nested := range []string{"torrent_info", "torrent", "detail", "data", "info", "meta_info"} {
		if child, ok := item[nested].(map[string]any); ok {
			if value := pick(child); value != nil {
				return value
			}
		}
	}
	return nil
}

// annotateTorrentResults 给 MoviePilot 结果打分排序，命中缺失集号的排在最前面。
func annotateTorrentResults(results []map[string]any, target scoreTarget) []map[string]any {
	type scored struct {
		item    map[string]any
		score   int
		seeders int
	}
	list := make([]scored, 0, len(results))
	for _, item := range results {
		title := anyToString(deepExtract(item, "title", "name", "torrent_name"))
		description := anyToString(deepExtract(item, "description", "desc", "subtitle"))
		score := scoreResource(title, description, target)
		applyTorrentDiscount(item, &score)
		seeders, _ := anyToInt(deepExtract(item, "seeders", "seeder"))
		score.Score += minInt(seeders/20, 10)
		item["match"] = score
		item["matchScore"] = score.Score
		list = append(list, scored{item: item, score: score.Score, seeders: seeders})
	}
	sort.SliceStable(list, func(i, j int) bool {
		if list[i].score == list[j].score {
			return list[i].seeders > list[j].seeders
		}
		return list[i].score > list[j].score
	})
	out := make([]map[string]any, 0, len(list))
	for _, entry := range list {
		out = append(out, entry.item)
	}
	return out
}

// annotateNormalizedResults 给网盘/影巢结果打分排序，逻辑与种子一致但没有促销字段。
func annotateNormalizedResults(results []normalizedResult, target scoreTarget) []normalizedResult {
	if len(target.Episodes) == 0 && target.Season <= 0 {
		return results
	}
	for i := range results {
		score := scoreResource(results[i].Title, results[i].Note, target)
		results[i].Match = &score
		results[i].MatchScore = score.Score
	}
	sort.SliceStable(results, func(i, j int) bool { return results[i].MatchScore > results[j].MatchScore })
	return results
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
