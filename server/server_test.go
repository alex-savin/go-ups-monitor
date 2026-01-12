package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/alex-savin/go-ups-monitor/config"
)

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()

	if cfg.Host != "0.0.0.0" {
		t.Errorf("expected Host '0.0.0.0', got '%s'", cfg.Host)
	}
	if cfg.Port != 8080 {
		t.Errorf("expected Port 8080, got %d", cfg.Port)
	}
	if cfg.WriteTimeout != 5*time.Second {
		t.Errorf("expected WriteTimeout 5s, got %v", cfg.WriteTimeout)
	}
	if cfg.ReadTimeout != 60*time.Second {
		t.Errorf("expected ReadTimeout 60s, got %v", cfg.ReadTimeout)
	}
	if cfg.PingInterval != 30*time.Second {
		t.Errorf("expected PingInterval 30s, got %v", cfg.PingInterval)
	}
	if cfg.PongWait != 10*time.Second {
		t.Errorf("expected PongWait 10s, got %v", cfg.PongWait)
	}
	if cfg.MaxMessageSize != 1<<20 {
		t.Errorf("expected MaxMessageSize 1MB, got %d", cfg.MaxMessageSize)
	}
	if cfg.BroadcastInterval != 5*time.Second {
		t.Errorf("expected BroadcastInterval 5s, got %v", cfg.BroadcastInterval)
	}
	if cfg.ShutdownTimeout != 10*time.Second {
		t.Errorf("expected ShutdownTimeout 10s, got %v", cfg.ShutdownTimeout)
	}
	if cfg.Logger == nil {
		t.Error("expected Logger to be set")
	}
}

func TestNew(t *testing.T) {
	appConfig := &config.Config{
		Devices: []config.Device{},
	}

	srv := New(DefaultConfig(), appConfig, "config.yml", nil)
	if srv == nil {
		t.Fatal("expected server to be created")
	}
	if srv.appConfig != appConfig {
		t.Error("expected appConfig to be set")
	}
	if srv.configPath != "config.yml" {
		t.Errorf("expected configPath 'config.yml', got '%s'", srv.configPath)
	}
}

func TestServer_Addr(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Host = "127.0.0.1"
	cfg.Port = 9000

	srv := New(cfg, &config.Config{}, "", nil)
	addr := srv.Addr()

	if addr != "127.0.0.1:9000" {
		t.Errorf("expected addr '127.0.0.1:9000', got '%s'", addr)
	}
}

func TestServer_HandleHealth(t *testing.T) {
	srv := New(DefaultConfig(), &config.Config{}, "", nil)

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	w := httptest.NewRecorder()

	srv.handleHealth(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected status 200, got %d", resp.StatusCode)
	}

	contentType := resp.Header.Get("Content-Type")
	if contentType != "application/json" {
		t.Errorf("expected Content-Type 'application/json', got '%s'", contentType)
	}

	var body map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if body["status"] != "healthy" {
		t.Errorf("expected status 'healthy', got '%v'", body["status"])
	}
	if _, ok := body["timestamp"]; !ok {
		t.Error("expected timestamp in response")
	}
}

func TestServer_HandleStatuses(t *testing.T) {
	appConfig := &config.Config{
		Devices: []config.Device{
			{Name: "test-ups", Type: "nut", Host: "localhost", Port: 3493},
		},
	}
	srv := New(DefaultConfig(), appConfig, "", nil)

	req := httptest.NewRequest(http.MethodGet, "/api/status", nil)
	w := httptest.NewRecorder()

	srv.handleStatuses(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected status 200, got %d", resp.StatusCode)
	}

	contentType := resp.Header.Get("Content-Type")
	if contentType != "application/json" {
		t.Errorf("expected Content-Type 'application/json', got '%s'", contentType)
	}
}

func TestServer_HandleCommands(t *testing.T) {
	srv := New(DefaultConfig(), &config.Config{}, "", nil)

	t.Run("default type is nut", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/commands", nil)
		w := httptest.NewRecorder()

		srv.handleCommands(w, req)

		resp := w.Result()
		if resp.StatusCode != http.StatusOK {
			t.Errorf("expected status 200, got %d", resp.StatusCode)
		}

		var commands []config.CommandInfo
		if err := json.NewDecoder(resp.Body).Decode(&commands); err != nil {
			t.Fatalf("failed to decode response: %v", err)
		}

		if len(commands) == 0 {
			t.Error("expected some commands")
		}
	})

	t.Run("with type parameter", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/commands?type=apcupsd", nil)
		w := httptest.NewRecorder()

		srv.handleCommands(w, req)

		resp := w.Result()
		if resp.StatusCode != http.StatusOK {
			t.Errorf("expected status 200, got %d", resp.StatusCode)
		}
	})
}

func TestServer_HandleExecuteCommand(t *testing.T) {
	appConfig := &config.Config{
		Devices: []config.Device{
			{Name: "test-ups", Type: "nut", Host: "localhost", Port: 3493},
		},
	}
	srv := New(DefaultConfig(), appConfig, "", nil)

	t.Run("method not allowed", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/command", nil)
		w := httptest.NewRecorder()

		srv.handleExecuteCommand(w, req)

		resp := w.Result()
		if resp.StatusCode != http.StatusMethodNotAllowed {
			t.Errorf("expected status 405, got %d", resp.StatusCode)
		}
	})

	t.Run("invalid request body", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/command", strings.NewReader("invalid"))
		w := httptest.NewRecorder()

		srv.handleExecuteCommand(w, req)

		resp := w.Result()
		if resp.StatusCode != http.StatusBadRequest {
			t.Errorf("expected status 400, got %d", resp.StatusCode)
		}
	})

	t.Run("missing required fields", func(t *testing.T) {
		body := `{"device": "", "command": ""}`
		req := httptest.NewRequest(http.MethodPost, "/api/command", strings.NewReader(body))
		w := httptest.NewRecorder()

		srv.handleExecuteCommand(w, req)

		resp := w.Result()
		if resp.StatusCode != http.StatusBadRequest {
			t.Errorf("expected status 400, got %d", resp.StatusCode)
		}
	})
}

func TestServer_HandleListDevices(t *testing.T) {
	appConfig := &config.Config{
		Devices: []config.Device{
			{Name: "ups1", Type: "nut", Host: "localhost", Port: 3493},
			{Name: "ups2", Type: "apcupsd", Host: "localhost", Port: 3551},
		},
	}
	srv := New(DefaultConfig(), appConfig, "", nil)

	req := httptest.NewRequest(http.MethodGet, "/api/devices", nil)
	w := httptest.NewRecorder()

	srv.handleListDevices(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected status 200, got %d", resp.StatusCode)
	}

	var devices []config.Device
	if err := json.NewDecoder(resp.Body).Decode(&devices); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if len(devices) != 2 {
		t.Errorf("expected 2 devices, got %d", len(devices))
	}
}

func TestServer_HandleTestDevice(t *testing.T) {
	srv := New(DefaultConfig(), &config.Config{}, "", nil)

	t.Run("method not allowed", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/device/test", nil)
		w := httptest.NewRecorder()

		srv.handleTestDevice(w, req)

		resp := w.Result()
		if resp.StatusCode != http.StatusMethodNotAllowed {
			t.Errorf("expected status 405, got %d", resp.StatusCode)
		}
	})

	t.Run("invalid request", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/device/test", strings.NewReader("invalid"))
		w := httptest.NewRecorder()

		srv.handleTestDevice(w, req)

		resp := w.Result()
		if resp.StatusCode != http.StatusBadRequest {
			t.Errorf("expected status 400, got %d", resp.StatusCode)
		}
	})

	t.Run("missing required fields", func(t *testing.T) {
		body := `{"name": "", "host": "", "type": ""}`
		req := httptest.NewRequest(http.MethodPost, "/api/device/test", strings.NewReader(body))
		w := httptest.NewRecorder()

		srv.handleTestDevice(w, req)

		resp := w.Result()
		if resp.StatusCode != http.StatusBadRequest {
			t.Errorf("expected status 400, got %d", resp.StatusCode)
		}
	})
}

func TestServer_HandleDeleteDevice(t *testing.T) {
	srv := New(DefaultConfig(), &config.Config{Devices: []config.Device{}}, "", nil)

	t.Run("missing name parameter", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodDelete, "/api/device", nil)
		w := httptest.NewRecorder()

		srv.handleDeleteDevice(w, req)

		resp := w.Result()
		if resp.StatusCode != http.StatusBadRequest {
			t.Errorf("expected status 400, got %d", resp.StatusCode)
		}
	})

	t.Run("device not found", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodDelete, "/api/device?name=nonexistent", nil)
		w := httptest.NewRecorder()

		srv.handleDeleteDevice(w, req)

		resp := w.Result()
		if resp.StatusCode != http.StatusNotFound {
			t.Errorf("expected status 404, got %d", resp.StatusCode)
		}
	})
}

func TestWSHub(t *testing.T) {
	cfg := DefaultConfig()
	hub := newWSHub(cfg)

	if hub == nil {
		t.Fatal("expected hub to be created")
	}
	if hub.clients == nil {
		t.Error("expected clients map to be initialized")
	}

	hub.broadcast([]byte(`{"test": true}`))

	hub.closeAll()
}

func TestServer_StartAndShutdown(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Host = "127.0.0.1"
	cfg.Port = 0

	appConfig := &config.Config{Devices: []config.Device{}}
	srv := New(cfg, appConfig, "", nil)

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	err := srv.Start(ctx)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}
