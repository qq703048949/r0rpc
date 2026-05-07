package app

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"r0rpc/internal/auth"
	"r0rpc/internal/config"
	"r0rpc/internal/model"
	"r0rpc/internal/rpc"
	"r0rpc/internal/store"
)

const adminAuthCookieName = "r0rpc_admin_token"

func AdminAuthCookieName() string {
	return adminAuthCookieName
}

const (
	defaultPresenceFlushInterval = 15 * time.Second
	defaultMaintenanceInterval   = 5 * time.Minute
	defaultPersistQueueSize      = 131072
	defaultPersistWorkerCount    = 32
	defaultClientQueueSize       = 2048
	defaultRawScopeKeepLatest    = 100
	persistTaskTimeout           = 15 * time.Second
	persistEnqueueWait           = 250 * time.Millisecond
)

type persistTaskKind string

const (
	persistTaskCreateRequest   persistTaskKind = "create_request"
	persistTaskCompleteRequest persistTaskKind = "complete_request"
	persistTaskRPCMetric       persistTaskKind = "rpc_metric"
	persistTaskDeviceMetric    persistTaskKind = "device_metric"
)

type persistTask struct {
	Kind               persistTaskKind
	RequestID          string
	ClientID           string
	GroupName          string
	ActionName         string
	Status             string
	HTTPCode           int
	RequestPayload     string
	ResponsePayload    string
	LatencyMS          int64
	ErrorMessage       string
	RequesterUserID    int64
	HasRequesterUserID bool
	StatTime           time.Time
}

type App struct {
	Config *config.Config
	Store  *store.Store
	Tokens *auth.TokenManager
	Hub    *rpc.Hub

	presenceMu        sync.Mutex
	lastPresenceFlush map[string]time.Time
	persistCh         chan persistTask
}

type InvokeRequest struct {
	Username string          `json:"username"`
	Password string          `json:"password"`
	ClientID string          `json:"clientId"`
	Payload  json.RawMessage `json:"payload"`
	Timeout  int             `json:"timeoutSeconds"`
}

func New(cfg config.Config, st *store.Store) *App {
	queueSize := cfg.PersistQueueSize
	if queueSize < defaultPersistQueueSize {
		queueSize = defaultPersistQueueSize
	}
	clientQueueSize := cfg.ClientQueueSize
	if clientQueueSize < defaultClientQueueSize {
		clientQueueSize = defaultClientQueueSize
	}
	hubMaxInFlight := cfg.ClientMaxInFlight
	if hubMaxInFlight < 256 {
		hubMaxInFlight = 256
	}
	return &App{
		Config:            &cfg,
		Store:             st,
		Tokens:            auth.NewTokenManager(cfg.JWTSecret),
		Hub:               rpc.NewHub(clientQueueSize, hubMaxInFlight),
		lastPresenceFlush: map[string]time.Time{},
		persistCh:         make(chan persistTask, queueSize),
	}
}

func (a *App) StartBackgroundJobs(ctx context.Context) {
	workerCount := a.Config.PersistWorkers
	if workerCount < defaultPersistWorkerCount {
		workerCount = defaultPersistWorkerCount
	}
	for i := 0; i < workerCount; i++ {
		go a.persistWorker(ctx, i+1)
	}

	go func() {
		presenceTicker := time.NewTicker(a.cleanupInterval())
		defer presenceTicker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-presenceTicker.C:
				cleanupCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
				_ = a.Store.MarkStaleDevicesOffline(cleanupCtx, time.Now().Add(-a.deviceOfflineGrace()))
				cancel()
				a.cleanupPresenceCache(time.Now())
			}
		}
	}()

	go func() {
		maintenanceTicker := time.NewTicker(defaultMaintenanceInterval)
		defer maintenanceTicker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-maintenanceTicker.C:
				cleanupCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
				_ = a.Store.CleanupOldRequests(cleanupCtx, a.Config.RawRetentionDays)
				_ = a.Store.TrimAllRPCRequestScopes(cleanupCtx, defaultRawScopeKeepLatest)
				_ = a.Store.CleanupOldMetrics(cleanupCtx, a.Config.AggregateRetentionDays)
				cancel()
			}
		}
	}()
}

func (a *App) LoginAdmin(ctx context.Context, username, password string) (string, *model.User, error) {
	user, err := a.Store.AuthenticateUser(ctx, username, password, false)
	if err != nil {
		return "", nil, err
	}
	if user.Role != "admin" {
		return "", nil, fmt.Errorf("admin role required")
	}
	token, err := a.Tokens.Issue(auth.Claims{
		UserID:   user.ID,
		Username: user.Username,
		Role:     user.Role,
	}, 12*time.Hour)
	return token, user, err
}

func (a *App) LoginClient(ctx context.Context, username, password, clientID, groupName, platform string, maxInFlight int, extra map[string]any, ip string) (string, *model.User, error) {
	user, err := a.Store.AuthenticateUser(ctx, username, password, true)
	if err != nil {
		return "", nil, err
	}
	if clientID == "" || groupName == "" {
		return "", nil, fmt.Errorf("clientId and group are required")
	}
	if platform == "" {
		platform = "xposed"
	}
	if maxInFlight <= 0 {
		maxInFlight = a.Config.ClientMaxInFlight
	}
	if maxInFlight < 256 {
		maxInFlight = 256
	}
	if maxInFlight > 1024 {
		maxInFlight = 1024
	}
	if err := a.Store.UpsertDevice(ctx, clientID, user.ID, groupName, platform, ip, extra); err != nil {
		return "", nil, err
	}
	a.Hub.Register(clientID, groupName, user.ID, platform, maxInFlight)
	a.markPresenceFlushed(clientID, time.Now())
	token, err := a.Tokens.Issue(auth.Claims{
		UserID:      user.ID,
		Username:    user.Username,
		Role:        "client",
		ClientID:    clientID,
		Group:       groupName,
		MaxInFlight: maxInFlight,
	}, 24*time.Hour)
	return token, user, err
}

func (a *App) PollClient(ctx context.Context, claims *auth.Claims, wait time.Duration, ip string) (*rpc.Job, error) {
	a.TouchClientPresence(ctx, claims.ClientID, claims.Group, claims.UserID, claims.MaxInFlight, "xposed", ip)
	return a.Hub.Poll(ctx, claims.ClientID, wait)
}

func (a *App) SubmitClientResult(ctx context.Context, claims *auth.Claims, result rpc.JobResult) error {
	a.TouchClientPresence(ctx, claims.ClientID, claims.Group, claims.UserID, claims.MaxInFlight, "", "")

	outcome, err := a.Hub.SubmitResult(claims.ClientID, result)
	if err != nil {
		if errors.Is(err, rpc.ErrResultClientMismatch) {
			log.Printf("reject mismatched result: client=%s request=%s", claims.ClientID, result.RequestID)
		}
		return err
	}

	switch {
	case outcome.Delivered:
		return nil
	case outcome.Duplicate:
		log.Printf("duplicate result ignored: client=%s request=%s", claims.ClientID, result.RequestID)
		return nil
	case outcome.Late:
		log.Printf("late result ignored after timeout: client=%s request=%s", claims.ClientID, result.RequestID)
		return nil
	default:
		return nil
	}
}

func (a *App) InvokeRPC(ctx context.Context, claims *auth.Claims, groupName, actionName string, req InvokeRequest) (rpc.JobResult, string, string, error) {
	requestID := randomID()
	timeout := a.Config.RequestTimeout
	if req.Timeout > 0 && req.Timeout < 120 {
		timeout = time.Duration(req.Timeout) * time.Second
	}

	requestPayload := buildStoredInvokeRequest(req)
	requestRecord := &model.RPCRequest{
		RequestID:          requestID,
		GroupName:          groupName,
		ActionName:         actionName,
		ClientID:           req.ClientID,
		RequestPayloadJSON: requestPayload,
		Status:             "pending",
		HTTPCode:           200,
	}
	if claims != nil {
		requestRecord.RequesterUserID = &claims.UserID
	}

	createTask := persistTask{
		Kind:           persistTaskCreateRequest,
		RequestID:      requestID,
		ClientID:       requestRecord.ClientID,
		GroupName:      groupName,
		ActionName:     actionName,
		Status:         requestRecord.Status,
		HTTPCode:       requestRecord.HTTPCode,
		RequestPayload: requestPayload,
	}
	if requestRecord.RequesterUserID != nil {
		createTask.RequesterUserID = *requestRecord.RequesterUserID
		createTask.HasRequesterUserID = true
	}

	job := &rpc.Job{
		RequestID:  requestID,
		Group:      groupName,
		Action:     actionName,
		ClientID:   req.ClientID,
		Payload:    req.Payload,
		CreatedAt:  time.Now(),
		DeadlineAt: time.Now().Add(timeout),
	}

	invokeCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	result, actualClientID, err := a.Hub.Invoke(invokeCtx, groupName, req.ClientID, job)
	if actualClientID != "" {
		requestRecord.ClientID = actualClientID
		createTask.ClientID = actualClientID
	}
	if err != nil {
		status := "timeout"
		httpCode := http.StatusGatewayTimeout
		switch {
		case errors.Is(err, rpc.ErrNoOnlineClient), errors.Is(err, rpc.ErrPreferredClientDown):
			status = "no_client"
			httpCode = http.StatusBadGateway
		case errors.Is(err, rpc.ErrClientQueueFull), errors.Is(err, rpc.ErrGroupSaturated):
			status = "rejected"
			httpCode = http.StatusTooManyRequests
		}
		usedClientID := requestRecord.ClientID
		rawResponse := buildStoredInvokeResponse(requestID, groupName, actionName, usedClientID, req.Payload, rpc.JobResult{
			RequestID: requestID,
			Status:    status,
			HTTPCode:  httpCode,
			Error:     err.Error(),
		})
		now := time.Now()
		completeTask := createTask
		completeTask.Kind = persistTaskCompleteRequest
		completeTask.ClientID = usedClientID
		completeTask.Status = status
		completeTask.HTTPCode = httpCode
		completeTask.ResponsePayload = rawResponse
		completeTask.LatencyMS = 0
		completeTask.ErrorMessage = err.Error()
		a.enqueuePersist(completeTask)
		a.enqueuePersist(persistTask{
			Kind:       persistTaskRPCMetric,
			StatTime:   now,
			ClientID:   usedClientID,
			GroupName:  groupName,
			ActionName: actionName,
			Status:     status,
			LatencyMS:  0,
		})
		if usedClientID != "" {
			a.enqueuePersist(persistTask{
				Kind:      persistTaskDeviceMetric,
				StatTime:  now,
				ClientID:  usedClientID,
				GroupName: groupName,
				Status:    status,
				LatencyMS: 0,
			})
		}
		return rpc.JobResult{}, requestID, usedClientID, err
	}

	if result.HTTPCode == 0 {
		result.HTTPCode = 200
	}
	if result.Status == "" {
		result.Status = "success"
	}

	completeTask := createTask
	completeTask.Kind = persistTaskCompleteRequest
	completeTask.ClientID = requestRecord.ClientID
	completeTask.Status = result.Status
	completeTask.HTTPCode = result.HTTPCode
	completeTask.ResponsePayload = buildStoredInvokeResponse(requestID, groupName, actionName, requestRecord.ClientID, req.Payload, result)
	completeTask.LatencyMS = result.LatencyMS
	completeTask.ErrorMessage = result.Error
	a.enqueuePersist(completeTask)
	a.enqueuePersist(persistTask{
		Kind:       persistTaskRPCMetric,
		StatTime:   time.Now(),
		ClientID:   requestRecord.ClientID,
		GroupName:  groupName,
		ActionName: actionName,
		Status:     result.Status,
		LatencyMS:  result.LatencyMS,
	})
	if requestRecord.ClientID != "" {
		a.enqueuePersist(persistTask{
			Kind:      persistTaskDeviceMetric,
			StatTime:  time.Now(),
			ClientID:  requestRecord.ClientID,
			GroupName: groupName,
			Status:    result.Status,
			LatencyMS: result.LatencyMS,
		})
	}
	return result, requestID, requestRecord.ClientID, nil
}

func buildStoredInvokeResponse(requestID, groupName, actionName, clientID string, requestPayload json.RawMessage, result rpc.JobResult) string {
	isOK := strings.EqualFold(result.Status, "success") && strings.TrimSpace(result.Error) == ""
	payload := map[string]any{
		"is_ok":          isOK,
		"requestId":      requestID,
		"group":          groupName,
		"action":         actionName,
		"clientId":       clientID,
		"requestPayload": jsonBodyOrObject(requestPayload),
		"status":         result.Status,
		"httpCode":       result.HTTPCode,
		"data":           jsonBodyOrObject(result.Payload),
		"latencyMs":      result.LatencyMS,
		"error":          result.Error,
	}
	if result.PayloadEncoding != "" {
		payload["payloadEncoding"] = result.PayloadEncoding
	}
	if result.PayloadRawSize > 0 {
		payload["payloadRawSize"] = result.PayloadRawSize
	}
	if result.PayloadCompressedSize > 0 {
		payload["payloadCompressedSize"] = result.PayloadCompressedSize
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return string(result.Payload)
	}
	return string(raw)
}

func buildStoredInvokeRequest(req InvokeRequest) string {
	payload := map[string]any{
		"clientId":       strings.TrimSpace(req.ClientID),
		"timeoutSeconds": req.Timeout,
		"payload":        jsonBodyOrObject(req.Payload),
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return string(req.Payload)
	}
	return string(raw)
}

func jsonBodyOrObject(raw json.RawMessage) any {
	trimmed := string(raw)
	if strings.TrimSpace(trimmed) == "" {
		return map[string]any{}
	}
	return json.RawMessage(trimmed)
}

func (a *App) VerifyTokenFromRequest(r *http.Request) (*auth.Claims, error) {
	header := strings.TrimSpace(r.Header.Get("Authorization"))
	if header != "" {
		const prefix = "Bearer "
		if len(header) <= len(prefix) || header[:len(prefix)] != prefix {
			return nil, fmt.Errorf("invalid authorization header")
		}
		return a.Tokens.Parse(header[len(prefix):])
	}

	cookie, err := r.Cookie(adminAuthCookieName)
	if err == nil && strings.TrimSpace(cookie.Value) != "" {
		return a.Tokens.Parse(strings.TrimSpace(cookie.Value))
	}
	if err != nil && !errors.Is(err, http.ErrNoCookie) {
		return nil, err
	}

	return nil, fmt.Errorf("missing authorization header")
}

func (a *App) RemoteIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err == nil {
		return host
	}
	return r.RemoteAddr
}

func (a *App) MustBootstrap(ctx context.Context) error {
	if err := store.BootstrapSchema(ctx, *a.Config); err != nil {
		return err
	}
	return a.Store.EnsureBootstrapAdmin(ctx, a.Config.BootstrapAdminUser, a.Config.BootstrapAdminPass)
}

func IsNotFound(err error) bool {
	return errors.Is(err, sql.ErrNoRows)
}

func (a *App) TouchClientPresence(ctx context.Context, clientID, groupName string, userID int64, maxInFlight int, platform, ip string) {
	if clientID == "" {
		return
	}
	if groupName != "" {
		a.Hub.Register(clientID, groupName, userID, platform, maxInFlight)
	}
	a.Hub.Touch(clientID)
	if !a.shouldFlushPresence(clientID, time.Now()) {
		return
	}
	if err := a.Store.TouchDevice(ctx, clientID, ip); err != nil {
		a.resetPresenceFlush(clientID)
		log.Printf("touch device failed: client=%s err=%v", clientID, err)
	}
}

func (a *App) presenceFlushInterval() time.Duration {
	seconds := a.Config.PresenceFlushSeconds
	if seconds <= 0 {
		seconds = int(defaultPresenceFlushInterval / time.Second)
	}
	return time.Duration(seconds) * time.Second
}

func (a *App) shouldFlushPresence(clientID string, now time.Time) bool {
	interval := a.presenceFlushInterval()
	a.presenceMu.Lock()
	defer a.presenceMu.Unlock()
	if last, ok := a.lastPresenceFlush[clientID]; ok && now.Sub(last) < interval {
		return false
	}
	a.lastPresenceFlush[clientID] = now
	return true
}

func (a *App) markPresenceFlushed(clientID string, ts time.Time) {
	a.presenceMu.Lock()
	defer a.presenceMu.Unlock()
	a.lastPresenceFlush[clientID] = ts
}

func (a *App) resetPresenceFlush(clientID string) {
	a.presenceMu.Lock()
	defer a.presenceMu.Unlock()
	delete(a.lastPresenceFlush, clientID)
}

func (a *App) cleanupInterval() time.Duration {
	interval := a.deviceOfflineGrace() / 4
	if interval < 5*time.Second {
		interval = 5 * time.Second
	}
	if interval > time.Minute {
		interval = time.Minute
	}
	return interval
}

func (a *App) deviceOfflineGrace() time.Duration {
	seconds := a.Config.DeviceOfflineSeconds
	if seconds <= 0 {
		if a.Config.DeviceOfflineMinutes > 0 {
			seconds = a.Config.DeviceOfflineMinutes * 60
		} else {
			seconds = 20
		}
	}
	return time.Duration(seconds) * time.Second
}

func (a *App) cleanupPresenceCache(now time.Time) {
	keepFor := a.deviceOfflineGrace() + a.presenceFlushInterval()*2
	a.presenceMu.Lock()
	defer a.presenceMu.Unlock()
	for clientID, ts := range a.lastPresenceFlush {
		if now.Sub(ts) > keepFor {
			delete(a.lastPresenceFlush, clientID)
		}
	}
}

func (a *App) enqueuePersist(task persistTask) {
	select {
	case a.persistCh <- task:
		return
	default:
	}

	go func(task persistTask) {
		timer := time.NewTimer(persistEnqueueWait)
		defer timer.Stop()
		select {
		case a.persistCh <- task:
			return
		case <-timer.C:
			if err := a.runPersistTask(task); err != nil {
				log.Printf("persist overflow fallback failed: kind=%s request=%s client=%s err=%v", task.Kind, task.RequestID, task.ClientID, err)
			}
		}
	}(task)
}

func (a *App) persistWorker(ctx context.Context, workerID int) {
	const persistBatchSize = 256

	for {
		select {
		case <-ctx.Done():
			return
		case task := <-a.persistCh:
			batch := make([]persistTask, 0, persistBatchSize)
			batch = append(batch, task)
		drainLoop:
			for len(batch) < persistBatchSize {
				select {
				case nextTask := <-a.persistCh:
					batch = append(batch, nextTask)
				default:
					break drainLoop
				}
			}
			if err := a.runPersistBatch(batch); err != nil {
				log.Printf("persist worker batch failed: worker=%d batch=%d err=%v", workerID, len(batch), err)
				if len(batch) > 1 {
					for _, item := range batch {
						if itemErr := a.runPersistTask(item); itemErr != nil {
							log.Printf("persist worker fallback failed: worker=%d kind=%s request=%s client=%s err=%v", workerID, item.Kind, item.RequestID, item.ClientID, itemErr)
						}
					}
				}
			}
		}
	}
}

func (a *App) runPersistTask(task persistTask) error {
	ctx, cancel := context.WithTimeout(context.Background(), persistTaskTimeout)
	defer cancel()

	var requesterUserID *int64
	if task.HasRequesterUserID {
		requesterUserID = &task.RequesterUserID
	}

	switch task.Kind {
	case persistTaskCreateRequest:
		return a.Store.CreateRPCRequest(ctx, &model.RPCRequest{
			RequestID:          task.RequestID,
			GroupName:          task.GroupName,
			ActionName:         task.ActionName,
			ClientID:           task.ClientID,
			RequesterUserID:    requesterUserID,
			RequestPayloadJSON: task.RequestPayload,
			Status:             task.Status,
			HTTPCode:           task.HTTPCode,
		})
	case persistTaskCompleteRequest:
		return a.Store.CompleteRPCRequest(ctx, &model.RPCRequest{
			RequestID:           task.RequestID,
			GroupName:           task.GroupName,
			ActionName:          task.ActionName,
			ClientID:            task.ClientID,
			RequesterUserID:     requesterUserID,
			RequestPayloadJSON:  task.RequestPayload,
			ResponsePayloadJSON: task.ResponsePayload,
			Status:              task.Status,
			HTTPCode:            task.HTTPCode,
			LatencyMS:           task.LatencyMS,
			ErrorMessage:        task.ErrorMessage,
		})
	case persistTaskRPCMetric:
		return a.Store.IncrementRPCDailyMetric(ctx, task.StatTime, task.ClientID, task.GroupName, task.ActionName, task.Status, task.LatencyMS)
	case persistTaskDeviceMetric:
		return a.Store.IncrementDailyMetric(ctx, task.StatTime, task.ClientID, task.GroupName, task.Status, task.LatencyMS)
	default:
		return fmt.Errorf("unknown persist task kind: %s", task.Kind)
	}
}

func randomID() string {
	buf := make([]byte, 12)
	if _, err := rand.Read(buf); err != nil {
		return fmt.Sprintf("req-%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(buf)
}
