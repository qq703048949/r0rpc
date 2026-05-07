package web

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"r0rpc/internal/app"
	"r0rpc/internal/auth"
	"r0rpc/internal/model"
	"r0rpc/internal/rpc"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

const invokeAuthCacheTTL = 12 * time.Hour

type invokeAuthCacheEntry struct {
	Claims    auth.Claims
	ExpiresAt time.Time
}

type Server struct {
	App           *app.App
	wsClients     *clientWSSessions
	invokeAuthMu  sync.Mutex
	invokeAuthMap map[string]invokeAuthCacheEntry
}

func New(app *app.App) *Server {
	return &Server{
		App:           app,
		wsClients:     newClientWSSessions(),
		invokeAuthMap: map[string]invokeAuthCacheEntry{},
	}
}

func (s *Server) Routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /static/{file...}", s.handleStatic)
	mux.HandleFunc("GET /", s.handleIndex)
	mux.HandleFunc("GET /healthz", s.handleHealth)
	mux.HandleFunc("POST /api/auth/login", s.handleAdminLogin)
	mux.HandleFunc("GET /api/users", s.requireRole("admin", s.handleListUsers))
	mux.HandleFunc("POST /api/users", s.requireRole("admin", s.handleCreateUser))
	mux.HandleFunc("PATCH /api/users/{id}/status", s.requireRole("admin", s.handlePatchUserStatus))
	mux.HandleFunc("PATCH /api/users/{id}/password", s.requireRole("admin", s.handlePatchUserPassword))
	mux.HandleFunc("GET /api/groups", s.requireRole("admin", s.handleGroups))
	mux.HandleFunc("GET /api/devices", s.requireRole("admin", s.handleDevices))
	mux.HandleFunc("GET /api/monitor/requests", s.requireRole("admin", s.handleMonitorRequests))
	mux.HandleFunc("GET /api/monitor/request-options", s.requireRole("admin", s.handleMonitorRequestOptions))
	mux.HandleFunc("GET /api/monitor/groups/summary", s.requireRole("admin", s.handleGroupSummary))
	mux.HandleFunc("GET /api/metrics/clients/weekly", s.requireRole("admin", s.handleWeeklyMetrics))
	mux.HandleFunc("GET /api/metrics/clients/{clientId}/daily", s.requireRole("admin", s.handleClientDailyMetrics))
	mux.HandleFunc("GET /api/metrics/trends", s.requireRole("admin", s.handleTrendMetrics))
	mux.HandleFunc("POST /api/client/login", s.handleClientLogin)
	mux.HandleFunc("GET /api/client/ws", s.handleClientWS)
	mux.HandleFunc("POST /api/client/poll", s.requireRole("client", s.handleClientPoll))
	mux.HandleFunc("POST /api/client/result", s.requireRole("client", s.handleClientResult))
	mux.HandleFunc("POST /api/client/logout", s.requireRole("client", s.handleClientLogout))
	mux.HandleFunc("GET /rpc/clientQueue", s.handleRPCClientQueue)
	mux.HandleFunc("POST /rpc/invoke/{group}/{action}", s.handleInvoke)
	return mux
}

func (s *Server) requireRole(role string, next func(http.ResponseWriter, *http.Request, *auth.Claims)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		claims, err := s.App.VerifyTokenFromRequest(r)
		if err != nil {
			writeError(w, http.StatusUnauthorized, err)
			return
		}
		if claims.Role != role && !(role == "client" && claims.Role == "admin") {
			writeError(w, http.StatusForbidden, fmt.Errorf("insufficient role"))
			return
		}
		next(w, r, claims)
	}
}

func (s *Server) handleStatic(w http.ResponseWriter, r *http.Request) {
	name := strings.TrimPrefix(r.URL.Path, "/static/")
	if name == "" || strings.Contains(name, "..") {
		http.NotFound(w, r)
		return
	}
	data, err := fs.ReadFile(uiFS, name)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	switch {
	case strings.HasSuffix(name, ".css"):
		w.Header().Set("Content-Type", "text/css; charset=utf-8")
	case strings.HasSuffix(name, ".js"):
		w.Header().Set("Content-Type", "application/javascript; charset=utf-8")
	case strings.HasSuffix(name, ".html"):
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
	default:
		w.Header().Set("Content-Type", http.DetectContentType(data))
	}
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write(data)
}

func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	data, err := fs.ReadFile(uiFS, "index.html")
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write(data)
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"status":   "ok",
		"name":     s.App.Config.AppName,
		"serverId": s.App.Config.ServerID,
		"time":     time.Now().Format(time.RFC3339),
	})
}

func (s *Server) handleAdminLogin(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	token, user, err := s.App.LoginAdmin(r.Context(), req.Username, req.Password)
	if err != nil {
		writeError(w, http.StatusUnauthorized, err)
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name:     app.AdminAuthCookieName(),
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   int((12 * time.Hour) / time.Second),
	})
	writeJSON(w, http.StatusOK, map[string]any{"token": token, "user": user})
}

func (s *Server) handleListUsers(w http.ResponseWriter, r *http.Request, claims *auth.Claims) {
	users, err := s.App.Store.ListUsers(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": users})
}

func (s *Server) handleCreateUser(w http.ResponseWriter, r *http.Request, claims *auth.Claims) {
	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
		Role     string `json:"role"`
		Enabled  bool   `json:"enabled"`
		CanRPC   bool   `json:"canRpc"`
		Notes    string `json:"notes"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if req.Role == "" {
		req.Role = "client"
	}
	if req.Role != "admin" && req.Role != "client" {
		writeError(w, http.StatusBadRequest, fmt.Errorf("role must be admin or client"))
		return
	}
	user, err := s.App.Store.CreateUser(r.Context(), req.Username, req.Password, req.Role, req.Enabled, req.CanRPC, req.Notes)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusCreated, user)
}

func (s *Server) handlePatchUserStatus(w http.ResponseWriter, r *http.Request, claims *auth.Claims) {
	userID, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	var req struct {
		Enabled bool `json:"enabled"`
		CanRPC  bool `json:"canRpc"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if err := s.App.Store.UpdateUserStatus(r.Context(), userID, req.Enabled, req.CanRPC); err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) handlePatchUserPassword(w http.ResponseWriter, r *http.Request, claims *auth.Claims) {
	userID, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	var req struct {
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if req.Password == "" {
		writeError(w, http.StatusBadRequest, fmt.Errorf("password required"))
		return
	}
	if err := s.App.Store.UpdateUserPassword(r.Context(), userID, req.Password); err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) handleGroups(w http.ResponseWriter, r *http.Request, claims *auth.Claims) {
	items, err := s.App.Store.ListGroups(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	items = s.applyRealtimeGroupStates(items)
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) handleDevices(w http.ResponseWriter, r *http.Request, claims *auth.Claims) {
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	statusFilter := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("status")))
	items, err := s.App.Store.ListDevices(r.Context(), r.URL.Query().Get("group"), r.URL.Query().Get("client"), "", limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	items = s.applyRealtimeDeviceStates(items)
	if statusFilter != "" {
		filtered := items[:0]
		for _, item := range items {
			if strings.ToLower(strings.TrimSpace(item.Status)) == statusFilter {
				filtered = append(filtered, item)
			}
		}
		items = filtered
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) handleRPCClientQueue(w http.ResponseWriter, r *http.Request) {
	groupName := strings.TrimSpace(r.URL.Query().Get("group"))
	if groupName == "" {
		writeError(w, http.StatusBadRequest, fmt.Errorf("group required"))
		return
	}

	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	status := strings.TrimSpace(r.URL.Query().Get("status"))

	items, err := s.App.Store.ListDevices(r.Context(), groupName, "", "", limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	items = s.applyRealtimeDeviceStates(items)
	sessions := s.realtimeSessions()

	queue := make([]map[string]any, 0, len(items))
	clientIDs := make([]string, 0, len(items))
	for _, item := range items {
		currentStatus := strings.ToLower(strings.TrimSpace(item.Status))
		if status == "" {
			if currentStatus != "online" {
				continue
			}
		} else if currentStatus != strings.ToLower(status) {
			continue
		}

		session := sessions[item.ClientID]
		queue = append(queue, map[string]any{
			"clientId":     item.ClientID,
			"group":        item.GroupName,
			"platform":     item.Platform,
			"status":       item.Status,
			"lastSeenAt":   item.LastSeenAt.Format(time.RFC3339),
			"lastIp":       item.LastIP,
			"pendingCount": session.Pending.Len(),
			"inFlight":     session.InFlight,
			"maxInFlight":  session.MaxInFlight,
		})
		clientIDs = append(clientIDs, item.ClientID)
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"group":     groupName,
		"count":     len(queue),
		"clientIds": clientIDs,
		"items":     queue,
	})
}

func (s *Server) handleMonitorRequests(w http.ResponseWriter, r *http.Request, claims *auth.Claims) {
	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	pageSize, _ := strconv.Atoi(r.URL.Query().Get("pageSize"))
	if page <= 0 {
		page = 1
	}
	if pageSize <= 0 || pageSize > 100 {
		pageSize = 20
	}
	items, total, err := s.App.Store.ListRPCRequests(r.Context(), r.URL.Query().Get("group"), r.URL.Query().Get("action"), r.URL.Query().Get("client"), r.URL.Query().Get("status"), page, pageSize)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	totalPages := 0
	if total > 0 {
		totalPages = int((total + int64(pageSize) - 1) / int64(pageSize))
	}
	writeJSON(w, http.StatusOK, model.RPCRequestPage{Items: items, Page: page, PageSize: pageSize, Total: total, TotalPages: totalPages})
}

func (s *Server) handleMonitorRequestOptions(w http.ResponseWriter, r *http.Request, claims *auth.Claims) {
	items, err := s.App.Store.ListRPCRequestFilterOptions(r.Context(), r.URL.Query().Get("group"), r.URL.Query().Get("action"), r.URL.Query().Get("client"))
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, items)
}
func (s *Server) handleGroupSummary(w http.ResponseWriter, r *http.Request, claims *auth.Claims) {
	hours, _ := strconv.Atoi(r.URL.Query().Get("hours"))
	items, err := s.App.Store.GroupSummary(r.Context(), hours)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) handleWeeklyMetrics(w http.ResponseWriter, r *http.Request, claims *auth.Claims) {
	items, err := s.App.Store.WeeklyMetrics(r.Context(), r.URL.Query().Get("group"), r.URL.Query().Get("client"))
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) handleClientDailyMetrics(w http.ResponseWriter, r *http.Request, claims *auth.Claims) {
	days, _ := strconv.Atoi(r.URL.Query().Get("days"))
	items, err := s.App.Store.ClientDailyMetrics(r.Context(), r.PathValue("clientId"), days)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) handleTrendMetrics(w http.ResponseWriter, r *http.Request, claims *auth.Claims) {
	days, _ := strconv.Atoi(r.URL.Query().Get("days"))
	items, err := s.App.Store.TrendMetrics(r.Context(), r.URL.Query().Get("group"), r.URL.Query().Get("action"), r.URL.Query().Get("client"), days)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) handleClientLogin(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Username    string         `json:"username"`
		Password    string         `json:"password"`
		ClientID    string         `json:"clientId"`
		Group       string         `json:"group"`
		Platform    string         `json:"platform"`
		MaxInFlight int            `json:"maxInFlight"`
		Extra       map[string]any `json:"extra"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if req.MaxInFlight <= 0 {
		req.MaxInFlight = s.App.Config.ClientMaxInFlight
	}
	if req.MaxInFlight < 256 {
		req.MaxInFlight = 256
	}
	if req.MaxInFlight > 1024 {
		req.MaxInFlight = 1024
	}
	token, user, err := s.App.LoginClient(r.Context(), req.Username, req.Password, req.ClientID, req.Group, req.Platform, req.MaxInFlight, req.Extra, s.App.RemoteIP(r))
	if err != nil {
		writeError(w, http.StatusUnauthorized, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"token": token, "user": user, "group": req.Group, "maxInFlight": req.MaxInFlight, "transport": "websocket", "wsUrl": s.clientWSURL(r, token)})
}

func (s *Server) handleClientPoll(w http.ResponseWriter, r *http.Request, claims *auth.Claims) {
	waitSeconds, _ := strconv.Atoi(r.URL.Query().Get("waitSeconds"))
	if waitSeconds <= 0 || waitSeconds > 55 {
		waitSeconds = 20
	}
	ctx, cancel := context.WithTimeout(r.Context(), time.Duration(waitSeconds+5)*time.Second)
	defer cancel()
	job, err := s.App.PollClient(ctx, claims, time.Duration(waitSeconds)*time.Second, s.App.RemoteIP(r))
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"job": job})
}

func (s *Server) handleClientResult(w http.ResponseWriter, r *http.Request, claims *auth.Claims) {
	var req struct {
		RequestID             string          `json:"requestId"`
		Status                string          `json:"status"`
		HTTPCode              int             `json:"httpCode"`
		Payload               json.RawMessage `json:"payload"`
		PayloadEncoding       string          `json:"payloadEncoding"`
		PayloadRawSize        int             `json:"payloadRawSize"`
		PayloadCompressedSize int             `json:"payloadCompressedSize"`
		Error                 string          `json:"error"`
		LatencyMS             int64           `json:"latencyMs"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if req.RequestID == "" {
		writeError(w, http.StatusBadRequest, fmt.Errorf("requestId required"))
		return
	}
	if strings.TrimSpace(req.Status) == "" {
		req.Status = "success"
	}
	result := rpc.JobResult{RequestID: req.RequestID, Status: req.Status, HTTPCode: req.HTTPCode, Payload: req.Payload, PayloadEncoding: req.PayloadEncoding, PayloadRawSize: req.PayloadRawSize, PayloadCompressedSize: req.PayloadCompressedSize, Error: req.Error, LatencyMS: req.LatencyMS}
	if err := normalizeClientJobResult(&result); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	err := s.App.SubmitClientResult(r.Context(), claims, result)
	if err != nil {
		if errors.Is(err, rpc.ErrResultClientMismatch) {
			writeError(w, http.StatusConflict, err)
			return
		}
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) handleClientLogout(w http.ResponseWriter, r *http.Request, claims *auth.Claims) {
	s.App.Hub.Unregister(claims.ClientID)
	_ = s.App.Store.MarkDeviceOffline(r.Context(), claims.ClientID)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) handleInvoke(w http.ResponseWriter, r *http.Request) {
	groupName := r.PathValue("group")
	actionName := r.PathValue("action")
	var req app.InvokeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil && err.Error() != "EOF" {
		writeError(w, http.StatusBadRequest, err)
		return
	}

	claims, err := s.authenticateInvokeRequest(r, req)
	if err != nil {
		writeError(w, http.StatusUnauthorized, err)
		return
	}

	result, requestID, clientID, err := s.App.InvokeRPC(r.Context(), claims, groupName, actionName, req)
	if err != nil {
		status := "failed"
		httpCode := http.StatusBadGateway
		switch {
		case errors.Is(err, context.DeadlineExceeded) || strings.Contains(strings.TrimSpace(err.Error()), "context deadline exceeded"):
			status = "timeout"
			httpCode = http.StatusGatewayTimeout
		case errors.Is(err, rpc.ErrNoOnlineClient), errors.Is(err, rpc.ErrPreferredClientDown):
			status = "no_client"
		case errors.Is(err, rpc.ErrClientQueueFull), errors.Is(err, rpc.ErrGroupSaturated):
			status = "rejected"
			httpCode = http.StatusTooManyRequests
		}
		writeJSON(w, httpCode, map[string]any{
			"is_ok":          false,
			"status":         status,
			"httpCode":       httpCode,
			"requestId":      requestID,
			"group":          groupName,
			"action":         actionName,
			"clientId":       clientID,
			"requestPayload": jsonBodyOrEmpty(req.Payload),
			"error":          err.Error(),
		})
		return
	}
	isOK := strings.EqualFold(result.Status, "success") && strings.TrimSpace(result.Error) == ""
	writeJSON(w, http.StatusOK, map[string]any{
		"is_ok":          isOK,
		"requestId":      requestID,
		"group":          groupName,
		"action":         actionName,
		"clientId":       clientID,
		"requestPayload": jsonBodyOrEmpty(req.Payload),
		"status":         result.Status,
		"httpCode":       result.HTTPCode,
		"data":           jsonBodyOrEmpty(result.Payload),
		"latencyMs":      result.LatencyMS,
		"error":          result.Error,
	})
}

func (s *Server) authenticateInvokeRequest(r *http.Request, req app.InvokeRequest) (*auth.Claims, error) {
	if strings.TrimSpace(r.Header.Get("Authorization")) != "" {
		claims, err := s.App.VerifyTokenFromRequest(r)
		if err == nil {
			if claims.Role != "admin" {
				return nil, fmt.Errorf("insufficient role")
			}
			return claims, nil
		}
	}

	if strings.TrimSpace(req.Username) == "" || strings.TrimSpace(req.Password) == "" {
		return nil, fmt.Errorf("missing authorization header or username/password")
	}

	if claims, ok := s.getInvokeAuthCache(req.Username, req.Password); ok {
		return claims, nil
	}

	user, err := s.App.Store.AuthenticateUser(r.Context(), req.Username, req.Password, false)
	if err != nil {
		return nil, err
	}
	if user.Role != "admin" {
		return nil, fmt.Errorf("admin role required")
	}
	claims := &auth.Claims{UserID: user.ID, Username: user.Username, Role: user.Role}
	s.putInvokeAuthCache(req.Username, req.Password, claims)
	return claims, nil
}

func (s *Server) getInvokeAuthCache(username, password string) (*auth.Claims, bool) {
	key := invokeAuthCacheKey(username, password)
	now := time.Now()

	s.invokeAuthMu.Lock()
	defer s.invokeAuthMu.Unlock()

	entry, ok := s.invokeAuthMap[key]
	if !ok {
		return nil, false
	}
	if now.After(entry.ExpiresAt) {
		delete(s.invokeAuthMap, key)
		return nil, false
	}
	entry.ExpiresAt = now.Add(invokeAuthCacheTTL)
	s.invokeAuthMap[key] = entry
	claims := entry.Claims
	return &auth.Claims{UserID: claims.UserID, Username: claims.Username, Role: claims.Role}, true
}

func (s *Server) putInvokeAuthCache(username, password string, claims *auth.Claims) {
	if claims == nil {
		return
	}

	now := time.Now()
	key := invokeAuthCacheKey(username, password)
	entry := invokeAuthCacheEntry{
		Claims:    auth.Claims{UserID: claims.UserID, Username: claims.Username, Role: claims.Role},
		ExpiresAt: now.Add(invokeAuthCacheTTL),
	}

	s.invokeAuthMu.Lock()
	defer s.invokeAuthMu.Unlock()
	for existingKey, existing := range s.invokeAuthMap {
		if now.After(existing.ExpiresAt) {
			delete(s.invokeAuthMap, existingKey)
		}
	}
	s.invokeAuthMap[key] = entry
}

func invokeAuthCacheKey(username, password string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(username) + "\x00" + password))
	return hex.EncodeToString(sum[:])
}

func (s *Server) realtimeSessions() map[string]rpc.ClientSession {
	seconds := s.App.Config.DeviceOfflineSeconds
	if seconds <= 0 {
		if s.App.Config.DeviceOfflineMinutes > 0 {
			seconds = s.App.Config.DeviceOfflineMinutes * 60
		} else {
			seconds = 20
		}
	}
	cutoff := time.Now().Add(-time.Duration(seconds) * time.Second)
	result := map[string]rpc.ClientSession{}
	for _, session := range s.App.Hub.OnlineClients() {
		if session.LastSeenAt.Before(cutoff) {
			continue
		}
		result[session.ClientID] = session
	}
	return result
}

func (s *Server) applyRealtimeDeviceStates(items []model.Device) []model.Device {
	sessions := s.realtimeSessions()
	for index := range items {
		session, ok := sessions[items[index].ClientID]
		if !ok {
			items[index].Status = "offline"
			continue
		}

		items[index].Status = "online"
		if session.LastSeenAt.After(items[index].LastSeenAt) {
			items[index].LastSeenAt = session.LastSeenAt
		}
		if items[index].GroupName == "" {
			items[index].GroupName = session.Group
		}
		if items[index].Platform == "" {
			items[index].Platform = session.Platform
		}
	}
	return items
}

func (s *Server) applyRealtimeGroupStates(items []model.GroupInfo) []model.GroupInfo {
	sessions := s.realtimeSessions()
	groupOnlineCounts := map[string]int64{}
	groupLastSeen := map[string]time.Time{}
	for _, session := range sessions {
		groupOnlineCounts[session.Group]++
		if session.LastSeenAt.After(groupLastSeen[session.Group]) {
			groupLastSeen[session.Group] = session.LastSeenAt
		}
	}

	for index := range items {
		if count, ok := groupOnlineCounts[items[index].GroupName]; ok && count > 0 {
			items[index].OnlineDevices = count
			items[index].Status = "online"
			items[index].StatusLabel = "Online"
			lastSeen := groupLastSeen[items[index].GroupName]
			if !lastSeen.IsZero() {
				items[index].LastSeenAt = &lastSeen
			}
		}
	}
	return items
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func writeError(w http.ResponseWriter, status int, err error) {
	writeJSON(w, status, map[string]any{"error": err.Error()})
}

func jsonBodyOrEmpty(raw json.RawMessage) json.RawMessage {
	trimmed := strings.TrimSpace(string(raw))
	if trimmed == "" {
		return json.RawMessage("{}")
	}
	return raw
}
