package config

import (
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/alex-savin/go-ups-monitor/ups"

	"gopkg.in/yaml.v3"
)

type Device struct {
	Type               string   `yaml:"type"`
	Name               string   `yaml:"name"`
	Host               string   `yaml:"host"`
	Port               int      `yaml:"port"`
	Username           string   `yaml:"username"`
	Password           string   `yaml:"password"`
	SelectedAttributes []string `yaml:"selected_attributes"`
}

type Logging struct {
	Level  string `yaml:"level,omitempty"`
	Output string `yaml:"output,omitempty"`
	Source bool   `yaml:"source,omitempty"`
}

type Server struct {
	Host string `yaml:"host,omitempty"`
	Port int    `yaml:"port,omitempty"`
}

type Config struct {
	Devices []Device `yaml:"devices"`
	Logging Logging  `yaml:"logging,omitempty"`
	Server  Server   `yaml:"server,omitempty"`
}

type UPSStatus struct {
	DeviceName         string            `json:"device_name"`
	Type               string            `json:"type"`
	Attributes         map[string]string `json:"attributes"`
	SelectedAttributes []string          `json:"-"`
}

type UPSCommand struct {
	ID      int                    `json:"id"`
	Device  string                 `json:"device"`
	Command string                 `json:"command"`
	Params  map[string]interface{} `json:"params,omitempty"`
}

type CommandResult struct {
	ID      int         `json:"id"`
	Success bool        `json:"success"`
	Message string      `json:"message"`
	Data    interface{} `json:"data,omitempty"`
}

type CommandInfo struct {
	Name      string `json:"name"`
	Supported bool   `json:"supported"`
}

var ApcupsdMapping = map[string]string{
	"Status":                      "status",
	"BatteryChargePercent":        "battery_charge",
	"TimeLeft":                    "time_left",
	"LineVoltage":                 "input_voltage",
	"LoadPercent":                 "load_percentage",
	"BatteryVoltage":              "battery_voltage",
	"OutputVoltage":               "output_voltage",
	"InternalTemp":                "internal_temperature",
	"MinimumBatteryChargePercent": "min_battery_charge",
	"NominalInputVoltage":         "nominal_input_voltage",
	"NominalPower":                "nominal_power",
	"Firmware":                    "firmware_version",
	"Model":                       "model",
	"SerialNumber":                "serial_number",
	"BatteryDate":                 "battery_date",
	"NominalBatteryVoltage":       "nominal_battery_voltage",
	"Hostname":                    "hostname",
	"Version":                     "version",
	"UPSName":                     "ups_name",
	"Cable":                       "cable",
	"Driver":                      "driver",
	"UPSMode":                     "ups_mode",
	"StartTime":                   "start_time",
	"MinimumTimeLeft":             "minimum_time_left",
	"MaximumTime":                 "maximum_time",
	"Sense":                       "sense",
	"LowTransferVoltage":          "low_transfer_voltage",
	"HighTransferVoltage":         "high_transfer_voltage",
	"AlarmDel":                    "alarm_delay",
	"LastTransfer":                "last_transfer",
	"NumberTransfers":             "number_transfers",
	"XOnBattery":                  "xon_battery",
	"TimeOnBattery":               "time_on_battery",
	"CumulativeTimeOnBattery":     "cumulative_time_on_battery",
	"XOffBattery":                 "xoff_battery",
	"LastSelftest":                "last_selftest",
	"Selftest":                    "selftest",
	"StatusFlags":                 "status_flags",
}

// MapSelectedAttributes applies the provided mapping to selected attribute names, defaulting to the original name when no mapping exists.
func MapSelectedAttributes(selected []string, mapping map[string]string) []string {
	mapped := make([]string, 0, len(selected))
	for _, attr := range selected {
		if unified, ok := mapping[attr]; ok {
			mapped = append(mapped, unified)
			continue
		}
		mapped = append(mapped, attr)
	}
	return mapped
}

var NutMapping = map[string]string{
	"ups.status":         "status",
	"battery.charge":     "battery_charge",
	"battery.runtime":    "time_left",
	"input.voltage":      "input_voltage",
	"ups.load":           "load_percentage",
	"battery.voltage":    "battery_voltage",
	"output.voltage":     "output_voltage",
	"ups.temperature":    "internal_temperature",
	"battery.mfr.date":   "battery_manufacture_date",
	"ups.mfr":            "manufacturer",
	"ups.model":          "model",
	"ups.serial":         "serial_number",
	"battery.date":       "battery_date",
	"battery.type":       "battery_type",
	"input.frequency":    "input_frequency",
	"output.frequency":   "output_frequency",
	"ups.realpower":      "real_power",
	"ups.delay.start":    "delay_start",
	"ups.delay.shutdown": "delay_shutdown",
}

func GetSupportedCommands(deviceType string) []CommandInfo {
	common := []CommandInfo{}
	switch deviceType {
	case "nut":
		common = []CommandInfo{
			{Name: "beeper.disable", Supported: true},
			{Name: "beeper.enable", Supported: true},
			{Name: "beeper.mute", Supported: true},
			{Name: "test.battery.start", Supported: true},
			{Name: "test.battery.stop", Supported: true},
			{Name: "calibrate.start", Supported: true},
			{Name: "calibrate.stop", Supported: true},
			{Name: "load.off", Supported: true},
			{Name: "load.on", Supported: true},
			{Name: "shutdown.return", Supported: true},
			{Name: "shutdown.stayoff", Supported: true},
		}
	case "apcupsd":
		common = []CommandInfo{
			{Name: "beeper.disable", Supported: false},
			{Name: "beeper.enable", Supported: false},
			{Name: "test.battery", Supported: false},
			{Name: "calibrate.start", Supported: false},
		}
	}
	return common
}

var statuses = make(map[string]*UPSStatus)
var mu sync.RWMutex

func LoadConfig() (*Config, error) {
	return LoadConfigFrom(os.Getenv("UPS_CONFIG_PATH"))
}

func LoadConfigFrom(path string) (*Config, error) {
	if path == "" {
		path = "config.yml"
	}
	file, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return &Config{}, nil
		}
		return nil, err
	}
	defer func() { _ = file.Close() }()
	var config Config
	decoder := yaml.NewDecoder(file)
	err = decoder.Decode(&config)
	if err == io.EOF {
		config = Config{}
		applyDefaults(&config)
		return &config, nil
	}
	if err == nil {
		applyDefaults(&config)
	}
	return &config, err
}

func applyDefaults(cfg *Config) {
	if cfg.Server.Host == "" {
		cfg.Server.Host = "0.0.0.0"
	}
	if cfg.Server.Port == 0 {
		cfg.Server.Port = 8080
	}
}

func SaveConfigTo(path string, cfg *Config) error {
	if path == "" {
		path = "config.yml"
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp := path + ".tmp"
	file, err := os.Create(tmp)
	if err != nil {
		return err
	}
	encoder := yaml.NewEncoder(file)
	encoder.SetIndent(2)
	// Persist only devices to keep config minimal.
	slim := struct {
		Devices []Device `yaml:"devices"`
	}{Devices: cfg.Devices}
	if err := encoder.Encode(slim); err != nil {
		_ = file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func AddOrUpdateDevice(cfg *Config, device Device) {
	replaced := false
	for i, d := range cfg.Devices {
		if d.Name == device.Name {
			cfg.Devices[i] = device
			replaced = true
			break
		}
	}
	if !replaced {
		cfg.Devices = append(cfg.Devices, device)
	}
}

// RemoveDevice deletes a device by name, returning true when a device was removed.
func RemoveDevice(cfg *Config, name string) bool {
	for i, d := range cfg.Devices {
		if d.Name == name {
			cfg.Devices = append(cfg.Devices[:i], cfg.Devices[i+1:]...)
			return true
		}
	}
	return false
}

func GetStatuses() []UPSStatus {
	return getStatusesFiltered(nil)
}

func GetStatusesForDevices(devices []Device) []UPSStatus {
	allowed := make(map[string]struct{}, len(devices))
	for _, d := range devices {
		allowed[d.Name] = struct{}{}
	}
	return getStatusesFiltered(allowed)
}

func getStatusesFiltered(allowed map[string]struct{}) []UPSStatus {
	mu.RLock()
	defer mu.RUnlock()
	var list []UPSStatus
	for _, s := range statuses {
		if allowed != nil {
			if _, ok := allowed[s.DeviceName]; !ok {
				continue
			}
		}
		filtered := UPSStatus{
			DeviceName: s.DeviceName,
			Type:       s.Type,
			Attributes: make(map[string]string),
		}

		if len(s.SelectedAttributes) > 0 {
			for _, attr := range s.SelectedAttributes {
				if val, ok := s.Attributes[attr]; ok {
					filtered.Attributes[attr] = val
				}
			}
			// Always include identity fields even when attributes are filtered
			for _, attr := range []string{
				"manufacturer", "ups_mfr", "mfr",
				"model", "ups_model", "device_model",
				"serial_number", "ups_serial", "serialno", "device_serial",
			} {
				if val, ok := s.Attributes[attr]; ok {
					filtered.Attributes[attr] = val
				}
			}
		} else {
			// Include all
			for k, v := range s.Attributes {
				filtered.Attributes[k] = v
			}
		}
		list = append(list, filtered)
	}
	return list
}

func UpdateStatus(deviceName string, status *UPSStatus) {
	mu.Lock()
	defer mu.Unlock()
	statuses[deviceName] = status
}

// DeleteStatus removes cached status for a device.
func DeleteStatus(deviceName string) {
	mu.Lock()
	defer mu.Unlock()
	delete(statuses, deviceName)
}

// PruneStatuses removes cached statuses for devices no longer configured.
func PruneStatuses(cfg *Config) {
	mu.Lock()
	defer mu.Unlock()
	allowed := make(map[string]struct{}, len(cfg.Devices))
	for _, d := range cfg.Devices {
		allowed[d.Name] = struct{}{}
	}
	for name := range statuses {
		if _, ok := allowed[name]; !ok {
			delete(statuses, name)
		}
	}
}

func ExecuteCommand(cmd UPSCommand, devices []Device) CommandResult {
	result := CommandResult{
		ID:      cmd.ID,
		Success: false,
		Message: "Command not supported",
	}

	// Find the device
	var device *Device
	for _, d := range devices {
		if d.Name == cmd.Device {
			device = &d
			break
		}
	}

	if device == nil {
		result.Message = "Device not found"
		return result
	}

	// Execute command based on device type
	switch device.Type {
	case "apcupsd":
		return executeApcupsdCommand(cmd, *device)
	case "nut":
		return executeNutCommand(cmd, *device)
	default:
		result.Message = "Unsupported device type"
		return result
	}
}

func executeApcupsdCommand(cmd UPSCommand, device Device) CommandResult {
	result := CommandResult{
		ID:      cmd.ID,
		Success: false,
		Message: "Command execution failed",
	}

	// For APCUPSD, we need to implement command mapping
	// Since APCUPSD uses a text-based protocol, we'll need to extend the client
	// For now, return not implemented for most commands
	switch cmd.Command {
	case "beeper.disable":
		result.Message = "Beeper disable not implemented for APCUPSD"
	case "beeper.enable":
		result.Message = "Beeper enable not implemented for APCUPSD"
	case "test.battery":
		result.Message = "Battery test not implemented for APCUPSD"
	case "calibrate.start":
		result.Message = "Calibration not implemented for APCUPSD"
	default:
		result.Message = fmt.Sprintf("Command '%s' not supported for APCUPSD", cmd.Command)
	}

	return result
}

func executeNutCommand(cmd UPSCommand, device Device) CommandResult {
	result := CommandResult{
		ID:      cmd.ID,
		Success: false,
		Message: "Command execution failed",
	}

	// For NUT, we can use the existing client to send commands
	client, err := ups.NutConnect(device.Host, device.Port)
	if err != nil {
		result.Message = fmt.Sprintf("Failed to connect to NUT server: %v", err)
		return result
	}
	defer func() { _, _ = client.Disconnect() }()

	// Authenticate if credentials provided
	if device.Username != "" {
		_, err = client.Authenticate(device.Username, device.Password)
		if err != nil {
			result.Message = fmt.Sprintf("Authentication failed: %v", err)
			return result
		}
	}

	// Map unified commands to NUT commands
	var nutCommand string
	switch cmd.Command {
	case "beeper.disable":
		nutCommand = fmt.Sprintf("SET %s ups.beeper.status disabled", device.Name)
	case "beeper.enable":
		nutCommand = fmt.Sprintf("SET %s ups.beeper.status enabled", device.Name)
	case "beeper.mute":
		nutCommand = fmt.Sprintf("SET %s ups.beeper.status muted", device.Name)
	case "test.battery.start":
		nutCommand = fmt.Sprintf("INSTCMD %s test.battery.start", device.Name)
	case "test.battery.stop":
		nutCommand = fmt.Sprintf("INSTCMD %s test.battery.stop", device.Name)
	case "calibrate.start":
		nutCommand = fmt.Sprintf("INSTCMD %s calibrate.start", device.Name)
	case "calibrate.stop":
		nutCommand = fmt.Sprintf("INSTCMD %s calibrate.stop", device.Name)
	case "load.off":
		nutCommand = fmt.Sprintf("INSTCMD %s load.off", device.Name)
	case "load.on":
		nutCommand = fmt.Sprintf("INSTCMD %s load.on", device.Name)
	case "shutdown.return":
		nutCommand = fmt.Sprintf("INSTCMD %s shutdown.return", device.Name)
	case "shutdown.stayoff":
		nutCommand = fmt.Sprintf("INSTCMD %s shutdown.stayoff", device.Name)
	default:
		result.Message = fmt.Sprintf("Command '%s' not supported for NUT", cmd.Command)
		return result
	}

	// Execute the command
	response, err := client.SendCommand(nutCommand)
	if err != nil {
		result.Message = fmt.Sprintf("Command execution failed: %v", err)
		return result
	}

	result.Success = true
	result.Message = "Command executed successfully"
	result.Data = response

	return result
}

func NewLogger(logging Logging) *slog.Logger {
	var level slog.Level
	switch strings.ToLower(logging.Level) {
	case "debug":
		level = slog.LevelDebug
	case "info":
		level = slog.LevelInfo
	case "warn":
		level = slog.LevelWarn
	case "error":
		level = slog.LevelError
	default:
		level = slog.LevelInfo
	}

	var writer io.Writer
	switch strings.ToLower(logging.Output) {
	case "stderr":
		writer = os.Stderr
	case "stdout":
		writer = os.Stdout
	default:
		writer = os.Stdout
	}

	handler := slog.NewTextHandler(writer, &slog.HandlerOptions{
		Level:     level,
		AddSource: logging.Source,
	})

	return slog.New(handler)
}
