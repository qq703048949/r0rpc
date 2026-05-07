package web

import (
	"bufio"
	"context"
	"crypto/sha1"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"r0rpc/internal/auth"
	"r0rpc/internal/rpc"
)

const (
	wsGUID            = "258EAFA5-E914-47DA-95CA-C5AB0DC85B11"
	wsMaxMessageBytes = 4 << 20
)

type wsEnvelope struct {
	Type        string         `json:"type"`
	Job         *rpc.Job       `json:"job,omitempty"`
	Result      *rpc.JobResult `json:"result,omitempty"`
	RequestID   string         `json:"requestId,omitempty"`
	OK          bool           `json:"ok,omitempty"`
	State       string         `json:"state,omitempty"`
	Error       string         `json:"error,omitempty"`
	ClientID    string         `json:"clientId,omitempty"`
	Group       string         `json:"group,omitempty"`
	ServerID    string         `json:"serverId,omitempty"`
	Time        string         `json:"time,omitempty"`
	MaxInFlight int            `json:"maxInFlight,omitempty"`
}

type clientWSSessions struct {
	mu     sync.Mutex
	nextID uint64
	items  map[string]*clientWSBinding
}

type clientWSBinding struct {
	connID uint64
	cancel context.CancelFunc
	conn   *wsConn
}

func newClientWSSessions() *clientWSSessions {
	return &clientWSSessions{items: map[string]*clientWSBinding{}}
}

func (m *clientWSSessions) replace(clientID string, conn *wsConn, cancel context.CancelFunc) uint64 {
	m.mu.Lock()
	m.nextID++
	connID := m.nextID
	previous := m.items[clientID]
	m.items[clientID] = &clientWSBinding{connID: connID, cancel: cancel, conn: conn}
	m.mu.Unlock()

	if previous != nil {
		previous.cancel()
		_ = previous.conn.Close()
	}
	return connID
}

func (m *clientWSSessions) clearIfCurrent(clientID string, connID uint64) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	current, ok := m.items[clientID]
	if !ok || current.connID != connID {
		return false
	}
	delete(m.items, clientID)
	return true
}

func (s *Server) handleClientWS(w http.ResponseWriter, r *http.Request) {
	claims, err := s.verifyClientWSClaims(r)
	if err != nil {
		writeError(w, http.StatusUnauthorized, err)
		return
	}

	conn, err := upgradeWebSocket(w, r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}

	ip := s.App.RemoteIP(r)
	session := s.App.Hub.Register(claims.ClientID, claims.Group, claims.UserID, "websocket", claims.MaxInFlight)
	s.App.TouchClientPresence(context.Background(), claims.ClientID, claims.Group, claims.UserID, claims.MaxInFlight, "websocket", ip)

	ctx, cancel := context.WithCancel(context.Background())
	connID := s.wsClients.replace(claims.ClientID, conn, cancel)

	defer func() {
		cancel()
		_ = conn.Close()
		if s.wsClients.clearIfCurrent(claims.ClientID, connID) {
			s.App.Hub.Unregister(claims.ClientID)
			_ = s.App.Store.MarkDeviceOffline(context.Background(), claims.ClientID)
		}
	}()

	if err := conn.WriteJSON(wsEnvelope{
		Type:        "welcome",
		ClientID:    claims.ClientID,
		Group:       claims.Group,
		ServerID:    s.App.Config.ServerID,
		Time:        time.Now().Format(time.RFC3339),
		MaxInFlight: session.MaxInFlight,
	}); err != nil {
		return
	}

	writerDone := make(chan error, 1)
	go func() {
		writerDone <- s.clientWSWriterLoop(ctx, claims, session, conn)
	}()
	pingDone := make(chan error, 1)
	go func() {
		pingDone <- s.clientWSPingLoop(ctx, conn)
	}()

	readerErr := s.clientWSReaderLoop(ctx, claims, conn, ip)
	cancel()
	writerErr := <-writerDone
	pingErr := <-pingDone
	if readerErr != nil && !isExpectedWSError(readerErr) {
		fmt.Printf("client ws reader closed: client=%s err=%v\n", claims.ClientID, readerErr)
	}
	if writerErr != nil && !isExpectedWSError(writerErr) {
		fmt.Printf("client ws writer closed: client=%s err=%v\n", claims.ClientID, writerErr)
	}
	if pingErr != nil && !isExpectedWSError(pingErr) {
		fmt.Printf("client ws ping closed: client=%s err=%v\n", claims.ClientID, pingErr)
	}
}

func (s *Server) clientWSWriterLoop(ctx context.Context, claims *auth.Claims, session *rpc.ClientSession, conn *wsConn) error {
	for {
		if ctx.Err() != nil {
			return nil
		}
		acquiredSession, err := s.App.Hub.AcquireDispatchSlot(ctx, claims.ClientID)
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return err
		}

		job, err := acquiredSession.Pending.Pop(ctx)
		if err != nil {
			s.App.Hub.ReleaseDispatchSlot(claims.ClientID)
			if ctx.Err() != nil {
				return nil
			}
			return err
		}
		if job == nil {
			s.App.Hub.ReleaseDispatchSlot(claims.ClientID)
			continue
		}
		if ctx.Err() != nil {
			s.App.Hub.ReleaseDispatchSlot(claims.ClientID)
			_ = s.App.Hub.Requeue(claims.ClientID, job)
			return nil
		}
		if err := conn.WriteJSON(wsEnvelope{Type: "job", Job: job}); err != nil {
			s.App.Hub.ReleaseDispatchSlot(claims.ClientID)
			_ = s.App.Hub.Requeue(claims.ClientID, job)
			return err
		}
	}
}

func (s *Server) clientHeartbeatTimeout() time.Duration {
	seconds := s.App.Config.DeviceOfflineSeconds
	if seconds <= 0 {
		if s.App.Config.DeviceOfflineMinutes > 0 {
			seconds = s.App.Config.DeviceOfflineMinutes * 60
		} else {
			seconds = 20
		}
	}
	return time.Duration(seconds) * time.Second
}

func (s *Server) clientWSPingInterval() time.Duration {
	seconds := s.App.Config.HeartbeatIntervalSeconds
	if seconds <= 0 {
		seconds = 5
	}
	return time.Duration(seconds) * time.Second
}

func (s *Server) clientWSPingLoop(ctx context.Context, conn *wsConn) error {
	ticker := time.NewTicker(s.clientWSPingInterval())
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			if err := conn.WritePing(nil); err != nil {
				return err
			}
		}
	}
}

func (s *Server) clientWSReaderLoop(ctx context.Context, claims *auth.Claims, conn *wsConn, ip string) error {
	heartbeatTimeout := s.clientHeartbeatTimeout()
	for {
		if err := conn.SetReadDeadline(time.Now().Add(heartbeatTimeout)); err != nil {
			return err
		}
		messageType, payload, err := conn.ReadMessage()
		if err != nil {
			return err
		}
		if ctx.Err() != nil {
			return nil
		}
		s.App.TouchClientPresence(context.Background(), claims.ClientID, claims.Group, claims.UserID, claims.MaxInFlight, "", ip)

		switch messageType {
		case wsMessageText:
			var envelope wsEnvelope
			if err := json.Unmarshal(payload, &envelope); err != nil {
				_ = conn.WriteJSON(wsEnvelope{Type: "error", Error: "invalid json payload"})
				continue
			}
			switch envelope.Type {
			case "heartbeat":
				if err := conn.WriteJSON(wsEnvelope{Type: "heartbeatAck", Time: time.Now().Format(time.RFC3339)}); err != nil {
					return err
				}
			case "result":
				if envelope.Result == nil {
					if err := conn.WriteJSON(wsEnvelope{Type: "resultAck", OK: false, Error: "result payload required"}); err != nil {
						return err
					}
					continue
				}
				if strings.TrimSpace(envelope.Result.Status) == "" {
					envelope.Result.Status = "success"
				}
				if err := normalizeClientJobResult(envelope.Result); err != nil {
					ack := wsEnvelope{Type: "resultAck", RequestID: envelope.Result.RequestID, OK: false, Error: err.Error(), State: "rejected"}
					if writeErr := conn.WriteJSON(ack); writeErr != nil {
						return writeErr
					}
					continue
				}
				err := s.App.SubmitClientResult(context.Background(), claims, *envelope.Result)
				ack := wsEnvelope{Type: "resultAck", RequestID: envelope.Result.RequestID, OK: err == nil}
				if err != nil {
					ack.Error = err.Error()
					if errors.Is(err, rpc.ErrResultClientMismatch) {
						ack.State = "rejected"
					} else {
						ack.State = "error"
					}
				} else {
					ack.State = "accepted"
				}
				if err := conn.WriteJSON(ack); err != nil {
					return err
				}
			default:
				if err := conn.WriteJSON(wsEnvelope{Type: "error", Error: "unsupported message type"}); err != nil {
					return err
				}
			}
		case wsMessagePong:
			continue
		default:
			continue
		}
	}
}

func (s *Server) verifyClientWSClaims(r *http.Request) (*auth.Claims, error) {
	var (
		claims *auth.Claims
		err    error
	)
	if token := strings.TrimSpace(r.URL.Query().Get("token")); token != "" {
		claims, err = s.App.Tokens.Parse(token)
	} else {
		claims, err = s.App.VerifyTokenFromRequest(r)
	}
	if err != nil {
		return nil, err
	}
	if claims.Role != "client" {
		return nil, fmt.Errorf("insufficient role")
	}
	return claims, nil
}

func (s *Server) clientWSURL(r *http.Request, token string) string {
	scheme := "ws"
	forwardedProto := strings.ToLower(strings.TrimSpace(r.Header.Get("X-Forwarded-Proto")))
	if forwardedProto == "https" || r.TLS != nil {
		scheme = "wss"
	}
	host := strings.TrimSpace(r.Header.Get("X-Forwarded-Host"))
	if host == "" {
		host = r.Host
	}
	if host == "" {
		host = "127.0.0.1"
	}
	return fmt.Sprintf("%s://%s/api/client/ws?token=%s", scheme, host, url.QueryEscape(token))
}

func isExpectedWSError(err error) bool {
	if err == nil {
		return true
	}
	if errors.Is(err, net.ErrClosed) || errors.Is(err, io.EOF) {
		return true
	}
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "use of closed network connection") || strings.Contains(message, "i/o timeout")
}

type wsConn struct {
	conn net.Conn
	rw   *bufio.ReadWriter
	mu   sync.Mutex
}

func upgradeWebSocket(w http.ResponseWriter, r *http.Request) (*wsConn, error) {
	if !headerHasToken(r.Header, "Connection", "upgrade") || !headerHasToken(r.Header, "Upgrade", "websocket") {
		return nil, fmt.Errorf("websocket upgrade headers required")
	}
	if strings.TrimSpace(r.Header.Get("Sec-WebSocket-Version")) != "13" {
		return nil, fmt.Errorf("unsupported websocket version")
	}
	key := strings.TrimSpace(r.Header.Get("Sec-WebSocket-Key"))
	if key == "" {
		return nil, fmt.Errorf("missing Sec-WebSocket-Key")
	}

	hijacker, ok := w.(http.Hijacker)
	if !ok {
		return nil, fmt.Errorf("server does not support websocket hijacking")
	}
	conn, rw, err := hijacker.Hijack()
	if err != nil {
		return nil, err
	}

	accept := websocketAccept(key)
	response := "HTTP/1.1 101 Switching Protocols\r\n" +
		"Upgrade: websocket\r\n" +
		"Connection: Upgrade\r\n" +
		"Sec-WebSocket-Accept: " + accept + "\r\n\r\n"
	if _, err := rw.WriteString(response); err != nil {
		_ = conn.Close()
		return nil, err
	}
	if err := rw.Flush(); err != nil {
		_ = conn.Close()
		return nil, err
	}
	return &wsConn{conn: conn, rw: rw}, nil
}

func headerHasToken(header http.Header, key, token string) bool {
	for _, value := range header.Values(key) {
		for _, part := range strings.Split(value, ",") {
			if strings.EqualFold(strings.TrimSpace(part), token) {
				return true
			}
		}
	}
	return false
}

func websocketAccept(key string) string {
	hash := sha1.Sum([]byte(key + wsGUID))
	return base64.StdEncoding.EncodeToString(hash[:])
}

const (
	wsMessageText  = 1
	wsMessagePing  = 9
	wsMessagePong  = 10
	wsMessageClose = 8
)

func (c *wsConn) Close() error {
	return c.conn.Close()
}

func (c *wsConn) SetReadDeadline(t time.Time) error {
	return c.conn.SetReadDeadline(t)
}

func (c *wsConn) WriteJSON(payload any) error {
	data, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	return c.writeFrame(wsMessageText, data)
}

func (c *wsConn) WritePong(payload []byte) error {
	return c.writeFrame(wsMessagePong, payload)
}

func (c *wsConn) WritePing(payload []byte) error {
	return c.writeFrame(wsMessagePing, payload)
}

func (c *wsConn) writeFrame(opcode byte, payload []byte) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if len(payload) > wsMaxMessageBytes {
		return fmt.Errorf("websocket payload too large")
	}
	if err := c.conn.SetWriteDeadline(time.Now().Add(15 * time.Second)); err != nil {
		return err
	}

	header := make([]byte, 0, 10)
	header = append(header, 0x80|opcode)
	switch {
	case len(payload) < 126:
		header = append(header, byte(len(payload)))
	case len(payload) <= 65535:
		header = append(header, 126, byte(len(payload)>>8), byte(len(payload)))
	default:
		header = append(header, 127)
		length := make([]byte, 8)
		binary.BigEndian.PutUint64(length, uint64(len(payload)))
		header = append(header, length...)
	}
	if _, err := c.rw.Write(header); err != nil {
		return err
	}
	if len(payload) > 0 {
		if _, err := c.rw.Write(payload); err != nil {
			return err
		}
	}
	return c.rw.Flush()
}

func (c *wsConn) ReadMessage() (byte, []byte, error) {
	for {
		head := make([]byte, 2)
		if _, err := io.ReadFull(c.rw, head); err != nil {
			return 0, nil, err
		}
		fin := head[0]&0x80 != 0
		opcode := head[0] & 0x0f
		masked := head[1]&0x80 != 0
		payloadLen := int(head[1] & 0x7f)

		if !fin {
			return 0, nil, fmt.Errorf("fragmented websocket frames are not supported")
		}

		switch payloadLen {
		case 126:
			ext := make([]byte, 2)
			if _, err := io.ReadFull(c.rw, ext); err != nil {
				return 0, nil, err
			}
			payloadLen = int(binary.BigEndian.Uint16(ext))
		case 127:
			ext := make([]byte, 8)
			if _, err := io.ReadFull(c.rw, ext); err != nil {
				return 0, nil, err
			}
			length := binary.BigEndian.Uint64(ext)
			if length > wsMaxMessageBytes {
				return 0, nil, fmt.Errorf("websocket payload too large")
			}
			payloadLen = int(length)
		}

		if payloadLen > wsMaxMessageBytes {
			return 0, nil, fmt.Errorf("websocket payload too large")
		}

		var maskKey [4]byte
		if masked {
			if _, err := io.ReadFull(c.rw, maskKey[:]); err != nil {
				return 0, nil, err
			}
		}

		payload := make([]byte, payloadLen)
		if payloadLen > 0 {
			if _, err := io.ReadFull(c.rw, payload); err != nil {
				return 0, nil, err
			}
		}
		if masked {
			for i := range payload {
				payload[i] ^= maskKey[i%4]
			}
		}

		switch opcode {
		case wsMessagePing:
			if err := c.WritePong(payload); err != nil {
				return 0, nil, err
			}
			continue
		case wsMessagePong:
			return wsMessagePong, payload, nil
		case wsMessageClose:
			return 0, nil, io.EOF
		case wsMessageText:
			return wsMessageText, payload, nil
		default:
			continue
		}
	}
}
