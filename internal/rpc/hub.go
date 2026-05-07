package rpc

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"sync/atomic"
	"time"
)

type Job struct {
	RequestID  string          `json:"requestId"`
	Group      string          `json:"group"`
	Action     string          `json:"action"`
	ClientID   string          `json:"clientId"`
	Payload    json.RawMessage `json:"payload"`
	CreatedAt  time.Time       `json:"createdAt"`
	DeadlineAt time.Time       `json:"deadlineAt"`
}

type JobResult struct {
	RequestID             string          `json:"requestId"`
	Status                string          `json:"status"`
	HTTPCode              int             `json:"httpCode"`
	Payload               json.RawMessage `json:"payload"`
	PayloadEncoding       string          `json:"payloadEncoding,omitempty"`
	PayloadRawSize        int             `json:"payloadRawSize,omitempty"`
	PayloadCompressedSize int             `json:"payloadCompressedSize,omitempty"`
	Error                 string          `json:"error"`
	LatencyMS             int64           `json:"latencyMs"`
}

type ClientSession struct {
	ClientID      string
	Group         string
	UserID        int64
	Platform      string
	LastSeenAt    time.Time
	Pending       *jobQueue
	MaxInFlight   int
	InFlight      int
	dispatchReady chan struct{}
}

type jobQueue struct {
	mu     sync.Mutex
	items  []*Job
	limit  int
	notify chan struct{}
	count  atomic.Int64
}

func newJobQueue(limit int) *jobQueue {
	if limit <= 0 {
		limit = 1
	}
	return &jobQueue{items: make([]*Job, 0, limit), limit: limit, notify: make(chan struct{})}
}

func (q *jobQueue) Len() int {
	return int(q.count.Load())
}

func (q *jobQueue) Cap() int {
	return q.limit
}

func (q *jobQueue) Push(job *Job) error {
	now := time.Now()
	if job == nil || job.ExpiredAt(now) {
		return nil
	}
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.count.Load() >= int64(q.limit) {
		q.pruneExpiredLocked(now)
		if q.count.Load() >= int64(q.limit) {
			return ErrClientQueueFull
		}
	}
	q.items = append(q.items, job)
	q.count.Add(1)
	q.signalLocked()
	return nil
}

func (q *jobQueue) Pop(ctx context.Context) (*Job, error) {
	for {
		q.mu.Lock()
		job, waitCh := q.popOrWaitLocked(time.Now())
		q.mu.Unlock()
		if job != nil {
			return job, nil
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-waitCh:
		}
	}
}

func (q *jobQueue) Poll(ctx context.Context, wait time.Duration) (*Job, error) {
	if wait <= 0 {
		return q.Pop(ctx)
	}
	timer := time.NewTimer(wait)
	defer timer.Stop()
	for {
		q.mu.Lock()
		job, waitCh := q.popOrWaitLocked(time.Now())
		q.mu.Unlock()
		if job != nil {
			return job, nil
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-timer.C:
			return nil, nil
		case <-waitCh:
		}
	}
}

func (q *jobQueue) popOrWaitLocked(now time.Time) (*Job, chan struct{}) {
	for len(q.items) > 0 {
		job := q.items[0]
		q.items[0] = nil
		q.items = q.items[1:]
		q.count.Add(-1)
		if job == nil || job.ExpiredAt(now) {
			continue
		}
		return job, nil
	}
	return nil, q.notify
}

func (q *jobQueue) pruneExpiredLocked(now time.Time) {
	if len(q.items) == 0 {
		return
	}
	filtered := q.items[:0]
	for _, job := range q.items {
		if job == nil || job.ExpiredAt(now) {
			continue
		}
		filtered = append(filtered, job)
	}
	for i := len(filtered); i < len(q.items); i++ {
		q.items[i] = nil
	}
	q.items = filtered
	q.count.Store(int64(len(filtered)))
}

func (q *jobQueue) signalLocked() {
	close(q.notify)
	q.notify = make(chan struct{})
}

func (j *Job) ExpiredAt(now time.Time) bool {
	if j == nil || j.DeadlineAt.IsZero() {
		return false
	}
	return !now.Before(j.DeadlineAt)
}

type waiterEntry struct {
	ClientID string
	ResultCh chan JobResult
}

type completedEntry struct {
	ClientID   string
	State      string
	FinishedAt time.Time
}

type SubmitOutcome struct {
	Delivered bool
	Duplicate bool
	Late      bool
}

var (
	ErrResultClientMismatch = errors.New("request result client mismatch")
	ErrResultNotWaiting     = errors.New("request result not waiting")
	ErrNoOnlineClient       = errors.New("no online client in group")
	ErrPreferredClientDown  = errors.New("preferred client not online")
	ErrClientQueueFull      = errors.New("client queue is full")
	ErrGroupSaturated       = errors.New("all online clients in group are saturated")
	ErrClientSessionGone    = errors.New("client session not found")
)

type Hub struct {
	mu                 sync.RWMutex
	pendingSize        int
	defaultMaxInFlight int
	sessions           map[string]*ClientSession
	groups             map[string]map[string]*ClientSession
	groupOrder         map[string][]string
	groupCursor        map[string]int
	waiters            map[string]waiterEntry
	completed          map[string]completedEntry
}

func NewHub(pendingSize, defaultMaxInFlight int) *Hub {
	if pendingSize <= 0 {
		pendingSize = 2048
	}
	if defaultMaxInFlight <= 0 {
		defaultMaxInFlight = 256
	}
	return &Hub{
		pendingSize:        pendingSize,
		defaultMaxInFlight: defaultMaxInFlight,
		sessions:           map[string]*ClientSession{},
		groups:             map[string]map[string]*ClientSession{},
		groupOrder:         map[string][]string{},
		groupCursor:        map[string]int{},
		waiters:            map[string]waiterEntry{},
		completed:          map[string]completedEntry{},
	}
}

func (h *Hub) Register(clientID, group string, userID int64, platform string, maxInFlight int) *ClientSession {
	h.mu.Lock()
	defer h.mu.Unlock()

	maxInFlight = h.normalizeMaxInFlight(maxInFlight)
	if existing, ok := h.sessions[clientID]; ok {
		if existing.Group != group {
			h.removeClientFromGroup(existing.Group, clientID)
		}
		existing.Group = group
		existing.UserID = userID
		existing.Platform = platform
		existing.LastSeenAt = time.Now()
		existing.MaxInFlight = maxInFlight
		h.ensureGroup(group)[clientID] = existing
		h.ensureOrder(group, clientID)
		h.signalDispatchLocked(existing)
		return existing
	}

	session := &ClientSession{
		ClientID:      clientID,
		Group:         group,
		UserID:        userID,
		Platform:      platform,
		LastSeenAt:    time.Now(),
		Pending:       newJobQueue(h.pendingSize),
		MaxInFlight:   maxInFlight,
		dispatchReady: make(chan struct{}),
	}
	h.sessions[clientID] = session
	h.ensureGroup(group)[clientID] = session
	h.ensureOrder(group, clientID)
	return session
}

func (h *Hub) Touch(clientID string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if session, ok := h.sessions[clientID]; ok {
		session.LastSeenAt = time.Now()
	}
}

func (h *Hub) Session(clientID string) (*ClientSession, bool) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	session, ok := h.sessions[clientID]
	return session, ok
}

func (h *Hub) Requeue(clientID string, job *Job) error {
	if job == nil {
		return nil
	}
	h.mu.RLock()
	session, ok := h.sessions[clientID]
	h.mu.RUnlock()
	if !ok {
		return ErrClientSessionGone
	}
	return session.Pending.Push(job)
}

func (h *Hub) Unregister(clientID string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	session, ok := h.sessions[clientID]
	if !ok {
		return
	}
	delete(h.sessions, clientID)
	h.removeClientFromGroup(session.Group, clientID)
	h.signalDispatchLocked(session)
}

func (h *Hub) Invoke(ctx context.Context, group, preferredClient string, job *Job) (JobResult, string, error) {
	session, err := h.pickSession(group, preferredClient)
	if err != nil {
		return JobResult{}, "", err
	}
	job.ClientID = session.ClientID

	waiter := make(chan JobResult, 1)
	h.storeWaiter(job.RequestID, session.ClientID, waiter)

	if ctx.Err() != nil {
		h.dropWaiter(job.RequestID)
		return JobResult{}, session.ClientID, ctx.Err()
	}
	if err := session.Pending.Push(job); err != nil {
		h.dropWaiter(job.RequestID)
		return JobResult{}, session.ClientID, err
	}

	select {
	case result := <-waiter:
		return result, session.ClientID, nil
	case <-ctx.Done():
		h.expireWaiter(job.RequestID)
		return JobResult{}, session.ClientID, ctx.Err()
	}
}

func (h *Hub) Poll(ctx context.Context, clientID string, wait time.Duration) (*Job, error) {
	session, err := h.AcquireDispatchSlot(ctx, clientID)
	if err != nil {
		return nil, err
	}

	job, err := session.Pending.Poll(ctx, wait)
	if err != nil {
		h.ReleaseDispatchSlot(clientID)
		return nil, err
	}
	if job == nil {
		h.ReleaseDispatchSlot(clientID)
		return nil, nil
	}
	return job, nil
}

func (h *Hub) AcquireDispatchSlot(ctx context.Context, clientID string) (*ClientSession, error) {
	for {
		h.mu.Lock()
		session, ok := h.sessions[clientID]
		if !ok {
			h.mu.Unlock()
			return nil, ErrClientSessionGone
		}
		if session.InFlight < session.MaxInFlight {
			session.InFlight++
			h.mu.Unlock()
			return session, nil
		}
		waitCh := session.dispatchReady
		h.mu.Unlock()

		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-waitCh:
		}
	}
}

func (h *Hub) SubmitResult(clientID string, result JobResult) (SubmitOutcome, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.cleanupCompletedLocked(time.Now())

	if waiter, ok := h.waiters[result.RequestID]; ok {
		if waiter.ClientID != clientID {
			return SubmitOutcome{}, ErrResultClientMismatch
		}
		delete(h.waiters, result.RequestID)
		h.releaseDispatchLocked(clientID)
		h.completed[result.RequestID] = completedEntry{
			ClientID:   clientID,
			State:      "completed",
			FinishedAt: time.Now(),
		}
		select {
		case waiter.ResultCh <- result:
		default:
		}
		return SubmitOutcome{Delivered: true}, nil
	}

	if completed, ok := h.completed[result.RequestID]; ok {
		if completed.ClientID != clientID {
			return SubmitOutcome{}, ErrResultClientMismatch
		}
		switch completed.State {
		case "completed":
			return SubmitOutcome{Duplicate: true}, nil
		case "expired":
			h.releaseDispatchLocked(clientID)
			h.completed[result.RequestID] = completedEntry{
				ClientID:   clientID,
				State:      "late",
				FinishedAt: time.Now(),
			}
			return SubmitOutcome{Late: true}, nil
		case "late":
			return SubmitOutcome{Duplicate: true}, nil
		default:
			return SubmitOutcome{Duplicate: true}, nil
		}
	}

	return SubmitOutcome{}, ErrResultNotWaiting
}

func (h *Hub) OnlineClients() []ClientSession {
	h.mu.RLock()
	defer h.mu.RUnlock()

	result := make([]ClientSession, 0, len(h.sessions))
	for _, item := range h.sessions {
		result = append(result, *item)
	}
	return result
}

func (h *Hub) ensureGroup(group string) map[string]*ClientSession {
	groupSessions, ok := h.groups[group]
	if !ok {
		groupSessions = map[string]*ClientSession{}
		h.groups[group] = groupSessions
	}
	return groupSessions
}

func (h *Hub) ensureOrder(group, clientID string) {
	order := h.groupOrder[group]
	for _, existing := range order {
		if existing == clientID {
			return
		}
	}
	h.groupOrder[group] = append(order, clientID)
}

func (h *Hub) removeClientFromGroup(group, clientID string) {
	if groupSessions, ok := h.groups[group]; ok {
		delete(groupSessions, clientID)
		if len(groupSessions) == 0 {
			delete(h.groups, group)
		}
	}
	order := h.groupOrder[group]
	filtered := order[:0]
	for _, existing := range order {
		if existing != clientID {
			filtered = append(filtered, existing)
		}
	}
	if len(filtered) == 0 {
		delete(h.groupOrder, group)
		delete(h.groupCursor, group)
		return
	}
	h.groupOrder[group] = append([]string(nil), filtered...)
	if cursor := h.groupCursor[group]; cursor >= len(filtered) {
		h.groupCursor[group] = 0
	}
}

func (h *Hub) pickSession(group, preferredClient string) (*ClientSession, error) {
	h.mu.Lock()
	defer h.mu.Unlock()

	if preferredClient != "" {
		session, ok := h.sessions[preferredClient]
		if !ok {
			return nil, ErrPreferredClientDown
		}
		return session, nil
	}

	order := h.groupOrder[group]
	if len(order) == 0 {
		return nil, ErrNoOnlineClient
	}

	start := h.groupCursor[group]
	if start >= len(order) {
		start = 0
	}
	sawOnline := false
	for offset := 0; offset < len(order); offset++ {
		idx := (start + offset) % len(order)
		clientID := order[idx]
		session, ok := h.sessions[clientID]
		if !ok || session.Group != group {
			continue
		}
		sawOnline = true
		if session.Pending.Len() >= session.Pending.Cap() {
			continue
		}
		h.groupCursor[group] = (idx + 1) % len(order)
		return session, nil
	}
	if sawOnline {
		return nil, ErrGroupSaturated
	}
	return nil, ErrNoOnlineClient
}

func (h *Hub) storeWaiter(requestID, clientID string, resultCh chan JobResult) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.cleanupCompletedLocked(time.Now())
	h.waiters[requestID] = waiterEntry{ClientID: clientID, ResultCh: resultCh}
}

func (h *Hub) dropWaiter(requestID string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	delete(h.waiters, requestID)
}

func (h *Hub) expireWaiter(requestID string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	waiter, ok := h.waiters[requestID]
	if !ok {
		return
	}
	delete(h.waiters, requestID)
	h.completed[requestID] = completedEntry{
		ClientID:   waiter.ClientID,
		State:      "expired",
		FinishedAt: time.Now(),
	}
	h.cleanupCompletedLocked(time.Now())
}

func (h *Hub) cleanupCompletedLocked(now time.Time) {
	const retention = 10 * time.Minute
	for requestID, item := range h.completed {
		if now.Sub(item.FinishedAt) > retention {
			delete(h.completed, requestID)
		}
	}
}

func (h *Hub) normalizeMaxInFlight(maxInFlight int) int {
	if maxInFlight <= 0 {
		return h.defaultMaxInFlight
	}
	if maxInFlight > 1024 {
		return 1024
	}
	return maxInFlight
}

func (h *Hub) ReleaseDispatchSlot(clientID string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.releaseDispatchLocked(clientID)
}

func (h *Hub) releaseDispatchLocked(clientID string) {
	session, ok := h.sessions[clientID]
	if !ok {
		return
	}
	if session.InFlight > 0 {
		session.InFlight--
	}
	h.signalDispatchLocked(session)
}

func (h *Hub) signalDispatchLocked(session *ClientSession) {
	if session == nil || session.dispatchReady == nil {
		return
	}
	close(session.dispatchReady)
	session.dispatchReady = make(chan struct{})
}
