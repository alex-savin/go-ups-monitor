package config

import (
	"fmt"
	"os"
	"testing"

	"github.com/alex-savin/go-ups-monitor/ups"
)

func TestLoadConfig(t *testing.T) {
	// Create a temporary config file
	configContent := `
devices:
  - type: apcupsd
    name: test-ups
    host: localhost
    port: 3551
  - type: nut
    name: test-nut-ups
    host: localhost
    port: 3493
    username: testuser
    password: testpass
logging:
  level: info
  output: stdout
  source: false
server:
  host: 0.0.0.0
  port: 8080
`

	tmpFile, err := os.CreateTemp("", "config_test_*.yml")
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}
	defer os.Remove(tmpFile.Name())

	if _, err := tmpFile.WriteString(configContent); err != nil {
		t.Fatalf("Failed to write config: %v", err)
	}
	tmpFile.Close()

	// Change to temp directory to load the config
	oldWd, _ := os.Getwd()
	os.Chdir(tmpFile.Name()[:len(tmpFile.Name())-len("config_test_*.yml")+12]) // Go to temp dir
	defer os.Chdir(oldWd)

	// Copy config to config.yml in temp dir
	if err := os.WriteFile("config.yml", []byte(configContent), 0644); err != nil {
		t.Fatalf("Failed to write config.yml: %v", err)
	}
	defer os.Remove("config.yml")

	config, err := LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig failed: %v", err)
	}

	if len(config.Devices) != 2 {
		t.Errorf("Expected 2 devices, got %d", len(config.Devices))
	}

	// Test first device
	if config.Devices[0].Type != "apcupsd" {
		t.Errorf("Expected device type 'apcupsd', got '%s'", config.Devices[0].Type)
	}
	if config.Devices[0].Name != "test-ups" {
		t.Errorf("Expected device name 'test-ups', got '%s'", config.Devices[0].Name)
	}

	// Test second device
	if config.Devices[1].Type != "nut" {
		t.Errorf("Expected device type 'nut', got '%s'", config.Devices[1].Type)
	}
	if config.Devices[1].Username != "testuser" {
		t.Errorf("Expected username 'testuser', got '%s'", config.Devices[1].Username)
	}

	// Test logging config
	if config.Logging.Level != "info" {
		t.Errorf("Expected log level 'info', got '%s'", config.Logging.Level)
	}

	// Test server config
	if config.Server.Port != 8080 {
		t.Errorf("Expected server port 8080, got %d", config.Server.Port)
	}
}

func TestUpdateStatus(t *testing.T) {
	// Clear any existing statuses
	statuses = make(map[string]*UPSStatus)

	status := &UPSStatus{
		DeviceName: "test-device",
		Type:       "apcupsd",
		Attributes: map[string]string{
			"status":         "ONLINE",
			"battery_charge": "100",
		},
	}

	UpdateStatus("test-device", status)

	// Test GetStatuses
	allStatuses := GetStatuses()
	if len(allStatuses) != 1 {
		t.Errorf("Expected 1 status, got %d", len(allStatuses))
	}

	if allStatuses[0].DeviceName != "test-device" {
		t.Errorf("Expected device name 'test-device', got '%s'", allStatuses[0].DeviceName)
	}

	// Test with selected attributes
	statusWithSelection := &UPSStatus{
		DeviceName: "test-device-2",
		Type:       "apcupsd",
		Attributes: map[string]string{
			"status":         "ONLINE",
			"battery_charge": "100",
			"time_left":      "60",
		},
		SelectedAttributes: []string{"status", "battery_charge"},
	}

	UpdateStatus("test-device-2", statusWithSelection)

	allStatuses = GetStatuses()
	if len(allStatuses) != 2 {
		t.Errorf("Expected 2 statuses, got %d", len(allStatuses))
	}

	// Find the status with selected attributes
	var selectedStatus *UPSStatus
	for _, s := range allStatuses {
		if s.DeviceName == "test-device-2" {
			selectedStatus = &s
			break
		}
	}

	if selectedStatus == nil {
		t.Fatal("Could not find test-device-2 status")
	}

	// Should only have selected attributes
	if len(selectedStatus.Attributes) != 2 {
		t.Errorf("Expected 2 attributes, got %d", len(selectedStatus.Attributes))
	}

	if _, exists := selectedStatus.Attributes["status"]; !exists {
		t.Error("Expected 'status' attribute to exist")
	}

	if _, exists := selectedStatus.Attributes["time_left"]; exists {
		t.Error("Expected 'time_left' attribute to be filtered out")
	}
}

func TestExecuteCommand_DeviceNotFound(t *testing.T) {
	devices := []Device{
		{Name: "ups1", Type: "apcupsd"},
	}

	cmd := UPSCommand{
		ID:      1,
		Device:  "nonexistent",
		Command: "beeper.disable",
	}

	result := ExecuteCommand(cmd, devices)

	if result.Success {
		t.Error("Expected command to fail for nonexistent device")
	}

	if result.Message != "Device not found" {
		t.Errorf("Expected 'Device not found' message, got '%s'", result.Message)
	}
}

func TestExecuteCommand_UnsupportedDeviceType(t *testing.T) {
	devices := []Device{
		{Name: "ups1", Type: "unsupported"},
	}

	cmd := UPSCommand{
		ID:      2,
		Device:  "ups1",
		Command: "beeper.disable",
	}

	result := ExecuteCommand(cmd, devices)

	if result.Success {
		t.Error("Expected command to fail for unsupported device type")
	}

	if result.Message != "Unsupported device type" {
		t.Errorf("Expected 'Unsupported device type' message, got '%s'", result.Message)
	}
}

func TestExecuteApcupsdCommand(t *testing.T) {
	device := Device{
		Name: "test-ups",
		Type: "apcupsd",
		Host: "localhost",
		Port: 3551,
	}

	cmd := UPSCommand{
		ID:      3,
		Device:  "test-ups",
		Command: "beeper.disable",
	}

	result := executeApcupsdCommand(cmd, device)

	if result.Success {
		t.Error("Expected APCUPSD command to fail (not implemented)")
	}

	if result.Message != "Beeper disable not implemented for APCUPSD" {
		t.Errorf("Expected not implemented message, got '%s'", result.Message)
	}
}

func TestExecuteNutCommand_ConnectionError(t *testing.T) {
	device := Device{
		Name: "test-ups",
		Type: "nut",
		Host: "nonexistent-host",
		Port: 3493,
	}

	cmd := UPSCommand{
		ID:      4,
		Device:  "test-ups",
		Command: "beeper.disable",
	}

	result := executeNutCommand(cmd, device)

	if result.Success {
		t.Error("Expected NUT command to fail due to connection error")
	}

	if result.Message == "" {
		t.Error("Expected error message for connection failure")
	}
}

func TestNewLogger(t *testing.T) {
	tests := []struct {
		name     string
		logging  Logging
		expected string // We'll check that logger is created without panic
	}{
		{
			name: "debug level",
			logging: Logging{
				Level:  "debug",
				Output: "stdout",
				Source: true,
			},
		},
		{
			name: "info level",
			logging: Logging{
				Level:  "info",
				Output: "stderr",
				Source: false,
			},
		},
		{
			name: "invalid level defaults to info",
			logging: Logging{
				Level:  "invalid",
				Output: "stdout",
				Source: false,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			logger := NewLogger(tt.logging)
			if logger == nil {
				t.Error("Expected logger to be created")
			}
		})
	}
}

func TestApcupsdMapping(t *testing.T) {
	// Test that mapping contains expected keys
	expectedMappings := map[string]string{
		"Status":               "status",
		"BatteryChargePercent": "battery_charge",
		"TimeLeft":             "time_left",
		"LineVoltage":          "input_voltage",
	}

	for rawKey, expectedUnified := range expectedMappings {
		if unified, exists := ApcupsdMapping[rawKey]; !exists {
			t.Errorf("Expected mapping for key '%s' to exist", rawKey)
		} else if unified != expectedUnified {
			t.Errorf("Expected mapping '%s' -> '%s', got '%s'", rawKey, expectedUnified, unified)
		}
	}
}

func TestNutMapping(t *testing.T) {
	// Test that mapping contains expected keys
	expectedMappings := map[string]string{
		"ups.status":      "status",
		"battery.charge":  "battery_charge",
		"battery.runtime": "time_left",
		"input.voltage":   "input_voltage",
	}

	for rawKey, expectedUnified := range expectedMappings {
		if unified, exists := NutMapping[rawKey]; !exists {
			t.Errorf("Expected mapping for key '%s' to exist", rawKey)
		} else if unified != expectedUnified {
			t.Errorf("Expected mapping '%s' -> '%s', got '%s'", rawKey, expectedUnified, unified)
		}
	}
}

// Mock NUT client for testing
type mockNutClient struct {
	connected bool
}

func (m *mockNutClient) SendCommand(cmd string) ([]string, error) {
	if !m.connected {
		return nil, fmt.Errorf("connection failed")
	}
	return []string{"OK"}, nil
}

func (m *mockNutClient) Disconnect() (bool, error) {
	m.connected = false
	return true, nil
}

func (m *mockNutClient) Authenticate(username, password string) ([]string, error) {
	return []string{"OK"}, nil
}

func (m *mockNutClient) GetUPSList() ([]ups.NutUPS, error) {
	return []ups.NutUPS{}, nil
}

// Note: This test would require mocking the ups.NutConnect function
// For now, we'll skip integration tests that require network connections
