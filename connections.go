package main

import (
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const connectionTimeout = 10 * time.Second

func testConnection(target string, s settings) map[string]any {
	targets := []string{target}
	if target == "all" {
		targets = []string{"emby", "tmdb", "mp"}
	}
	results := map[string]any{}
	for _, service := range targets {
		started := time.Now()
		result := probeConnection(service, s)
		result["checkedAt"] = time.Now().UTC().Format(time.RFC3339)
		result["latencyMs"] = time.Since(started).Milliseconds()
		results[service] = result
	}
	return results
}

func probeConnection(target string, s settings) map[string]any {
	fields := map[string][]string{
		"emby": {s.EmbyURL, s.EmbyAPIKey},
		"tmdb": {s.TMDBAPIKey},
		"mp":   {s.MPUrl, s.MPToken},
	}
	required, supported := fields[target]
	if !supported {
		return map[string]any{"ok": false, "configured": false, "error": "不支持的连接类型"}
	}
	for _, value := range required {
		if strings.TrimSpace(value) == "" {
			return map[string]any{"ok": false, "configured": false, "error": "请先填写服务连接配置"}
		}
	}
	var info map[string]any
	var err error
	switch target {
	case "emby":
		err = probeEmby(s, "/System/Info", nil, &info)
		if err == nil && firstString(info, "ServerName", "FriendlyName", "Version", "Id") == "" {
			err = errors.New("返回内容不是有效的 Emby 服务信息，请检查地址")
		}
	case "tmdb":
		err = requestJSON(http.MethodGet, buildBaseURL(tmdbBaseURL, "/configuration", map[string]string{"api_key": s.TMDBAPIKey}), nil, nil, &info, connectionTimeout)
		if err == nil && info == nil {
			err = errors.New("TMDB 返回了空响应")
		}
	case "mp":
		// MoviePilot's module list validates API_TOKEN through X-API-KEY and performs no search or download.
		err = requestJSON(http.MethodGet, buildBaseURL(s.MPUrl, "/api/v1/system/modulelist", nil), map[string]string{"X-API-KEY": s.MPToken, "Accept": "application/json"}, nil, &info, connectionTimeout)
		if err == nil && info["success"] != true {
			err = errors.New(firstNonEmpty(firstString(info, "message", "detail"), "返回内容不是有效的 MoviePilot 接口响应，请检查地址"))
		}
	}
	if err != nil {
		return map[string]any{"ok": false, "configured": true, "error": connectionError(err, s)}
	}
	result := map[string]any{"ok": true, "configured": true}
	if target == "emby" {
		result["name"] = firstString(info, "ServerName", "FriendlyName")
		if s.EmbyUserID != "" {
			var page embyItemsResp
			err := probeEmby(s, "/Users/"+url.PathEscape(s.EmbyUserID)+"/Items", map[string]string{"Limit": "1"}, &page)
			if err != nil {
				result["warning"] = "用户 ID 不可用，扫描将使用全局媒体库：" + connectionError(err, s)
			} else {
				result["userId"] = s.EmbyUserID
			}
		}
	}
	return result
}

func probeEmby(s settings, route string, query map[string]string, out any) error {
	if query == nil {
		query = map[string]string{}
	}
	query["api_key"] = s.EmbyAPIKey
	return requestJSON(http.MethodGet, buildBaseURL(s.EmbyURL, route, query), map[string]string{"X-Emby-Token": s.EmbyAPIKey, "Accept": "application/json"}, nil, out, connectionTimeout)
}

func connectionError(err error, s settings) string {
	var statusErr httpStatusError
	if errors.As(err, &statusErr) {
		if statusErr.Status == http.StatusUnauthorized || statusErr.Status == http.StatusForbidden {
			return "认证失败，请检查 API Key / Token"
		}
		if statusErr.Status == http.StatusNotFound {
			return "未找到服务接口，请检查服务地址和端口"
		}
		return fmt.Sprintf("服务返回 HTTP %d，请检查服务运行情况", statusErr.Status)
	}
	if isTimeoutError(err) {
		return "连接超时，请检查服务地址和网络"
	}
	message := err.Error()
	for _, secret := range []string{s.EmbyAPIKey, s.TMDBAPIKey, s.MPToken} {
		if secret != "" {
			message = strings.ReplaceAll(message, url.QueryEscape(secret), "[已隐藏]")
			message = strings.ReplaceAll(message, secret, "[已隐藏]")
		}
	}
	return message
}
