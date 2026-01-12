// Package monitor provides UPS device monitoring with automatic polling and status updates.
package monitor

import (
	"context"
	"fmt"
	"log/slog"
	"math/rand"
	"reflect"
	"strconv"
	"sync"
	"time"

	"github.com/alex-savin/go-ups-monitor/config"
	"github.com/alex-savin/go-ups-monitor/ups"
)

// Config holds monitor configuration options.
type Config struct {
	// PollInterval is the base interval between device polls.
	PollInterval time.Duration
	// MaxBackoff is the maximum backoff duration after errors.
	MaxBackoff time.Duration
	// JitterFactor is the jitter percentage (0.0-1.0) applied to backoff.
	JitterFactor float64
	// Logger is the logger to use. If nil, a default logger is created.
	Logger *slog.Logger
}

// DefaultConfig returns the default monitor configuration.
func DefaultConfig() Config {
	return Config{
		PollInterval: 10 * time.Second,
		MaxBackoff:   60 * time.Second,
		JitterFactor: 0.3,
		Logger:       slog.Default(),
	}
}

// Monitor manages polling of UPS devices and maintains their status.
type Monitor struct {
	cfg     Config
	mu      sync.RWMutex
	devices map[string]context.CancelFunc
	logger  *slog.Logger
}

// New creates a new Monitor with the given configuration.
func New(cfg Config) *Monitor {
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}
	if cfg.PollInterval == 0 {
		cfg.PollInterval = 10 * time.Second
	}
	if cfg.MaxBackoff == 0 {
		cfg.MaxBackoff = 60 * time.Second
	}
	if cfg.JitterFactor == 0 {
		cfg.JitterFactor = 0.3
	}
	return &Monitor{
		cfg:     cfg,
		devices: make(map[string]context.CancelFunc),
		logger:  cfg.Logger,
	}
}

// StartDevice begins polling the specified device.
func (m *Monitor) StartDevice(ctx context.Context, device config.Device) {
	m.mu.Lock()
	defer m.mu.Unlock()
	// Stop existing poller if any
	if cancel, exists := m.devices[device.Name]; exists {
		cancel()
	}
	deviceCtx, cancel := context.WithCancel(ctx)
	m.devices[device.Name] = cancel
	go m.pollDevice(deviceCtx, device)
}

// StopDevice stops polling the specified device.
func (m *Monitor) StopDevice(name string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if cancel, exists := m.devices[name]; exists {
		cancel()
		delete(m.devices, name)
	}
}

// StopAll stops all device pollers.
func (m *Monitor) StopAll() {
	m.mu.Lock()
	defer m.mu.Unlock()
	for name, cancel := range m.devices {
		cancel()
		delete(m.devices, name)
	}
}

// ActiveDevices returns the names of devices currently being polled.
func (m *Monitor) ActiveDevices() []string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	names := make([]string, 0, len(m.devices))
	for name := range m.devices {
		names = append(names, name)
	}
	return names
}

// deviceConnections holds client connections for a device.
type deviceConnections struct {
	apcClient *ups.Client
	nutClient *ups.NutClient
}

// closeConnections closes all active client connections.
func (dc *deviceConnections) closeConnections() {
	if dc.apcClient != nil {
		_ = dc.apcClient.Close()
		dc.apcClient = nil
	}
	if dc.nutClient != nil {
		_, _ = dc.nutClient.Disconnect()
		dc.nutClient = nil
	}
}

// pollDeviceOnce attempts a single poll of the device and returns the status or error.
func (m *Monitor) pollDeviceOnce(device config.Device, dc *deviceConnections) (*config.UPSStatus, error) {
	var err error

	switch device.Type {
	case "apcupsd":
		if dc.apcClient == nil {
			m.logger.Debug("Connecting to apcupsd", "name", device.Name, "host", device.Host, "port", device.Port)
			dc.apcClient, err = ups.Dial("tcp", fmt.Sprintf("%s:%d", device.Host, device.Port))
			if err != nil {
				return nil, err
			}
			m.logger.Debug("Connected to apcupsd", "name", device.Name)
		}
		return GetApcupsdStatus(dc.apcClient, device)

	case "nut":
		if dc.nutClient == nil {
			m.logger.Debug("Connecting to NUT", "name", device.Name, "host", device.Host, "port", device.Port)
			c, connErr := ups.NutConnect(device.Host, device.Port)
			if connErr != nil {
				return nil, connErr
			}
			dc.nutClient = &c
			m.logger.Debug("Connected to NUT", "name", device.Name)
		}
		return GetNutStatus(dc.nutClient, device)

	default:
		return nil, fmt.Errorf("unsupported device type: %s", device.Type)
	}
}

// calculateBackoff computes the next backoff duration with jitter.
func (m *Monitor) calculateBackoff(currentBackoff time.Duration) time.Duration {
	backoff := minDuration(currentBackoff*2, m.cfg.MaxBackoff)
	jitter := time.Duration(float64(backoff) * m.cfg.JitterFactor * (rand.Float64()*2 - 1))
	backoff += jitter
	if backoff < m.cfg.PollInterval {
		return m.cfg.PollInterval
	}
	return backoff
}

func (m *Monitor) pollDevice(ctx context.Context, device config.Device) {
	dc := &deviceConnections{}
	defer func() {
		dc.closeConnections()
		m.logger.Info("Device poller stopped", "name", device.Name)
	}()

	backoff := m.cfg.PollInterval
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		m.logger.Debug("Polling device", "name", device.Name, "type", device.Type)
		status, err := m.pollDeviceOnce(device, dc)

		if err != nil {
			m.logger.Error("Error polling device", "name", device.Name, "error", err)
			backoff = m.calculateBackoff(backoff)
			dc.closeConnections()
		} else {
			config.UpdateStatus(device.Name, status)
			m.logger.Debug("Polled device successfully", "name", device.Name, "type", device.Type, "attrs", len(status.Attributes))
			backoff = m.cfg.PollInterval
		}

		select {
		case <-ctx.Done():
			return
		case <-time.After(backoff):
		}
	}
}

// GetApcupsdStatus retrieves UPS status using an existing apcupsd client.
func GetApcupsdStatus(client *ups.Client, device config.Device) (*config.UPSStatus, error) {
	status, err := client.Status()
	if err != nil {
		return nil, err
	}

	v := reflect.ValueOf(status).Elem()
	t := v.Type()
	attributes := make(map[string]string)

	for i := 0; i < v.NumField(); i++ {
		field := t.Field(i)
		fieldName := field.Name
		fieldValue := v.Field(i)
		var value string

		switch fieldValue.Kind() {
		case reflect.String:
			value = fieldValue.String()
		case reflect.Int, reflect.Int32, reflect.Int64:
			if fieldValue.Type() == reflect.TypeOf(time.Duration(0)) {
				dur := fieldValue.Interface().(time.Duration)
				value = strconv.FormatInt(int64(dur/time.Second), 10)
			} else {
				value = strconv.Itoa(int(fieldValue.Int()))
			}
		case reflect.Float32, reflect.Float64:
			value = strconv.FormatFloat(fieldValue.Float(), 'f', -1, 64)
		case reflect.Bool:
			value = strconv.FormatBool(fieldValue.Bool())
		default:
			if fieldValue.Type() == reflect.TypeOf(time.Time{}) {
				value = fieldValue.Interface().(time.Time).Format(time.RFC3339)
			} else {
				value = fmt.Sprintf("%v", fieldValue.Interface())
			}
		}
		attributes[fieldName] = value
	}

	upsStatus := &config.UPSStatus{
		DeviceName:         device.Name,
		Type:               device.Type,
		Attributes:         attributes,
		SelectedAttributes: config.MapSelectedAttributes(device.SelectedAttributes, config.ApcupsdMapping),
	}

	unified := make(map[string]string)
	for rawKey, value := range upsStatus.Attributes {
		if unifiedKey, ok := config.ApcupsdMapping[rawKey]; ok {
			unified[unifiedKey] = value
		}
	}
	upsStatus.Attributes = unified

	return upsStatus, nil
}

// GetNutStatus retrieves UPS status using an existing NUT client.
func GetNutStatus(client *ups.NutClient, device config.Device) (*config.UPSStatus, error) {
	if device.Username != "" {
		if _, err := client.Authenticate(device.Username, device.Password); err != nil {
			return nil, fmt.Errorf("authentication failed: %v", err)
		}
	}

	upsList, err := client.GetUPSList()
	if err != nil {
		return nil, fmt.Errorf("failed to get UPS list: %v", err)
	}

	var targetUPS *ups.NutUPS
	for _, u := range upsList {
		if u.Name == device.Name {
			targetUPS = &u
			break
		}
	}
	if targetUPS == nil {
		return nil, fmt.Errorf("UPS %s not found on server", device.Name)
	}

	variables, err := targetUPS.GetVariables()
	if err != nil {
		return nil, fmt.Errorf("failed to get variables for UPS %s: %v", device.Name, err)
	}

	status := &config.UPSStatus{
		DeviceName:         device.Name,
		Type:               device.Type,
		Attributes:         make(map[string]string),
		SelectedAttributes: config.MapSelectedAttributes(device.SelectedAttributes, config.NutMapping),
	}

	for _, variable := range variables {
		var value string
		switch v := variable.Value.(type) {
		case string:
			value = v
		case int64:
			value = strconv.FormatInt(v, 10)
		case float64:
			value = strconv.FormatFloat(v, 'f', -1, 64)
		case bool:
			value = strconv.FormatBool(v)
		default:
			value = fmt.Sprintf("%v", v)
		}
		status.Attributes[variable.Name] = value
	}

	unified := make(map[string]string)
	for rawKey, value := range status.Attributes {
		if unifiedKey, ok := config.NutMapping[rawKey]; ok {
			unified[unifiedKey] = value
		}
	}
	status.Attributes = unified

	return status, nil
}

// FetchApcupsdStatus creates a new connection and fetches status (one-shot).
func FetchApcupsdStatus(device config.Device) (*config.UPSStatus, error) {
	client, err := ups.Dial("tcp", fmt.Sprintf("%s:%d", device.Host, device.Port))
	if err != nil {
		return nil, err
	}
	defer func() { _ = client.Close() }()
	return GetApcupsdStatus(client, device)
}

// FetchNutStatus creates a new connection and fetches status (one-shot).
func FetchNutStatus(device config.Device) (*config.UPSStatus, error) {
	client, err := ups.NutConnect(device.Host, device.Port)
	if err != nil {
		return nil, err
	}
	defer func() { _, _ = client.Disconnect() }()
	return GetNutStatus(&client, device)
}

// FetchStatus fetches status for a device based on its type.
func FetchStatus(device config.Device) (*config.UPSStatus, error) {
	switch device.Type {
	case "apcupsd":
		return FetchApcupsdStatus(device)
	case "nut":
		return FetchNutStatus(device)
	default:
		return nil, fmt.Errorf("unsupported device type: %s", device.Type)
	}
}

func minDuration(a, b time.Duration) time.Duration {
	if a < b {
		return a
	}
	return b
}
