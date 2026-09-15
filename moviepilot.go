package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

func handleMPSearch(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Keyword        string `json:"keyword"`
		TMDBID         string `json:"tmdbId"`
		OriginalTitle  string `json:"originalTitle"`
		Season         int    `json:"season"`
		Episodes       []int  `json:"episodes"`
		NumberingBasis string `json:"numberingBasis"`
	}
	if err := readJSON(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	// Resource order has no verified mapping to MoviePilot's TMDB order.
	if body.NumberingBasis == "resource" {
		body.TMDBID, body.OriginalTitle = "", ""
		body.Season, body.Episodes = 0, nil
	}
	s := store.Get()
	if s.MPUrl == "" || s.MPToken == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "请先配置 MoviePilot 地址和 API Token"})
		return
	}
	var allResults []map[string]any
	var errors []string
	keywords := []string{body.Keyword}
	if body.OriginalTitle != "" && body.OriginalTitle != body.Keyword {
		keywords = append(keywords, body.OriginalTitle)
	}
	for _, kw := range keywords {
		r, err := mpDoSearch(s, kw, body.TMDBID)
		if err != nil {
			errors = append(errors, kw+": "+err.Error())
		}
		allResults = append(allResults, r...)
	}
	target := scoreTarget{Season: body.Season, Episodes: body.Episodes}
	if body.NumberingBasis != "resource" {
		allResults = annotateTorrentResults(allResults, target)
	}
	result := map[string]any{"results": allResults}
	if len(errors) > 0 {
		result["errors"] = errors
	}
	writeJSON(w, http.StatusOK, result)
}

func mpDoSearch(s settings, keyword string, tmdbID string) ([]map[string]any, error) {
	client := &http.Client{Timeout: 30 * time.Second}
	mpURL := s.MPUrl
	token := s.MPToken

	if mpURL == "" || token == "" {
		return nil, fmt.Errorf("MP 地址或 Token 未配置")
	}

	var allResults []map[string]any

	// 1. 标题模糊搜
	u1 := fmt.Sprintf("%s/api/v1/search/title?keyword=%s", mpURL, url.QueryEscape(keyword))
	req1, _ := http.NewRequest(http.MethodGet, u1, nil)
	req1.Header.Set("Accept", "application/json")
	req1.Header.Set("x-api-key", token)
	resp1, err := client.Do(req1)
	if err != nil {
		return nil, fmt.Errorf("MP 请求失败(%s): %w", u1, err)
	}
	raw1, _ := io.ReadAll(resp1.Body)
	resp1.Body.Close()
	if resp1.StatusCode >= 400 {
		return nil, fmt.Errorf("MP HTTP %d: %s", resp1.StatusCode, shortBody(raw1))
	}
	allResults = append(allResults, mpParseResults(raw1)...)

	// 2. TMDB 精确搜
	if tmdbID != "" {
		u2 := fmt.Sprintf("%s/api/v1/search/media/tmdb:%s", mpURL, tmdbID)
		req2, _ := http.NewRequest(http.MethodGet, u2, nil)
		req2.Header.Set("Accept", "application/json")
		req2.Header.Set("x-api-key", token)
		resp2, err := client.Do(req2)
		if err == nil {
			raw2, _ := io.ReadAll(resp2.Body)
			resp2.Body.Close()
			allResults = append(allResults, mpParseResults(raw2)...)
		}
	}

	// 去重
	seen := map[string]bool{}
	var deduped []map[string]any
	for _, item := range allResults {
		key := ""
		if enc, ok := item["enclosure"].(string); ok && enc != "" {
			key = enc
		} else if title, ok := item["title"].(string); ok {
			key = title
		}
		if key == "" || seen[key] {
			continue
		}
		seen[key] = true
		deduped = append(deduped, item)
	}
	return deduped, nil
}

func mpParseResults(raw []byte) []map[string]any {
	var results []map[string]any
	var wrapper struct {
		Success bool             `json:"success"`
		Data    []map[string]any `json:"data"`
	}
	if json.Unmarshal(raw, &wrapper) == nil && len(wrapper.Data) > 0 {
		for _, item := range wrapper.Data {
			if ti, ok := item["torrent_info"].(map[string]any); ok {
				results = append(results, ti)
			} else if ri, ok := item["torrents"].([]any); ok {
				for _, t := range ri {
					if tm, ok := t.(map[string]any); ok {
						results = append(results, tm)
					}
				}
			} else {
				results = append(results, item)
			}
		}
		return results
	}
	json.Unmarshal(raw, &results)
	return results
}

func handleMPDownload(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Title       string         `json:"title"`
		TorrentURL  string         `json:"torrentUrl"`
		Magnet      string         `json:"magnet"`
		Description string         `json:"description"`
		TMDBID      string         `json:"tmdbId"`
		RawData     map[string]any `json:"rawData"`
	}
	if err := readJSON(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	s := store.Get()
	if s.MPUrl == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "请先配置 MoviePilot 地址"})
		return
	}
	if err := mpDownload(s, body.RawData, body.TMDBID); err != nil {
		writeError(w, statusFromError(err), err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"message": "已提交 MoviePilot 下载"})
}

func handleMPSubscribeStatus(w http.ResponseWriter, r *http.Request) {
	var body struct {
		TMDBID    string `json:"tmdbId"`
		MediaType string `json:"mediaType"`
		Season    int    `json:"season"`
	}
	if err := readJSON(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	s := store.Get()
	if strings.TrimSpace(s.MPUrl) == "" || strings.TrimSpace(s.MPToken) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "请先配置 MoviePilot 地址和 API Token"})
		return
	}
	result, err := mpSubscriptionStatus(s, body.TMDBID, body.MediaType, body.Season)
	if err != nil {
		writeError(w, statusFromError(err), err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func handleMPSubscribe(w http.ResponseWriter, r *http.Request) {
	var body struct {
		TMDBID    string `json:"tmdbId"`
		MediaType string `json:"mediaType"`
		Season    int    `json:"season"`
		Title     string `json:"title"`
	}
	if err := readJSON(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	s := store.Get()
	if strings.TrimSpace(s.MPUrl) == "" || strings.TrimSpace(s.MPToken) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "请先配置 MoviePilot 地址和 API Token"})
		return
	}
	result, err := mpSubscribe(s, body.TMDBID, body.MediaType, body.Season, body.Title)
	if err != nil {
		writeError(w, statusFromError(err), err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func mpSubscriptionStatus(s settings, tmdbID string, mediaType string, season int) (map[string]any, error) {
	baseURL := strings.TrimRight(strings.TrimSpace(s.MPUrl), "/")
	token := strings.TrimSpace(s.MPToken)
	tmdbID = strings.TrimSpace(tmdbID)
	if baseURL == "" || token == "" {
		return nil, fmt.Errorf("MP 地址或 Token 未配置")
	}
	if tmdbID == "" {
		return nil, fmt.Errorf("tmdbId 不能为空")
	}
	endpoint := fmt.Sprintf("%s/api/v1/subscribe/media/tmdb:%s?token=%s", baseURL, url.PathEscape(tmdbID), url.QueryEscape(token))
	if !strings.EqualFold(strings.TrimSpace(mediaType), "movie") && season > 0 {
		endpoint += fmt.Sprintf("&season=%d", season)
	}
	client := &http.Client{Timeout: 30 * time.Second}
	req, err := http.NewRequest(http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("MoviePilot 订阅状态请求失败：%w", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 8192))
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("MoviePilot 订阅状态失败 HTTP %d: %s", resp.StatusCode, shortBody(raw))
	}
	var decoded any
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &decoded); err != nil {
			return map[string]any{"exists": false, "raw": strings.TrimSpace(string(raw))}, nil
		}
	}
	return normalizeMPSubscriptionStatus(decoded), nil
}

func mpSubscribe(s settings, tmdbID string, mediaType string, season int, title string) (map[string]any, error) {
	baseURL := strings.TrimRight(strings.TrimSpace(s.MPUrl), "/")
	token := strings.TrimSpace(s.MPToken)
	tmdbID = strings.TrimSpace(tmdbID)
	mediaType = strings.TrimSpace(mediaType)
	if baseURL == "" || token == "" {
		return nil, fmt.Errorf("MP 地址或 Token 未配置")
	}
	if tmdbID == "" {
		return nil, fmt.Errorf("tmdbId 不能为空")
	}
	if mediaType == "" {
		mediaType = "tv"
	}
	tmdbNumber, _ := strconv.Atoi(tmdbID)
	if tmdbNumber <= 0 {
		return nil, fmt.Errorf("tmdbId 无效")
	}
	mediaType = strings.ToLower(mediaType)
	if mediaType != "movie" {
		mediaType = "tv"
	}
	cleanTitle := strings.TrimSpace(title)
	if cleanTitle == "" {
		cleanTitle = fmt.Sprintf("TMDB %d", tmdbNumber)
	}
	media := map[string]any{"media_type": mediaType, "tmdbId": tmdbNumber}
	payload := map[string]any{
		"notification_type": "MEDIA_APPROVED",
		"subject":           cleanTitle,
		"media":             media,
		"request":           map[string]any{"requestedBy_username": "emby-ecer"},
	}
	if mediaType != "movie" && season > 0 {
		payload["extra"] = []map[string]any{{"name": "Requested Seasons", "value": strconv.Itoa(season)}}
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	client := &http.Client{Timeout: 30 * time.Second}
	req, err := http.NewRequest(http.MethodPost, baseURL+"/api/v1/subscribe/seerr", bytes.NewReader(raw))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", token)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("MoviePilot 发送订阅失败：%w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 16384))
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("MoviePilot 发送订阅失败 HTTP %d: %s", resp.StatusCode, shortBody(body))
	}
	var result map[string]any
	if len(body) == 0 {
		return map[string]any{"ok": true}, nil
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return map[string]any{"ok": true, "raw": strings.TrimSpace(string(body))}, nil
	}
	return result, nil
}

func normalizeMPSubscriptionStatus(decoded any) map[string]any {
	result := map[string]any{"exists": false}
	data := mpExtractSubscriptionData(decoded)
	if data == nil {
		if decoded != nil {
			result["data"] = decoded
		}
		return result
	}
	result["data"] = data
	if value := firstNonEmpty(anyToString(data["id"]), anyToString(data["subscribe_id"]), anyToString(data["subscribeId"]), anyToString(data["media_id"]), anyToString(data["mediaId"])); value != "" {
		result["id"] = value
		result["exists"] = true
	}
	if value := firstNonEmpty(anyToString(data["name"]), anyToString(data["title"])); value != "" {
		result["title"] = value
	}
	if value := firstNonEmpty(anyToString(data["year"]), anyToString(data["release_year"])); value != "" {
		result["year"] = value
	}
	if value := firstNonEmpty(anyToString(data["type"]), anyToString(data["media_type"]), anyToString(data["mediaType"])); value != "" {
		result["mediaType"] = value
	}
	if seasonValue, ok := anyToInt(data["season"]); ok {
		result["season"] = seasonValue
	}
	if statusValue := firstNonEmpty(anyToString(data["status"]), anyToString(data["state"])); statusValue != "" {
		result["status"] = statusValue
	}
	if result["exists"] == false {
		if allNilMap(data) {
			return result
		}
		if len(data) > 0 {
			result["exists"] = true
		}
	}
	return result
}

func mpExtractSubscriptionData(decoded any) map[string]any {
	switch value := decoded.(type) {
	case map[string]any:
		if data, ok := value["data"]; ok {
			return mpExtractSubscriptionData(data)
		}
		return value
	case []any:
		for _, item := range value {
			if mapped := mpExtractSubscriptionData(item); mapped != nil {
				return mapped
			}
		}
	}
	return nil
}

func anyToInt(value any) (int, bool) {
	switch v := value.(type) {
	case int:
		return v, true
	case int64:
		return int(v), true
	case float64:
		return int(v), true
	case string:
		v = strings.TrimSpace(v)
		if v == "" {
			return 0, false
		}
		return parseInt(v), true
	case json.Number:
		i, err := v.Int64()
		if err == nil {
			return int(i), true
		}
	}
	return 0, false
}

func allNilMap(values map[string]any) bool {
	if len(values) == 0 {
		return true
	}
	for _, value := range values {
		if value == nil {
			continue
		}
		if text := anyToString(value); text != "" && text != "0" && !strings.EqualFold(text, "null") {
			return false
		}
	}
	return true
}

func mpDownload(s settings, rawData map[string]any, tmdbID string) error {
	timeout := time.Duration(getenvInt("MP_DOWNLOAD_TIMEOUT_SECONDS", 90)) * time.Second
	client := &http.Client{Timeout: timeout}
	mpURL := strings.TrimRight(strings.TrimSpace(s.MPUrl), "/")
	payload := map[string]any{"torrent_in": rawData}
	if tmdbID != "" {
		payload["tmdbid"], _ = strconv.Atoi(tmdbID)
	}
	raw, _ := json.Marshal(payload)
	req, _ := http.NewRequest(http.MethodPost, mpURL+"/api/v1/download/add", bytes.NewReader(raw))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", s.MPToken)
	resp, err := client.Do(req)
	if err != nil {
		if isTimeoutError(err) {
			return fmt.Errorf("MoviePilot 下载提交超时（%s）：MoviePilot 没有及时返回，请检查 %s 是否卡住或把 MP_DOWNLOAD_TIMEOUT_SECONDS 调大", timeout, mpURL)
		}
		return fmt.Errorf("MoviePilot 下载失败：%w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if resp.StatusCode >= 400 {
		return fmt.Errorf("MoviePilot 下载失败 HTTP %d: %s", resp.StatusCode, shortBody(body))
	}
	return nil
}
