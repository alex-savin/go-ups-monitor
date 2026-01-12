// Package server provides an HTTP/WebSocket server for UPS monitoring.
package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"

	"github.com/alex-savin/go-ups-monitor/config"
	"github.com/alex-savin/go-ups-monitor/monitor"
)

// Config holds server configuration options.
type Config struct {
	// Host is the address to bind to.
	Host string
	// Port is the port to listen on.
	Port int
	// WriteTimeout is the timeout for WebSocket writes.
	WriteTimeout time.Duration
	// ReadTimeout is the timeout for WebSocket reads.
	ReadTimeout time.Duration
	// PingInterval is the interval between WebSocket pings.
	PingInterval time.Duration
	// PongWait is the timeout for pong responses.
	PongWait time.Duration
	// MaxMessageSize is the maximum WebSocket message size.
	MaxMessageSize int64
	// BroadcastInterval is the interval between status broadcasts.
	BroadcastInterval time.Duration
	// ShutdownTimeout is the graceful shutdown timeout.
	ShutdownTimeout time.Duration
	// Logger is the logger to use.
	Logger *slog.Logger
}

// DefaultConfig returns the default server configuration.
func DefaultConfig() Config {
	return Config{
		Host:              "0.0.0.0",
		Port:              8080,
		WriteTimeout:      5 * time.Second,
		ReadTimeout:       60 * time.Second,
		PingInterval:      30 * time.Second,
		PongWait:          10 * time.Second,
		MaxMessageSize:    1 << 20, // 1MB
		BroadcastInterval: 5 * time.Second,
		ShutdownTimeout:   10 * time.Second,
		Logger:            slog.Default(),
	}
}

// Server is an HTTP/WebSocket server for UPS monitoring.
type Server struct {
	cfg        Config
	appConfig  *config.Config
	configPath string
	configMu   sync.Mutex
	monitor    *monitor.Monitor
	hub        *wsHub
	httpServer *http.Server
	logger     *slog.Logger
	activeWS   int64
}

// New creates a new Server with the given configuration.
func New(cfg Config, appConfig *config.Config, configPath string, mon *monitor.Monitor) *Server {
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}
	s := &Server{
		cfg:        cfg,
		appConfig:  appConfig,
		configPath: configPath,
		monitor:    mon,
		hub:        newWSHub(cfg),
		logger:     cfg.Logger,
	}
	return s
}

// Start starts the server and blocks until the context is cancelled.
func (s *Server) Start(ctx context.Context) error {
	// Start status broadcaster
	go s.statusBroadcaster(ctx)

	// Set up routes
	mux := http.NewServeMux()
	mux.HandleFunc("/ws", s.handleWebSocket)
	mux.HandleFunc("/health", s.handleHealth)
	mux.HandleFunc("/api/status", s.handleStatuses)
	mux.HandleFunc("/api/commands", s.handleCommands)
	mux.HandleFunc("/api/command", s.handleExecuteCommand)
	mux.HandleFunc("/api/device", s.handleDevice(ctx))
	mux.HandleFunc("/api/device/test", s.handleTestDevice)
	mux.HandleFunc("/api/devices", s.handleListDevices)

	addr := fmt.Sprintf("%s:%d", s.cfg.Host, s.cfg.Port)
	s.httpServer = &http.Server{
		Addr:    addr,
		Handler: s.loggingMiddleware(mux),
	}

	// Initial broadcast
	if payload, err := s.buildStatusPayload(); err == nil {
		s.hub.broadcast(payload)
	}

	// Start server in goroutine
	errCh := make(chan error, 1)
	go func() {
		s.logger.Info("Starting server", "host", s.cfg.Host, "port", s.cfg.Port)
		if err := s.httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
		close(errCh)
	}()

	// Wait for context cancellation or error
	select {
	case <-ctx.Done():
		s.logger.Info("Shutting down server...")
	case err := <-errCh:
		return err
	}

	// Graceful shutdown
	s.hub.closeAll()
	shutdownCtx, cancel := context.WithTimeout(context.Background(), s.cfg.ShutdownTimeout)
	defer cancel()
	if err := s.httpServer.Shutdown(shutdownCtx); err != nil {
		s.logger.Warn("Graceful shutdown failed", "error", err)
		return err
	}
	s.logger.Info("Server stopped gracefully")
	return nil
}

// Addr returns the server address.
func (s *Server) Addr() string {
	return fmt.Sprintf("%s:%d", s.cfg.Host, s.cfg.Port)
}

func (s *Server) loggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.logger.Info("HTTP request", "method", r.Method, "path", r.URL.Path, "remote", r.RemoteAddr, "host", r.Host)
		next.ServeHTTP(w, r)
	})
}

func (s *Server) buildStatusPayload() ([]byte, error) {
	s.configMu.Lock()
	devices := make([]config.Device, len(s.appConfig.Devices))
	copy(devices, s.appConfig.Devices)
	s.configMu.Unlock()
	return json.Marshal(config.GetStatusesForDevices(devices))
}

func (s *Server) statusBroadcaster(ctx context.Context) {
	ticker := time.NewTicker(s.cfg.BroadcastInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			s.logger.Info("Status broadcaster stopped")
			return
		case <-ticker.C:
			payload, err := s.buildStatusPayload()
			if err != nil {
				s.logger.Error("Failed to build status payload", "error", err)
				continue
			}
			s.hub.broadcast(payload)
		}
	}
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(`{"status": "healthy", "timestamp": "` + time.Now().Format(time.RFC3339) + `"}`))
}

func (s *Server) handleStatuses(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	s.configMu.Lock()
	devices := make([]config.Device, len(s.appConfig.Devices))
	copy(devices, s.appConfig.Devices)
	s.configMu.Unlock()
	if err := json.NewEncoder(w).Encode(config.GetStatusesForDevices(devices)); err != nil {
		s.logger.Error("Failed to encode statuses", "error", err)
		w.WriteHeader(http.StatusInternalServerError)
	}
}

func (s *Server) handleCommands(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	deviceType := r.URL.Query().Get("type")
	if deviceType == "" {
		deviceType = "nut"
	}
	if err := json.NewEncoder(w).Encode(config.GetSupportedCommands(deviceType)); err != nil {
		s.logger.Error("Failed to encode commands", "error", err)
		w.WriteHeader(http.StatusInternalServerError)
	}
}

func (s *Server) handleExecuteCommand(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		_, _ = w.Write([]byte(`{"error":"method not allowed"}`))
		return
	}
	var cmd config.UPSCommand
	if err := json.NewDecoder(r.Body).Decode(&cmd); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":"invalid request body"}`))
		return
	}
	if cmd.Device == "" || cmd.Command == "" {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":"device and command are required"}`))
		return
	}
	s.configMu.Lock()
	devices := make([]config.Device, len(s.appConfig.Devices))
	copy(devices, s.appConfig.Devices)
	s.configMu.Unlock()
	result := config.ExecuteCommand(cmd, devices)
	s.logger.Info("HTTP command executed", "device", cmd.Device, "command", cmd.Command, "success", result.Success)
	if !result.Success {
		w.WriteHeader(http.StatusBadRequest)
	}
	_ = json.NewEncoder(w).Encode(result)
}

func (s *Server) handleListDevices(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	s.configMu.Lock()
	devices := make([]config.Device, len(s.appConfig.Devices))
	copy(devices, s.appConfig.Devices)
	s.configMu.Unlock()
	if err := json.NewEncoder(w).Encode(devices); err != nil {
		s.logger.Error("Failed to encode devices", "error", err)
		w.WriteHeader(http.StatusInternalServerError)
	}
}

// AddDeviceRequest is the request body for adding a device.
type AddDeviceRequest struct {
	Type               string   `json:"type"`
	Name               string   `json:"name"`
	Host               string   `json:"host"`
	Port               int      `json:"port"`
	Username           string   `json:"username"`
	Password           string   `json:"password"`
	SelectedAttributes []string `json:"selected_attributes"`
}

func (s *Server) handleDevice(ctx context.Context) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.Method {
		case http.MethodPost:
			s.handleAddDevice(ctx, w, r)
		case http.MethodDelete:
			s.handleDeleteDevice(w, r)
		default:
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
	}
}

func (s *Server) handleAddDevice(ctx context.Context, w http.ResponseWriter, r *http.Request) {
	var req AddDeviceRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":"invalid request"}`))
		return
	}
	if req.Name == "" || req.Host == "" || req.Type == "" {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":"missing required fields"}`))
		return
	}
	device := config.Device{
		Type:               req.Type,
		Name:               req.Name,
		Host:               req.Host,
		Port:               req.Port,
		Username:           req.Username,
		Password:           req.Password,
		SelectedAttributes: req.SelectedAttributes,
	}
	if device.Port == 0 {
		switch device.Type {
		case "apcupsd":
			device.Port = 3551
		case "nut":
			device.Port = 3493
		default:
			w.WriteHeader(http.StatusBadRequest)
			w.Write([]byte(`{"error":"unsupported device type"}`))
			return
		}
	}
	// Test connection
	status, err := monitor.FetchStatus(device)
	if err != nil {
		s.logger.Error("Device validation failed", "name", device.Name, "type", device.Type, "error", err)
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(`{"error":"failed to connect to device"}`))
		return
	}
	s.configMu.Lock()
	config.AddOrUpdateDevice(s.appConfig, device)
	if err := config.SaveConfigTo(s.configPath, s.appConfig); err != nil {
		s.configMu.Unlock()
		s.logger.Error("Failed to save config", "error", err)
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(`{"error":"failed to save config"}`))
		return
	}
	s.configMu.Unlock()
	config.UpdateStatus(device.Name, status)
	if s.monitor != nil {
		s.monitor.StartDevice(ctx, device)
	}
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(device)
}

func (s *Server) handleDeleteDevice(w http.ResponseWriter, r *http.Request) {
	name := r.URL.Query().Get("name")
	if name == "" {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(`{"error":"name required"}`))
		return
	}
	s.configMu.Lock()
	removed := config.RemoveDevice(s.appConfig, name)
	if removed {
		if err := config.SaveConfigTo(s.configPath, s.appConfig); err != nil {
			s.configMu.Unlock()
			s.logger.Error("Failed to save config", "error", err)
			w.WriteHeader(http.StatusInternalServerError)
			w.Write([]byte(`{"error":"failed to save config"}`))
			return
		}
	}
	s.configMu.Unlock()
	if !removed {
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte(`{"error":"device not found"}`))
		return
	}
	if s.monitor != nil {
		s.monitor.StopDevice(name)
	}
	config.DeleteStatus(name)
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(`{"status":"deleted"}`))
}

func (s *Server) handleTestDevice(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	var req AddDeviceRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"success": false, "error": "invalid request"})
		return
	}
	if req.Name == "" || req.Host == "" || req.Type == "" {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"success": false, "error": "missing required fields"})
		return
	}
	device := config.Device{
		Type:     req.Type,
		Name:     req.Name,
		Host:     req.Host,
		Port:     req.Port,
		Username: req.Username,
		Password: req.Password,
	}
	if device.Port == 0 {
		switch device.Type {
		case "apcupsd":
			device.Port = 3551
		case "nut":
			device.Port = 3493
		default:
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"success": false, "error": "unsupported device type"})
			return
		}
	}
	status, err := monitor.FetchStatus(device)
	if err != nil {
		s.logger.Warn("Device test failed", "name", device.Name, "type", device.Type, "error", err)
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"success": false, "error": err.Error()})
		return
	}
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success":    true,
		"attributes": status.Attributes,
	})
}

func (s *Server) handleWebSocket(w http.ResponseWriter, r *http.Request) {
	s.logger.Info("WebSocket upgrade attempt", "remote", r.RemoteAddr, "origin", r.Header.Get("Origin"))
	conn, err := s.hub.upgrader.Upgrade(w, r, nil)
	if err != nil {
		s.logger.Error("WebSocket upgrade failed", "remote", r.RemoteAddr, "origin", r.Header.Get("Origin"), "error", err)
		return
	}
	s.logger.Info("WebSocket upgrade succeeded", "remote", r.RemoteAddr, "origin", r.Header.Get("Origin"))
	conn.SetReadLimit(s.cfg.MaxMessageSize)
	_ = conn.SetReadDeadline(time.Now().Add(s.cfg.ReadTimeout))
	conn.SetPongHandler(func(string) error {
		_ = conn.SetReadDeadline(time.Now().Add(s.cfg.ReadTimeout))
		return nil
	})
	reason := "normal"
	count := atomic.AddInt64(&s.activeWS, 1)
	s.logger.Info("WebSocket client connected", "remote", r.RemoteAddr, "active", count)
	s.hub.add(conn)
	done := make(chan struct{})
	go func() {
		ticker := time.NewTicker(s.cfg.PingInterval)
		defer ticker.Stop()
		for {
			select {
			case <-done:
				return
			case <-ticker.C:
				deadline := time.Now().Add(s.cfg.PongWait)
				if err := conn.WriteControl(websocket.PingMessage, []byte("ping"), deadline); err != nil {
					s.logger.Warn("WebSocket ping failed", "error", err)
					reason = "ping_failed"
					_ = conn.Close()
					return
				}
			}
		}
	}()
	defer func() {
		close(done)
		s.hub.remove(conn)
		remaining := atomic.AddInt64(&s.activeWS, -1)
		s.logger.Info("WebSocket client disconnected", "remote", r.RemoteAddr, "reason", reason, "active", remaining)
	}()
	// Send initial status
	if err := s.hub.sendLast(conn); err != nil {
		s.logger.Error("Failed to send cached status", "error", err)
		reason = "write_initial_failed"
		return
	}
	if s.hub.last == nil {
		s.configMu.Lock()
		devices := make([]config.Device, len(s.appConfig.Devices))
		copy(devices, s.appConfig.Devices)
		s.configMu.Unlock()
		payload, marshalErr := json.Marshal(config.GetStatusesForDevices(devices))
		if marshalErr != nil {
			s.logger.Error("Failed to marshal initial status", "error", marshalErr)
			reason = "marshal_failed"
			return
		}
		_ = conn.SetWriteDeadline(time.Now().Add(s.cfg.WriteTimeout))
		if writeErr := conn.WriteMessage(websocket.TextMessage, payload); writeErr != nil {
			s.logger.Error("Failed to send initial status", "error", writeErr)
			reason = "write_initial_failed"
			return
		}
		_ = conn.SetWriteDeadline(time.Time{})
	}
	for {
		var cmd config.UPSCommand
		if err := conn.ReadJSON(&cmd); err != nil {
			if websocket.IsCloseError(err, websocket.CloseGoingAway, websocket.CloseNormalClosure) || errors.Is(err, net.ErrClosed) {
				reason = "client_close"
				return
			}
			if netErr, ok := err.(interface{ Timeout() bool }); ok && netErr.Timeout() {
				reason = "read_timeout"
				return
			}
			s.logger.Error("Failed to read command", "error", err)
			reason = "read_failed"
			return
		}
		s.logger.Info("WS received command", "remote", r.RemoteAddr, "id", cmd.ID, "device", cmd.Device, "command", cmd.Command)
		result := config.ExecuteCommand(cmd, s.appConfig.Devices)
		s.logger.Info("WS send command result", "remote", r.RemoteAddr, "id", cmd.ID, "device", cmd.Device, "success", result.Success)
		_ = conn.SetWriteDeadline(time.Now().Add(s.cfg.WriteTimeout))
		if err := conn.WriteJSON(result); err != nil {
			s.logger.Error("Failed to send command result", "error", err)
			reason = "write_failed"
			return
		}
		_ = conn.SetWriteDeadline(time.Time{})
	}
}

// wsHub manages WebSocket connections.
type wsHub struct {
	mu       sync.Mutex
	clients  map[*websocket.Conn]struct{}
	last     []byte
	upgrader websocket.Upgrader
	cfg      Config
}

func newWSHub(cfg Config) *wsHub {
	return &wsHub{
		clients: make(map[*websocket.Conn]struct{}),
		upgrader: websocket.Upgrader{
			ReadBufferSize:  4096,
			WriteBufferSize: 4096,
			CheckOrigin:     func(r *http.Request) bool { return true },
		},
		cfg: cfg,
	}
}

func (h *wsHub) add(conn *websocket.Conn) {
	h.mu.Lock()
	h.clients[conn] = struct{}{}
	h.mu.Unlock()
}

func (h *wsHub) remove(conn *websocket.Conn) {
	h.mu.Lock()
	delete(h.clients, conn)
	h.mu.Unlock()
	_ = conn.Close()
}

func (h *wsHub) broadcast(payload []byte) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.last = payload
	for conn := range h.clients {
		_ = conn.SetWriteDeadline(time.Now().Add(h.cfg.WriteTimeout))
		if err := conn.WriteMessage(websocket.TextMessage, payload); err != nil {
			_ = conn.Close()
			delete(h.clients, conn)
		}
		_ = conn.SetWriteDeadline(time.Time{})
	}
}

func (h *wsHub) sendLast(conn *websocket.Conn) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.last == nil {
		return nil
	}
	_ = conn.SetWriteDeadline(time.Now().Add(h.cfg.WriteTimeout))
	err := conn.WriteMessage(websocket.TextMessage, h.last)
	_ = conn.SetWriteDeadline(time.Time{})
	return err
}

func (h *wsHub) closeAll() {
	h.mu.Lock()
	defer h.mu.Unlock()
	for conn := range h.clients {
		_ = conn.WriteControl(
			websocket.CloseMessage,
			websocket.FormatCloseMessage(websocket.CloseGoingAway, "server shutdown"),
			time.Now().Add(h.cfg.WriteTimeout),
		)
		_ = conn.Close()
		delete(h.clients, conn)
	}
}
