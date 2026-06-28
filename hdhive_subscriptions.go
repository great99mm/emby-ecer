package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io"
	"log"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

const defaultHDHiveURL = "https://hdhive.com"

var (
	subRunMu              sync.Mutex
	subRunActive          bool
	subLastRunAt          time.Time
	hdhiveCheckinMu       sync.Mutex
	hdhiveLastCheckinDate string
	hdhiveUserAgent       = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/125.0 Safari/537.36"
)

type subscription struct {
	ID           string             `json:"id"`
	Title        string             `json:"title"`
	MediaType    string             `json:"mediaType"`
	TMDBID       int                `json:"tmdbId"`
	TMDBYear     string             `json:"tmdbYear,omitempty"`
	PosterPath   string             `json:"posterPath,omitempty"`
	Overview     string             `json:"overview,omitempty"`
	Season       int                `json:"season"`
	Enabled      bool               `json:"enabled"`
	AutoTransfer bool               `json:"autoTransfer"`
	TargetCID    string             `json:"targetCid"`
	CreatedAt    string             `json:"createdAt"`
	UpdatedAt    string             `json:"updatedAt"`
	LastRunAt    string             `json:"lastRunAt,omitempty"`
	LastStatus   string             `json:"lastStatus,omitempty"`
	LastMessage  string             `json:"lastMessage,omitempty"`
	LastResults  []normalizedResult `json:"lastResults,omitempty"`
	Archived     bool               `json:"archived,omitempty"`
	ArchivedAt   string             `json:"archivedAt,omitempty"`
}

type subscriptionStore struct {
	mu    sync.RWMutex
	path  string
	items map[string]subscription
}

type subscriptionRunResult struct {
	StartedAt  string                `json:"startedAt"`
	FinishedAt string                `json:"finishedAt"`
	Total      int                   `json:"total"`
	Success    int                   `json:"success"`
	Failed     int                   `json:"failed"`
	Items      []subscriptionRunItem `json:"items"`
}

type subscriptionRunItem struct {
	Subscription subscription       `json:"subscription"`
	Results      []normalizedResult `json:"results,omitempty"`
	Transferred  *transferResult    `json:"transferred,omitempty"`
	Error        string             `json:"error,omitempty"`
}

type hdhiveClient struct {
	baseURL string
	cookie  string
	client  *http.Client
}

type hdhiveResource struct {
	Title        string
	ResourceName string
	Slug         string
	URL          string
	Password     string
	Size         string
	Qualities    []string
	Resolution   []string
	UnlockPoints int
	Locked       bool
	Source       string
	CreatedAt    string
}

type hdhiveCheckinResult struct {
	OK           bool           `json:"ok"`
	Status       string         `json:"status"`
	Message      string         `json:"message"`
	PointsEarned int            `json:"pointsEarned"`
	Points       int            `json:"points,omitempty"`
	CheckedIn    *bool          `json:"checkedIn,omitempty"`
	Raw          map[string]any `json:"raw,omitempty"`
}

func newSubscriptionStore(path string) *subscriptionStore {
	store := &subscriptionStore{path: path, items: map[string]subscription{}}
	var items []subscription
	if stateDB != nil {
		stateDB.ImportJSONFile("subscriptions", path, &items)
	}
	if err := loadStateJSON("subscriptions", path, &items); err == nil {
		for _, item := range items {
			item = normalizeSubscription(item)
			if strings.TrimSpace(item.ID) == "" {
				continue
			}
			if existingID, ok := store.findDuplicateIDLocked(item); ok {
				store.items[existingID] = mergeSubscription(store.items[existingID], item)
			} else {
				store.items[item.ID] = item
			}
		}
		_ = store.persistLocked()
	}
	return store
}

func (s *subscriptionStore) List() []subscription {
	return s.ListArchived(false)
}

func (s *subscriptionStore) ArchivedList() []subscription {
	return s.ListArchived(true)
}

func (s *subscriptionStore) ListArchived(archived bool) []subscription {
	s.mu.RLock()
	defer s.mu.RUnlock()
	items := make([]subscription, 0, len(s.items))
	for _, item := range s.items {
		if item.Archived != archived {
			continue
		}
		items = append(items, item)
	}
	sort.Slice(items, func(left, right int) bool {
		return strings.ToLower(items[left].Title) < strings.ToLower(items[right].Title)
	})
	return items
}

func (s *subscriptionStore) Save(item subscription) (subscription, error) {
	item = normalizeSubscription(item)
	if item.Title == "" {
		return item, badRequest("订阅标题不能为空")
	}
	now := time.Now().Format(time.RFC3339)
	if item.ID == "" {
		item.ID = randomID(8)
	}
	if item.CreatedAt == "" {
		item.CreatedAt = now
	}
	item.UpdatedAt = now
	s.mu.Lock()
	if existingID, ok := s.findDuplicateIDLocked(item); ok {
		existing := s.items[existingID]
		item.ID = existing.ID
		item.CreatedAt = existing.CreatedAt
		item.LastRunAt = existing.LastRunAt
		item.LastStatus = existing.LastStatus
		item.LastMessage = existing.LastMessage
		item.LastResults = existing.LastResults
		merged := mergeSubscription(existing, item)
		s.items[existingID] = merged
		err := s.persistLocked()
		s.mu.Unlock()
		return merged, err
	}
	s.items[item.ID] = item
	err := s.persistLocked()
	s.mu.Unlock()
	return item, err
}

func normalizeSubscription(item subscription) subscription {
	item.Title = strings.TrimSpace(item.Title)
	item.MediaType = normalizeHDHiveMediaType(item.MediaType)
	item.TMDBYear = strings.TrimSpace(item.TMDBYear)
	item.PosterPath = strings.TrimSpace(item.PosterPath)
	item.Overview = strings.TrimSpace(item.Overview)
	item.TargetCID = strings.TrimSpace(item.TargetCID)
	item.ArchivedAt = strings.TrimSpace(item.ArchivedAt)
	if item.MediaType == "movie" {
		item.Season = 0
	}
	return item
}

func (s *subscriptionStore) findDuplicateIDLocked(item subscription) (string, bool) {
	key := subscriptionDedupKey(item)
	if key == "" {
		return "", false
	}
	for id, current := range s.items {
		if strings.TrimSpace(item.ID) != "" && id == item.ID {
			continue
		}
		if subscriptionDedupKey(current) == key {
			return id, true
		}
	}
	return "", false
}

func subscriptionDedupKey(item subscription) string {
	mediaType := normalizeHDHiveMediaType(item.MediaType)
	if item.TMDBID > 0 {
		season := item.Season
		if mediaType == "movie" {
			season = 0
		}
		return fmt.Sprintf("%s:%d:%d", mediaType, item.TMDBID, season)
	}
	title := strings.ToLower(strings.TrimSpace(item.Title))
	if title == "" {
		return ""
	}
	return fmt.Sprintf("%s:%s:%d", mediaType, title, item.Season)
}

func mergeSubscription(existing, incoming subscription) subscription {
	merged := existing
	merged.Title = firstNonEmpty(incoming.Title, existing.Title)
	merged.MediaType = firstNonEmpty(incoming.MediaType, existing.MediaType)
	if incoming.TMDBID > 0 {
		merged.TMDBID = incoming.TMDBID
	}
	merged.TMDBYear = firstNonEmpty(incoming.TMDBYear, existing.TMDBYear)
	merged.PosterPath = firstNonEmpty(incoming.PosterPath, existing.PosterPath)
	merged.Overview = firstNonEmpty(incoming.Overview, existing.Overview)
	merged.Season = incoming.Season
	merged.Enabled = incoming.Enabled || existing.Enabled
	merged.AutoTransfer = incoming.AutoTransfer || existing.AutoTransfer
	merged.TargetCID = firstNonEmpty(incoming.TargetCID, existing.TargetCID, "0")
	merged.UpdatedAt = firstNonEmpty(incoming.UpdatedAt, existing.UpdatedAt)
	if !existing.Archived || incoming.Archived {
		merged.Archived = incoming.Archived
		merged.ArchivedAt = incoming.ArchivedAt
	}
	return normalizeSubscription(merged)
}

func (s *subscriptionStore) Delete(ids []string) error {
	s.mu.Lock()
	for _, id := range ids {
		delete(s.items, strings.TrimSpace(id))
	}
	err := s.persistLocked()
	s.mu.Unlock()
	return err
}

func (s *subscriptionStore) Archive(ids []string, archived bool) ([]subscription, error) {
	now := time.Now().Format(time.RFC3339)
	s.mu.Lock()
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		item, ok := s.items[id]
		if !ok {
			continue
		}
		item.Archived = archived
		item.ArchivedAt = ""
		if archived {
			item.ArchivedAt = now
			item.Enabled = false
		}
		item.UpdatedAt = now
		s.items[id] = item
	}
	err := s.persistLocked()
	s.mu.Unlock()
	return s.List(), err
}

func (s *subscriptionStore) UpdateResult(id string, results []normalizedResult, status, message string) {
	s.mu.Lock()
	if item, ok := s.items[id]; ok {
		item.LastRunAt = time.Now().Format(time.RFC3339)
		item.LastStatus = status
		item.LastMessage = message
		item.LastResults = results
		item.UpdatedAt = item.LastRunAt
		s.items[id] = item
		_ = s.persistLocked()
	}
	s.mu.Unlock()
}

func (s *subscriptionStore) ReplaceUnlockedResult(id, slug string, result normalizedResult) (subscription, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	item, ok := s.items[strings.TrimSpace(id)]
	if !ok {
		return subscription{}, badRequest("订阅不存在")
	}
	slug = strings.TrimSpace(slug)
	updated := false
	for index := range item.LastResults {
		current := item.LastResults[index]
		if strings.TrimSpace(current.HDHiveSlug) != slug {
			continue
		}
		result.Title = firstNonEmpty(cleanHDHiveLockedTitle(result.Title), cleanHDHiveLockedTitle(current.Title))
		result.Source = "HDHive"
		result.Query = firstNonEmpty(result.Query, current.Query)
		result.Datetime = firstNonEmpty(result.Datetime, current.Datetime)
		result.HDHiveSlug = slug
		result.HDHiveLocked = false
		result.UnlockPoints = current.UnlockPoints
		item.LastResults[index] = result
		updated = true
		break
	}
	if !updated {
		return subscription{}, badRequest("未找到待解锁资源")
	}
	item.LastRunAt = time.Now().Format(time.RFC3339)
	item.UpdatedAt = item.LastRunAt
	item.LastStatus = "success"
	item.LastMessage = formatSubscriptionRunMessage(item.LastResults)
	s.items[item.ID] = item
	return item, s.persistLocked()
}

func (s *subscriptionStore) persistLocked() error {
	items := make([]subscription, 0, len(s.items))
	for _, item := range s.items {
		items = append(items, item)
	}
	sort.Slice(items, func(left, right int) bool { return items[left].CreatedAt < items[right].CreatedAt })
	return saveStateJSON("subscriptions", s.path, items)
}

func handleListSubscriptions(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, subscriptionListResponse())
}

func subscriptionListResponse() map[string]any {
	return map[string]any{"items": subStore.List(), "archivedItems": subStore.ArchivedList(), "running": subscriptionRunIsActive()}
}

func handleSaveSubscription(w http.ResponseWriter, r *http.Request) {
	var item subscription
	if err := readJSON(r, &item); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	saved, err := subStore.Save(item)
	if err != nil {
		writeError(w, statusFromError(err), err)
		return
	}
	writeJSON(w, http.StatusOK, saved)
}

func handleSubscriptionWebhook(w http.ResponseWriter, r *http.Request) {
	s := store.Get()
	if strings.TrimSpace(s.SubWebhookToken) == "" {
		writeError(w, http.StatusForbidden, errors.New("订阅 Webhook Token 未配置"))
		return
	}
	if !validSubscriptionWebhookToken(r, s.SubWebhookToken) {
		writeError(w, http.StatusUnauthorized, errors.New("订阅 Webhook 鉴权失败"))
		return
	}
	var body struct {
		Title     string `json:"title"`
		Name      string `json:"name"`
		TMDBID    int    `json:"tmdbId"`
		MediaType string `json:"mediaType"`
		Type      string `json:"type"`
		Season    int    `json:"season"`
	}
	if err := readJSON(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	mediaType := normalizeHDHiveMediaType(firstNonEmpty(body.MediaType, body.Type))
	if mediaType != "tv" && mediaType != "movie" {
		writeError(w, http.StatusBadRequest, errors.New("mediaType 仅支持 tv 或 movie"))
		return
	}
	title := firstNonEmpty(body.Title, body.Name)
	if strings.TrimSpace(title) == "" {
		writeError(w, http.StatusBadRequest, errors.New("缺少 title/name"))
		return
	}
	item := subscription{
		Title:        title,
		MediaType:    mediaType,
		TMDBID:       body.TMDBID,
		Season:       body.Season,
		Enabled:      true,
		AutoTransfer: false,
		TargetCID:    "0",
	}
	if mediaType == "movie" {
		item.Season = 0
	}
	if item.TMDBID > 0 {
		applySubscriptionTMDBMetadata(s, &item)
	}
	saved, err := subStore.Save(item)
	if err != nil {
		writeError(w, statusFromError(err), err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "item": saved})
}

func validSubscriptionWebhookToken(r *http.Request, expected string) bool {
	expected = strings.TrimSpace(expected)
	if expected == "" {
		return false
	}
	candidates := []string{
		strings.TrimSpace(r.Header.Get("X-Webhook-Token")),
		strings.TrimSpace(r.URL.Query().Get("token")),
	}
	if auth := strings.TrimSpace(r.Header.Get("Authorization")); auth != "" {
		if strings.HasPrefix(strings.ToLower(auth), "bearer ") {
			candidates = append(candidates, strings.TrimSpace(auth[7:]))
		} else {
			candidates = append(candidates, auth)
		}
	}
	for _, candidate := range candidates {
		if candidate == expected {
			return true
		}
	}
	return false
}

func applySubscriptionTMDBMetadata(s settings, item *subscription) {
	if item == nil || strings.TrimSpace(s.TMDBAPIKey) == "" || item.TMDBID <= 0 {
		return
	}
	if item.MediaType == "movie" {
		var movie struct {
			Title         string `json:"title"`
			OriginalTitle string `json:"original_title"`
			ReleaseDate   string `json:"release_date"`
			PosterPath    string `json:"poster_path"`
			Overview      string `json:"overview"`
		}
		if err := tmdbGet(s, fmt.Sprintf("/movie/%d", item.TMDBID), map[string]string{"language": "zh-CN"}, &movie); err == nil {
			item.Title = firstNonEmpty(movie.Title, movie.OriginalTitle, item.Title)
			item.TMDBYear = firstYear(movie.ReleaseDate)
			item.PosterPath = movie.PosterPath
			item.Overview = movie.Overview
		}
		return
	}
	var tv tmdbTVDetail
	if err := tmdbGet(s, fmt.Sprintf("/tv/%d", item.TMDBID), map[string]string{"language": "zh-CN"}, &tv); err != nil {
		return
	}
	item.Title = firstNonEmpty(tv.Name, tv.OriginalName, item.Title)
	item.TMDBYear = firstYear(tv.FirstAirDate)
	item.PosterPath = tv.PosterPath
	item.Overview = tv.Overview
}

func handleDeleteSubscription(w http.ResponseWriter, r *http.Request) {
	var body struct {
		IDs []string `json:"ids"`
	}
	if err := readJSON(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if err := subStore.Delete(body.IDs); err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	response := subscriptionListResponse()
	response["ok"] = true
	writeJSON(w, http.StatusOK, response)
}

func handleArchiveSubscriptions(w http.ResponseWriter, r *http.Request) {
	var body struct {
		IDs      []string `json:"ids"`
		Archived *bool    `json:"archived"`
	}
	if err := readJSON(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	archived := true
	if body.Archived != nil {
		archived = *body.Archived
	}
	_, err := subStore.Archive(body.IDs, archived)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	response := subscriptionListResponse()
	response["ok"] = true
	writeJSON(w, http.StatusOK, response)
}

func handleRunSubscriptions(w http.ResponseWriter, r *http.Request) {
	var body struct {
		IDs []string `json:"ids"`
	}
	_ = readJSON(r, &body)
	result, err := runSubscriptions(store.Get(), body.IDs)
	if err != nil {
		writeError(w, statusFromError(err), err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func handleHDHiveSearch(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Keyword   string `json:"keyword"`
		MediaType string `json:"mediaType"`
		TMDBID    int    `json:"tmdbId"`
	}
	if err := readJSON(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	results, err := searchHDHiveNormalized(store.Get(), body.Keyword, body.MediaType, body.TMDBID)
	if err != nil {
		writeError(w, statusFromError(err), err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"query": strings.TrimSpace(body.Keyword), "total": len(results), "results": results})
}

func handleHDHiveLogin(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	_ = readJSON(r, &body)
	s := store.Get()
	username := firstNonEmpty(body.Username, s.HDHiveUsername)
	password := firstNonEmpty(body.Password, s.HDHivePassword)
	if username == "" || password == "" {
		writeError(w, http.StatusBadRequest, errors.New("请先填写 HDHive 用户名和密码"))
		return
	}
	client := newHDHiveClient(settings{HDHiveURL: s.HDHiveURL, HDHiveUsername: username, HDHivePassword: password})
	user, err := client.Login(username, password)
	if err != nil {
		writeError(w, statusFromError(err), err)
		return
	}
	cookie := client.cookieHeader()
	if cookie == "" {
		writeError(w, http.StatusBadGateway, errors.New("HDHive 登录成功但未返回 Cookie"))
		return
	}
	if err := store.UpdateHDHiveAuth(username, password, cookie); err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "user": user, "settings": maskSettings(store.Get())})
}

func handleHDHiveCheckin(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Gamble bool `json:"gamble"`
	}
	_ = readJSON(r, &body)
	result, err := runHDHiveCheckin(store.Get(), body.Gamble)
	if err != nil {
		writeError(w, statusFromError(err), err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func handleHDHiveUnlock(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Slug           string `json:"slug"`
		Title          string `json:"title"`
		SubscriptionID string `json:"subscriptionId"`
	}
	if err := readJSON(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	settingsSnapshot := store.Get()
	if err := requireHDHiveAuth(settingsSnapshot); err != nil {
		writeError(w, statusFromError(err), err)
		return
	}
	client, err := newAuthenticatedHDHiveClient(settingsSnapshot)
	if err != nil {
		writeError(w, statusFromError(err), err)
		return
	}
	activeClient := client
	unlocked, err := client.Unlock(body.Slug)
	if err != nil {
		if isHDHiveTokenError(err) && hasHDHiveCredentials(settingsSnapshot) {
			syncHDHiveCookie(client)
			refreshedClient, refreshErr := refreshHDHiveLogin(settingsSnapshot)
			if refreshErr != nil {
				writeError(w, statusFromError(refreshErr), fmt.Errorf("HDHive token 失效，自动登录失败：%w", refreshErr))
				return
			}
			unlocked, err = refreshedClient.Unlock(body.Slug)
			activeClient = refreshedClient
			syncHDHiveCookie(refreshedClient)
		}
		if err != nil {
			syncHDHiveCookie(activeClient)
			writeError(w, statusFromError(err), err)
			return
		}
	}
	syncHDHiveCookie(activeClient)
	if !is115Link(unlocked.URL) {
		writeError(w, http.StatusBadGateway, errors.New("HDHive 解锁后未返回 115 链接"))
		return
	}
	result := normalizedResult{
		Title:        firstNonEmpty(cleanHDHiveLockedTitle(body.Title), unlocked.ResourceName, unlocked.Title, "HDHive 资源"),
		URL:          unlocked.URL,
		Password:     firstNonEmpty(unlocked.Password, extractPassword(unlocked.URL)),
		Source:       "HDHive",
		HDHiveSlug:   strings.TrimSpace(body.Slug),
		HDHiveLocked: false,
	}
	response := map[string]any{"result": result}
	if strings.TrimSpace(body.SubscriptionID) != "" {
		updated, updateErr := subStore.ReplaceUnlockedResult(body.SubscriptionID, body.Slug, result)
		if updateErr != nil {
			writeError(w, statusFromError(updateErr), updateErr)
			return
		}
		response["subscription"] = updated
	}
	writeJSON(w, http.StatusOK, response)
}

func runSubscriptions(s settings, ids []string) (subscriptionRunResult, error) {
	if !beginSubscriptionRun() {
		return subscriptionRunResult{}, badRequest("订阅扫描正在运行")
	}
	defer endSubscriptionRun()
	started := time.Now()
	selected := selectSubscriptions(subStore.List(), ids)
	result := subscriptionRunResult{StartedAt: started.Format(time.RFC3339), Total: len(selected), Items: make([]subscriptionRunItem, 0, len(selected))}
	for _, item := range selected {
		runItem := subscriptionRunItem{Subscription: item}
		resources, err := searchSubscriptionResources(s, item)
		if err != nil {
			runItem.Error = err.Error()
			result.Failed++
			subStore.UpdateResult(item.ID, nil, "error", err.Error())
			result.Items = append(result.Items, runItem)
			continue
		}
		runItem.Results = resources
		message := formatSubscriptionRunMessage(resources)
		if shouldAutoTransferSubscription(s, item) && len(resources) > 0 {
			for _, candidate := range resources {
				if strings.TrimSpace(candidate.URL) == "" {
					continue
				}
				transfer, transferErr := transfer115(s, transferRequest{URL: candidate.URL, Password: candidate.Password, TargetCID: subscriptionTargetCID(item, s)})
				if transferErr != nil {
					runItem.Error = transferErr.Error()
					message += "，自动转存失败：" + transferErr.Error()
				} else {
					runItem.Transferred = &transfer
					message += "，已自动转存"
				}
				break
			}
		}
		if runItem.Error != "" {
			result.Failed++
			subStore.UpdateResult(item.ID, resources, "warning", message)
		} else {
			result.Success++
			subStore.UpdateResult(item.ID, resources, "success", message)
		}
		result.Items = append(result.Items, runItem)
	}
	result.FinishedAt = time.Now().Format(time.RFC3339)
	subLastRunAt = time.Now()
	return result, nil
}

func searchSubscriptionResources(s settings, item subscription) ([]normalizedResult, error) {
	mediaType := normalizeHDHiveMediaType(item.MediaType)
	results := make([]normalizedResult, 0)
	errorsText := make([]string, 0)
	if pansouResults, err := searchKeyword(s, item.Title); err == nil {
		if values, ok := pansouResults["results"].([]normalizedResult); ok {
			results = append(results, values...)
		}
	} else {
		errorsText = append(errorsText, "PanSou: "+err.Error())
	}
	if hdhiveResults, err := searchHDHiveNormalized(s, item.Title, mediaType, item.TMDBID); err == nil {
		results = append(results, hdhiveResults...)
	} else {
		errorsText = append(errorsText, "HDHive: "+err.Error())
	}
	if len(results) == 0 && len(errorsText) > 0 {
		return nil, errors.New(strings.Join(errorsText, "；"))
	}
	results = dedupeNormalizedResults(results, 40)
	return rankWithOpenAI(s, item, results), nil
}

func formatSubscriptionRunMessage(resources []normalizedResult) string {
	pansouCount := 0
	hdhiveCount := 0
	lockedCount := 0
	unlockPoints := 0
	for _, item := range resources {
		if strings.EqualFold(item.Source, "HDHive") {
			hdhiveCount++
			if item.HDHiveLocked {
				lockedCount++
				unlockPoints += item.UnlockPoints
			}
		} else {
			pansouCount++
		}
	}
	parts := []string{fmt.Sprintf("找到 %d 条候选资源", len(resources)), fmt.Sprintf("PanSou %d", pansouCount), fmt.Sprintf("HDHive %d", hdhiveCount)}
	if lockedCount > 0 {
		parts = append(parts, fmt.Sprintf("待审批解锁 %d 条 / %d 积分", lockedCount, unlockPoints))
	}
	return strings.Join(parts, "，")
}

func cleanHDHiveLockedTitle(title string) string {
	title = strings.TrimSpace(title)
	title = strings.ReplaceAll(title, "｜需解锁", "")
	title = strings.ReplaceAll(title, "｜未获取到115链接", "")
	return strings.TrimSpace(title)
}

func searchHDHiveNormalized(s settings, keyword, mediaType string, tmdbID int) ([]normalizedResult, error) {
	if err := requireHDHiveAuth(s); err != nil {
		return nil, err
	}
	client, err := newAuthenticatedHDHiveClient(s)
	if err != nil {
		return nil, err
	}
	activeClient := client
	resources, err := client.Search(keyword, mediaType, tmdbID)
	if err != nil {
		if isHDHiveTokenError(err) && hasHDHiveCredentials(s) {
			syncHDHiveCookie(client)
			refreshedClient, refreshErr := refreshHDHiveLogin(s)
			if refreshErr != nil {
				return nil, fmt.Errorf("HDHive token 失效，自动登录失败：%w", refreshErr)
			}
			resources, err = refreshedClient.Search(keyword, mediaType, tmdbID)
			activeClient = refreshedClient
			syncHDHiveCookie(refreshedClient)
		}
		if err != nil {
			syncHDHiveCookie(activeClient)
			return nil, err
		}
	}
	syncHDHiveCookie(activeClient)
	results := make([]normalizedResult, 0, len(resources))
	for _, item := range resources {
		if item.URL != "" && !is115Link(item.URL) {
			continue
		}
		title := firstNonEmpty(item.ResourceName, item.Title, "HDHive 资源") + formatHDHiveMeta(item)
		if item.URL == "" {
			title += "｜需解锁"
		}
		results = append(results, normalizedResult{
			Title:        title,
			URL:          item.URL,
			Password:     firstNonEmpty(item.Password, extractPassword(item.URL)),
			Source:       "HDHive",
			Datetime:     item.CreatedAt,
			Query:        strings.TrimSpace(keyword),
			HDHiveSlug:   item.Slug,
			HDHiveLocked: item.URL == "" && item.Slug != "",
			UnlockPoints: item.UnlockPoints,
		})
		if len(results) >= 30 {
			break
		}
	}
	return dedupeNormalizedResults(results, 30), nil
}

func newHDHiveClient(s settings) *hdhiveClient {
	jar, _ := cookiejar.New(nil)
	return &hdhiveClient{baseURL: strings.TrimRight(fallback(s.HDHiveURL, defaultHDHiveURL), "/"), cookie: s.HDHiveCookie, client: &http.Client{Timeout: 30 * time.Second, Jar: jar}}
}

func newAuthenticatedHDHiveClient(s settings) (*hdhiveClient, error) {
	if err := requireHDHiveAuth(s); err != nil {
		return nil, err
	}
	client := newHDHiveClient(s)
	if !hasHDHiveCredentials(s) {
		return client, nil
	}
	if strings.TrimSpace(s.HDHiveCookie) == "" {
		if _, err := client.Login(s.HDHiveUsername, s.HDHivePassword); err != nil {
			return nil, err
		}
		syncHDHiveCookie(client)
		return client, nil
	}
	if _, err := client.CheckConnection(); err != nil {
		if _, loginErr := client.Login(s.HDHiveUsername, s.HDHivePassword); loginErr != nil {
			return nil, fmt.Errorf("HDHive Cookie 失效，自动登录失败：%w", loginErr)
		}
		syncHDHiveCookie(client)
	}
	return client, nil
}

func refreshHDHiveLogin(s settings) (*hdhiveClient, error) {
	if !hasHDHiveCredentials(s) {
		return nil, badRequest("HDHive token 失效，请先在授权页填写用户名和密码")
	}
	client := newHDHiveClient(settings{HDHiveURL: s.HDHiveURL, HDHiveUsername: s.HDHiveUsername, HDHivePassword: s.HDHivePassword})
	if _, err := client.Login(s.HDHiveUsername, s.HDHivePassword); err != nil {
		return nil, err
	}
	syncHDHiveCookie(client)
	return client, nil
}

func runHDHiveCheckin(s settings, gamble bool) (hdhiveCheckinResult, error) {
	client, err := newAuthenticatedHDHiveClient(s)
	if err != nil {
		return hdhiveCheckinResult{}, err
	}
	result, err := client.Checkin(gamble)
	if err != nil && isHDHiveTokenError(err) && hasHDHiveCredentials(s) {
		refreshedClient, refreshErr := refreshHDHiveLogin(s)
		if refreshErr != nil {
			return hdhiveCheckinResult{}, fmt.Errorf("HDHive token 失效，自动登录失败：%w", refreshErr)
		}
		client = refreshedClient
		result, err = client.Checkin(gamble)
	}
	syncHDHiveCookie(client)
	if err != nil {
		return hdhiveCheckinResult{}, err
	}
	return result, nil
}

func runDailyHDHiveCheckin(s settings) {
	if err := requireHDHiveAuth(s); err != nil {
		return
	}
	today := time.Now().Format("2006-01-02")
	hdhiveCheckinMu.Lock()
	if hdhiveLastCheckinDate == today {
		hdhiveCheckinMu.Unlock()
		return
	}
	hdhiveLastCheckinDate = today
	hdhiveCheckinMu.Unlock()
	result, err := runHDHiveCheckin(s, false)
	if err != nil {
		hdhiveCheckinMu.Lock()
		if hdhiveLastCheckinDate == today {
			hdhiveLastCheckinDate = ""
		}
		hdhiveCheckinMu.Unlock()
		log.Printf("HDHive daily check-in failed: %v", err)
		return
	}
	log.Printf("HDHive daily check-in: %s, points earned: %d", result.Message, result.PointsEarned)
}

func hasHDHiveCredentials(s settings) bool {
	return strings.TrimSpace(s.HDHiveUsername) != "" && strings.TrimSpace(s.HDHivePassword) != ""
}

func requireHDHiveAuth(s settings) error {
	if strings.TrimSpace(s.HDHiveCookie) != "" || hasHDHiveCredentials(s) {
		return nil
	}
	return badRequest("缺少配置：hdhiveCookie 或 hdhiveUsername/hdhivePassword")
}

func (c *hdhiveClient) CheckConnection() (map[string]any, error) {
	for _, path := range []string{"/user/settings", "/"} {
		raw, err := c.fetchText(path)
		if err != nil {
			continue
		}
		if user := extractHDHiveCurrentUser(raw); len(user) > 0 {
			return user, nil
		}
	}
	return nil, errors.New("HDHive Cookie 无效或未登录")
}

func (c *hdhiveClient) Login(username, password string) (map[string]any, error) {
	username = strings.TrimSpace(username)
	password = strings.TrimSpace(password)
	if username == "" || password == "" {
		return nil, badRequest("HDHive 用户名或密码为空")
	}
	pagePath := "/login"
	raw, err := c.fetchText(pagePath)
	if err != nil {
		return nil, err
	}
	actionID, err := c.resolveActionID(raw, "login")
	if err != nil || actionID == "" {
		return nil, firstErr(err, errors.New("未找到 HDHive login action"))
	}
	body, _ := json.Marshal([]map[string]string{{"username": username, "password": password}})
	responseRaw, err := c.postHDHiveAction(pagePath, actionID, body)
	if err != nil {
		return nil, err
	}
	if user := extractHDHiveCurrentUser(string(responseRaw)); len(user) > 0 {
		return user, nil
	}
	if user, err := c.CheckConnection(); err == nil && len(user) > 0 {
		return user, nil
	}
	if message := extractHDHiveActionMessage(responseRaw); message != "" {
		return nil, errors.New("HDHive 登录失败：" + message)
	}
	return nil, errors.New("HDHive 登录失败，未获取到用户信息")
}

func (c *hdhiveClient) Checkin(gamble bool) (hdhiveCheckinResult, error) {
	if result, err := c.checkinViaWeb(gamble); err == nil {
		return result, nil
	} else if !strings.Contains(err.Error(), "404") && !strings.Contains(err.Error(), "未获取到 HDHive 签到页面") {
		return hdhiveCheckinResult{}, err
	}
	return c.checkinViaAPI(gamble)
}

func (c *hdhiveClient) checkinViaWeb(gamble bool) (hdhiveCheckinResult, error) {
	pagePath, pageHTML, err := c.loadCheckinPage()
	if err != nil {
		return hdhiveCheckinResult{}, err
	}
	actionID, err := c.resolveActionID(pageHTML, "checkIn")
	if err != nil || actionID == "" {
		return hdhiveCheckinResult{}, firstErr(err, errors.New("未找到 HDHive checkIn action"))
	}
	body, _ := json.Marshal([]bool{gamble})
	responseRaw, err := c.postHDHiveAction(pagePath, actionID, body)
	if err != nil {
		return hdhiveCheckinResult{}, err
	}
	payload := parseHDHiveActionPayload(responseRaw)
	return checkinResultFromPayload(payload, "HDHive 签到完成"), nil
}

func (c *hdhiveClient) loadCheckinPage() (string, string, error) {
	for _, path := range []string{"/user/signin", "/user/checkin"} {
		raw, err := c.fetchText(path)
		if err == nil && strings.TrimSpace(raw) != "" {
			return path, raw, nil
		}
	}
	return "", "", errors.New("未获取到 HDHive 签到页面，请检查 Cookie")
}

func (c *hdhiveClient) checkinViaAPI(gamble bool) (hdhiveCheckinResult, error) {
	body := map[string]any{}
	if gamble {
		body["is_gambler"] = true
	}
	raw, _ := json.Marshal(body)
	req, err := http.NewRequest(http.MethodPost, c.baseURL+"/api/checkin", bytes.NewReader(raw))
	if err != nil {
		return hdhiveCheckinResult{}, err
	}
	c.setHeaders(req, "application/json")
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.client.Do(req)
	if err != nil {
		return hdhiveCheckinResult{}, err
	}
	defer resp.Body.Close()
	respRaw, _ := io.ReadAll(io.LimitReader(resp.Body, 2*1024*1024))
	if resp.StatusCode >= 400 {
		return hdhiveCheckinResult{}, fmt.Errorf("HDHive 签到失败：HTTP %d %s", resp.StatusCode, shortBody(respRaw))
	}
	var payload map[string]any
	if err := json.Unmarshal(respRaw, &payload); err != nil {
		return hdhiveCheckinResult{}, err
	}
	return checkinResultFromPayload(payload, "HDHive 签到完成"), nil
}

func (c *hdhiveClient) Search(keyword, mediaType string, tmdbID int) ([]hdhiveResource, error) {
	mediaType = normalizeHDHiveMediaType(mediaType)
	if tmdbID > 0 {
		if resources, err := c.searchByTMDB(tmdbID, mediaType); err == nil && len(resources) > 0 {
			return resources, nil
		}
	}
	keyword = strings.TrimSpace(keyword)
	if keyword == "" {
		return nil, badRequest("缺少 HDHive 搜索关键词")
	}
	return c.searchByKeyword(keyword, mediaType)
}

func (c *hdhiveClient) searchByTMDB(tmdbID int, mediaType string) ([]hdhiveResource, error) {
	slug := strconv.Itoa(tmdbID)
	if raw, err := c.fetchText(fmt.Sprintf("/tmdb/%s/%d", mediaType, tmdbID)); err == nil {
		if resources := hdhiveResourcesFromHTML(raw); len(resources) > 0 {
			return resources, nil
		}
		pattern := regexp.MustCompile(fmt.Sprintf(`NEXT_REDIRECT;replace;/%s/([^;]+);307`, regexp.QuoteMeta(mediaType)))
		if match := pattern.FindStringSubmatch(raw); len(match) > 1 {
			slug = strings.TrimSpace(match[1])
		}
	}
	resources, err := c.resourcesFromDetail(fmt.Sprintf("/%s/%s", mediaType, url.PathEscape(slug)))
	if (err != nil || len(resources) == 0) && slug != strconv.Itoa(tmdbID) {
		return c.resourcesFromDetail(fmt.Sprintf("/%s/%d", mediaType, tmdbID))
	}
	return resources, err
}

func (c *hdhiveClient) searchByKeyword(keyword, mediaType string) ([]hdhiveResource, error) {
	raw, err := c.fetchText(fmt.Sprintf("/%s?keyword=%s", mediaType, url.QueryEscape(keyword)))
	if err != nil {
		return nil, err
	}
	candidates := searchHDHiveMediaCandidates(raw, keyword, mediaType)
	if len(candidates) == 0 {
		return nil, nil
	}
	merged := make([]hdhiveResource, 0)
	seen := map[string]bool{}
	for _, candidate := range candidates {
		slug := strings.TrimSpace(anyToString(candidate["slug"]))
		if slug == "" {
			continue
		}
		resources, err := c.resourcesFromDetail(fmt.Sprintf("/%s/%s", mediaType, url.PathEscape(slug)))
		if err != nil {
			continue
		}
		for _, resource := range resources {
			key := strings.ToLower(firstNonEmpty(resource.Slug, resource.Title, resource.URL))
			if key == "" || seen[key] {
				continue
			}
			seen[key] = true
			merged = append(merged, resource)
			if len(merged) >= 30 {
				return merged, nil
			}
		}
	}
	return merged, nil
}

func (c *hdhiveClient) resourcesFromDetail(path string) ([]hdhiveResource, error) {
	raw, err := c.fetchText(path)
	if err != nil {
		return nil, err
	}
	return hdhiveResourcesFromHTML(raw), nil
}

func hdhiveResourcesFromHTML(raw string) []hdhiveResource {
	rows := extractHDHiveArray(raw, "115")
	resources := make([]hdhiveResource, 0, len(rows))
	for index, row := range rows {
		panType := normalizeHDHivePanType(row["pan_type"])
		if panType == "" {
			panType = "115"
		}
		if panType != "115" {
			continue
		}
		resource := mapHDHiveResource(row, index)
		resources = append(resources, resource)
	}
	return resources
}

func (c *hdhiveClient) Unlock(slug string) (hdhiveResource, error) {
	slug = strings.TrimSpace(slug)
	if slug == "" {
		return hdhiveResource{}, errors.New("HDHive 资源 slug 为空")
	}
	pagePath := "/resource/115/" + url.PathEscape(slug)
	raw, err := c.fetchText(pagePath)
	if err != nil {
		return hdhiveResource{}, err
	}
	if share := extractHDHiveShareLink(raw); share != "" {
		return hdhiveResource{Slug: slug, URL: share, Password: extractPassword(share)}, nil
	}
	actionID, err := c.resolveActionID(raw, "unlockResource")
	if err != nil || actionID == "" {
		return hdhiveResource{}, firstErr(err, errors.New("未找到 HDHive unlockResource action"))
	}
	body, _ := json.Marshal([]string{slug})
	responseRaw, err := c.postHDHiveAction(pagePath, actionID, body)
	if err != nil {
		return hdhiveResource{}, err
	}
	share := extractHDHiveShareLink(string(responseRaw))
	if share == "" {
		share = extractHDHiveActionShareLink(string(responseRaw))
	}
	if share == "" {
		return hdhiveResource{}, errors.New("HDHive 解锁后未返回 115 链接")
	}
	return hdhiveResource{Slug: slug, URL: share, Password: extractPassword(share)}, nil
}

func (c *hdhiveClient) postHDHiveAction(pagePath, actionID string, body []byte) ([]byte, error) {
	for attempt := 0; attempt < 2; attempt++ {
		_ = c.prefetchActionToken()
		req, err := http.NewRequest(http.MethodPost, c.baseURL+pagePath, bytes.NewReader(body))
		if err != nil {
			return nil, err
		}
		c.setHeaders(req, "text/x-component")
		req.Header.Set("Origin", c.baseURL)
		req.Header.Set("Referer", c.baseURL+pagePath)
		req.Header.Set("Next-Action", actionID)
		req.Header.Set("Content-Type", "text/plain;charset=UTF-8")
		resp, err := c.client.Do(req)
		if err != nil {
			return nil, err
		}
		responseRaw, _ := io.ReadAll(io.LimitReader(resp.Body, 2*1024*1024))
		resp.Body.Close()
		if resp.StatusCode < 400 {
			return responseRaw, nil
		}
		if attempt == 0 && isHDHiveActionTokenInvalid(responseRaw) {
			continue
		}
		return nil, fmt.Errorf("HDHive 解锁失败：HTTP %d %s", resp.StatusCode, shortBody(responseRaw))
	}
	return nil, errors.New("HDHive 解锁失败：action token 刷新后仍无效")
}

func (c *hdhiveClient) fetchText(path string) (string, error) {
	endpoint := path
	if !strings.HasPrefix(endpoint, "http") {
		endpoint = c.baseURL + path
	}
	req, err := http.NewRequest(http.MethodGet, endpoint, nil)
	if err != nil {
		return "", err
	}
	c.setHeaders(req, "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")
	resp, err := c.client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 5*1024*1024))
	if resp.StatusCode >= 400 {
		return "", fmt.Errorf("HDHive 请求失败：HTTP %d %s", resp.StatusCode, shortBody(raw))
	}
	return string(raw), nil
}

func (c *hdhiveClient) resolveActionID(pageHTML, actionName string) (string, error) {
	for _, path := range extractHDHiveChunkPaths(pageHTML) {
		raw, err := c.fetchText(path)
		if err != nil {
			continue
		}
		if actionID := extractHDHiveActionID(raw, actionName); actionID != "" {
			return actionID, nil
		}
	}
	return "", errors.New("未找到 Server Action")
}

func (c *hdhiveClient) prefetchActionToken() error {
	req, err := http.NewRequest(http.MethodHead, c.baseURL+"/login", nil)
	if err != nil {
		return err
	}
	c.setHeaders(req, "*/*")
	resp, err := c.client.Do(req)
	if err != nil {
		return err
	}
	resp.Body.Close()
	return nil
}

func (c *hdhiveClient) setHeaders(req *http.Request, accept string) {
	req.Header.Set("User-Agent", hdhiveUserAgent)
	req.Header.Set("Accept", accept)
	if cookieHeader := c.cookieHeader(); cookieHeader != "" {
		req.Header.Set("Cookie", cookieHeader)
	}
}

func (c *hdhiveClient) cookieHeader() string {
	pairs := parseCookieHeader(c.cookie)
	if c.client != nil && c.client.Jar != nil {
		if base, err := url.Parse(c.baseURL); err == nil {
			for _, cookie := range c.client.Jar.Cookies(base) {
				if cookie.Name != "" {
					pairs[cookie.Name] = cookie.Value
				}
			}
		}
	}
	return serializeCookieHeader(pairs)
}

func syncHDHiveCookie(client *hdhiveClient) {
	if client == nil || store == nil {
		return
	}
	if cookie := client.cookieHeader(); cookie != "" {
		if err := store.UpdateHDHiveCookie(cookie); err != nil {
			log.Printf("HDHive cookie update failed: %v", err)
		}
		client.cookie = cookie
	}
}

func parseCookieHeader(header string) map[string]string {
	pairs := map[string]string{}
	for _, part := range strings.Split(header, ";") {
		part = strings.TrimSpace(part)
		if part == "" || !strings.Contains(part, "=") {
			continue
		}
		name, value, _ := strings.Cut(part, "=")
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		pairs[name] = strings.TrimSpace(value)
	}
	return pairs
}

func serializeCookieHeader(pairs map[string]string) string {
	if len(pairs) == 0 {
		return ""
	}
	keys := make([]string, 0, len(pairs))
	for key := range pairs {
		if key != "" {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, key := range keys {
		parts = append(parts, key+"="+pairs[key])
	}
	return strings.Join(parts, "; ")
}

func isHDHiveActionTokenInvalid(raw []byte) bool {
	text := strings.ToLower(string(raw))
	return strings.Contains(text, "action_token_invalid") || strings.Contains(text, "action_token_required")
}

func isHDHiveTokenError(err error) bool {
	if err == nil {
		return false
	}
	text := strings.ToLower(err.Error())
	markers := []string{
		"action_token_invalid",
		"action_token_required",
		"access_token_invalid",
		"access_token_required",
		"token_invalid",
		"token_expired",
		"jwt",
		"未登录",
		"登录",
		"刷新页面后重试",
	}
	for _, marker := range markers {
		if strings.Contains(text, marker) {
			return true
		}
	}
	return false
}

func mapHDHiveResource(row map[string]any, index int) hdhiveResource {
	resourceURL := firstNonEmpty(anyToString(row["full_url"]), anyToString(row["url"]), anyToString(row["share_link"]))
	accessCode := firstNonEmpty(anyToString(row["access_code"]), extractPassword(resourceURL))
	if resourceURL != "" && accessCode != "" && !strings.Contains(resourceURL, "password=") && !strings.Contains(resourceURL, "pwd=") {
		joiner := "?"
		if strings.Contains(resourceURL, "?") {
			joiner = "&"
		}
		resourceURL += joiner + url.Values{"password": []string{accessCode}}.Encode()
	}
	return hdhiveResource{
		Title:        firstNonEmpty(anyToString(row["title"]), fmt.Sprintf("HDHive 资源 #%d", index+1)),
		ResourceName: firstNonEmpty(anyToString(row["remark"]), anyToString(row["resource_name"]), anyToString(row["title"])),
		Slug:         normalizeHDHiveSlug(anyToString(row["slug"])),
		URL:          sanitizeHDHiveURL(resourceURL),
		Password:     accessCode,
		Size:         anyToString(row["share_size"]),
		Qualities:    stringListFromAny(row["source"]),
		Resolution:   stringListFromAny(row["video_resolution"]),
		UnlockPoints: parseInt(anyToString(row["unlock_points"])),
		Locked:       resourceURL == "",
		Source:       "HDHive",
		CreatedAt:    anyToString(row["created_at"]),
	}
}

func extractHDHiveArray(raw, fieldName string) []map[string]any {
	for _, token := range []string{fmt.Sprintf(`"%s":[`, fieldName), fmt.Sprintf(`\"%s\":[`, fieldName)} {
		payload := extractBracketPayload(raw, token)
		if payload == "" {
			continue
		}
		for _, candidate := range []string{payload, strings.ReplaceAll(strings.ReplaceAll(strings.ReplaceAll(payload, `\"`, `"`), `\/`, `/`), `\u0026`, `&`)} {
			var rows []map[string]any
			if json.Unmarshal([]byte(candidate), &rows) == nil && len(rows) > 0 {
				return rows
			}
		}
	}
	return nil
}

func extractBracketPayload(raw, token string) string {
	index := strings.Index(raw, token)
	if index < 0 {
		return ""
	}
	start := strings.Index(raw[index:], "[")
	if start < 0 {
		return ""
	}
	start += index
	depth := 0
	inString := false
	escaped := false
	for pos := start; pos < len(raw); pos++ {
		char := raw[pos]
		if escaped {
			escaped = false
			continue
		}
		if char == '\\' {
			escaped = true
			continue
		}
		if char == '"' {
			inString = !inString
			continue
		}
		if inString {
			continue
		}
		if char == '[' {
			depth++
		} else if char == ']' {
			depth--
			if depth == 0 {
				return raw[start : pos+1]
			}
		}
	}
	return ""
}

func extractHDHiveCurrentUser(raw string) map[string]any {
	normalized := strings.ReplaceAll(strings.ReplaceAll(strings.ReplaceAll(raw, `\"`, `"`), `\/`, `/`), `\u0026`, `&`)
	payload := extractObjectPayload(normalized, `"currentUser":{`)
	if payload == "" {
		payload = extractObjectPayload(raw, `"currentUser":{`)
	}
	if payload == "" {
		return nil
	}
	var user map[string]any
	if json.Unmarshal([]byte(payload), &user) != nil {
		return nil
	}
	return user
}

func extractObjectPayload(raw, token string) string {
	index := strings.Index(raw, token)
	if index < 0 {
		return ""
	}
	start := strings.Index(raw[index:], "{")
	if start < 0 {
		return ""
	}
	start += index
	depth := 0
	inString := false
	escaped := false
	for pos := start; pos < len(raw); pos++ {
		char := raw[pos]
		if escaped {
			escaped = false
			continue
		}
		if char == '\\' {
			escaped = true
			continue
		}
		if char == '"' {
			inString = !inString
			continue
		}
		if inString {
			continue
		}
		if char == '{' {
			depth++
		} else if char == '}' {
			depth--
			if depth == 0 {
				return raw[start : pos+1]
			}
		}
	}
	return ""
}

func searchHDHiveMediaCandidates(raw, keyword, mediaType string) []map[string]any {
	rows := extractHDHiveArray(raw, "data")
	keywordNorm := normalizeHDHiveKeyword(keyword)
	keywordTokens := hdhiveKeywordTokens(keyword)
	type scoredCandidate struct {
		score int
		hit   bool
		row   map[string]any
	}
	scored := make([]scoredCandidate, 0)
	for _, row := range rows {
		slug := strings.TrimSpace(anyToString(row["slug"]))
		if slug == "" {
			continue
		}
		title := firstNonEmpty(anyToString(row["title"]), anyToString(row["name"]), anyToString(row["original_title"]), anyToString(row["original_name"]))
		normalizedTitle := normalizeHDHiveKeyword(title)
		if normalizedTitle == "" {
			continue
		}
		hit := hdhiveTitleMatchesKeyword(normalizedTitle, keywordNorm, keywordTokens)
		if !hit {
			continue
		}
		score := 0
		score += 120
		if strings.EqualFold(anyToString(row["type"]), mediaType) {
			score += 20
		}
		if title != "" {
			score += 5
		}
		scored = append(scored, scoredCandidate{score: score, hit: hit, row: row})
	}
	sort.Slice(scored, func(left, right int) bool { return scored[left].score > scored[right].score })
	out := make([]map[string]any, 0)
	for _, item := range scored {
		out = append(out, item.row)
		if len(out) >= 3 {
			break
		}
	}
	return out
}

func hdhiveTitleMatchesKeyword(normalizedTitle, keywordNorm string, keywordTokens []string) bool {
	if normalizedTitle == "" || keywordNorm == "" {
		return false
	}
	if strings.Contains(normalizedTitle, keywordNorm) || strings.Contains(keywordNorm, normalizedTitle) {
		return true
	}
	if len(keywordTokens) == 0 {
		return false
	}
	matched := 0
	for _, token := range keywordTokens {
		if strings.Contains(normalizedTitle, token) {
			matched++
		}
	}
	if len(keywordTokens) == 1 {
		return matched == 1 && len(keywordTokens[0]) >= 4
	}
	return matched >= 2 || matched == len(keywordTokens)
}

func hdhiveKeywordTokens(value string) []string {
	tokens := make([]string, 0)
	seen := map[string]bool{}
	for _, part := range regexp.MustCompile("[\\s\\-_·:：,.，。!！?？/\\\\'\"`()\\[\\]（）]+").Split(value, -1) {
		token := normalizeHDHiveKeyword(part)
		if len(token) < 2 || seen[token] {
			continue
		}
		seen[token] = true
		tokens = append(tokens, token)
	}
	return tokens
}

func extractHDHiveChunkPaths(raw string) []string {
	patterns := []*regexp.Regexp{
		regexp.MustCompile(`/_next/static/chunks/[A-Za-z0-9._()/-]+\.js`),
		regexp.MustCompile(`static/chunks/[A-Za-z0-9._()/-]+\.js`),
		regexp.MustCompile(`app/\(auth\)/login/page-[A-Za-z0-9]+\.js`),
		regexp.MustCompile(`app/\(no-layout\)/resource/[A-Za-z0-9._()/-]+\.js`),
	}
	seen := map[string]bool{}
	paths := make([]string, 0)
	for _, pattern := range patterns {
		for _, match := range pattern.FindAllString(raw, -1) {
			path := strings.TrimSpace(match)
			if strings.HasPrefix(path, "static/chunks/") {
				path = "/_next/" + path
			} else if strings.HasPrefix(path, "app/") {
				path = "/_next/static/chunks/" + path
			}
			if !seen[path] {
				seen[path] = true
				paths = append(paths, path)
			}
		}
	}
	return paths
}

func extractHDHiveActionID(raw, actionName string) string {
	escaped := regexp.QuoteMeta(actionName)
	patterns := []*regexp.Regexp{
		regexp.MustCompile(`createServerReference\)\("([A-Za-z0-9]+)".{0,200}?,"` + escaped + `"\)`),
		regexp.MustCompile(`createServerReference[^"]*\("([A-Za-z0-9]+)".{0,200}?"` + escaped + `"`),
	}
	for _, pattern := range patterns {
		if match := pattern.FindStringSubmatch(raw); len(match) > 1 {
			return match[1]
		}
	}
	return ""
}

func extractHDHiveShareLink(raw string) string {
	patterns := []*regexp.Regexp{
		regexp.MustCompile(`\\?"full_url\\?":\\?"(https?://(?:115|share\.115|115cdn)[^\\"]+)\\?"`),
		regexp.MustCompile(`\\?"url\\?":\\?"(https?://(?:115|share\.115|115cdn)[^\\"]+)\\?"`),
		regexp.MustCompile(`NEXT_REDIRECT;replace;(https?://(?:115|share\.115|115cdn)[^;]+);307`),
	}
	for _, pattern := range patterns {
		if match := pattern.FindStringSubmatch(raw); len(match) > 1 {
			share := sanitizeHDHiveURL(match[1])
			if code := extractHDHiveAccessCode(raw); code != "" && !strings.Contains(share, "password=") && !strings.Contains(share, "pwd=") {
				joiner := "?"
				if strings.Contains(share, "?") {
					joiner = "&"
				}
				share += joiner + url.Values{"password": []string{code}}.Encode()
			}
			return share
		}
	}
	return ""
}

func extractHDHiveActionShareLink(raw string) string {
	var payload map[string]any
	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "1:") {
			continue
		}
		if json.Unmarshal([]byte(strings.TrimPrefix(line, "1:")), &payload) == nil {
			break
		}
	}
	data, _ := payload["data"].(map[string]any)
	if response, ok := payload["response"].(map[string]any); ok {
		data, _ = response["data"].(map[string]any)
	}
	if data == nil {
		return ""
	}
	share := sanitizeHDHiveURL(firstNonEmpty(anyToString(data["full_url"]), anyToString(data["url"])))
	code := anyToString(data["access_code"])
	if share != "" && code != "" && !strings.Contains(share, "password=") && !strings.Contains(share, "pwd=") {
		joiner := "?"
		if strings.Contains(share, "?") {
			joiner = "&"
		}
		share += joiner + url.Values{"password": []string{code}}.Encode()
	}
	return share
}

func extractHDHiveActionMessage(raw []byte) string {
	for _, line := range strings.Split(string(raw), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "1:") {
			line = strings.TrimPrefix(line, "1:")
		}
		var payload map[string]any
		if json.Unmarshal([]byte(line), &payload) != nil {
			continue
		}
		if message := anyToString(payload["message"]); message != "" {
			return message
		}
		if response, ok := payload["response"].(map[string]any); ok {
			if message := anyToString(response["message"]); message != "" {
				return message
			}
		}
		if errObj, ok := payload["error"].(map[string]any); ok {
			if message := anyToString(errObj["message"]); message != "" {
				return message
			}
		}
	}
	return ""
}

func parseHDHiveActionPayload(raw []byte) map[string]any {
	text := strings.TrimSpace(string(raw))
	if text == "" {
		return map[string]any{"success": false, "message": "空响应"}
	}
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "1:") {
			line = strings.TrimPrefix(line, "1:")
			var payload map[string]any
			if json.Unmarshal([]byte(line), &payload) == nil {
				return normalizeHDHiveActionPayload(payload)
			}
		}
	}
	var payload map[string]any
	if json.Unmarshal([]byte(text), &payload) == nil {
		return normalizeHDHiveActionPayload(payload)
	}
	return map[string]any{"success": false, "message": "未获取到响应数据"}
}

func normalizeHDHiveActionPayload(payload map[string]any) map[string]any {
	if response, ok := payload["response"].(map[string]any); ok {
		response["raw"] = payload
		return response
	}
	if errObj, ok := payload["error"].(map[string]any); ok {
		return map[string]any{"success": false, "message": firstNonEmpty(anyToString(errObj["message"]), anyToString(errObj["description"]), "请求失败"), "data": errObj["data"], "raw": payload}
	}
	return payload
}

func checkinResultFromPayload(payload map[string]any, fallbackMessage string) hdhiveCheckinResult {
	data, _ := payload["data"].(map[string]any)
	if data == nil {
		data = map[string]any{}
	}
	checkedIn := optionalBool(firstNonNil(data["checked_in"], data["checkedIn"], payload["checked_in"], payload["checkedIn"]))
	message := firstNonEmpty(anyToString(payload["message"]), anyToString(data["message"]), fallbackMessage)
	pointsEarned := firstInt(data["points_earned"], data["pointsEarned"], data["earned"], data["change"], payload["points_earned"], payload["pointsEarned"])
	points := firstInt(data["total_points"], data["totalPoints"], data["balance"], payload["points"])
	status := "success"
	normalizedMessage := strings.ToLower(message)
	if checkedIn != nil && !*checkedIn || strings.Contains(message, "已签到") || strings.Contains(message, "已经签到") || strings.Contains(message, "今日已签到") || strings.Contains(message, "今天已签到") || strings.Contains(normalizedMessage, "already") {
		status = "already_checked"
	}
	return hdhiveCheckinResult{OK: status == "success", Status: status, Message: message, PointsEarned: pointsEarned, Points: points, CheckedIn: checkedIn, Raw: payload}
}

func extractHDHiveAccessCode(raw string) string {
	for _, pattern := range []*regexp.Regexp{regexp.MustCompile(`\\?"access_code\\?":\\?"([A-Za-z0-9]{4})\\?"`)} {
		if match := pattern.FindStringSubmatch(raw); len(match) > 1 {
			return match[1]
		}
	}
	return ""
}

func testOpenAICompatible(s settings) error {
	_, err := callOpenAICompatible(s, []map[string]string{{"role": "user", "content": "ping"}}, 4)
	return err
}

func rankWithOpenAI(s settings, sub subscription, results []normalizedResult) []normalizedResult {
	if s.OpenAIBaseURL == "" || s.OpenAIAPIKey == "" || s.OpenAIModel == "" || len(results) < 2 {
		return results
	}
	candidates := make([]map[string]any, 0, len(results))
	for index, result := range results {
		candidates = append(candidates, map[string]any{"index": index, "title": result.Title, "source": result.Source})
	}
	payload, _ := json.Marshal(map[string]any{"subscription": sub, "candidates": candidates})
	content, err := callOpenAICompatible(s, []map[string]string{
		{"role": "system", "content": "你是媒体资源识别助手。只输出 JSON 数组，每项包含 index 和 score，score 0-100。优先匹配标题、季、集、清晰度和115可转存资源。"},
		{"role": "user", "content": string(payload)},
	}, 800)
	if err != nil {
		log.Printf("OpenAI compatible ranking failed: %v", err)
		return results
	}
	type scored struct {
		Index int `json:"index"`
		Score int `json:"score"`
	}
	var scores []scored
	if err := json.Unmarshal([]byte(extractJSONArray(content)), &scores); err != nil {
		return results
	}
	scoreMap := map[int]int{}
	for _, item := range scores {
		scoreMap[item.Index] = item.Score
	}
	sorted := append([]normalizedResult(nil), results...)
	sort.SliceStable(sorted, func(left, right int) bool { return scoreMap[left] > scoreMap[right] })
	return sorted
}

func callOpenAICompatible(s settings, messages []map[string]string, maxTokens int) (string, error) {
	endpoint := strings.TrimRight(s.OpenAIBaseURL, "/") + "/chat/completions"
	body := map[string]any{"model": s.OpenAIModel, "messages": messages, "temperature": 0.1, "max_tokens": maxTokens}
	raw, _ := json.Marshal(body)
	req, err := http.NewRequest(http.MethodPost, endpoint, bytes.NewReader(raw))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+s.OpenAIAPIKey)
	client := *httpCli
	client.Timeout = 45 * time.Second
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	respRaw, _ := io.ReadAll(io.LimitReader(resp.Body, 2*1024*1024))
	if resp.StatusCode >= 400 {
		return "", fmt.Errorf("OpenAI 兼容接口失败：HTTP %d %s", resp.StatusCode, shortBody(respRaw))
	}
	var decoded struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(respRaw, &decoded); err != nil {
		return "", err
	}
	if len(decoded.Choices) == 0 {
		return "", errors.New("OpenAI 兼容接口未返回 choices")
	}
	return decoded.Choices[0].Message.Content, nil
}

func startSubscriptionScheduler() {
	go func() {
		ticker := time.NewTicker(time.Minute)
		defer ticker.Stop()
		for range ticker.C {
			s := store.Get()
			go runDailyHDHiveCheckin(s)
			if !s.SubEnabled || subscriptionRunIsActive() {
				continue
			}
			interval := time.Duration(clampIntervalHours(s.SubInterval)) * time.Hour
			if !subLastRunAt.IsZero() && time.Since(subLastRunAt) < interval {
				continue
			}
			go func(snapshot settings) {
				if _, err := runSubscriptions(snapshot, nil); err != nil {
					log.Printf("subscription auto scan failed: %v", err)
				}
			}(s)
		}
	}()
}

func selectSubscriptions(items []subscription, ids []string) []subscription {
	set := map[string]bool{}
	for _, id := range ids {
		if strings.TrimSpace(id) != "" {
			set[strings.TrimSpace(id)] = true
		}
	}
	selected := make([]subscription, 0)
	for _, item := range items {
		if !item.Enabled || item.Archived {
			continue
		}
		if len(set) > 0 && !set[item.ID] {
			continue
		}
		selected = append(selected, item)
	}
	return selected
}

func beginSubscriptionRun() bool {
	subRunMu.Lock()
	defer subRunMu.Unlock()
	if subRunActive {
		return false
	}
	subRunActive = true
	return true
}

func endSubscriptionRun() { subRunMu.Lock(); subRunActive = false; subRunMu.Unlock() }

func subscriptionRunIsActive() bool { subRunMu.Lock(); defer subRunMu.Unlock(); return subRunActive }

func shouldAutoTransferSubscription(s settings, item subscription) bool {
	return s.SubAutoTransfer || item.AutoTransfer
}

func subscriptionTargetCID(item subscription, s settings) string {
	targetCID := strings.TrimSpace(item.TargetCID)
	if targetCID == "" || targetCID == "0" {
		return firstNonEmpty(s.P115TargetCID, "0")
	}
	return targetCID
}

func normalizeHDHiveMediaType(value string) string {
	if strings.EqualFold(strings.TrimSpace(value), "movie") {
		return "movie"
	}
	return "tv"
}

func normalizeHDHivePanType(value any) string {
	if value == nil {
		return "115"
	}
	text := strings.TrimSpace(anyToString(value))
	if text == "" || strings.EqualFold(text, "null") || strings.EqualFold(text, "nil") || text == "<nil>" {
		return "115"
	}
	normalized := regexp.MustCompile(`[^a-z0-9]+`).ReplaceAllString(strings.ToLower(text), "")
	if normalized == "115" || normalized == "115com" || normalized == "115wangpan" || normalized == "115netdisk" || normalized == "" {
		return "115"
	}
	return normalized
}

func normalizeHDHiveSlug(value string) string {
	return regexp.MustCompile(`[^A-Za-z0-9]`).ReplaceAllString(strings.TrimSpace(value), "")
}

func normalizeHDHiveKeyword(value string) string {
	return regexp.MustCompile("[\\s\\-_·:：,.，。!！?？/\\\\'\"`()\\[\\]]+").ReplaceAllString(strings.ToLower(strings.TrimSpace(value)), "")
}

func sanitizeHDHiveURL(value string) string {
	value = html.UnescapeString(strings.TrimSpace(value))
	value = strings.ReplaceAll(value, `\/`, `/`)
	value = strings.ReplaceAll(value, `\u0026`, `&`)
	value = strings.TrimRight(value, "&#&")
	if decoded, err := url.QueryUnescape(value); err == nil && strings.HasPrefix(decoded, "http") {
		return decoded
	}
	return value
}

func stringListFromAny(value any) []string {
	items := make([]string, 0)
	for _, item := range anySlice(value) {
		if text := strings.TrimSpace(anyToString(item)); text != "" {
			items = append(items, text)
		}
	}
	return items
}

func formatHDHiveMeta(item hdhiveResource) string {
	parts := compactStringSlice([]string{item.Size, strings.Join(item.Resolution, "/"), strings.Join(item.Qualities, "/")})
	if item.UnlockPoints > 0 {
		parts = append(parts, fmt.Sprintf("%d积分", item.UnlockPoints))
	}
	if len(parts) == 0 {
		return ""
	}
	return "｜" + strings.Join(parts, "｜")
}

func extractJSONArray(value string) string {
	start := strings.Index(value, "[")
	end := strings.LastIndex(value, "]")
	if start >= 0 && end > start {
		return value[start : end+1]
	}
	return value
}

func firstErr(primary error, fallbackErr error) error {
	if primary != nil {
		return primary
	}
	return fallbackErr
}

func firstNonNil(values ...any) any {
	for _, value := range values {
		if value != nil {
			return value
		}
	}
	return nil
}

func optionalBool(value any) *bool {
	switch v := value.(type) {
	case nil:
		return nil
	case bool:
		return &v
	case string:
		if strings.TrimSpace(v) == "" {
			return nil
		}
		parsed := parseBool(v)
		return &parsed
	default:
		text := strings.TrimSpace(fmt.Sprint(v))
		if text == "" || strings.EqualFold(text, "null") {
			return nil
		}
		parsed := parseBool(text)
		return &parsed
	}
}

func firstInt(values ...any) int {
	for _, value := range values {
		switch v := value.(type) {
		case nil:
			continue
		case int:
			if v != 0 {
				return v
			}
		case int64:
			if v != 0 {
				return int(v)
			}
		case float64:
			if v != 0 {
				return int(v)
			}
		case json.Number:
			if n, err := v.Int64(); err == nil && n != 0 {
				return int(n)
			}
		case string:
			if n, err := strconv.Atoi(strings.TrimSpace(v)); err == nil && n != 0 {
				return n
			}
		}
	}
	return 0
}

func parseBool(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "1", "true", "yes", "y", "on", "启用", "是":
		return true
	default:
		return false
	}
}

func getenvBool(key string, fallbackValue bool) bool {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallbackValue
	}
	return parseBool(value)
}

func clampIntervalHours(value int) int {
	if value <= 0 {
		return 6
	}
	if value > 168 {
		return 168
	}
	return value
}
