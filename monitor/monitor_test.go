package monitor

import (
	"context"
	"log/slog"
	"testing"
	"time"
)

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()

	if cfg.PollInterval != 10*time.Second {
		t.Errorf("expected PollInterval 10s, got %v", cfg.PollInterval)
	}
	if cfg.MaxBackoff != 60*time.Second {
		t.Errorf("expected MaxBackoff 60s, got %v", cfg.MaxBackoff)
	}
	if cfg.JitterFactor != 0.3 {
		t.Errorf("expected JitterFactor 0.3, got %v", cfg.JitterFactor)
	}
	if cfg.Logger == nil {
		t.Error("expected Logger to be set")
	}
}

func TestNew(t *testing.T) {
	t.Run("with default config", func(t *testing.T) {
		mon := New(DefaultConfig())
		if mon == nil {
			t.Fatal("expected monitor to be created")
		}
		if mon.cfg.PollInterval != 10*time.Second {
			t.Errorf("expected PollInterval 10s, got %v", mon.cfg.PollInterval)
		}
	})

	t.Run("with zero values applies defaults", func(t *testing.T) {
		mon := New(Config{})
		if mon.cfg.PollInterval != 10*time.Second {
			t.Errorf("expected PollInterval 10s, got %v", mon.cfg.PollInterval)
		}
		if mon.cfg.MaxBackoff != 60*time.Second {
			t.Errorf("expected MaxBackoff 60s, got %v", mon.cfg.MaxBackoff)
		}
		if mon.cfg.JitterFactor != 0.3 {
			t.Errorf("expected JitterFactor 0.3, got %v", mon.cfg.JitterFactor)
		}
	})

	t.Run("with custom config", func(t *testing.T) {
		cfg := Config{
			PollInterval: 5 * time.Second,
			MaxBackoff:   30 * time.Second,
			JitterFactor: 0.5,
			Logger:       slog.Default(),
		}
		mon := New(cfg)
		if mon.cfg.PollInterval != 5*time.Second {
			t.Errorf("expected PollInterval 5s, got %v", mon.cfg.PollInterval)
		}
		if mon.cfg.MaxBackoff != 30*time.Second {
			t.Errorf("expected MaxBackoff 30s, got %v", mon.cfg.MaxBackoff)
		}
		if mon.cfg.JitterFactor != 0.5 {
			t.Errorf("expected JitterFactor 0.5, got %v", mon.cfg.JitterFactor)
		}
	})
}

func TestMonitor_ActiveDevices(t *testing.T) {
	mon := New(DefaultConfig())

	devices := mon.ActiveDevices()
	if len(devices) != 0 {
		t.Errorf("expected 0 active devices, got %d", len(devices))
	}
}

func TestMonitor_StopDevice(t *testing.T) {
	mon := New(DefaultConfig())

	mon.StopDevice("non-existent")

	devices := mon.ActiveDevices()
	if len(devices) != 0 {
		t.Errorf("expected 0 active devices, got %d", len(devices))
	}
}

func TestMonitor_StopAll(t *testing.T) {
	mon := New(DefaultConfig())

	mon.StopAll()

	devices := mon.ActiveDevices()
	if len(devices) != 0 {
		t.Errorf("expected 0 active devices, got %d", len(devices))
	}
}

func TestMonitor_StartAndStopDevice(t *testing.T) {
	mon := New(Config{
		PollInterval: 100 * time.Millisecond,
		MaxBackoff:   200 * time.Millisecond,
		JitterFactor: 0.1,
		Logger:       slog.Default(),
	})

	_, cancel := context.WithCancel(context.Background())
	defer cancel()

	mon.StopAll()
}

func TestMinDuration(t *testing.T) {
	tests := []struct {
		name     string
		a, b     time.Duration
		expected time.Duration
	}{
		{"a smaller", 5 * time.Second, 10 * time.Second, 5 * time.Second},
		{"b smaller", 10 * time.Second, 5 * time.Second, 5 * time.Second},
		{"equal", 5 * time.Second, 5 * time.Second, 5 * time.Second},
		{"zero a", 0, 5 * time.Second, 0},
		{"zero b", 5 * time.Second, 0, 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := minDuration(tt.a, tt.b)
			if result != tt.expected {
				t.Errorf("minDuration(%v, %v) = %v, expected %v", tt.a, tt.b, result, tt.expected)
			}
		})
	}
}
