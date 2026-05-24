package main

import (
	"bytes"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"mime"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	defaultPanSouURL       = "https://so.252035.xyz"
	seriesScanCacheVersion = "complete-series-archive-v1"
)

var (
	appUsers        map[string]string
	appUsersMu      sync.RWMutex
	jwtSecret       []byte
	store           *settingsStore
	tmdbCache       *tmdbCacheStore
	seriesScanCache *seriesScanCacheStore
	activeScanMu    sync.Mutex
	activeScanJobID string
	httpCli         = &http.Client{Timeout: 45 * time.Second}

	pansouToken = struct {
		sync.Mutex
		Token     string
		ExpiresAt time.Time
	}{}

	jobMgr = newJobManager()
)

type jobStatus string

const (
	jobPending jobStatus = "pending"
	jobRunning jobStatus = "running"
	jobDone    jobStatus = "done"
	jobError   jobStatus = "error"
)

type job struct {
	ID        string    `json:"id"`
	Type      string    `json:"type"`
	Status    jobStatus `json:"status"`
	Progress  int       `json:"progress"`
	Message   string    `json:"message"`
	Current   string    `json:"current,omitempty"`
	Result    any       `json:"result,omitempty"`
	Error     string    `json:"error,omitempty"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

type jobManager struct {
	mu   sync.RWMutex
	jobs map[string]*job
}

func newJobManager() *jobManager {
	return &jobManager{jobs: map[string]*job{}}
}

func (m *jobManager) create(typ string) *job {
	m.mu.Lock()
	defer m.mu.Unlock()
	id := randomID(12)
	j := &job{ID: id, Type: typ, Status: jobPending, Progress: 0, CreatedAt: time.Now(), UpdatedAt: time.Now()}
	m.jobs[id] = j
	// 清理超过 1 小时的旧任务
	for k, v := range m.jobs {
		if v.Status != jobRunning && v.Status != jobPending && time.Since(v.UpdatedAt) > time.Hour {
			delete(m.jobs, k)
		}
	}
	return j
}

func (m *jobManager) get(id string) *job {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.jobs[id]
}

func (m *jobManager) update(id string, fn func(*job)) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if j, ok := m.jobs[id]; ok {
		fn(j)
		j.UpdatedAt = time.Now()
	}
}

func activateScanJob(id string) {
	activeScanMu.Lock()
	previous := activeScanJobID
	activeScanJobID = id
	activeScanMu.Unlock()

	if previous != "" && previous != id {
		jobMgr.update(previous, func(j *job) {
			if j.Status == jobRunning || j.Status == jobPending {
				j.Status = jobError
				j.Error = "已被新的扫描任务替换"
				j.Message = "已被新的扫描任务替换"
				j.Current = "已替换"
			}
		})
	}
}

func isActiveScanJob(id string) bool {
	activeScanMu.Lock()
	defer activeScanMu.Unlock()
	return activeScanJobID == id
}

func finishActiveScanJob(id string) {
	activeScanMu.Lock()
	if activeScanJobID == id {
		activeScanJobID = ""
	}
	activeScanMu.Unlock()
}

func currentActiveScanJobID() string {
	activeScanMu.Lock()
	defer activeScanMu.Unlock()
	return activeScanJobID
}

func randomID(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return fmt.Sprintf("%x", b)
}

type settings struct {
	EmbyURL         string `json:"embyUrl"`
	EmbyAPIKey      string `json:"embyApiKey"`
	EmbyUserID      string `json:"embyUserId"`
	TMDBAPIKey      string `json:"tmdbApiKey"`
	ScanConcurrency int    `json:"scanConcurrency"`
	PansouURL       string `json:"pansouUrl"`
	PansouUsername  string `json:"pansouUsername"`
	PansouPassword  string `json:"pansouPassword"`
	PansouToken     string `json:"pansouToken"`
	P115Cookie      string `json:"p115Cookie"`
	P115TargetCID   string `json:"p115TargetCid"`
	MPUrl           string `json:"mpUrl"`
	MPToken         string `json:"mpToken"`
}

type settingsStore struct {
	mu   sync.RWMutex
	path string
	data settings
}

type tmdbCacheEntry struct {
	ExpiresAt int64           `json:"expiresAt"`
	Payload   json.RawMessage `json:"payload"`
}

type tmdbCacheStore struct {
	mu   sync.RWMutex
	path string
	ttl  time.Duration
	data map[string]tmdbCacheEntry
}

type seriesScanCacheEntry struct {
	Fingerprint string           `json:"fingerprint"`
	Matched     bool             `json:"matched"`
	Missing     []missingEpisode `json:"missing,omitempty"`
	Unmatched   *unmatchedMedia  `json:"unmatched,omitempty"`
	Complete    bool             `json:"complete,omitempty"`
	UpdatedAt   int64            `json:"updatedAt"`
}

type seriesScanCacheStore struct {
	mu    sync.RWMutex
	path  string
	data  map[string]seriesScanCacheEntry
	dirty bool
}

func main() {
	configPath := getenv("CONFIG_PATH", filepath.Join("data", "config.json"))
	// 先从持久化文件加载账号密码，没有则用环境变量
	usersPath := filepath.Join(filepath.Dir(configPath), "users.json")
	appUsers = loadUsers(usersPath)
	if len(appUsers) == 0 {
		appUsers = parseUsers(getenv("APP_USERS", "admin:admin123"))
	}
	jwtSecret = []byte(os.Getenv("APP_JWT_SECRET"))
	if len(jwtSecret) == 0 {
		jwtSecret = make([]byte, 32)
		_, _ = rand.Read(jwtSecret)
	}

	store = newSettingsStore(configPath)
	tmdbCache = newTMDBCacheStore(getenv("TMDB_CACHE_PATH", filepath.Join(filepath.Dir(configPath), "tmdb-cache.json")), time.Duration(getenvInt("TMDB_CACHE_TTL_HOURS", 24))*time.Hour)
	seriesScanCache = newSeriesScanCacheStore(getenv("SERIES_SCAN_CACHE_PATH", filepath.Join(filepath.Dir(configPath), "series-scan-cache.json")))

	port := getenv("PORT", "3000")
	server := &http.Server{
		Addr:              ":" + port,
		Handler:           http.HandlerFunc(route),
		ReadHeaderTimeout: 10 * time.Second,
	}

	log.Printf("Emby 115 Helper (Go) running at http://localhost:%s", port)
	log.Printf("Default PanSou: %s", store.Get().PansouURL)
	if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatal(err)
	}
}

func route(w http.ResponseWriter, r *http.Request) {
	setCommonHeaders(w)
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return
	}

	defer func() {
		if recovered := recover(); recovered != nil {
			log.Printf("panic: %v", recovered)
			writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "服务器错误"})
		}
	}()

	path := r.URL.Path
	if path == "/api/health" {
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "name": "Emby 115 Missing Helper", "runtime": "go", "time": time.Now().Format(time.RFC3339)})
		return
	}

	if path == "/api/auth/login" && r.Method == http.MethodPost {
		handleLogin(w, r)
		return
	}

	if strings.HasPrefix(path, "/api/") {
		user, ok := requireAuth(w, r)
		if !ok {
			return
		}
		handleAPI(w, r, user)
		return
	}

	serveStatic(w, r)
}

func handleAPI(w http.ResponseWriter, r *http.Request, user string) {
	switch {
	case r.URL.Path == "/api/auth/verify" && r.Method == http.MethodPost:
		writeJSON(w, http.StatusOK, map[string]any{"valid": true, "username": user})

	case r.URL.Path == "/api/auth/logout" && r.Method == http.MethodPost:
		writeJSON(w, http.StatusOK, map[string]any{"message": "退出成功"})

	case r.URL.Path == "/api/auth/change-password" && r.Method == http.MethodPost:
		handleChangePassword(w, r, user)

	case r.URL.Path == "/api/settings" && r.Method == http.MethodGet:
		writeJSON(w, http.StatusOK, maskSettings(store.Get()))

	case r.URL.Path == "/api/settings" && r.Method == http.MethodPost:
		var body map[string]any
		if err := readJSON(r, &body); err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		next, err := store.Update(body)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		clearPanSouTokenCache()
		writeJSON(w, http.StatusOK, maskSettings(next))

	case r.URL.Path == "/api/settings/test" && r.Method == http.MethodPost:
		var body struct {
			Target string `json:"target"`
		}
		_ = readJSON(r, &body)
		if body.Target == "" {
			body.Target = "all"
		}
		writeJSON(w, http.StatusOK, testConnection(body.Target, store.Get()))

	case r.URL.Path == "/api/scan/last" && r.Method == http.MethodGet:
		result, err := loadScanResult()
		if err != nil {
			writeJSON(w, http.StatusOK, map[string]any{"scannedAt": "", "summary": map[string]any{}, "missing": []any{}, "unmatched": map[string]any{}})
			return
		}
		writeJSON(w, http.StatusOK, result)

	case r.URL.Path == "/api/search-results" && r.Method == http.MethodGet:
		results, err := loadSearchResults()
		if err != nil {
			writeJSON(w, http.StatusOK, []any{})
			return
		}
		writeJSON(w, http.StatusOK, results)

	case r.URL.Path == "/api/scan" && r.Method == http.MethodPost:
		var body struct {
			AiredOnly  bool   `json:"airedOnly"`
			MaxSeries  int    `json:"maxSeries"`
			RecentOnly bool   `json:"recentOnly"`
			ClearCache bool   `json:"clearCache"`
			SeriesID   string `json:"seriesId"`
		}
		body.AiredOnly = true
		_ = readJSON(r, &body)
		if body.ClearCache {
			clearLocalScanCaches()
		}
		result, err := scanLibrary(store.Get(), body.AiredOnly, body.MaxSeries, body.RecentOnly, lastScanTime(), strings.TrimSpace(body.SeriesID), nil)
		if err != nil {
			writeError(w, statusFromError(err), err)
			return
		}
		_ = saveScanResult(result)
		writeJSON(w, http.StatusOK, result)

	case r.URL.Path == "/api/search" && r.Method == http.MethodPost:
		var body struct {
			Keyword string `json:"keyword"`
		}
		if err := readJSON(r, &body); err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		result, err := searchKeyword(store.Get(), body.Keyword)
		if err != nil {
			writeError(w, statusFromError(err), err)
			return
		}
		writeJSON(w, http.StatusOK, result)

	case r.URL.Path == "/api/search-missing" && r.Method == http.MethodPost:
		var body searchMissingRequest
		if err := readJSON(r, &body); err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		missing := body.Missing
		if missing.Query == "" {
			missing = bodyToMissing(body.Raw)
		}
		result, err := searchMissingEpisode(store.Get(), missing)
		if err != nil {
			writeError(w, statusFromError(err), err)
			return
		}
		writeJSON(w, http.StatusOK, result)

	case r.URL.Path == "/api/115/transfer" && r.Method == http.MethodPost:
		var body transferRequest
		if err := readJSON(r, &body); err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		result, err := transfer115(store.Get(), body)
		if err != nil {
			writeError(w, statusFromError(err), err)
			return
		}
		writeJSON(w, http.StatusOK, result)

	case r.URL.Path == "/api/jobs" && r.Method == http.MethodPost:
		handleCreateJob(w, r)

	case r.URL.Path == "/api/jobs/active" && r.Method == http.MethodGet:
		handleGetActiveJob(w, r)

	case strings.HasPrefix(r.URL.Path, "/api/jobs/") && r.Method == http.MethodGet:
		handleGetJob(w, r)

	case r.URL.Path == "/api/mp/search" && r.Method == http.MethodPost:
		handleMPSearch(w, r)

	case r.URL.Path == "/api/mp/download" && r.Method == http.MethodPost:
		handleMPDownload(w, r)

	default:
		writeJSON(w, http.StatusNotFound, map[string]any{"error": "接口不存在"})
	}
}

func newSettingsStore(path string) *settingsStore {
	data := settingsFromEnv()
	if raw, err := os.ReadFile(path); err == nil && len(raw) > 0 {
		var saved settings
		if err := json.Unmarshal(raw, &saved); err == nil {
			data = saved
		}
	}
	if strings.TrimSpace(data.PansouURL) == "" {
		data.PansouURL = defaultPanSouURL
	}
	if strings.TrimSpace(data.P115TargetCID) == "" {
		data.P115TargetCID = "0"
	}
	data.ScanConcurrency = clampScanConcurrency(data.ScanConcurrency)
	return &settingsStore{path: path, data: data}
}

func settingsFromEnv() settings {
	return settings{
		EmbyURL:         os.Getenv("EMBY_URL"),
		EmbyAPIKey:      os.Getenv("EMBY_API_KEY"),
		EmbyUserID:      os.Getenv("EMBY_USER_ID"),
		TMDBAPIKey:      os.Getenv("TMDB_API_KEY"),
		ScanConcurrency: clampScanConcurrency(getenvInt("SCAN_CONCURRENCY", 4)),
		PansouURL:       getenv("PANSOU_URL", defaultPanSouURL),
		PansouUsername:  os.Getenv("PANSOU_USERNAME"),
		PansouPassword:  os.Getenv("PANSOU_PASSWORD"),
		PansouToken:     os.Getenv("PANSOU_TOKEN"),
		P115Cookie:      os.Getenv("P115_COOKIE"),
		P115TargetCID:   getenv("P115_TARGET_CID", "0"),
	}
}

func (s *settingsStore) Get() settings {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.data
}

func (s *settingsStore) Update(input map[string]any) (settings, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	next := s.data
	setPlain := func(field string, apply func(string)) {
		if raw, ok := input[field]; ok {
			apply(strings.TrimSpace(fmt.Sprint(raw)))
		}
	}
	setSecret := func(field string, apply func(string)) {
		if raw, ok := input[field]; ok {
			value := strings.TrimSpace(fmt.Sprint(raw))
			if value == "" {
				return
			}
			if value == "__clear__" {
				apply("")
				return
			}
			apply(value)
		}
	}

	setPlain("embyUrl", func(v string) { next.EmbyURL = strings.TrimRight(v, "/") })
	setSecret("embyApiKey", func(v string) { next.EmbyAPIKey = v })
	setPlain("embyUserId", func(v string) { next.EmbyUserID = v })
	setSecret("tmdbApiKey", func(v string) { next.TMDBAPIKey = v })
	setPlain("scanConcurrency", func(v string) {
		next.ScanConcurrency = clampScanConcurrency(parseInt(v))
	})
	setPlain("pansouUrl", func(v string) {
		if v == "" {
			v = defaultPanSouURL
		}
		next.PansouURL = strings.TrimRight(v, "/")
	})
	setPlain("pansouUsername", func(v string) { next.PansouUsername = v })
	setSecret("pansouPassword", func(v string) { next.PansouPassword = v })
	setSecret("pansouToken", func(v string) { next.PansouToken = v })
	setSecret("p115Cookie", func(v string) { next.P115Cookie = v })
	setPlain("p115TargetCid", func(v string) {
		if v == "" {
			v = "0"
		}
		next.P115TargetCID = v
	})
	setPlain("mpUrl", func(v string) { next.MPUrl = strings.TrimRight(v, "/") })
	setSecret("mpToken", func(v string) { next.MPToken = v })

	if next.PansouURL == "" {
		next.PansouURL = defaultPanSouURL
	}
	if next.P115TargetCID == "" {
		next.P115TargetCID = "0"
	}
	next.ScanConcurrency = clampScanConcurrency(next.ScanConcurrency)

	s.data = next
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return next, err
	}
	raw, _ := json.MarshalIndent(next, "", "  ")
	return next, os.WriteFile(s.path, raw, 0o600)
}

func maskSettings(s settings) map[string]any {
	return map[string]any{
		"embyUrl":         s.EmbyURL,
		"embyApiKey":      maskSecret(s.EmbyAPIKey, 4),
		"embyUserId":      s.EmbyUserID,
		"tmdbApiKey":      maskSecret(s.TMDBAPIKey, 4),
		"scanConcurrency": clampScanConcurrency(s.ScanConcurrency),
		"pansouUrl":       s.PansouURL,
		"pansouUsername":  s.PansouUsername,
		"pansouPassword":  maskSecret(s.PansouPassword, 4),
		"pansouToken":     maskSecret(s.PansouToken, 4),
		"p115Cookie":      maskSecret(s.P115Cookie, 12),
		"p115TargetCid":   fallback(s.P115TargetCID, "0"),
		"mpUrl":           s.MPUrl,
		"mpToken":         maskSecret(s.MPToken, 4),
		"ready": map[string]bool{
			"emby":   s.EmbyURL != "" && s.EmbyAPIKey != "",
			"tmdb":   s.TMDBAPIKey != "",
			"pansou": s.PansouURL != "",
			"p115":   s.P115Cookie != "",
			"mp":     s.MPUrl != "" && s.MPToken != "",
		},
	}
}

func maskSecret(value string, keep int) string {
	if value == "" {
		return ""
	}
	if len(value) <= keep*2 {
		return "••••••"
	}
	return value[:keep] + "••••" + value[len(value)-keep:]
}

func handleLogin(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := readJSON(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	body.Username = strings.TrimSpace(body.Username)
	appUsersMu.RLock()
	pwdOk := appUsers[body.Username] == body.Password
	appUsersMu.RUnlock()
	if body.Username == "" || !pwdOk {
		writeJSON(w, http.StatusUnauthorized, map[string]any{"error": "用户名或密码错误"})
		return
	}
	expires := time.Now().Add(time.Duration(getenvInt("APP_TOKEN_TTL_SECONDS", 86400)) * time.Second)
	token, err := signToken(body.Username, expires)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"token": token, "username": body.Username, "expires_at": expires.Unix()})
}

func handleChangePassword(w http.ResponseWriter, r *http.Request, username string) {
	var body struct {
		OldPassword string `json:"oldPassword"`
		NewPassword string `json:"newPassword"`
	}
	if err := readJSON(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	body.OldPassword = strings.TrimSpace(body.OldPassword)
	body.NewPassword = strings.TrimSpace(body.NewPassword)
	if body.OldPassword == "" || body.NewPassword == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "新旧密码不能为空"})
		return
	}
	if body.NewPassword == body.OldPassword {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "新密码不能与旧密码相同"})
		return
	}
	appUsersMu.Lock()
	defer appUsersMu.Unlock()
	if appUsers[username] != body.OldPassword {
		writeJSON(w, http.StatusForbidden, map[string]any{"error": "旧密码错误"})
		return
	}
	appUsers[username] = body.NewPassword
	// 持久化到文件
	configPath := getenv("CONFIG_PATH", filepath.Join("data", "config.json"))
	usersPath := filepath.Join(filepath.Dir(configPath), "users.json")
	_ = os.MkdirAll(filepath.Dir(usersPath), 0o755)
	raw, _ := json.MarshalIndent(appUsers, "", "  ")
	_ = os.WriteFile(usersPath, raw, 0o600)
	writeJSON(w, http.StatusOK, map[string]any{"message": "密码已修改"})
}

func signToken(username string, expires time.Time) (string, error) {
	header := map[string]string{"alg": "HS256", "typ": "JWT"}
	payload := map[string]any{"sub": username, "username": username, "iat": time.Now().Unix(), "exp": expires.Unix()}
	headerRaw, err := json.Marshal(header)
	if err != nil {
		return "", err
	}
	payloadRaw, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	head := base64.RawURLEncoding.EncodeToString(headerRaw)
	body := base64.RawURLEncoding.EncodeToString(payloadRaw)
	sig := hmacSHA256(head + "." + body)
	return head + "." + body + "." + sig, nil
}

func verifyToken(token string) (string, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return "", errors.New("令牌格式错误")
	}
	expected := hmacSHA256(parts[0] + "." + parts[1])
	if len(parts[2]) != len(expected) || !hmac.Equal([]byte(parts[2]), []byte(expected)) {
		return "", errors.New("令牌签名无效")
	}
	raw, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return "", errors.New("令牌内容错误")
	}
	var payload struct {
		Sub      string `json:"sub"`
		Username string `json:"username"`
		Exp      int64  `json:"exp"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		return "", errors.New("令牌内容错误")
	}
	if payload.Exp < time.Now().Unix() {
		return "", errors.New("令牌已过期")
	}
	if payload.Username != "" {
		return payload.Username, nil
	}
	return payload.Sub, nil
}

func hmacSHA256(value string) string {
	mac := hmac.New(sha256.New, jwtSecret)
	mac.Write([]byte(value))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

func requireAuth(w http.ResponseWriter, r *http.Request) (string, bool) {
	header := r.Header.Get("Authorization")
	if !strings.HasPrefix(header, "Bearer ") {
		writeJSON(w, http.StatusUnauthorized, map[string]any{"error": "未授权：缺少认证令牌"})
		return "", false
	}
	user, err := verifyToken(strings.TrimPrefix(header, "Bearer "))
	if err != nil {
		writeJSON(w, http.StatusUnauthorized, map[string]any{"error": "未授权：" + err.Error()})
		return "", false
	}
	return user, true
}

type apiError struct {
	Status int
	Err    error
}

func (e apiError) Error() string { return e.Err.Error() }

func badRequest(msg string) error {
	return apiError{Status: http.StatusBadRequest, Err: errors.New(msg)}
}

func statusFromError(err error) int {
	var apiErr apiError
	if errors.As(err, &apiErr) {
		return apiErr.Status
	}
	return http.StatusInternalServerError
}

func requireFields(s settings, fields ...string) error {
	missing := make([]string, 0)
	for _, field := range fields {
		switch field {
		case "embyUrl":
			if s.EmbyURL == "" {
				missing = append(missing, field)
			}
		case "embyApiKey":
			if s.EmbyAPIKey == "" {
				missing = append(missing, field)
			}
		case "tmdbApiKey":
			if s.TMDBAPIKey == "" {
				missing = append(missing, field)
			}
		case "pansouUrl":
			if s.PansouURL == "" {
				missing = append(missing, field)
			}
		case "p115Cookie":
			if s.P115Cookie == "" {
				missing = append(missing, field)
			}
		}
	}
	if len(missing) > 0 {
		return badRequest("缺少配置：" + strings.Join(missing, ", "))
	}
	return nil
}

func testConnection(target string, s settings) map[string]any {
	targets := []string{target}
	if target == "all" {
		targets = []string{"emby", "tmdb", "pansou", "p115"}
	}
	result := map[string]any{}
	for _, item := range targets {
		switch item {
		case "emby":
			if err := requireFields(s, "embyUrl", "embyApiKey"); err != nil {
				result[item] = failResult(err)
				continue
			}
			var info map[string]any
			err := embyGet(s, "/System/Info", nil, &info)
			if err != nil {
				result[item] = failResult(err)
			} else {
				result[item] = map[string]any{"ok": true, "name": firstString(info, "ServerName", "FriendlyName", "LocalAddress")}
			}
		case "tmdb":
			if err := requireFields(s, "tmdbApiKey"); err != nil {
				result[item] = failResult(err)
				continue
			}
			var info map[string]any
			err := tmdbGet(s, "/configuration", nil, &info)
			if err != nil {
				result[item] = failResult(err)
			} else {
				result[item] = map[string]any{"ok": true}
			}
		case "pansou":
			if err := requireFields(s, "pansouUrl"); err != nil {
				result[item] = failResult(err)
				continue
			}
			var info map[string]any
			err := requestJSON(http.MethodGet, buildBaseURL(s.PansouURL, "/api/health", nil), nil, nil, &info, 20*time.Second)
			if err != nil {
				result[item] = failResult(err)
			} else {
				result[item] = map[string]any{"ok": true, "auth_enabled": info["auth_enabled"], "plugins": info["plugin_count"]}
			}
		case "p115":
			if err := requireFields(s, "p115Cookie"); err != nil {
				result[item] = failResult(err)
				continue
			}
			var info map[string]any
			err := requestJSON(http.MethodGet, "https://webapi.115.com/files/index_info", headers115(s.P115Cookie, false), nil, &info, 20*time.Second)
			if err != nil {
				result[item] = failResult(err)
				continue
			}
			if state, _ := info["state"].(bool); !state {
				result[item] = failResult(errors.New(getErrorMessage(info, "Cookie 无效")))
			} else {
				result[item] = map[string]any{"ok": true, "name": "115 用户"}
			}
		}
	}
	return result
}

func failResult(err error) map[string]any {
	return map[string]any{"ok": false, "error": err.Error()}
}

type embyItemsResp struct {
	Items []embyItem `json:"Items"`
}

type embyItem struct {
	ID                 string            `json:"Id"`
	Name               string            `json:"Name"`
	Type               string            `json:"Type"`
	OriginalTitle      string            `json:"OriginalTitle"`
	ProductionYear     int               `json:"ProductionYear"`
	PremiereDate       string            `json:"PremiereDate"`
	DateLastSaved      string            `json:"DateLastSaved"`
	DateLastMediaAdded string            `json:"DateLastMediaAdded"`
	RecursiveItemCount int               `json:"RecursiveItemCount"`
	ProviderIDs        map[string]string `json:"ProviderIds"`
}

type embyEpisodesResp struct {
	Items []embyEpisode `json:"Items"`
}

type embyEpisode struct {
	ID                string            `json:"Id"`
	SeriesID          string            `json:"SeriesId"`
	Name              string            `json:"Name"`
	ParentIndexNumber int               `json:"ParentIndexNumber"`
	IndexNumber       int               `json:"IndexNumber"`
	ProviderIDs       map[string]string `json:"ProviderIds"`
	LocationType      string            `json:"LocationType"`
	IsMissing         bool              `json:"IsMissing"`
	Path              string            `json:"Path"`
	MediaSources      []any             `json:"MediaSources"`
}

type tmdbSearchItem struct {
	ID            int    `json:"id"`
	Name          string `json:"name"`
	OriginalName  string `json:"original_name"`
	Title         string `json:"title"`
	OriginalTitle string `json:"original_title"`
	FirstAirDate  string `json:"first_air_date"`
	ReleaseDate   string `json:"release_date"`
}

type tmdbSearchResp struct {
	Results []tmdbSearchItem `json:"results"`
}

type tmdbFindResp struct {
	TVResults    []tmdbSearchItem `json:"tv_results"`
	MovieResults []tmdbSearchItem `json:"movie_results"`
}

type tmdbTVDetail struct {
	ID           int          `json:"id"`
	Name         string       `json:"name"`
	OriginalName string       `json:"original_name"`
	FirstAirDate string       `json:"first_air_date"`
	PosterPath   string       `json:"poster_path"`
	Seasons      []tmdbSeason `json:"seasons"`
}

type tmdbSeason struct {
	SeasonNumber int    `json:"season_number"`
	EpisodeCount int    `json:"episode_count"`
	PosterPath   string `json:"poster_path"`
}

type tmdbSeasonDetail struct {
	Episodes []tmdbEpisodeDetail `json:"episodes"`
}

type tmdbEpisodeDetail struct {
	ID            int    `json:"id"`
	EpisodeNumber int    `json:"episode_number"`
	Name          string `json:"name"`
	AirDate       string `json:"air_date"`
	Overview      string `json:"overview"`
}

type missingEpisode struct {
	ID            string `json:"id"`
	MediaType     string `json:"mediaType"`
	EmbySeriesID  string `json:"embySeriesId"`
	EmbyTitle     string `json:"embyTitle"`
	TMDBID        int    `json:"tmdbId"`
	TMDBEpisodeID int    `json:"tmdbEpisodeId"`
	TMDBMatchName string `json:"tmdbMatchName"`
	TMDBMatchYear string `json:"tmdbMatchYear"`
	OfficialTitle string `json:"officialTitle"`
	OriginalTitle string `json:"originalTitle"`
	Season        int    `json:"season"`
	Episode       int    `json:"episode"`
	Code          string `json:"code"`
	CompareKey    string `json:"compareKey"`
	CompareReason string `json:"compareReason"`
	EpisodeName   string `json:"episodeName"`
	AirDate       string `json:"airDate"`
	Query         string `json:"query"`
	PosterPath    string `json:"posterPath"`
	Overview      string `json:"overview"`
	TMDBURL       string `json:"tmdbUrl"`
	TotalEpisodes int    `json:"totalEpisodes"`
	OwnedEpisodes int    `json:"ownedEpisodes"`
}

type unmatchedMedia struct {
	ID          string            `json:"id"`
	Name        string            `json:"name"`
	Year        int               `json:"year,omitempty"`
	Type        string            `json:"type"`
	ProviderIDs map[string]string `json:"providerIds"`
	Reason      string            `json:"reason"`
}

type scanDiagnosticEntry struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Action string `json:"action"`
	Reason string `json:"reason"`
}

type scanCompareEntry struct {
	ID              string `json:"id"`
	Name            string `json:"name"`
	TMDBID          int    `json:"tmdbId"`
	TMDBName        string `json:"tmdbName"`
	TMDBYear        string `json:"tmdbYear"`
	EmbyEpisodes    int    `json:"embyEpisodes"`
	EmbySeasonCount int    `json:"embySeasonCount"`
	TMDBEpisodes    int    `json:"tmdbEpisodes"`
	OwnedEpisodes   int    `json:"ownedEpisodes"`
	MissingEpisodes int    `json:"missingEpisodes"`
	Reason          string `json:"reason"`
}

func scanLibrary(s settings, airedOnly bool, maxSeries int, recentOnly bool, changedSince time.Time, onlySeriesID string, onProgress func(processed, total int, message, current string, snapshot map[string]any)) (map[string]any, error) {
	if err := requireFields(s, "embyUrl", "embyApiKey", "tmdbApiKey"); err != nil {
		return nil, err
	}

	itemRoute := embyItemsRoute(s)
	seriesItems := make([]embyItem, 0)
	movieItems := make([]embyItem, 0)
	seriesTotal := 0
	movieTotal := 0
	onlySeriesID = strings.TrimSpace(onlySeriesID)
	fullSeriesScan := maxSeries <= 0 && onlySeriesID == ""

	itemStart := 0
	itemPageLimit := 1000
	for {
		var page embyItemsResp
		if err := embyGet(s, itemRoute, map[string]string{
			"Recursive":        "true",
			"IncludeItemTypes": "Series,Movie",
			"Fields":           "ProviderIds,SortName,OriginalTitle,PremiereDate,ProductionYear,DateLastSaved,DateLastMediaAdded,RecursiveItemCount,Path",
			"SortBy":           "SortName",
			"StartIndex":       strconv.Itoa(itemStart),
			"Limit":            strconv.Itoa(itemPageLimit),
		}, &page); err != nil {
			return nil, err
		}

		for _, item := range page.Items {
			switch item.Type {
			case "Series":
				seriesTotal++
				if onlySeriesID != "" && item.ID != onlySeriesID {
					continue
				}
				seriesItems = append(seriesItems, item)
			case "Movie":
				movieTotal++
				movieItems = append(movieItems, item)
			}
		}

		if len(page.Items) < itemPageLimit {
			break
		}
		itemStart += itemPageLimit
	}
	if maxSeries > 0 && maxSeries < len(seriesItems) {
		fullSeriesScan = false
	}
	if maxSeries > 0 && maxSeries < len(seriesItems) {
		seriesItems = seriesItems[:maxSeries]
	}
	defer func() {
		if seriesScanCache != nil {
			_ = seriesScanCache.Flush()
		}
	}()

	var mu sync.Mutex
	missing := make([]missingEpisode, 0)
	unmatchedSeries := make([]unmatchedMedia, 0)
	unmatchedMovies := make([]unmatchedMedia, 0)
	matchedSeries := 0
	matchedMovies := 0
	cachedSeries := 0
	rescannedSeries := 0
	skippedSeries := make([]scanDiagnosticEntry, 0)
	comparedSeries := make([]scanCompareEntry, 0)
	totalWork := len(seriesItems)*3 + len(movieItems)
	workDone := 0
	currentSeriesIDs := map[string]bool{}
	if fullSeriesScan {
		for _, series := range seriesItems {
			currentSeriesIDs[series.ID] = true
		}
	}
	if totalWork <= 0 {
		totalWork = 1
	}
	buildSnapshot := func() (int, int, map[string]any) {
		mu.Lock()
		defer mu.Unlock()
		missingCopy := append([]missingEpisode(nil), missing...)
		seriesCopy := append([]unmatchedMedia(nil), unmatchedSeries...)
		movieCopy := append([]unmatchedMedia(nil), unmatchedMovies...)
		skippedCopy := append([]scanDiagnosticEntry(nil), skippedSeries...)
		comparedCopy := append([]scanCompareEntry(nil), comparedSeries...)
		sortMissingEpisodes(missingCopy)
		return workDone, totalWork, map[string]any{
			"scannedAt": time.Now().Format(time.RFC3339),
			"summary": map[string]any{
				"seriesTotal":          seriesTotal,
				"seriesScanned":        len(seriesItems),
				"seriesCached":         cachedSeries,
				"seriesRescanned":      rescannedSeries,
				"scanMode":             scanModeLabel(recentOnly),
				"movieTotal":           movieTotal,
				"matchedSeries":        matchedSeries,
				"unmatchedSeries":      len(unmatchedSeries),
				"matchedMovies":        matchedMovies,
				"unmatchedMovies":      len(unmatchedMovies),
				"totalMissingEpisodes": len(missing),
				"airedOnly":            airedOnly,
			},
			"diagnostics": map[string]any{
				"cacheHits":       cachedSeries,
				"rescannedSeries": rescannedSeries,
				"unmatchedSeries": len(unmatchedSeries),
				"comparedCount":   len(comparedSeries),
				"skippedCount":    len(skippedSeries),
				"skipped":         limitScanDiagnostics(skippedCopy, 120),
				"compared":        limitCompareDiagnostics(comparedCopy, 500),
			},
			"missing": missingCopy,
			"unmatched": map[string]any{
				"series": limitUnmatched(seriesCopy, 80),
				"movies": limitUnmatched(movieCopy, 80),
			},
		}
	}
	adjustTotal := func(delta int) {
		if delta <= 0 {
			return
		}
		mu.Lock()
		totalWork += delta
		mu.Unlock()
	}
	advanceProgress := func(delta int, message, current string) {
		if delta > 0 {
			mu.Lock()
			workDone += delta
			mu.Unlock()
		}
		if onProgress == nil {
			return
		}
		processed, total, snapshot := buildSnapshot()
		onProgress(processed, total, message, current, snapshot)
	}
	advanceProgress(0, "开始扫描媒体库...", "初始化")
	addSkipped := func(series embyItem, action, reason string) {
		mu.Lock()
		skippedSeries = append(skippedSeries, scanDiagnosticEntry{ID: series.ID, Name: series.Name, Action: action, Reason: reason})
		mu.Unlock()
	}
	addCompared := func(entry scanCompareEntry) {
		mu.Lock()
		comparedSeries = append(comparedSeries, entry)
		mu.Unlock()
	}
	seriesWorkers := clampScanConcurrency(s.ScanConcurrency)
	movieWorkers := seriesWorkers
	if movieWorkers > 2 {
		movieWorkers = maxInt(2, seriesWorkers/2)
	}

	parallelFor(seriesItems, seriesWorkers, func(series embyItem) {
		title := fallback(series.Name, "未知剧集")
		forceRescanSeries := onlySeriesID != ""
		if !forceRescanSeries {
			if entry, ok := seriesScanCache.Get(series.ID); ok && entry.Complete {
				mu.Lock()
				matchedSeries++
				cachedSeries++
				mu.Unlock()
				addSkipped(series, "complete-archive", "上次完整扫描确认一个都不缺，跳过本次扫描")
				advanceProgress(3, fmt.Sprintf("《%s》在完整存档中，跳过扫描", title), title)
				return
			}
		}
		mu.Lock()
		rescannedSeries++
		mu.Unlock()
		resolved, err := resolveTmdbTV(s, series)
		if err != nil || resolved == 0 {
			unmatched := simpleMedia(series, "找不到 TMDB 剧集 ID")
			mu.Lock()
			unmatchedSeries = append(unmatchedSeries, unmatched)
			mu.Unlock()
			seriesScanCache.Delete(series.ID)
			advanceProgress(3, fmt.Sprintf("扫描《%s》时未找到 TMDB 剧集 ID", title), title)
			return
		}
		advanceProgress(1, fmt.Sprintf("已匹配《%s》的 TMDB 信息", title), title)

		owned := map[string]bool{}
		ownedTMDBEpisodes := map[int]bool{}
		embySeasons := map[int]bool{}

		seriesEpisodes, err := loadSeriesEpisodes(s, series.ID, func(page, count int) {
			adjustTotal(1)
			advanceProgress(1, fmt.Sprintf("正在读取《%s》的 Emby 剧集", title), title)
		})
		if err != nil {
			unmatched := simpleMedia(series, "读取 Emby 单剧集数失败："+err.Error())
			mu.Lock()
			unmatchedSeries = append(unmatchedSeries, unmatched)
			mu.Unlock()
			seriesScanCache.Delete(series.ID)
			advanceProgress(2, fmt.Sprintf("读取《%s》剧集失败", title), title)
			return
		}
		advanceProgress(1, fmt.Sprintf("已读取《%s》的 Emby 集数", title), title)

		for _, ep := range seriesEpisodes {
			if ep.ParentIndexNumber > 0 && ep.IndexNumber > 0 {
				owned[fmt.Sprintf("%d:%d", ep.ParentIndexNumber, ep.IndexNumber)] = true
			}
			if ep.ParentIndexNumber > 0 {
				embySeasons[ep.ParentIndexNumber] = true
			}
			if id := parseInt(providerID(ep.ProviderIDs, "tmdb")); id > 0 {
				ownedTMDBEpisodes[id] = true
			}
		}

		var tv tmdbTVDetail
		if err := tmdbGet(s, fmt.Sprintf("/tv/%d", resolved), map[string]string{"language": "zh-CN"}, &tv); err != nil {
			mu.Lock()
			unmatchedSeries = append(unmatchedSeries, simpleMedia(series, "读取 TMDB 剧集失败："+err.Error()))
			mu.Unlock()
			advanceProgress(1, fmt.Sprintf("读取《%s》TMDB 剧集详情失败", title), title)
			return
		}
		advanceProgress(1, fmt.Sprintf("开始比对《%s》的季集信息", title), title)
		officialTitle := fallback(tv.Name, series.Name)
		originalTitle := fallback(tv.OriginalName, fallback(series.OriginalTitle, series.Name))
		localMissing := make([]missingEpisode, 0)
		totalTMDBCount := 0
		ownedCount := 0
		cacheable := true
		seasonWork := 0
		for _, season := range tv.Seasons {
			if season.SeasonNumber > 0 && season.EpisodeCount > 0 {
				seasonWork++
			}
		}
		adjustTotal(seasonWork)
		for _, season := range tv.Seasons {
			if season.SeasonNumber <= 0 || season.EpisodeCount <= 0 {
				continue
			}
			var seasonDetail tmdbSeasonDetail
			if err := tmdbGet(s, fmt.Sprintf("/tv/%d/season/%d", resolved, season.SeasonNumber), map[string]string{"language": "zh-CN"}, &seasonDetail); err != nil {
				cacheable = false
				advanceProgress(1, fmt.Sprintf("《%s》第 %d 季读取失败，跳过", officialTitle, season.SeasonNumber), fmt.Sprintf("%s / 第%d季", officialTitle, season.SeasonNumber))
				continue
			}
			// 跳过“幽灵季”：Emby 中没有该季，且 TMDB 该季也没有任何已播出集数
			if airedOnly && !embySeasons[season.SeasonNumber] {
				hasAired := false
				for _, ep := range seasonDetail.Episodes {
					if ep.AirDate != "" && ep.AirDate <= time.Now().Format("2006-01-02") {
						hasAired = true
						break
					}
				}
				if !hasAired {
					continue
				}
			}
			for _, ep := range seasonDetail.Episodes {
				if ep.EpisodeNumber <= 0 {
					continue
				}
				if airedOnly && ep.AirDate != "" && ep.AirDate > time.Now().Format("2006-01-02") {
					continue
				}
				totalTMDBCount++
				key := fmt.Sprintf("%d:%d", season.SeasonNumber, ep.EpisodeNumber)
				if owned[key] || (ep.ID > 0 && ownedTMDBEpisodes[ep.ID]) {
					ownedCount++
					continue
				}
				code := fmt.Sprintf("S%02dE%02d", season.SeasonNumber, ep.EpisodeNumber)
				compareReason := "Emby 中未找到相同季集号"
				if ep.ID > 0 {
					compareReason = "Emby 中未找到相同季集号，也未找到相同 TMDB 集 ID"
				}
				localMissing = append(localMissing, missingEpisode{
					ID:            fmt.Sprintf("%d-%d-%d", resolved, season.SeasonNumber, ep.EpisodeNumber),
					MediaType:     "episode",
					EmbySeriesID:  series.ID,
					EmbyTitle:     series.Name,
					TMDBID:        resolved,
					TMDBEpisodeID: ep.ID,
					TMDBMatchName: officialTitle,
					TMDBMatchYear: firstYear(tv.FirstAirDate),
					OfficialTitle: officialTitle,
					OriginalTitle: originalTitle,
					Season:        season.SeasonNumber,
					Episode:       ep.EpisodeNumber,
					Code:          code,
					CompareKey:    fmt.Sprintf("季集号 %d:%d / TMDB集ID %d", season.SeasonNumber, ep.EpisodeNumber, ep.ID),
					CompareReason: compareReason,
					EpisodeName:   ep.Name,
					AirDate:       ep.AirDate,
					Query:         officialTitle + " " + code,
					PosterPath:    fallback(tv.PosterPath, season.PosterPath),
					Overview:      ep.Overview,
					TMDBURL:       fmt.Sprintf("https://www.themoviedb.org/tv/%d/season/%d/episode/%d", resolved, season.SeasonNumber, ep.EpisodeNumber),
				})
			}
			advanceProgress(1, fmt.Sprintf("已比对《%s》第 %d 季", officialTitle, season.SeasonNumber), fmt.Sprintf("%s / 第%d季", officialTitle, season.SeasonNumber))
		}

		// 填入每集的总数和拥有数
		for i := range localMissing {
			localMissing[i].TotalEpisodes = totalTMDBCount
			localMissing[i].OwnedEpisodes = ownedCount
		}
		compareReason := "已匹配并完成比对，未发现缺集"
		if len(localMissing) > 0 {
			compareReason = fmt.Sprintf("已匹配并发现 %d 集缺失", len(localMissing))
		} else if totalTMDBCount == 0 {
			compareReason = "TMDB 没有可比对的已播出集数"
		} else if len(seriesEpisodes) == 0 {
			compareReason = "Emby 没有读取到实际已拥有剧集，且未发现可加入的缺集"
		} else if ownedCount >= totalTMDBCount {
			compareReason = "Emby 读取到的季集号/TMDB 集 ID 已覆盖 TMDB 已播出集数"
		}
		addCompared(scanCompareEntry{
			ID:              series.ID,
			Name:            series.Name,
			TMDBID:          resolved,
			TMDBName:        officialTitle,
			TMDBYear:        firstYear(tv.FirstAirDate),
			EmbyEpisodes:    len(seriesEpisodes),
			EmbySeasonCount: len(embySeasons),
			TMDBEpisodes:    totalTMDBCount,
			OwnedEpisodes:   ownedCount,
			MissingEpisodes: len(localMissing),
			Reason:          compareReason,
		})

		mu.Lock()
		matchedSeries++
		missing = append(missing, localMissing...)
		mu.Unlock()
		if cacheable && len(localMissing) == 0 {
			seriesScanCache.Set(series.ID, seriesScanCacheEntry{Matched: true, Complete: true, UpdatedAt: time.Now().Unix()})
		} else {
			seriesScanCache.Delete(series.ID)
		}
	})

	parallelFor(movieItems, movieWorkers, func(movie embyItem) {
		title := fallback(movie.Name, "未知电影")
		resolved, err := resolveTmdbMovie(s, movie)
		mu.Lock()
		if err != nil || resolved == 0 {
			unmatchedMovies = append(unmatchedMovies, simpleMedia(movie, "找不到 TMDB 电影 ID"))
			mu.Unlock()
			advanceProgress(1, fmt.Sprintf("扫描电影《%s》时未找到 TMDB 电影 ID", title), title)
			return
		}
		matchedMovies++
		mu.Unlock()
		advanceProgress(1, fmt.Sprintf("已完成电影《%s》比对", title), title)
	})
	if fullSeriesScan {
		seriesScanCache.Prune(currentSeriesIDs)
	}
	_, _, result := buildSnapshot()
	return result, nil
}

func resolveTmdbTV(s settings, series embyItem) (int, error) {
	// 1. Validate direct TMDB ID from Emby
	if id := parseInt(providerID(series.ProviderIDs, "tmdb")); id > 0 {
		return id, nil
	}
	// 2. TVDB -> TMDB (also validate)
	if tvdb := providerID(series.ProviderIDs, "tvdb"); tvdb != "" {
		if id, err := tmdbFindExternal(s, tvdb, "tvdb_id", true); err == nil && id > 0 {
			return id, nil
		}
	}
	// 3. IMDb -> TMDB (also validate)
	if imdb := providerID(series.ProviderIDs, "imdb"); imdb != "" {
		if id, err := tmdbFindExternal(s, imdb, "imdb_id", true); err == nil && id > 0 {
			return id, nil
		}
	}
	// 4. Title search (strict: title + year must match)
	for _, keyword := range tvSearchQueries(series) {
		for _, withYear := range []bool{true, false} {
			query := map[string]string{"language": "zh-CN", "query": keyword}
			if withYear && effectiveYear(series) > 0 {
				query["first_air_date_year"] = strconv.Itoa(effectiveYear(series))
			}
			var resp tmdbSearchResp
			if err := tmdbGet(s, "/search/tv", query, &resp); err != nil {
				return 0, err
			}
			if best := bestTMDBTVMatch(series, resp.Results); best != nil {
				return best.ID, nil
			}
			if effectiveYear(series) <= 0 {
				break
			}
		}
	}
	return 0, nil
}

func resolveTmdbMovie(s settings, movie embyItem) (int, error) {
	if id := parseInt(providerID(movie.ProviderIDs, "tmdb")); id > 0 {
		var m struct {
			Title         string `json:"title"`
			OriginalTitle string `json:"original_title"`
			ReleaseDate   string `json:"release_date"`
		}
		if err := tmdbGet(s, fmt.Sprintf("/movie/%d", id), map[string]string{"language": "zh-CN"}, &m); err == nil {
			if verifyTMDBMatch(movie, m.Title, m.OriginalTitle, m.ReleaseDate) {
				return id, nil
			}
		}
	}
	if imdb := providerID(movie.ProviderIDs, "imdb"); imdb != "" {
		if id, err := tmdbFindExternal(s, imdb, "imdb_id", false); err == nil && id > 0 {
			var m struct {
				Title         string `json:"title"`
				OriginalTitle string `json:"original_title"`
				ReleaseDate   string `json:"release_date"`
			}
			if err := tmdbGet(s, fmt.Sprintf("/movie/%d", id), map[string]string{"language": "zh-CN"}, &m); err == nil {
				if verifyTMDBMatch(movie, m.Title, m.OriginalTitle, m.ReleaseDate) {
					return id, nil
				}
			}
		}
	}
	query := map[string]string{"language": "zh-CN", "query": movie.Name}
	if effectiveYear(movie) > 0 {
		query["year"] = strconv.Itoa(effectiveYear(movie))
	}
	var resp tmdbSearchResp
	if err := tmdbGet(s, "/search/movie", query, &resp); err != nil {
		return 0, err
	}
	if best := bestTMDBMovieMatch(movie, resp.Results); best != nil {
		return best.ID, nil
	}
	return 0, nil
}

func tmdbFindExternal(s settings, externalID, source string, tv bool) (int, error) {
	var resp tmdbFindResp
	if err := tmdbGet(s, "/find/"+url.PathEscape(externalID), map[string]string{"external_source": source, "language": "zh-CN"}, &resp); err != nil {
		return 0, err
	}
	if tv && len(resp.TVResults) > 0 {
		return resp.TVResults[0].ID, nil
	}
	if !tv && len(resp.MovieResults) > 0 {
		return resp.MovieResults[0].ID, nil
	}
	return 0, nil
}

type searchMissingRequest struct {
	Missing missingEpisode         `json:"missing"`
	Raw     map[string]interface{} `json:"-"`
}

func (r *searchMissingRequest) UnmarshalJSON(data []byte) error {
	type alias searchMissingRequest
	var a alias
	_ = json.Unmarshal(data, &a)
	var raw map[string]interface{}
	_ = json.Unmarshal(data, &raw)
	*r = searchMissingRequest(a)
	r.Raw = raw
	return nil
}

type pansouSearchResp struct {
	Total        int                         `json:"total"`
	MergedByType map[string][]pansouLinkItem `json:"merged_by_type"`
	Results      []pansouResult              `json:"results"`
}

// PanSou 统一响应信封：{code, message, data: {...}}
type pansouEnvelope struct {
	Code    int              `json:"code"`
	Message string           `json:"message"`
	Data    pansouSearchResp `json:"data"`
}

func unmarshalPansouResp(raw []byte, resp *pansouSearchResp) error {
	// 先尝试带 data 信封的格式
	var env pansouEnvelope
	if err := json.Unmarshal(raw, &env); err == nil && env.Data.Total > 0 {
		*resp = env.Data
		return nil
	}
	// 兜底：直接解析到顶层字段（兼容旧版/其他部署）
	return json.Unmarshal(raw, resp)
}

type pansouLinkItem struct {
	Type      string   `json:"type"`
	URL       string   `json:"url"`
	Password  string   `json:"password"`
	Note      string   `json:"note"`
	Datetime  string   `json:"datetime"`
	Source    string   `json:"source"`
	Images    []string `json:"images"`
	WorkTitle string   `json:"work_title"`
	Title     string   `json:"title"`
}

type pansouResult struct {
	Title    string           `json:"title"`
	Channel  string           `json:"channel"`
	Datetime string           `json:"datetime"`
	Images   []string         `json:"images"`
	Links    []pansouLinkItem `json:"links"`
}

type normalizedResult struct {
	Title    string   `json:"title"`
	URL      string   `json:"url"`
	Password string   `json:"password"`
	Source   string   `json:"source"`
	Datetime string   `json:"datetime"`
	Images   []string `json:"images"`
	Query    string   `json:"query"`
}

func searchKeyword(s settings, keyword string) (map[string]any, error) {
	if err := requireFields(s, "pansouUrl"); err != nil {
		return nil, err
	}
	keyword = strings.TrimSpace(keyword)
	if keyword == "" {
		return nil, badRequest("缺少搜索关键词")
	}
	payload := map[string]any{
		"kw":          keyword,
		"res":         "merge",
		"src":         "all",
		"cloud_types": []string{"115"},
		"filter": map[string]any{
			"include": []string{},
			"exclude": []string{},
		},
	}
	headers, err := pansouAuthHeaders(s)
	if err != nil {
		return nil, err
	}
	headers["Content-Type"] = "application/json"
	endpoint := buildBaseURL(s.PansouURL, "/api/search", nil)
	var raw json.RawMessage
	if err := requestJSON(http.MethodPost, endpoint, headers, payload, &raw, 8*time.Second); err != nil {
		return nil, err
	}
	var resp pansouSearchResp
	if err := unmarshalPansouResp(raw, &resp); err != nil {
		return nil, err
	}
	results := normalizePansouResults(resp, keyword)
	return map[string]any{
		"query":    keyword,
		"queries":  []string{keyword},
		"total":    len(results),
		"results":  results,
		"rawTotal": resp.Total,
		"requests": []map[string]any{{
			"method":  "POST",
			"url":     endpoint,
			"payload": payload,
		}},
	}, nil
}

func searchMissingEpisode(s settings, missing missingEpisode) (map[string]any, error) {
	if err := requireFields(s, "pansouUrl"); err != nil {
		return nil, err
	}
	query := strings.TrimSpace(missing.Query)
	if query == "" && missing.OfficialTitle != "" && missing.Season > 0 && missing.Episode > 0 {
		query = fmt.Sprintf("%s S%02dE%02d", missing.OfficialTitle, missing.Season, missing.Episode)
	}
	if query == "" {
		return nil, badRequest("缺少搜索关键词")
	}
	code := missing.Code
	if code == "" && missing.Season > 0 && missing.Episode > 0 {
		code = fmt.Sprintf("S%02dE%02d", missing.Season, missing.Episode)
	}

	headers, err := pansouAuthHeaders(s)
	if err != nil {
		return nil, err
	}
	headers["Content-Type"] = "application/json"
	endpoint := buildBaseURL(s.PansouURL, "/api/search", nil)

	queries := []string{query}
	if titleOnly := strings.TrimSpace(missing.OfficialTitle); titleOnly != "" && !stringSliceContains(queries, titleOnly) {
		queries = append(queries, titleOnly)
	}

	requests := make([]map[string]any, 0, len(queries))
	allResults := make([]normalizedResult, 0)
	rawTotal := 0
	for _, kw := range queries {
		payload := map[string]any{
			"kw":          kw,
			"res":         "merge",
			"src":         "all",
			"cloud_types": []string{"115"},
			"filter": map[string]any{
				"include": compactStringSlice([]string{code}),
				"exclude": []string{},
			},
			"ext": map[string]any{"title_en": missing.OriginalTitle, "is_all": true},
		}
		var raw json.RawMessage
		if err := requestJSON(http.MethodPost, endpoint, headers, payload, &raw, 8*time.Second); err != nil {
			return nil, err
		}
		var resp pansouSearchResp
		if err := unmarshalPansouResp(raw, &resp); err != nil {
			return nil, err
		}
		rawTotal += resp.Total
		results := normalizePansouResults(resp, kw)
		allResults = append(allResults, results...)
		requests = append(requests, map[string]any{
			"method":  "POST",
			"url":     endpoint,
			"payload": payload,
		})
	}
	results := dedupeNormalizedResults(allResults, 30)
	return map[string]any{
		"query":    query,
		"queries":  queries,
		"total":    len(results),
		"results":  results,
		"rawTotal": rawTotal,
		"requests": requests,
	}, nil
}

func normalizePansouResults(resp pansouSearchResp, query string) []normalizedResult {
	items := make([]normalizedResult, 0)

	// 优先从 merged_by_type 取（遍历所有 key，不再限定 "115"/"oneonefive"）
	for _, list := range resp.MergedByType {
		for _, item := range list {
			items = append(items, normalizedResult{
				Title:    firstNonEmpty(item.Note, item.WorkTitle, item.Title, "115 资源"),
				URL:      item.URL,
				Password: firstNonEmpty(item.Password, extractPassword(item.URL)),
				Source:   fallback(item.Source, "PanSou"),
				Datetime: item.Datetime,
				Images:   item.Images,
				Query:    query,
			})
		}
	}

	// merged_by_type 为空时，从 results 兜底（请求已限定 cloud_types，不再过滤 link type）
	if len(items) == 0 {
		for _, result := range resp.Results {
			for _, link := range result.Links {
				items = append(items, normalizedResult{
					Title:    firstNonEmpty(link.WorkTitle, result.Title, "115 资源"),
					URL:      link.URL,
					Password: firstNonEmpty(link.Password, extractPassword(link.URL)),
					Source:   sourceName(result.Channel),
					Datetime: firstNonEmpty(link.Datetime, result.Datetime),
					Images:   result.Images,
					Query:    query,
				})
			}
		}
	}
	return items
}

func dedupeNormalizedResults(items []normalizedResult, limit int) []normalizedResult {
	seen := map[string]bool{}
	out := make([]normalizedResult, 0)
	for _, item := range items {
		if item.URL == "" {
			continue
		}
		key := item.URL + "|" + item.Password
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, item)
		if limit > 0 && len(out) >= limit {
			break
		}
	}
	return out
}

func linkType(link pansouLinkItem) string {
	if link.Type != "" {
		return link.Type
	}
	if strings.Contains(link.URL, "115") {
		return "115"
	}
	return ""
}

func sourceName(channel string) string {
	if strings.TrimSpace(channel) == "" {
		return "PanSou"
	}
	return "tg:" + strings.TrimSpace(channel)
}

func pansouAuthHeaders(s settings) (map[string]string, error) {
	if s.PansouToken != "" {
		return map[string]string{"Authorization": "Bearer " + s.PansouToken}, nil
	}
	if s.PansouUsername == "" || s.PansouPassword == "" {
		return map[string]string{}, nil
	}
	pansouToken.Lock()
	defer pansouToken.Unlock()
	if pansouToken.Token != "" && time.Now().Before(pansouToken.ExpiresAt.Add(-time.Minute)) {
		return map[string]string{"Authorization": "Bearer " + pansouToken.Token}, nil
	}
	var loginResp struct {
		Token     string `json:"token"`
		ExpiresAt int64  `json:"expires_at"`
	}
	err := requestJSON(http.MethodPost, buildBaseURL(s.PansouURL, "/api/auth/login", nil), map[string]string{"Content-Type": "application/json"}, map[string]string{"username": s.PansouUsername, "password": s.PansouPassword}, &loginResp, 20*time.Second)
	if err != nil {
		return nil, err
	}
	if loginResp.Token == "" {
		return nil, errors.New("PanSou 未返回 token")
	}
	pansouToken.Token = loginResp.Token
	pansouToken.ExpiresAt = time.Unix(loginResp.ExpiresAt, 0)
	if loginResp.ExpiresAt == 0 {
		pansouToken.ExpiresAt = time.Now().Add(time.Hour)
	}
	return map[string]string{"Authorization": "Bearer " + loginResp.Token}, nil
}

func clearPanSouTokenCache() {
	pansouToken.Lock()
	defer pansouToken.Unlock()
	pansouToken.Token = ""
	pansouToken.ExpiresAt = time.Time{}
}

type transferRequest struct {
	URL       string `json:"url"`
	Link      string `json:"link"`
	Password  string `json:"password"`
	TargetCID string `json:"targetCid"`
}

type transferResult struct {
	OK        bool   `json:"ok"`
	Title     string `json:"title"`
	Count     int    `json:"count"`
	TargetCID string `json:"targetCid"`
	ShareCode string `json:"shareCode"`
	Message   string `json:"message"`
}

type shareSnapResp struct {
	State bool   `json:"state"`
	Error string `json:"error"`
	Msg   string `json:"msg"`
	Data  struct {
		Count      int         `json:"count"`
		ShareTitle string      `json:"share_title"`
		ShareInfo  shareInfo   `json:"shareinfo"`
		List       []shareItem `json:"list"`
	} `json:"data"`
}

type shareInfo struct {
	ShareTitle string `json:"share_title"`
}

type shareItem struct {
	Name string `json:"n"`
	CID  any    `json:"cid"`
	FID  any    `json:"fid"`
}

func transfer115(s settings, body transferRequest) (transferResult, error) {
	if err := requireFields(s, "p115Cookie"); err != nil {
		return transferResult{}, err
	}
	link := firstNonEmpty(body.URL, body.Link)
	shareCode, receiveCode, err := parse115Share(link, body.Password)
	if err != nil {
		return transferResult{}, err
	}
	targetCID := firstNonEmpty(body.TargetCID, s.P115TargetCID, "0")
	title, items, err := list115Share(s.P115Cookie, shareCode, receiveCode)
	if err != nil {
		return transferResult{}, err
	}
	fileIDs := make([]string, 0)
	for _, item := range items {
		id := anyToString(item.FID)
		if id == "" {
			id = anyToString(item.CID)
		}
		if id != "" {
			fileIDs = append(fileIDs, id)
		}
	}
	if len(fileIDs) == 0 {
		return transferResult{}, errors.New("115 分享中没有可转存文件")
	}

	form := url.Values{}
	form.Set("cid", targetCID)
	form.Set("share_code", shareCode)
	form.Set("receive_code", receiveCode)
	form.Set("file_id", strings.Join(fileIDs, ","))
	var resp map[string]any
	if err := requestForm("https://webapi.115.com/share/receive", headers115(s.P115Cookie, true), form, &resp, 35*time.Second); err != nil {
		return transferResult{}, err
	}
	if state, _ := resp["state"].(bool); !state {
		return transferResult{}, errors.New(getErrorMessage(resp, "115 转存失败"))
	}
	return transferResult{OK: true, Title: title, Count: len(fileIDs), TargetCID: targetCID, ShareCode: shareCode, Message: "转存成功"}, nil
}

// ---- 后台任务系统 ----

func handleCreateJob(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Type       string `json:"type"` // "scan" or "scan-search"
		AiredOnly  bool   `json:"airedOnly"`
		MaxSeries  int    `json:"maxSeries"`
		RecentOnly bool   `json:"recentOnly"`
		ClearCache bool   `json:"clearCache"`
		SeriesID   string `json:"seriesId"`
	}
	body.AiredOnly = true
	_ = readJSON(r, &body)

	if body.Type != "scan" && body.Type != "scan-search" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "任务类型仅支持 scan 或 scan-search"})
		return
	}

	s := store.Get()
	if body.Type == "scan" {
		if err := requireFields(s, "embyUrl", "embyApiKey", "tmdbApiKey"); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
			return
		}
	} else {
		if err := requireFields(s, "embyUrl", "embyApiKey", "tmdbApiKey", "pansouUrl"); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
			return
		}
	}

	j := jobMgr.create(body.Type)
	activateScanJob(j.ID)
	go runJob(j.ID, s, body.Type, body.AiredOnly, body.MaxSeries, body.RecentOnly, body.ClearCache, strings.TrimSpace(body.SeriesID))
	writeJSON(w, http.StatusAccepted, map[string]any{"jobId": j.ID})
}

func handleGetJob(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimPrefix(r.URL.Path, "/api/jobs/")
	id = strings.TrimSpace(id)
	if id == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "缺少任务 ID"})
		return
	}
	j := jobMgr.get(id)
	if j == nil {
		writeJSON(w, http.StatusNotFound, map[string]any{"error": "任务不存在或已过期"})
		return
	}
	writeJSON(w, http.StatusOK, j)
}

func handleGetActiveJob(w http.ResponseWriter, r *http.Request) {
	id := currentActiveScanJobID()
	if id == "" {
		writeJSON(w, http.StatusOK, map[string]any{"job": nil})
		return
	}
	j := jobMgr.get(id)
	if j == nil || (j.Status != jobRunning && j.Status != jobPending) {
		finishActiveScanJob(id)
		writeJSON(w, http.StatusOK, map[string]any{"job": nil})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"job": j})
}

func runJob(id string, s settings, typ string, airedOnly bool, maxSeries int, recentOnly bool, clearCache bool, seriesID string) {
	defer finishActiveScanJob(id)
	update := func(fn func(*job)) bool {
		if !isActiveScanJob(id) {
			return false
		}
		jobMgr.update(id, fn)
		return true
	}
	changedSince := lastScanTime()
	modeText := "全量增量模式"
	if recentOnly {
		modeText = "最近变更模式"
		if changedSince.IsZero() {
			modeText = "最近变更模式（首次将执行全量）"
		}
	}
	update(func(j *job) {
		j.Status = jobRunning
		j.Progress = 1
		j.Message = "开始扫描媒体库..."
		j.Current = modeText
	})
	if clearCache {
		update(func(j *job) {
			j.Message = "正在清空本地缓存..."
			j.Current = "清空缓存"
		})
		clearLocalScanCaches()
	}

	scanProgressMax := 99
	searchProgressBase := 60
	if typ == "scan-search" {
		scanProgressMax = 55
	}

	// 进度条 ticker：扫描期间逐步更新
	done := make(chan struct{})
	go func() {
		ticker := time.NewTicker(800 * time.Millisecond)
		for {
			select {
			case <-ticker.C:
				update(func(j *job) {
					if j.Status == jobRunning && j.Progress < 8 {
						j.Progress++
					}
				})
			case <-done:
				ticker.Stop()
				return
			}
		}
	}()

	result, err := scanLibrary(s, airedOnly, maxSeries, recentOnly, changedSince, seriesID, func(processed, total int, detail, current string, snapshot map[string]any) {
		if total <= 0 {
			total = 1
		}
		progress := (processed * scanProgressMax) / total
		if progress < 1 {
			progress = 1
		}
		if processed >= total {
			progress = scanProgressMax
		}
		missingCount := 0
		switch items := snapshot["missing"].(type) {
		case []missingEpisode:
			missingCount = len(items)
		case []any:
			missingCount = len(items)
		}
		update(func(j *job) {
			if progress > j.Progress {
				j.Progress = progress
			}
			j.Message = fmt.Sprintf("扫描中 (%d/%d)，已发现 %d 集缺失｜%s", processed, total, missingCount, detail)
			j.Current = current
			j.Result = map[string]any{"scan": snapshot}
		})
	})
	close(done)
	if err != nil {
		update(func(j *job) { j.Status = jobError; j.Error = err.Error(); j.Message = "扫描失败" })
		return
	}
	if strings.TrimSpace(seriesID) != "" {
		result = mergeSingleSeriesScanResult(seriesID, result)
	}
	_ = saveScanResult(result)
	missingCount := 0
	switch items := result["missing"].(type) {
	case []missingEpisode:
		missingCount = len(items)
	case []any:
		missingCount = len(items)
	}

	update(func(j *job) {
		if typ == "scan" {
			j.Progress = 100
			j.Status = jobDone
			j.Message = fmt.Sprintf("扫描完成，共发现 %d 集缺失", missingCount)
			j.Current = "完成"
		} else {
			j.Progress = searchProgressBase
			j.Message = fmt.Sprintf("扫描完成，共发现 %d 集缺失，开始搜索资源", missingCount)
			j.Current = "搜索准备中"
		}
		j.Result = map[string]any{"scan": result}
	})

	if typ == "scan" {
		return
	}

	// scan-search: 按剧名去重后搜索（不逐集搜）
	missingList, _ := result["missing"].([]missingEpisode)
	if len(missingList) == 0 {
		update(func(j *job) { j.Status = jobDone; j.Progress = 100; j.Message = "无缺失集数，无需搜索" })
		return
	}

	// 按剧名去重
	type showGroup struct {
		Title    string
		Episodes []missingEpisode
	}
	showMap := map[string]*showGroup{}
	var showOrder []string
	for _, ep := range missingList {
		key := ep.OfficialTitle
		if key == "" {
			key = ep.EmbyTitle
		}
		if g, ok := showMap[key]; ok {
			g.Episodes = append(g.Episodes, ep)
		} else {
			showMap[key] = &showGroup{Title: key, Episodes: []missingEpisode{ep}}
			showOrder = append(showOrder, key)
		}
	}

	type searchItem struct {
		Title    string             `json:"title"`
		Episodes []missingEpisode   `json:"episodes"`
		Results  []normalizedResult `json:"results,omitempty"`
		Error    string             `json:"error,omitempty"`
	}

	searched := make([]searchItem, 0, len(showOrder))
	total := len(showOrder)
	for i, title := range showOrder {
		grp := showMap[title]
		progress := searchProgressBase
		if total > 0 {
			progress = searchProgressBase + (i*(99-searchProgressBase))/total
		}
		update(func(j *job) {
			j.Progress = progress
			j.Message = fmt.Sprintf("搜索中 (%d/%d): %s (缺 %d 集)", i+1, total, title, len(grp.Episodes))
			j.Current = title
		})

		item := searchItem{Title: title, Episodes: grp.Episodes}
		res, err := searchKeyword(s, title)
		if err != nil {
			item.Error = err.Error()
		} else if results, ok := res["results"].([]normalizedResult); ok {
			item.Results = results
		}
		searched = append(searched, item)
	}

	_ = saveSearchResults(searched)
	update(func(j *job) {
		j.Status = jobDone
		j.Progress = 100
		j.Message = fmt.Sprintf("扫描搜索完成，共 %d 个剧集", len(showOrder))
		j.Current = "完成"
		j.Result = map[string]any{"scan": result, "searched": searched}
	})
}

// ----

func parse115Share(link, password string) (string, string, error) {
	raw := link + " " + password
	re := regexp.MustCompile(`(?i)(?:115cdn\.com|115\.com|anxia\.com)/s/([A-Za-z0-9]+)|/s/([A-Za-z0-9]+)`)
	match := re.FindStringSubmatch(raw)
	if len(match) == 0 {
		return "", "", errors.New("无法识别 115 分享链接")
	}
	shareCode := firstNonEmpty(match[1], match[2])
	receiveCode := strings.TrimSpace(firstNonEmpty(password, extractPassword(raw)))
	if receiveCode == "" {
		return "", "", errors.New("缺少 115 访问码 / password")
	}
	return shareCode, receiveCode, nil
}

func list115Share(cookie, shareCode, receiveCode string) (string, []shareItem, error) {
	offset := 0
	limit := 100
	count := 1<<31 - 1
	items := make([]shareItem, 0)
	title := "115 分享"
	for len(items) < count && offset < 5000 {
		query := map[string]string{
			"share_code":   shareCode,
			"receive_code": receiveCode,
			"offset":       strconv.Itoa(offset),
			"limit":        strconv.Itoa(limit),
			"cid":          "",
		}
		var resp shareSnapResp
		if err := requestJSON(http.MethodGet, buildBaseURL("https://webapi.115.com", "/share/snap", query), headers115(cookie, false), nil, &resp, 35*time.Second); err != nil {
			return "", nil, err
		}
		if !resp.State {
			return "", nil, errors.New(firstNonEmpty(resp.Error, resp.Msg, "115 分享链接无效或访问码错误"))
		}
		if resp.Data.ShareTitle != "" {
			title = resp.Data.ShareTitle
		} else if resp.Data.ShareInfo.ShareTitle != "" {
			title = resp.Data.ShareInfo.ShareTitle
		} else if len(resp.Data.List) > 0 && resp.Data.List[0].Name != "" {
			title = resp.Data.List[0].Name
		}
		items = append(items, resp.Data.List...)
		if resp.Data.Count > 0 {
			count = resp.Data.Count
		} else {
			count = len(items)
		}
		if len(resp.Data.List) == 0 {
			break
		}
		offset += len(resp.Data.List)
	}
	return title, items, nil
}

func embyGet(s settings, route string, query map[string]string, out any) error {
	if query == nil {
		query = map[string]string{}
	}
	query["api_key"] = s.EmbyAPIKey
	headers := map[string]string{"Accept": "application/json", "X-Emby-Token": s.EmbyAPIKey}
	return requestJSON(http.MethodGet, buildBaseURL(s.EmbyURL, route, query), headers, nil, out, 45*time.Second)
}

func embyItemsRoute(s settings) string {
	if s.EmbyUserID != "" {
		return "/Users/" + url.PathEscape(s.EmbyUserID) + "/Items"
	}
	return "/Items"
}

func loadSeriesEpisodes(s settings, seriesID string, onPage func(page, count int)) ([]embyEpisode, error) {
	startIndex := 0
	pageLimit := 200
	items := make([]embyEpisode, 0, 256)
	pageNum := 0
	for {
		var page embyEpisodesResp
		route := "/Shows/" + url.PathEscape(seriesID) + "/Episodes"
		if err := embyGet(s, route, map[string]string{
			"IsMissing":  "false",
			"Fields":     "SeriesId,ProviderIds,ParentIndexNumber,IndexNumber,PremiereDate,Path,SeasonId,LocationType,IsMissing,MediaSources",
			"SortBy":     "ParentIndexNumber,IndexNumber",
			"StartIndex": strconv.Itoa(startIndex),
			"Limit":      strconv.Itoa(pageLimit),
		}, &page); err != nil {
			return nil, err
		}
		pageNum++
		for _, ep := range page.Items {
			if !isActualEmbyEpisode(ep) {
				continue
			}
			items = append(items, ep)
		}
		if onPage != nil {
			onPage(pageNum, len(items))
		}
		if len(page.Items) < pageLimit {
			break
		}
		startIndex += pageLimit
	}
	return items, nil
}

func tmdbGet(s settings, route string, query map[string]string, out any) error {
	if query == nil {
		query = map[string]string{}
	}
	query["api_key"] = s.TMDBAPIKey
	endpoint := buildBaseURL("https://api.themoviedb.org/3", route, query)
	if tmdbCache != nil && tmdbCache.Get(endpoint, out) {
		return nil
	}
	if err := requestJSON(http.MethodGet, endpoint, map[string]string{"Accept": "application/json"}, nil, out, 35*time.Second); err != nil {
		return err
	}
	if tmdbCache != nil {
		tmdbCache.Set(endpoint, out)
	}
	return nil
}

func requestJSON(method, endpoint string, headers map[string]string, body any, out any, timeout time.Duration) error {
	var lastErr error
	for attempt := 1; attempt <= 1; attempt++ {
		if attempt > 1 {
			time.Sleep(2 * time.Second)
		}
		var reader io.Reader
		if body != nil {
			raw, err := json.Marshal(body)
			if err != nil {
				return err
			}
			reader = bytes.NewReader(raw)
		}
		req, err := http.NewRequest(method, endpoint, reader)
		if err != nil {
			return err
		}
		for key, value := range headers {
			req.Header.Set(key, value)
		}
		client := *httpCli
		client.Timeout = timeout
		resp, err := client.Do(req)
		if err != nil {
			if strings.Contains(err.Error(), "timeout") || strings.Contains(err.Error(), "deadline") || strings.Contains(err.Error(), "Timeout") {
				lastErr = fmt.Errorf("请求超时（%v），正在重试(%d/3)...", timeout, attempt)
				continue
			}
			return err
		}
		raw, readErr := io.ReadAll(io.LimitReader(resp.Body, 50*1024*1024))
		resp.Body.Close()
		if readErr != nil {
			lastErr = readErr
			continue
		}
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			return fmt.Errorf("请求失败：HTTP %d %s", resp.StatusCode, shortBody(raw))
		}
		if out == nil {
			return nil
		}
		if len(raw) == 0 {
			return nil
		}
		if err := json.Unmarshal(raw, out); err != nil {
			return fmt.Errorf("解析响应失败：%w", err)
		}
		return nil
	}
	if lastErr != nil {
		return lastErr
	}
	return fmt.Errorf("请求失败，已重试3次")
}

func requestForm(endpoint string, headers map[string]string, form url.Values, out any, timeout time.Duration) error {
	req, err := http.NewRequest(http.MethodPost, endpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return err
	}
	for key, value := range headers {
		req.Header.Set(key, value)
	}
	client := *httpCli
	client.Timeout = timeout
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 50*1024*1024))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("请求失败：HTTP %d %s", resp.StatusCode, shortBody(raw))
	}
	return json.Unmarshal(raw, out)
}

func headers115(cookie string, form bool) map[string]string {
	headers := map[string]string{
		"User-Agent": "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0 Safari/537.36 MicroMessenger/6.8.0 NetType/WIFI MiniProgramEnv/Mac MacWechat/WMPF",
		"Referer":    "https://servicewechat.com/wx2c744c010a61b0fa/94/page-frame.html",
		"Accept":     "*/*",
		"Cookie":     cookie,
	}
	if form {
		headers["Content-Type"] = "application/x-www-form-urlencoded"
	}
	return headers
}

func buildBaseURL(base, route string, query map[string]string) string {
	u, err := url.Parse(strings.TrimRight(base, "/"))
	if err != nil {
		return base
	}
	basePath := strings.TrimRight(u.Path, "/")
	u.Path = basePath + "/" + strings.TrimLeft(route, "/")
	q := u.Query()
	for key, value := range query {
		if value != "" {
			q.Set(key, value)
		}
	}
	u.RawQuery = q.Encode()
	return u.String()
}

func setCommonHeaders(w http.ResponseWriter) {
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "GET,POST,OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type,Authorization")
}

func serveStatic(w http.ResponseWriter, r *http.Request) {
	publicDir := getenv("PUBLIC_DIR", "public")
	requestPath := r.URL.Path
	if requestPath == "/" {
		requestPath = "/index.html"
	}
	clean := filepath.Clean(strings.TrimPrefix(requestPath, "/"))
	if strings.HasPrefix(clean, "..") {
		writeJSON(w, http.StatusForbidden, map[string]any{"error": "Forbidden"})
		return
	}
	filePath := filepath.Join(publicDir, clean)
	raw, err := os.ReadFile(filePath)
	if err != nil {
		raw, err = os.ReadFile(filepath.Join(publicDir, "index.html"))
		if err != nil {
			writeJSON(w, http.StatusNotFound, map[string]any{"error": "页面不存在"})
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write(raw)
		return
	}
	contentType := mime.TypeByExtension(filepath.Ext(filePath))
	if contentType == "" {
		contentType = "application/octet-stream"
	}
	w.Header().Set("Content-Type", contentType)
	_, _ = w.Write(raw)
}

func readJSON(r *http.Request, out any) error {
	if r.Body == nil {
		return nil
	}
	defer r.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(r.Body, 2*1024*1024))
	if err != nil {
		return err
	}
	if len(strings.TrimSpace(string(raw))) == 0 {
		return nil
	}
	return json.Unmarshal(raw, out)
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func writeError(w http.ResponseWriter, status int, err error) {
	writeJSON(w, status, map[string]any{"error": err.Error()})
}

func parseUsers(value string) map[string]string {
	users := map[string]string{}
	for _, pair := range strings.Split(value, ",") {
		pair = strings.TrimSpace(pair)
		if pair == "" {
			continue
		}
		idx := strings.Index(pair, ":")
		if idx > 0 {
			users[pair[:idx]] = pair[idx+1:]
		}
	}
	return users
}

func loadUsers(path string) map[string]string {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var users map[string]string
	if err := json.Unmarshal(raw, &users); err != nil {
		return nil
	}
	return users
}

func getenv(key, fallbackValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallbackValue
}

func getenvInt(key string, fallbackValue int) int {
	if value, err := strconv.Atoi(os.Getenv(key)); err == nil && value > 0 {
		return value
	}
	return fallbackValue
}

func clampScanConcurrency(value int) int {
	if value <= 0 {
		return 4
	}
	if value > 16 {
		return 16
	}
	return value
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func fallback(value, fallbackValue string) string {
	if value != "" {
		return value
	}
	return fallbackValue
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func compactStringSlice(values []string) []string {
	out := make([]string, 0)
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			out = append(out, strings.TrimSpace(value))
		}
	}
	return out
}

func stringSliceContains(values []string, target string) bool {
	target = strings.TrimSpace(target)
	for _, value := range values {
		if strings.EqualFold(strings.TrimSpace(value), target) {
			return true
		}
	}
	return false
}

func firstString(m map[string]any, keys ...string) string {
	for _, key := range keys {
		if value, ok := m[key].(string); ok && value != "" {
			return value
		}
	}
	return "ok"
}

func providerID(ids map[string]string, name string) string {
	for key, value := range ids {
		if strings.EqualFold(key, name) {
			return value
		}
	}
	return ""
}

func bestTMDBTVMatch(series embyItem, items []tmdbSearchItem) *tmdbSearchItem {
	if len(items) == 0 {
		return nil
	}
	targets := []string{normalizeTitle(series.Name), normalizeTitle(series.OriginalTitle)}
	for i := range items {
		item := &items[i]
		candidates := []string{normalizeTitle(item.Name), normalizeTitle(item.OriginalName)}
		if titleMatchesAny(targets, candidates) && yearClose(effectiveYear(series), item.FirstAirDate) {
			return item
		}
	}
	for i := range items {
		item := &items[i]
		if yearClose(effectiveYear(series), item.FirstAirDate) {
			return item
		}
	}
	return nil
}

func bestTMDBMovieMatch(movie embyItem, items []tmdbSearchItem) *tmdbSearchItem {
	if len(items) == 0 {
		return nil
	}
	targets := []string{normalizeTitle(movie.Name), normalizeTitle(movie.OriginalTitle)}
	for i := range items {
		item := &items[i]
		candidates := []string{normalizeTitle(item.Title), normalizeTitle(item.OriginalTitle)}
		if titleMatchesAny(targets, candidates) && yearClose(effectiveYear(movie), item.ReleaseDate) {
			return item
		}
	}
	return nil
}

func effectiveYear(item embyItem) int {
	if item.ProductionYear > 0 {
		return item.ProductionYear
	}
	if len(item.PremiereDate) >= 4 {
		if y, err := strconv.Atoi(item.PremiereDate[:4]); err == nil {
			return y
		}
	}
	return 0
}

func verifyTMDBMatch(item embyItem, tmdbName, tmdbOriginalName, tmdbDate string) bool {
	targets := []string{normalizeTitle(item.Name), normalizeTitle(item.OriginalTitle)}
	candidates := []string{normalizeTitle(tmdbName), normalizeTitle(tmdbOriginalName)}
	if !titleMatchesAny(targets, candidates) {
		return false
	}
	return yearClose(effectiveYear(item), tmdbDate)
}

func titleMatchesAny(targets, candidates []string) bool {
	for _, target := range targets {
		if target == "" {
			continue
		}
		for _, candidate := range candidates {
			if candidate == "" {
				continue
			}
			if target == candidate || titleContains(target, candidate) {
				return true
			}
		}
	}
	return false
}

func titleContains(a, b string) bool {
	if len([]rune(a)) < 4 || len([]rune(b)) < 4 {
		return false
	}
	return strings.Contains(a, b) || strings.Contains(b, a)
}

func tvSearchQueries(series embyItem) []string {
	queries := []string{series.Name, cleanSeasonSuffix(series.Name), series.OriginalTitle, cleanSeasonSuffix(series.OriginalTitle)}
	for _, query := range append([]string{}, queries...) {
		queries = append(queries, knownTitleAliases(query)...)
	}
	out := make([]string, 0, len(queries))
	for _, query := range queries {
		query = strings.TrimSpace(query)
		if query != "" && !stringSliceContains(out, query) {
			out = append(out, query)
		}
	}
	return out
}

func knownTitleAliases(value string) []string {
	key := normalizeTitle(value)
	aliases := map[string][]string{
		"权欲第四章武力":       {"Power Book IV: Force"},
		"欢迎来到实力至上主义的教室": {"ようこそ実力至上主義の教室へ", "Classroom of the Elite"},
		"邻家的天使同学":       {"关于邻家的天使大人不知不觉把我惯成了废人", "The Angel Next Door Spoils Me Rotten"},
		"犯罪记录":          {"Criminal Record"},
	}
	return aliases[key]
}

func cleanSeasonSuffix(value string) string {
	value = strings.TrimSpace(value)
	patterns := []*regexp.Regexp{
		regexp.MustCompile(`(?i)\s+S\d{1,2}\s*$`),
		regexp.MustCompile(`(?i)\s+Season\s*\d{1,2}\s*$`),
		regexp.MustCompile(`\s+第[0-9一二三四五六七八九十百]+季\s*$`),
	}
	for _, pattern := range patterns {
		value = pattern.ReplaceAllString(value, "")
	}
	return strings.TrimSpace(value)
}

func normalizeTitle(value string) string {
	value = strings.ToLower(cleanSeasonSuffix(value))
	replacer := strings.NewReplacer(
		" ", "",
		"-", "",
		"_", "",
		":", "",
		"：", "",
		"·", "",
		"•", "",
		"（", "",
		"）", "",
		"(", "",
		")", "",
		"[", "",
		"]", "",
	)
	return replacer.Replace(value)
}

func yearClose(year int, date string) bool {
	if year <= 0 || len(date) < 4 {
		return true
	}
	parsed, err := strconv.Atoi(date[:4])
	if err != nil {
		return true
	}
	diff := year - parsed
	if diff < 0 {
		diff = -diff
	}
	return diff <= 1
}

func firstYear(date string) string {
	if len(date) >= 4 {
		return date[:4]
	}
	return ""
}

func parseInt(value string) int {
	n, _ := strconv.Atoi(value)
	return n
}

func cloneMissingEpisodes(items []missingEpisode) []missingEpisode {
	if len(items) == 0 {
		return nil
	}
	out := make([]missingEpisode, len(items))
	copy(out, items)
	return out
}

func cloneSeriesScanCacheEntry(entry seriesScanCacheEntry) seriesScanCacheEntry {
	cloned := entry
	cloned.Missing = cloneMissingEpisodes(entry.Missing)
	if entry.Unmatched != nil {
		value := *entry.Unmatched
		cloned.Unmatched = &value
	}
	return cloned
}

func seriesFingerprint(item embyItem, airedOnly bool) string {
	return strings.Join([]string{
		seriesScanCacheVersion,
		strings.TrimSpace(item.ID),
		strings.TrimSpace(item.Name),
		strings.TrimSpace(item.OriginalTitle),
		strconv.Itoa(effectiveYear(item)),
		strings.TrimSpace(item.DateLastSaved),
		strings.TrimSpace(item.DateLastMediaAdded),
		strconv.Itoa(item.RecursiveItemCount),
		strconv.FormatBool(airedOnly),
		providerID(item.ProviderIDs, "tmdb"),
		providerID(item.ProviderIDs, "tvdb"),
		providerID(item.ProviderIDs, "imdb"),
	}, "|")
}

func simpleMedia(item embyItem, reason string) unmatchedMedia {
	return unmatchedMedia{ID: item.ID, Name: item.Name, Year: effectiveYear(item), Type: item.Type, ProviderIDs: item.ProviderIDs, Reason: reason}
}

func isActualEmbyEpisode(ep embyEpisode) bool {
	if ep.IsMissing {
		return false
	}
	if strings.EqualFold(ep.LocationType, "Virtual") {
		return false
	}
	return true
}

func limitUnmatched(items []unmatchedMedia, limit int) []unmatchedMedia {
	if len(items) > limit {
		return items[:limit]
	}
	return items
}

func limitScanDiagnostics(items []scanDiagnosticEntry, limit int) []scanDiagnosticEntry {
	if len(items) > limit {
		return items[:limit]
	}
	return items
}

func limitCompareDiagnostics(items []scanCompareEntry, limit int) []scanCompareEntry {
	sort.Slice(items, func(i, j int) bool {
		if items[i].MissingEpisodes == items[j].MissingEpisodes {
			return items[i].Name < items[j].Name
		}
		return items[i].MissingEpisodes > items[j].MissingEpisodes
	})
	if len(items) > limit {
		return items[:limit]
	}
	return items
}

func sortMissingEpisodes(items []missingEpisode) {
	sort.Slice(items, func(i, j int) bool {
		if items[i].OfficialTitle == items[j].OfficialTitle {
			if items[i].Season == items[j].Season {
				return items[i].Episode < items[j].Episode
			}
			return items[i].Season < items[j].Season
		}
		return items[i].OfficialTitle < items[j].OfficialTitle
	})
}

func parallelFor(items []embyItem, limit int, worker func(embyItem)) {
	if limit <= 0 {
		limit = 1
	}
	sem := make(chan struct{}, limit)
	var wg sync.WaitGroup
	for _, item := range items {
		wg.Add(1)
		sem <- struct{}{}
		go func(item embyItem) {
			defer wg.Done()
			defer func() { <-sem }()
			worker(item)
		}(item)
	}
	wg.Wait()
}

func bodyToMissing(raw map[string]interface{}) missingEpisode {
	b, _ := json.Marshal(raw)
	var missing missingEpisode
	_ = json.Unmarshal(b, &missing)
	return missing
}

func extractPassword(text string) string {
	patterns := []*regexp.Regexp{
		regexp.MustCompile(`(?i)[?&](?:password|pwd|receive_code)=([A-Za-z0-9]+)`),
		regexp.MustCompile(`(?i)(?:访问码|提取码|密码)[:：\s]*([A-Za-z0-9]{4})`),
	}
	for _, pattern := range patterns {
		match := pattern.FindStringSubmatch(text)
		if len(match) > 1 {
			return match[1]
		}
	}
	return ""
}

func anyToString(value any) string {
	switch v := value.(type) {
	case string:
		return v
	case float64:
		return strconv.FormatInt(int64(v), 10)
	case int:
		return strconv.Itoa(v)
	case json.Number:
		return v.String()
	default:
		return strings.TrimSpace(fmt.Sprint(v))
	}
}

func getErrorMessage(m map[string]any, fallbackValue string) string {
	for _, key := range []string{"error", "msg", "message", "error_msg"} {
		if value, ok := m[key].(string); ok && value != "" {
			return value
		}
	}
	return fallbackValue
}

func shortBody(raw []byte) string {
	text := strings.TrimSpace(string(raw))
	if len(text) > 180 {
		return text[:180]
	}
	return text
}

func newTMDBCacheStore(path string, ttl time.Duration) *tmdbCacheStore {
	store := &tmdbCacheStore{path: path, ttl: ttl, data: map[string]tmdbCacheEntry{}}
	if raw, err := os.ReadFile(path); err == nil && len(raw) > 0 {
		_ = json.Unmarshal(raw, &store.data)
	}
	store.cleanupExpired()
	return store
}

func newSeriesScanCacheStore(path string) *seriesScanCacheStore {
	store := &seriesScanCacheStore{path: path, data: map[string]seriesScanCacheEntry{}}
	if raw, err := os.ReadFile(path); err == nil && len(raw) > 0 {
		_ = json.Unmarshal(raw, &store.data)
	}
	return store
}

func (s *seriesScanCacheStore) Get(key string) (seriesScanCacheEntry, bool) {
	if s == nil {
		return seriesScanCacheEntry{}, false
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	entry, ok := s.data[key]
	if !ok {
		return seriesScanCacheEntry{}, false
	}
	return cloneSeriesScanCacheEntry(entry), true
}

func (s *seriesScanCacheStore) Set(key string, entry seriesScanCacheEntry) {
	if s == nil || strings.TrimSpace(key) == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.data[key] = cloneSeriesScanCacheEntry(entry)
	s.dirty = true
}

func (s *seriesScanCacheStore) Delete(key string) {
	if s == nil || strings.TrimSpace(key) == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.data[key]; ok {
		delete(s.data, key)
		s.dirty = true
	}
}

func (s *seriesScanCacheStore) Prune(valid map[string]bool) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	changed := false
	for key := range s.data {
		if !valid[key] {
			delete(s.data, key)
			changed = true
		}
	}
	if changed {
		s.dirty = true
	}
	_ = s.flushLocked()
}

func (s *seriesScanCacheStore) Flush() error {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.flushLocked()
}

func (s *seriesScanCacheStore) flushLocked() error {
	if !s.dirty {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(s.data, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(s.path, raw, 0o600); err != nil {
		return err
	}
	s.dirty = false
	return nil
}

func (s *seriesScanCacheStore) Clear() error {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.data = map[string]seriesScanCacheEntry{}
	s.dirty = true
	return s.flushLocked()
}

func (s *tmdbCacheStore) Get(key string, out any) bool {
	if s == nil {
		return false
	}
	s.mu.RLock()
	entry, ok := s.data[key]
	s.mu.RUnlock()
	if !ok || entry.ExpiresAt < time.Now().Unix() {
		return false
	}
	return json.Unmarshal(entry.Payload, out) == nil
}

func (s *tmdbCacheStore) Set(key string, value any) {
	if s == nil {
		return
	}
	raw, err := json.Marshal(value)
	if err != nil {
		return
	}
	s.mu.Lock()
	s.data[key] = tmdbCacheEntry{ExpiresAt: time.Now().Add(s.ttl).Unix(), Payload: raw}
	s.cleanupExpiredLocked()
	_ = s.persistLocked()
	s.mu.Unlock()
}

func (s *tmdbCacheStore) cleanupExpired() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cleanupExpiredLocked()
	_ = s.persistLocked()
}

func (s *tmdbCacheStore) cleanupExpiredLocked() {
	now := time.Now().Unix()
	for key, entry := range s.data {
		if entry.ExpiresAt < now {
			delete(s.data, key)
		}
	}
}

func (s *tmdbCacheStore) persistLocked() error {
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(s.data, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(s.path, raw, 0o600)
}

func (s *tmdbCacheStore) Clear() error {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.data = map[string]tmdbCacheEntry{}
	return s.persistLocked()
}

func scanResultPath() string {
	return getenv("SCAN_RESULT_PATH", filepath.Join(filepath.Dir(getenv("CONFIG_PATH", filepath.Join("data", "config.json"))), "scan-result.json"))
}

func lastScanTime() time.Time {
	result, err := loadScanResult()
	if err != nil {
		return time.Time{}
	}
	if value, ok := result["scannedAt"].(string); ok && strings.TrimSpace(value) != "" {
		if t, err := time.Parse(time.RFC3339, value); err == nil {
			return t
		}
	}
	return time.Time{}
}

func clearLocalScanCaches() {
	if seriesScanCache != nil {
		_ = seriesScanCache.Clear()
	}
	if tmdbCache != nil {
		_ = tmdbCache.Clear()
	}
}

func scanModeLabel(recentOnly bool) string {
	if recentOnly {
		return "recent"
	}
	return "full"
}

func itemChangedSince(item embyItem, changedSince time.Time) bool {
	if changedSince.IsZero() {
		return true
	}
	for _, value := range []string{item.DateLastSaved, item.DateLastMediaAdded, item.PremiereDate} {
		if t := parseFlexibleTime(value); !t.IsZero() && t.After(changedSince) {
			return true
		}
	}
	return false
}

func parseFlexibleTime(value string) time.Time {
	value = strings.TrimSpace(value)
	if value == "" {
		return time.Time{}
	}
	for _, layout := range []string{time.RFC3339Nano, time.RFC3339, "2006-01-02 15:04:05", "2006-01-02"} {
		if t, err := time.Parse(layout, value); err == nil {
			return t
		}
	}
	return time.Time{}
}

func saveScanResult(result map[string]any) error {
	path := scanResultPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, raw, 0o600)
}

func mergeSingleSeriesScanResult(seriesID string, fresh map[string]any) map[string]any {
	seriesID = strings.TrimSpace(seriesID)
	if seriesID == "" {
		return fresh
	}
	previous, err := loadScanResult()
	if err != nil || previous == nil {
		return fresh
	}

	mergedMissing := make([]any, 0)
	for _, item := range anySlice(previous["missing"]) {
		if missingItemSeriesID(item) == seriesID {
			continue
		}
		mergedMissing = append(mergedMissing, item)
	}
	mergedMissing = append(mergedMissing, anySlice(fresh["missing"])...)

	previous["missing"] = mergedMissing
	previous["scannedAt"] = time.Now().Format(time.RFC3339)
	if summary, ok := previous["summary"].(map[string]any); ok {
		summary["totalMissingEpisodes"] = len(mergedMissing)
		summary["scanMode"] = "single"
		if freshSummary, ok := fresh["summary"].(map[string]any); ok {
			summary["seriesRescanned"] = freshSummary["seriesRescanned"]
			summary["seriesCached"] = freshSummary["seriesCached"]
			summary["unmatchedSeries"] = freshSummary["unmatchedSeries"]
		}
	}
	previous["diagnostics"] = fresh["diagnostics"]
	previous["unmatched"] = fresh["unmatched"]
	return previous
}

func anySlice(value any) []any {
	switch items := value.(type) {
	case []any:
		return items
	case []missingEpisode:
		out := make([]any, 0, len(items))
		for _, item := range items {
			out = append(out, item)
		}
		return out
	default:
		return []any{}
	}
}

func missingItemSeriesID(item any) string {
	switch v := item.(type) {
	case missingEpisode:
		return v.EmbySeriesID
	case map[string]any:
		return strings.TrimSpace(fmt.Sprint(v["embySeriesId"]))
	default:
		return ""
	}
}

func loadScanResult() (map[string]any, error) {
	path := scanResultPath()
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var result map[string]any
	if err := json.Unmarshal(raw, &result); err != nil {
		return nil, err
	}
	return result, nil
}

func searchResultsPath() string {
	return getenv("SEARCH_RESULTS_PATH", filepath.Join(filepath.Dir(getenv("CONFIG_PATH", filepath.Join("data", "config.json"))), "search-results.json"))
}

func saveSearchResults(searched any) error {
	path := searchResultsPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(searched, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, raw, 0o600)
}

func loadSearchResults() ([]any, error) {
	path := searchResultsPath()
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var results []any
	if err := json.Unmarshal(raw, &results); err != nil {
		return nil, err
	}
	return results, nil
}

// ---- MoviePilot 搜索/下载 ----

// ---- MoviePilot 核心函数 ----

// ---- MoviePilot 搜索/下载 ----

func handleMPSearch(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Keyword       string `json:"keyword"`
		TMDBID        string `json:"tmdbId"`
		OriginalTitle string `json:"originalTitle"`
	}
	if err := readJSON(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
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
		return nil, fmt.Errorf("MP HTTP %d: %s", resp1.StatusCode, string(raw1)[:200])
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

func mpDownload(s settings, rawData map[string]any, tmdbID string) error {
	client := &http.Client{Timeout: 30 * time.Second}
	mpURL := s.MPUrl
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
		return fmt.Errorf("MoviePilot 下载失败：%w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if resp.StatusCode >= 400 {
		return fmt.Errorf("MoviePilot 下载失败 HTTP %d: %s", resp.StatusCode, shortBody(body))
	}
	return nil
}
