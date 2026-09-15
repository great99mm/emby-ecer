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
	seriesScanCacheVersion = "library-identity-ended-v3"
)

// tmdbBaseURL 是变量而非常量，便于测试指向本地 mock
var tmdbBaseURL = "https://api.themoviedb.org/3"

var (
	appUsers        map[string]string
	appUsersMu      sync.RWMutex
	jwtSecret       []byte
	store           *settingsStore
	seriesScanCache *seriesScanCacheStore
	activeScanMu    sync.Mutex
	scanStartMu     sync.Mutex
	scanExecutionMu sync.Mutex
	activeScanJobID string
	httpCli         = &http.Client{Timeout: 45 * time.Second}

	jobMgr *jobManager
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
	mu          sync.RWMutex
	path        string
	jobs        map[string]*job
	lastPersist time.Time
}

type persistedJobState struct {
	Jobs map[string]*job `json:"jobs"`
}

func newJobManager(path string) *jobManager {
	m := &jobManager{path: path, jobs: map[string]*job{}}
	var state persistedJobState
	if stateDB != nil {
		stateDB.ImportJSONFile("jobs", path, &state)
	}
	if err := loadStateJSON("jobs", path, &state); err == nil {
		for id, item := range state.Jobs {
			if item == nil || item.Type != "scan" || strings.TrimSpace(id) == "" {
				continue
			}
			cloned := cloneJob(item)
			if cloned.Status == jobRunning || cloned.Status == jobPending {
				cloned.Status = jobError
				cloned.Error = "服务重启，任务已中断"
				cloned.Message = "服务重启，任务已中断"
				cloned.Current = "已中断"
				cloned.UpdatedAt = time.Now()
			}
			m.jobs[id] = cloned
		}
	}
	m.cleanupLocked()
	_ = m.persistLocked()
	return m
}

func (m *jobManager) create(typ string) *job {
	m.mu.Lock()
	defer m.mu.Unlock()
	id := randomID(12)
	j := &job{ID: id, Type: typ, Status: jobPending, Progress: 0, CreatedAt: time.Now(), UpdatedAt: time.Now()}
	m.jobs[id] = j
	m.cleanupLocked()
	_ = m.persistLocked()
	return cloneJob(j)
}

func (m *jobManager) get(id string) *job {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if j, ok := m.jobs[id]; ok {
		return cloneJob(j)
	}
	return nil
}

func (m *jobManager) update(id string, fn func(*job)) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if j, ok := m.jobs[id]; ok {
		fn(j)
		j.UpdatedAt = time.Now()
		m.cleanupLocked()
		// Keep progress in memory; checkpoint large partial results at most every
		// 30 seconds. Completion and errors must still be durable immediately.
		if j.Status == jobDone || j.Status == jobError || time.Since(m.lastPersist) >= 30*time.Second {
			_ = m.persistLocked()
		}
	}
}

func (m *jobManager) persist() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.persistLocked()
}

func (m *jobManager) cleanupLocked() {
	for k, v := range m.jobs {
		if v == nil || (v.Status != jobRunning && v.Status != jobPending && time.Since(v.UpdatedAt) > time.Hour) {
			delete(m.jobs, k)
		}
	}
}

func (m *jobManager) persistLocked() error {
	if m == nil || strings.TrimSpace(m.path) == "" {
		return nil
	}
	state := persistedJobState{Jobs: map[string]*job{}}
	for id, item := range m.jobs {
		if item == nil {
			continue
		}
		state.Jobs[id] = cloneJob(item)
	}
	if err := saveStateJSON("jobs", m.path, state); err != nil {
		return err
	}
	m.lastPersist = time.Now()
	return nil
}

func cloneJob(in *job) *job {
	if in == nil {
		return nil
	}
	cloned := *in
	return &cloned
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
	EmbyURL            string   `json:"embyUrl"`
	EmbyAPIKey         string   `json:"embyApiKey"`
	EmbyUserID         string   `json:"embyUserId"`
	TMDBAPIKey         string   `json:"tmdbApiKey"`
	ScanConcurrency    int      `json:"scanConcurrency"`
	ExcludedLibraries  []string `json:"excludedLibraries"`
	ScanAutoEnabled    bool     `json:"scanAutoEnabled"`
	ScanAutoInterval   int      `json:"scanAutoInterval"`
	ScanAutoRecentOnly bool     `json:"scanAutoRecentOnly"`

	MPUrl   string `json:"mpUrl"`
	MPToken string `json:"mpToken"`
}

type settingsStore struct {
	mu   sync.RWMutex
	path string
	data settings
}

type seriesScanCacheEntry struct {
	SeriesStatus string           `json:"seriesStatus,omitempty"`
	InProduction bool             `json:"inProduction,omitempty"`
	Verification string           `json:"verification,omitempty"`
	Fingerprint  string           `json:"fingerprint"`
	Matched      bool             `json:"matched"`
	Missing      []missingEpisode `json:"missing,omitempty"`
	Unmatched    *unmatchedMedia  `json:"unmatched,omitempty"`
	Name         string           `json:"name,omitempty"`
	TMDBID       int              `json:"tmdbId,omitempty"`
	TMDBName     string           `json:"tmdbName,omitempty"`
	TMDBYear     string           `json:"tmdbYear,omitempty"`
	Manual       bool             `json:"manual,omitempty"`
	Complete     bool             `json:"complete,omitempty"`
	UpdatedAt    int64            `json:"updatedAt"`
}

type seriesScanCacheStore struct {
	mu    sync.RWMutex
	path  string
	data  map[string]seriesScanCacheEntry
	dirty bool
}

func main() {
	configPath := getenv("CONFIG_PATH", filepath.Join("data", "config.json"))
	var err error
	stateDB, err = initSQLiteState(sqlitePathFromConfig(configPath))
	if err != nil {
		log.Fatal(err)
	}
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
	seriesScanCache = newSeriesScanCacheStore(getenv("SERIES_SCAN_CACHE_PATH", filepath.Join(filepath.Dir(configPath), "series-scan-cache.json")))
	jobMgr = newJobManager(getenv("JOB_STATE_PATH", filepath.Join(filepath.Dir(configPath), "jobs.json")))
	episodeIgnores = newEpisodeIgnoreStore(getenv("EPISODE_IGNORES_PATH", filepath.Join(filepath.Dir(configPath), "episode-ignores.json")))
	startScanScheduler()

	port := getenv("PORT", "3000")
	server := &http.Server{
		Addr:              ":" + port,
		Handler:           http.HandlerFunc(route),
		ReadHeaderTimeout: 10 * time.Second,
	}

	log.Printf("Emby Ecer running at http://localhost:%s", port)
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
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "name": "Emby Ecer", "runtime": "go", "time": time.Now().Format(time.RFC3339)})
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
		writeJSON(w, http.StatusOK, maskSettings(next))

	case r.URL.Path == "/api/settings/test" && r.Method == http.MethodPost:
		var body struct {
			Target   string         `json:"target"`
			Settings map[string]any `json:"settings"`
		}
		if err := readJSON(r, &body); err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		if body.Target == "" {
			body.Target = "all"
		}
		draft := settingsWithOverrides(store.Get(), body.Settings)
		writeJSON(w, http.StatusOK, testConnection(body.Target, draft))

	case r.URL.Path == "/api/scan/last" && r.Method == http.MethodGet:
		result, err := loadScanResult()
		if err != nil {
			writeJSON(w, http.StatusOK, map[string]any{"scannedAt": "", "summary": map[string]any{}, "missing": []any{}, "unmatched": map[string]any{}})
			return
		}
		writeJSON(w, http.StatusOK, result)

	case r.URL.Path == "/api/scan" && r.Method == http.MethodPost:
		var body struct {
			AiredOnly  bool     `json:"airedOnly"`
			MaxSeries  int      `json:"maxSeries"`
			RecentOnly bool     `json:"recentOnly"`
			SeriesID   string   `json:"seriesId"`
			SeriesIDs  []string `json:"seriesIds"`
		}
		body.AiredOnly = true
		_ = readJSON(r, &body)
		result, err := scanLibrary(store.Get(), body.AiredOnly, body.MaxSeries, body.RecentOnly, lastScanTime(), joinSeriesIDs(body.SeriesID, body.SeriesIDs), nil)
		if err != nil {
			writeError(w, statusFromError(err), err)
			return
		}
		ids := parseSeriesIDSet(joinSeriesIDs(body.SeriesID, body.SeriesIDs))
		if len(ids) > 0 {
			result = mergeSelectedSeriesScanResult(selectedResultIDs(ids, result), result)
		}
		_ = saveScanResult(result)
		writeJSON(w, http.StatusOK, result)

	case r.URL.Path == "/api/scan/verify" && r.Method == http.MethodPost:
		handleVerifyScan(w, r)

	case r.URL.Path == "/api/emby/libraries" && r.Method == http.MethodGet:
		handleEmbyLibraries(w, r)

	case r.URL.Path == "/api/episode-ignores" && r.Method == http.MethodGet:
		handleGetEpisodeIgnores(w, r)

	case r.URL.Path == "/api/episode-ignores" && r.Method == http.MethodPost:
		handleAddEpisodeIgnores(w, r)

	case r.URL.Path == "/api/episode-ignores/delete" && r.Method == http.MethodPost:
		handleDeleteEpisodeIgnores(w, r)

	case r.URL.Path == "/api/exemptions" && r.Method == http.MethodGet:
		handleGetExemptions(w, r)

	case r.URL.Path == "/api/exemptions" && r.Method == http.MethodPost:
		handleAddExemptions(w, r)

	case r.URL.Path == "/api/exemptions/delete" && r.Method == http.MethodPost:
		handleDeleteExemptions(w, r)

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

	case r.URL.Path == "/api/mp/subscribe" && r.Method == http.MethodPost:
		handleMPSubscribe(w, r)

	case r.URL.Path == "/api/mp/subscribe/status" && r.Method == http.MethodPost:
		handleMPSubscribeStatus(w, r)

	default:
		writeJSON(w, http.StatusNotFound, map[string]any{"error": "接口不存在"})
	}
}

func newSettingsStore(path string) *settingsStore {
	data := settingsFromEnv()
	var saved settings
	if stateDB != nil {
		stateDB.ImportJSONFile("settings", path, &saved)
	}
	if err := loadStateJSON("settings", path, &saved); err == nil {
		data = saved
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

		ExcludedLibraries:  splitCommaList(os.Getenv("SCAN_EXCLUDED_LIBRARIES")),
		ScanAutoEnabled:    getenvBool("SCAN_AUTO_ENABLED", false),
		ScanAutoInterval:   clampIntervalHours(getenvInt("SCAN_AUTO_INTERVAL_HOURS", 12)),
		ScanAutoRecentOnly: getenvBool("SCAN_AUTO_RECENT_ONLY", true),

		MPUrl:   os.Getenv("MP_URL"),
		MPToken: os.Getenv("MP_TOKEN"),
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

	next := settingsWithOverrides(s.data, input)
	s.data = next
	return next, saveStateJSON("settings", s.path, next)
}

// settingsWithOverrides applies form values without changing stored settings.
func settingsWithOverrides(next settings, input map[string]any) settings {
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
	if raw, ok := input["excludedLibraries"]; ok {
		next.ExcludedLibraries = toStringList(raw)
	}
	setPlain("scanAutoEnabled", func(v string) { next.ScanAutoEnabled = parseBool(v) })
	setPlain("scanAutoInterval", func(v string) { next.ScanAutoInterval = clampIntervalHours(parseInt(v)) })
	setPlain("scanAutoRecentOnly", func(v string) { next.ScanAutoRecentOnly = parseBool(v) })
	setPlain("mpUrl", func(v string) { next.MPUrl = strings.TrimRight(v, "/") })
	setSecret("mpToken", func(v string) { next.MPToken = v })
	next.ScanConcurrency = clampScanConcurrency(next.ScanConcurrency)

	return next
}

func maskSettings(s settings) map[string]any {
	return map[string]any{
		"embyUrl":         s.EmbyURL,
		"embyApiKey":      maskSecret(s.EmbyAPIKey, 4),
		"embyUserId":      s.EmbyUserID,
		"tmdbApiKey":      maskSecret(s.TMDBAPIKey, 4),
		"scanConcurrency": clampScanConcurrency(s.ScanConcurrency),

		"excludedLibraries":  compactStringSlice(s.ExcludedLibraries),
		"scanAutoEnabled":    s.ScanAutoEnabled,
		"scanAutoInterval":   clampIntervalHours(s.ScanAutoInterval),
		"scanAutoRecentOnly": s.ScanAutoRecentOnly,

		"mpUrl":   s.MPUrl,
		"mpToken": maskSecret(s.MPToken, 4),
		"ready": map[string]bool{
			"emby": s.EmbyURL != "" && s.EmbyAPIKey != "",
			"tmdb": s.TMDBAPIKey != "",
			"mp":   s.MPUrl != "" && s.MPToken != "",
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
	password, exists := appUsers[body.Username]
	pwdOk := exists && password != "" && body.Password != "" && hmac.Equal([]byte(password), []byte(body.Password))
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
	configPath := getenv("CONFIG_PATH", filepath.Join("data", "config.json"))
	usersPath := filepath.Join(filepath.Dir(configPath), "users.json")
	_ = saveStateJSON("users", usersPath, appUsers)
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

type httpStatusError struct {
	Status int
	Body   string
}

func (e httpStatusError) Error() string {
	if e.Body == "" {
		return fmt.Sprintf("请求失败：HTTP %d", e.Status)
	}
	return fmt.Sprintf("请求失败：HTTP %d %s", e.Status, e.Body)
}

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
		}
	}
	if len(missing) > 0 {
		return badRequest("缺少配置：" + strings.Join(missing, ", "))
	}
	return nil
}

func failResult(err error) map[string]any {
	return map[string]any{"ok": false, "error": err.Error()}
}

type embyItemsResp struct {
	Items            []embyItem `json:"Items"`
	TotalRecordCount int        `json:"TotalRecordCount"`
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
	IndexNumberEnd    int               `json:"IndexNumberEnd"`
	ProviderIDs       map[string]string `json:"ProviderIds"`
	LocationType      string            `json:"LocationType"`
	IsMissing         bool              `json:"IsMissing"`
	Path              string            `json:"Path"`
	MediaSources      []any             `json:"MediaSources"`
}

type tmdbSearchItem struct {
	ID            int     `json:"id"`
	Name          string  `json:"name"`
	OriginalName  string  `json:"original_name"`
	Title         string  `json:"title"`
	OriginalTitle string  `json:"original_title"`
	FirstAirDate  string  `json:"first_air_date"`
	ReleaseDate   string  `json:"release_date"`
	Overview      string  `json:"overview"`
	PosterPath    string  `json:"poster_path"`
	BackdropPath  string  `json:"backdrop_path"`
	MediaType     string  `json:"media_type"`
	VoteAverage   float64 `json:"vote_average"`
}

type tmdbSearchResp struct {
	Results []tmdbSearchItem `json:"results"`
}

type tmdbFindResp struct {
	TVResults    []tmdbSearchItem `json:"tv_results"`
	MovieResults []tmdbSearchItem `json:"movie_results"`
}

type tmdbTVDetail struct {
	Status       string       `json:"status"`
	InProduction bool         `json:"in_production"`
	ID           int          `json:"id"`
	Name         string       `json:"name"`
	OriginalName string       `json:"original_name"`
	FirstAirDate string       `json:"first_air_date"`
	Overview     string       `json:"overview"`
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
	MergedSeries  bool   `json:"mergedSeries,omitempty"`
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
	SeriesStatus    string            `json:"seriesStatus,omitempty"`
	SourceSeriesIDs []string          `json:"sourceSeriesIds,omitempty"`
	ID              string            `json:"id"`
	Name            string            `json:"name"`
	TMDBID          int               `json:"tmdbId"`
	TMDBName        string            `json:"tmdbName"`
	TMDBYear        string            `json:"tmdbYear"`
	EmbyEpisodes    int               `json:"embyEpisodes"`
	EmbySeasonCount int               `json:"embySeasonCount"`
	TMDBEpisodes    int               `json:"tmdbEpisodes"`
	OwnedEpisodes   int               `json:"ownedEpisodes"`
	MissingEpisodes int               `json:"missingEpisodes"`
	NeedsReview     bool              `json:"needsReview,omitempty"`
	LocalOrder      *localOrderReport `json:"localOrder,omitempty"`
	Reason          string            `json:"reason"`
}

func scanLibrary(s settings, airedOnly bool, maxSeries int, recentOnly bool, changedSince time.Time, onlySeriesID string, onProgress func(processed, total int, message, current string, snapshot map[string]any)) (map[string]any, error) {
	if !scanExecutionMu.TryLock() {
		return nil, errors.New("已有扫描任务正在运行，请等待完成")
	}
	defer scanExecutionMu.Unlock()
	if err := requireFields(s, "embyUrl", "embyApiKey", "tmdbApiKey"); err != nil {
		return nil, err
	}

	seriesItems := make([]embyItem, 0)
	movieItems := make([]embyItem, 0)
	seriesTotal := 0
	movieTotal := 0
	onlySeriesID = strings.TrimSpace(onlySeriesID)
	onlySeriesIDs := parseSeriesIDSet(onlySeriesID)
	fullSeriesScan := maxSeries <= 0 && len(onlySeriesIDs) == 0

	allItems, err := loadLibraryItems(s)
	if err != nil {
		return nil, err
	}
	excludedItems := excludedLibraryItems(s)
	identityGroups := seriesIdentityGroups(allItems, excludedItems)
	expandSelectedSeries(onlySeriesIDs, identityGroups)
	for _, item := range allItems {
		if excludedItems[item.ID] {
			continue
		}
		switch item.Type {
		case "Series":
			seriesTotal++
			if len(onlySeriesIDs) > 0 && !onlySeriesIDs[item.ID] {
				continue
			}
			seriesItems = append(seriesItems, item)
		case "Movie":
			movieTotal++
			movieItems = append(movieItems, item)
		}
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

	scannedIDs := make([]string, 0, len(seriesItems))
	for _, item := range seriesItems {
		scannedIDs = append(scannedIDs, item.ID)
	}
	var mu sync.Mutex
	missing := make([]missingEpisode, 0)
	missingIndex := map[string]int{}
	appendMissing := func(items []missingEpisode) {
		for _, item := range items {
			key := fmt.Sprintf("%d:%d:%d", item.TMDBID, item.Season, item.Episode)
			if item.TMDBID <= 0 {
				key = item.EmbySeriesID + ":" + key
			}
			if index, ok := missingIndex[key]; ok {
				if item.EmbySeriesID < missing[index].EmbySeriesID {
					missing[index] = item
				}
				continue
			}
			missingIndex[key] = len(missing)
			missing = append(missing, item)
		}
	}
	unmatchedSeries := make([]unmatchedMedia, 0)
	unmatchedMovies := make([]unmatchedMedia, 0)
	matchedSeries := 0
	matchedMovies := 0
	cachedSeries := 0
	rescannedSeries := 0
	skippedSeries := make([]scanDiagnosticEntry, 0)
	comparedSeries := make([]scanCompareEntry, 0)
	reviewSeries := make([]scanCompareEntry, 0)
	totalWork := len(seriesItems) * 3
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
		mode := scanModeLabel(recentOnly)
		if len(onlySeriesIDs) > 0 {
			mode = "single"
		}
		missingCopy := append([]missingEpisode(nil), missing...)
		seriesCopy := append([]unmatchedMedia(nil), unmatchedSeries...)
		movieCopy := append([]unmatchedMedia(nil), unmatchedMovies...)
		skippedCopy := append([]scanDiagnosticEntry(nil), skippedSeries...)
		comparedCopy := append([]scanCompareEntry(nil), comparedSeries...)
		reviewCopy := append([]scanCompareEntry(nil), reviewSeries...)
		processed, total := workDone, totalWork
		snapshot := map[string]any{
			"scannerVersion":   seriesScanCacheVersion,
			"scannedSeriesIds": scannedIDs,
			"scannedAt":        time.Now().Format(time.RFC3339),
			"summary": map[string]any{
				"seriesTotal":          seriesTotal,
				"seriesScanned":        len(seriesItems),
				"seriesCached":         cachedSeries,
				"seriesRescanned":      rescannedSeries,
				"scanMode":             mode,
				"movieTotal":           movieTotal,
				"matchedSeries":        matchedSeries,
				"unmatchedSeries":      len(unmatchedSeries),
				"seriesNeedsReview":    len(reviewSeries),
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
				"review":          reviewCopy,
			},
			"missing": missingCopy,
			"unmatched": map[string]any{
				"series": limitUnmatched(seriesCopy, 80),
				"movies": limitUnmatched(movieCopy, 80),
			},
		}
		mu.Unlock()
		sortMissingEpisodes(missingCopy)
		return processed, total, snapshot
	}
	adjustTotal := func(delta int) {
		if delta <= 0 {
			return
		}
		mu.Lock()
		totalWork += delta
		mu.Unlock()
	}
	var progressMu sync.Mutex
	var lastProgress time.Time
	advanceProgress := func(delta int, message, current string) {
		if delta > 0 {
			mu.Lock()
			workDone += delta
			mu.Unlock()
		}
		if onProgress == nil {
			return
		}
		// Do not copy/sort the entire growing result for each episode or season.
		// TryLock also keeps other scan workers moving while a checkpoint is saved.
		if !progressMu.TryLock() {
			return
		}
		defer progressMu.Unlock()
		if time.Since(lastProgress) < 2*time.Second {
			return
		}
		lastProgress = time.Now()
		processed, total, snapshot := buildSnapshot()
		onProgress(processed, total, message, current, snapshot)
	}
	advanceProgress(0, "开始扫描媒体库...", "初始化")

	// 最近变更模式：Emby 侧没动过的剧直接沿用上次结果，不再跑 TMDB 比对
	carriedMissing := map[string][]missingEpisode{}
	carriedReviews := map[string]scanCompareEntry{}
	incremental := recentOnly && !changedSince.IsZero() && len(onlySeriesIDs) == 0
	if incremental {
		carriedMissing, carriedReviews, incremental = previousScanBySeries()
	}
	fingerprint := func(series embyItem) string {
		return identityFingerprint(series, identityGroups[parseInt(providerID(series.ProviderIDs, "tmdb"))], airedOnly)
	}
	skipUnchanged := func(series embyItem) bool {
		entry, ok := seriesScanCache.Get(series.ID)
		if !ok || (!entry.Manual && (entry.SeriesStatus != "Ended" || entry.InProduction || entry.Fingerprint != fingerprint(series))) {
			return false
		}
		if entry, ok := carriedReviews[series.ID]; ok && entry.LocalOrder == nil {
			return false
		}
		return incremental && !itemChangedSince(series, changedSince)
	}
	pendingSeriesCount := 0
	for _, series := range seriesItems {
		if skipUnchanged(series) {
			continue
		}
		if len(onlySeriesIDs) == 0 && !recentOnly {
			if entry, ok := seriesScanCache.Get(series.ID); ok && automaticArchiveValid(entry, fingerprint(series)) {
				continue
			}
		}
		pendingSeriesCount++
	}

	// 一次性拉全库单集，替代逐剧请求；只有一两部剧要扫时，逐剧读取更划算
	inventory := map[string]*seriesInventory{}
	useInventory := false
	if pendingSeriesCount > 1 && pendingSeriesCount*4 >= seriesTotal {
		advanceProgress(0, "正在拉取全库单集缓存...", "初始化")
		loaded, invErr := loadEpisodeInventory(s, func(page, seriesCount int) {
			advanceProgress(0, fmt.Sprintf("正在拉取全库单集缓存（第 %d 页 / 已覆盖 %d 部剧）...", page, seriesCount), "初始化")
		})
		if invErr != nil {
			log.Printf("全库单集缓存拉取失败，回退到逐剧读取: %v", invErr)
		} else {
			inventory = loaded
			useInventory = true
			advanceProgress(0, fmt.Sprintf("全库单集缓存就绪，覆盖 %d 部剧", len(inventory)), "初始化")
		}
	}

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

	parallelFor(seriesItems, seriesWorkers, func(series embyItem) {
		title := fallback(series.Name, "未知剧集")
		if skipUnchanged(series) {
			carried := carriedMissing[series.ID]
			mu.Lock()
			matchedSeries++
			cachedSeries++
			appendMissing(carried)
			if entry, ok := carriedReviews[series.ID]; ok {
				reviewSeries = append(reviewSeries, entry)
			}
			mu.Unlock()
			addSkipped(series, "unchanged", fmt.Sprintf("自上次扫描以来 Emby 侧无变化，沿用上次结果（%d 集缺失）", len(carried)))
			advanceProgress(3, fmt.Sprintf("《%s》自上次扫描无变化，跳过", title), title)
			return
		}
		forceRescanSeries := len(onlySeriesIDs) > 0 || recentOnly
		if !forceRescanSeries {
			if entry, ok := seriesScanCache.Get(series.ID); ok && automaticArchiveValid(entry, fingerprint(series)) {
				mu.Lock()
				matchedSeries++
				cachedSeries++
				mu.Unlock()
				action := "complete-archive"
				reason := "上次完整扫描确认一个都不缺，跳过本次扫描"
				if entry.Manual {
					action = "manual-ignore"
					reason = "已手动加入免检名单，跳过本次扫描"
				}
				addSkipped(series, action, reason)
				advanceProgress(3, fmt.Sprintf("《%s》在免检名单中，跳过扫描", title), title)
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

		var inv *seriesInventory
		var seriesEpisodes []embyEpisode
		if useInventory {
			if cached, ok := inventory[series.ID]; ok {
				inv = cached
			} else {
				inv = newSeriesInventory()
			}
			advanceProgress(1, fmt.Sprintf("已读取《%s》的 Emby 集数", title), title)
		} else {
			var err error
			seriesEpisodes, err = loadSeriesEpisodes(s, series.ID, func(page, count int) {
				adjustTotal(1)
				advanceProgress(1, fmt.Sprintf("正在读取《%s》的 Emby 剧集", title), title)
			})
			seriesEpisodes = excludeSeriesEpisodes(seriesEpisodes, excludedItems)
			if err != nil {
				unmatched := simpleMedia(series, "读取 Emby 单剧集数失败："+err.Error())
				mu.Lock()
				unmatchedSeries = append(unmatchedSeries, unmatched)
				mu.Unlock()
				seriesScanCache.Delete(series.ID)
				advanceProgress(2, fmt.Sprintf("读取《%s》剧集失败", title), title)
				return
			}
			inv = buildSeriesInventory(seriesEpisodes)
			advanceProgress(1, fmt.Sprintf("已读取《%s》的 Emby 集数", title), title)
		}
		embySeasons := inv.Seasons

		var tv tmdbTVDetail
		if err := tmdbGet(s, fmt.Sprintf("/tv/%d", resolved), map[string]string{"language": "zh-CN"}, &tv); err != nil {
			mu.Lock()
			unmatchedSeries = append(unmatchedSeries, simpleMedia(series, "读取 TMDB 剧集失败："+err.Error()))
			mu.Unlock()
			advanceProgress(1, fmt.Sprintf("读取《%s》TMDB 剧集详情失败", title), title)
			return
		}
		advanceProgress(1, fmt.Sprintf("开始比对《%s》的季集信息", title), title)
		if total, reason := numberingReview(inv, tv); reason != "" {
			// Load original filenames only for incompatible orders. The fast full
			// inventory remains small, and source URLs never enter saved results.
			var localOrder *localOrderReport
			if useInventory {
				seriesEpisodes, err = loadSeriesEpisodes(s, series.ID, nil)
				seriesEpisodes = excludeSeriesEpisodes(seriesEpisodes, excludedItems)
			}
			if err == nil {
				localOrder = inspectLocalOrder(seriesEpisodes)
			} else {
				localOrder = &localOrderReport{Issue: "读取原始文件编号失败，请单剧重扫后再检查。"}
			}
			mu.Lock()
			matchedSeries++
			reviewSeries = append(reviewSeries, scanCompareEntry{
				ID: series.ID, Name: series.Name, TMDBID: resolved,
				TMDBName: fallback(tv.Name, series.Name), TMDBYear: firstYear(tv.FirstAirDate),
				EmbyEpisodes: inv.Total, EmbySeasonCount: len(inv.Seasons),
				TMDBEpisodes: total, NeedsReview: true, LocalOrder: localOrder, Reason: reason,
			})
			mu.Unlock()
			seriesScanCache.Delete(series.ID)
			return
		}
		group := identityGroups[resolved]
		sourceIDs := []string{series.ID}
		if useInventory && len(group) > 1 {
			inv, sourceIDs = mergedIdentityInventory(inv, series.ID, group, inventory, tv)
			embySeasons = inv.Seasons
		} else if len(group) > 1 {
			// Emby already returns its merged logical show through /Shows/id/Episodes.
			for _, item := range group {
				if item.ID != series.ID {
					sourceIDs = append(sourceIDs, item.ID)
				}
			}
		}
		officialTitle := fallback(tv.Name, series.Name)
		originalTitle := fallback(tv.OriginalName, fallback(series.OriginalTitle, series.Name))
		localMissing := make([]missingEpisode, 0)
		totalTMDBCount := 0
		ownedCount := 0
		cacheable := tv.Status == "Ended" && !tv.InProduction
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
			// Only skip details when every expected episode number is present.
			if inv.coversSeason(season.SeasonNumber, season.EpisodeCount) {
				totalTMDBCount += season.EpisodeCount
				ownedCount += season.EpisodeCount
				advanceProgress(1, fmt.Sprintf("《%s》第 %d 季已完整，跳过 TMDB 比对", officialTitle, season.SeasonNumber), fmt.Sprintf("%s / 第%d季", officialTitle, season.SeasonNumber))
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
					cacheable = false
					continue
				}
				isOwned := inv.has(season.SeasonNumber, ep.EpisodeNumber) || (ep.ID > 0 && inv.TMDBEpisodeIDs[ep.ID])
				// 手动忽略的单集不参与统计，避免它一直把健康度压在 99%
				if !isOwned && episodeIgnores.Has(series.ID, season.SeasonNumber, ep.EpisodeNumber) {
					continue
				}
				totalTMDBCount++
				if isOwned {
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
					MergedSeries:  len(sourceIDs) > 1,
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
		} else if inv.Total == 0 {
			compareReason = "Emby 没有读取到实际已拥有剧集，且未发现可加入的缺集"
		} else if ownedCount >= totalTMDBCount {
			compareReason = "Emby 读取到的季集号/TMDB 集 ID 已覆盖 TMDB 已播出集数"
		}
		if len(localMissing) == 0 && totalTMDBCount > 0 && tv.Status != "Ended" {
			compareReason = "当前已播出内容已齐；尚未确认完结，后续继续检查更新"
		}
		if len(sourceIDs) > 1 {
			compareReason = fmt.Sprintf("已合并核对 %d 条同剧记录；", len(sourceIDs)) + compareReason
		}
		addCompared(scanCompareEntry{
			SeriesStatus:    tv.Status,
			SourceSeriesIDs: sourceIDs,
			ID:              series.ID,
			Name:            series.Name,
			TMDBID:          resolved,
			TMDBName:        officialTitle,
			TMDBYear:        firstYear(tv.FirstAirDate),
			EmbyEpisodes:    inv.Total,
			EmbySeasonCount: len(embySeasons),
			TMDBEpisodes:    totalTMDBCount,
			OwnedEpisodes:   ownedCount,
			MissingEpisodes: len(localMissing),
			Reason:          compareReason,
		})

		mu.Lock()
		matchedSeries++
		appendMissing(localMissing)
		mu.Unlock()
		seriesScanCache.Set(series.ID, seriesScanCacheEntry{
			Verification: seriesScanCacheVersion, Fingerprint: fingerprint(series), Matched: true,
			SeriesStatus: tv.Status, InProduction: tv.InProduction,
			Complete: cacheable && totalTMDBCount > 0 && len(localMissing) == 0,
			Name:     series.Name, TMDBID: resolved, TMDBName: officialTitle, TMDBYear: firstYear(tv.FirstAirDate), UpdatedAt: time.Now().Unix(),
		})
	})

	// Movies have no missing episodes. Retain library counts without thousands
	// of unrelated TMDB detail/search requests after the series scan.
	for _, movie := range movieItems {
		if parseInt(providerID(movie.ProviderIDs, "tmdb")) > 0 {
			matchedMovies++
		} else {
			unmatchedMovies = append(unmatchedMovies, simpleMedia(movie, "Emby 未提供 TMDB 电影 ID"))
		}
	}
	if fullSeriesScan {
		seriesScanCache.Prune(currentSeriesIDs)
	}
	processed, total, result := buildSnapshot()
	if onProgress != nil {
		onProgress(processed, total, "媒体库比对完成", "完成", result)
	}
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

func handleCreateJob(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Type       string   `json:"type"`
		AiredOnly  bool     `json:"airedOnly"`
		MaxSeries  int      `json:"maxSeries"`
		RecentOnly bool     `json:"recentOnly"`
		SeriesID   string   `json:"seriesId"`
		SeriesIDs  []string `json:"seriesIds"`
	}
	body.AiredOnly = true
	_ = readJSON(r, &body)

	if body.Type != "scan" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "任务类型仅支持 scan"})
		return
	}

	s := store.Get()
	if err := requireFields(s, "embyUrl", "embyApiKey", "tmdbApiKey"); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}

	j, created := startScanJob()
	if created {
		go runJob(j.ID, s, body.AiredOnly, body.MaxSeries, body.RecentOnly, joinSeriesIDs(body.SeriesID, body.SeriesIDs))
	}
	writeJSON(w, http.StatusAccepted, map[string]any{"jobId": j.ID})
}

func startScanJob() (*job, bool) {
	scanStartMu.Lock()
	defer scanStartMu.Unlock()
	if id := currentActiveScanJobID(); id != "" {
		if j := jobMgr.get(id); j != nil && (j.Status == jobPending || j.Status == jobRunning) {
			return j, false
		}
	}
	j := jobMgr.create("scan")
	activateScanJob(j.ID)
	return j, true
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
	writeJSON(w, http.StatusOK, jobResponse(j, r))
}

func handleGetActiveJob(w http.ResponseWriter, r *http.Request) {
	id := currentActiveScanJobID()
	if id == "" {
		writeJSON(w, http.StatusOK, map[string]any{"job": nil})
		return
	}
	j := jobMgr.get(id)
	if j == nil {
		// 任务可能还在初始化中，不要误清活跃标记
		writeJSON(w, http.StatusOK, map[string]any{"job": nil})
		return
	}
	// 返回任务本身，即使已完成/出错也返回，让前端自行决定展示
	writeJSON(w, http.StatusOK, map[string]any{"job": jobResponse(j, r)})
}

// Normal polling only needs progress. Full results remain available explicitly.
func jobResponse(j *job, r *http.Request) *job {
	if j != nil && r.URL.Query().Get("summary") == "1" {
		j = cloneJob(j)
		j.Result = nil
	}
	return j
}

func handleGetExemptions(w http.ResponseWriter, r *http.Request) {
	manual, complete := exemptionLists()
	writeJSON(w, http.StatusOK, map[string]any{"manual": manual, "complete": complete})
}

func handleAddExemptions(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Items []seriesExemptionInput `json:"items"`
	}
	if err := readJSON(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	count := 0
	ignoredIDs := make([]string, 0, len(body.Items))
	for _, item := range body.Items {
		id := strings.TrimSpace(item.ID)
		if id == "" {
			continue
		}
		seriesScanCache.Set(id, seriesScanCacheEntry{
			Matched:   true,
			Complete:  true,
			Manual:    true,
			Name:      strings.TrimSpace(item.Name),
			TMDBID:    item.TMDBID,
			TMDBName:  strings.TrimSpace(item.TMDBName),
			TMDBYear:  strings.TrimSpace(item.TMDBYear),
			UpdatedAt: time.Now().Unix(),
		})
		count++
		ignoredIDs = append(ignoredIDs, id)
	}
	_ = seriesScanCache.Flush()
	removeSeriesFromSavedScanResult(ignoredIDs)
	manual, complete := exemptionLists()
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "count": count, "manual": manual, "complete": complete})
}

func handleDeleteExemptions(w http.ResponseWriter, r *http.Request) {
	var body struct {
		IDs []string `json:"ids"`
	}
	if err := readJSON(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	count := 0
	for _, id := range body.IDs {
		if strings.TrimSpace(id) == "" {
			continue
		}
		seriesScanCache.Delete(id)
		count++
	}
	_ = seriesScanCache.Flush()
	manual, complete := exemptionLists()
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "count": count, "manual": manual, "complete": complete})
}

type seriesExemptionInput struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	TMDBID   int    `json:"tmdbId"`
	TMDBName string `json:"tmdbName"`
	TMDBYear string `json:"tmdbYear"`
}

func runJob(id string, s settings, airedOnly bool, maxSeries int, recentOnly bool, seriesID string) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("runJob panic: %v", r)
			jobMgr.update(id, func(j *job) {
				j.Status = jobError
				j.Error = fmt.Sprintf("内部错误: %v", r)
				j.Message = "扫描异常中断"
				j.Current = "异常中断"
			})
		}
		finishActiveScanJob(id)
	}()
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
	scanProgressMax := 99

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
	if err != nil {
		update(func(j *job) { j.Status = jobError; j.Error = err.Error(); j.Message = "扫描失败" })
		return
	}
	if strings.TrimSpace(seriesID) != "" {
		result = mergeSelectedSeriesScanResult(selectedResultIDs(parseSeriesIDSet(seriesID), result), result)
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
		j.Progress = 100
		j.Status = jobDone
		j.Message = fmt.Sprintf("扫描完成，共发现 %d 集缺失", missingCount)
		j.Current = "完成"
		j.Result = map[string]any{"scan": result}
	})
}

func embyGet(s settings, route string, query map[string]string, out any) error {
	if query == nil {
		query = map[string]string{}
	}
	query["api_key"] = s.EmbyAPIKey
	headers := map[string]string{"Accept": "application/json", "X-Emby-Token": s.EmbyAPIKey}
	return requestJSON(http.MethodGet, buildBaseURL(s.EmbyURL, route, query), headers, nil, out, 45*time.Second)
}

func validateEmbyUserID(s settings) error {
	userID := strings.TrimSpace(s.EmbyUserID)
	if userID == "" {
		return nil
	}
	var page embyItemsResp
	return embyGet(s, "/Users/"+url.PathEscape(userID)+"/Items", map[string]string{
		"Recursive":        "true",
		"IncludeItemTypes": "Series,Movie",
		"Limit":            "1",
	}, &page)
}

func loadLibraryItems(s settings) ([]embyItem, error) {
	routes := []string{embyItemsRoute(s)}
	if strings.TrimSpace(s.EmbyUserID) != "" {
		routes = append(routes, "/Items")
	}
	var firstErr error
	for index, route := range routes {
		items, err := loadLibraryItemsFromRoute(s, route)
		if err == nil {
			if index > 0 {
				log.Printf("Emby UserId %q 不可用，已回退到全局 /Items", s.EmbyUserID)
			}
			return items, nil
		}
		if firstErr == nil {
			firstErr = err
		}
	}
	return nil, firstErr
}

func loadLibraryItemsFromRoute(s settings, itemRoute string) ([]embyItem, error) {
	items := make([]embyItem, 0)
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
		items = append(items, page.Items...)
		if len(page.Items) < itemPageLimit {
			break
		}
		itemStart += itemPageLimit
	}
	return items, nil
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
			"Fields":     "SeriesId,ProviderIds,ParentIndexNumber,IndexNumber,IndexNumberEnd,PremiereDate,Path,SeasonId,LocationType,IsMissing,MediaSources",
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
	endpoint := buildBaseURL(tmdbBaseURL, route, query)
	if err := requestJSON(http.MethodGet, endpoint, map[string]string{"Accept": "application/json"}, nil, out, 35*time.Second); err != nil {
		return err
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
			if isTimeoutError(err) {
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
			return httpStatusError{Status: resp.StatusCode, Body: shortBody(raw)}
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

func isTimeoutError(err error) bool {
	if err == nil {
		return false
	}
	if os.IsTimeout(err) {
		return true
	}
	text := err.Error()
	return strings.Contains(text, "timeout") || strings.Contains(text, "deadline") || strings.Contains(text, "Timeout")
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
	var users map[string]string
	if stateDB != nil {
		stateDB.ImportJSONFile("users", path, &users)
	}
	if err := loadStateJSON("users", path, &users); err != nil {
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

// toStringList 兼容前端传数组或逗号分隔字符串两种写法。
func toStringList(raw any) []string {
	switch value := raw.(type) {
	case nil:
		return []string{}
	case []string:
		return compactStringSlice(value)
	case []any:
		out := make([]string, 0, len(value))
		for _, item := range value {
			if text := strings.TrimSpace(anyToString(item)); text != "" {
				out = append(out, text)
			}
		}
		return out
	default:
		return splitCommaList(anyToString(raw))
	}
}

func splitCommaList(value string) []string {
	out := make([]string, 0)
	for _, part := range strings.FieldsFunc(value, func(r rune) bool { return r == ',' || r == '，' || r == '\n' || r == '\t' }) {
		if part = strings.TrimSpace(part); part != "" {
			out = append(out, part)
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

func anyToString(value any) string {
	switch v := value.(type) {
	case nil:
		return ""
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

func shortBody(raw []byte) string {
	text := strings.TrimSpace(string(raw))
	if len(text) > 180 {
		return text[:180]
	}
	return text
}

func newSeriesScanCacheStore(path string) *seriesScanCacheStore {
	store := &seriesScanCacheStore{path: path, data: map[string]seriesScanCacheEntry{}}
	if stateDB != nil {
		stateDB.ImportJSONFile("series_scan_cache", path, &store.data)
	}
	if err := loadStateJSON("series_scan_cache", path, &store.data); err != nil {
		store.data = map[string]seriesScanCacheEntry{}
	}
	for id, entry := range store.data {
		if entry.Complete && !entry.Manual && (entry.Verification != seriesScanCacheVersion || entry.SeriesStatus != "Ended" || entry.InProduction) {
			delete(store.data, id)
			store.dirty = true
		}
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
	if err := saveStateJSON("series_scan_cache", s.path, s.data); err != nil {
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
	return saveStateJSON("scan_result", path, result)
}

func mergeSingleSeriesScanResult(seriesID string, fresh map[string]any) map[string]any {
	return mergeSelectedSeriesScanResult(parseSeriesIDSet(seriesID), fresh)
}

func removeSeriesFromSavedScanResult(seriesIDs []string) {
	if len(seriesIDs) == 0 {
		return
	}
	set := map[string]bool{}
	for _, id := range seriesIDs {
		if strings.TrimSpace(id) != "" {
			set[strings.TrimSpace(id)] = true
		}
	}
	if len(set) == 0 {
		return
	}
	result, err := loadScanResult()
	if err != nil || result == nil {
		return
	}
	filtered := make([]any, 0)
	for _, item := range anySlice(result["missing"]) {
		if set[missingItemSeriesID(item)] {
			continue
		}
		filtered = append(filtered, item)
	}
	result["missing"] = filtered
	if summary, ok := result["summary"].(map[string]any); ok {
		summary["totalMissingEpisodes"] = len(filtered)
	}
	_ = saveScanResult(result)
}

func anySlice(value any) []any {
	switch items := value.(type) {
	case []string:
		out := make([]any, 0, len(items))
		for _, item := range items {
			out = append(out, item)
		}
		return out
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

type seriesExemptionView struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	TMDBID    int    `json:"tmdbId,omitempty"`
	TMDBName  string `json:"tmdbName,omitempty"`
	TMDBYear  string `json:"tmdbYear,omitempty"`
	Manual    bool   `json:"manual"`
	UpdatedAt int64  `json:"updatedAt"`
}

func exemptionLists() ([]seriesExemptionView, []seriesExemptionView) {
	manual := make([]seriesExemptionView, 0)
	complete := make([]seriesExemptionView, 0)
	if seriesScanCache == nil {
		return manual, complete
	}
	seriesScanCache.mu.RLock()
	defer seriesScanCache.mu.RUnlock()
	for id, entry := range seriesScanCache.data {
		if !entry.Complete {
			continue
		}
		item := seriesExemptionView{ID: id, Name: fallback(entry.Name, id), TMDBID: entry.TMDBID, TMDBName: entry.TMDBName, TMDBYear: entry.TMDBYear, Manual: entry.Manual, UpdatedAt: entry.UpdatedAt}
		if entry.Manual {
			manual = append(manual, item)
		} else {
			complete = append(complete, item)
		}
	}
	sort.Slice(manual, func(i, j int) bool { return manual[i].Name < manual[j].Name })
	sort.Slice(complete, func(i, j int) bool { return complete[i].Name < complete[j].Name })
	return manual, complete
}

func joinSeriesIDs(primary string, ids []string) string {
	parts := make([]string, 0, len(ids)+1)
	if strings.TrimSpace(primary) != "" {
		parts = append(parts, strings.TrimSpace(primary))
	}
	for _, id := range ids {
		if strings.TrimSpace(id) != "" {
			parts = append(parts, strings.TrimSpace(id))
		}
	}
	return strings.Join(parts, ",")
}

func parseSeriesIDSet(value string) map[string]bool {
	set := map[string]bool{}
	for _, part := range strings.FieldsFunc(value, func(r rune) bool { return r == ',' || r == '，' || r == '\n' || r == '\t' || r == ' ' }) {
		part = strings.TrimSpace(part)
		if part != "" {
			set[part] = true
		}
	}
	return set
}

func loadScanResult() (map[string]any, error) {
	path := scanResultPath()
	var result map[string]any
	if stateDB != nil {
		stateDB.ImportJSONFile("scan_result", path, &result)
	}
	if err := loadStateJSON("scan_result", path, &result); err != nil {
		return nil, err
	}
	return result, nil
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
