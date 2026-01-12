package ups

import (
	"bytes"
	"testing"
	"time"
)

func TestParseKVString(t *testing.T) {
	status := &Status{}

	// Test string parsing
	if !status.parseKVString(keyAPC, "TESTAPC") {
		t.Error("Expected parseKVString to return true for APC key")
	}
	if status.APC != "TESTAPC" {
		t.Errorf("Expected APC to be 'TESTAPC', got '%s'", status.APC)
	}

	if !status.parseKVString(keyStatus, "ONLINE") {
		t.Error("Expected parseKVString to return true for STATUS key")
	}
	if status.Status != "ONLINE" {
		t.Errorf("Expected Status to be 'ONLINE', got '%s'", status.Status)
	}

	// Test non-string key
	if status.parseKVString(keyLineV, "120.0") {
		t.Error("Expected parseKVString to return false for LineV key")
	}
}

func TestParseKVFloat(t *testing.T) {
	status := &Status{}

	// Test LineVoltage parsing
	match, err := status.parseKVFloat(keyLineV, "120.5 Volts")
	if err != nil {
		t.Errorf("Unexpected error parsing LineVoltage: %v", err)
	}
	if !match {
		t.Error("Expected parseKVFloat to return true for LineV key")
	}
	if status.LineVoltage != 120.5 {
		t.Errorf("Expected LineVoltage to be 120.5, got %f", status.LineVoltage)
	}

	// Test BatteryChargePercent parsing
	match, err = status.parseKVFloat(keyBCharge, "85 Percent")
	if err != nil {
		t.Errorf("Unexpected error parsing BatteryChargePercent: %v", err)
	}
	if !match {
		t.Error("Expected parseKVFloat to return true for BCharge key")
	}
	if status.BatteryChargePercent != 85.0 {
		t.Errorf("Expected BatteryChargePercent to be 85.0, got %f", status.BatteryChargePercent)
	}

	// Test invalid key
	match, err = status.parseKVFloat(keyAPC, "TEST")
	if err != nil {
		t.Errorf("Unexpected error for invalid key: %v", err)
	}
	if match {
		t.Error("Expected parseKVFloat to return false for APC key")
	}
}

func TestParseKVTime(t *testing.T) {
	status := &Status{}

	// Test normal time parsing
	match, err := status.parseKVTime(keyDate, "2023-01-01 12:00:00 -0500")
	if err != nil {
		t.Errorf("Unexpected error parsing date: %v", err)
	}
	if !match {
		t.Error("Expected parseKVTime to return true for Date key")
	}
	expectedTime, _ := time.Parse(timeFormatLong, "2023-01-01 12:00:00 -0500")
	if !status.Date.Equal(expectedTime) {
		t.Errorf("Expected Date to be %v, got %v", expectedTime, status.Date)
	}

	// Test N/A time parsing
	match, err = status.parseKVTime(keyXOnBat, "N/A")
	if err != nil {
		t.Errorf("Unexpected error parsing N/A time: %v", err)
	}
	if !match {
		t.Error("Expected parseKVTime to return true for XOnBat key with N/A")
	}
	if !status.XOnBattery.IsZero() {
		t.Error("Expected XOnBattery to be zero time for N/A value")
	}

	// Test invalid key
	match, err = status.parseKVTime(keyAPC, "TEST")
	if err != nil {
		t.Errorf("Unexpected error for invalid key: %v", err)
	}
	if match {
		t.Error("Expected parseKVTime to return false for APC key")
	}
}

func TestParseKVDuration(t *testing.T) {
	status := &Status{}

	// Test TimeLeft parsing
	match, err := status.parseKVDuration(keyTimeLeft, "1.5 Minutes")
	if err != nil {
		t.Errorf("Unexpected error parsing TimeLeft: %v", err)
	}
	if !match {
		t.Error("Expected parseKVDuration to return true for TimeLeft key")
	}
	expectedDuration := 90 * time.Second
	if status.TimeLeft != expectedDuration {
		t.Errorf("Expected TimeLeft to be %v, got %v", expectedDuration, status.TimeLeft)
	}

	// Test AlarmDel parsing (special case that ignores errors)
	match, err = status.parseKVDuration(keyAlarmDel, "INVALID")
	if err != nil {
		t.Errorf("Unexpected error parsing AlarmDel: %v", err)
	}
	if !match {
		t.Error("Expected parseKVDuration to return true for AlarmDel key")
	}

	// Test invalid key
	match, err = status.parseKVDuration(keyAPC, "TEST")
	if err != nil {
		t.Errorf("Unexpected error for invalid key: %v", err)
	}
	if match {
		t.Error("Expected parseKVDuration to return false for APC key")
	}
}

func TestParseDuration(t *testing.T) {
	tests := []struct {
		input    string
		expected time.Duration
		hasError bool
	}{
		{"10 Seconds", 10 * time.Second, false},
		{"5 minutes", 5 * time.Minute, false},
		{"2.5 minutes", 2*time.Minute + 30*time.Second, false},
		{"invalid", 0, true},
		{"10", 0, true},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result, err := parseDuration(tt.input)
			if tt.hasError {
				if err == nil {
					t.Error("Expected error but got none")
				}
			} else {
				if err != nil {
					t.Errorf("Unexpected error: %v", err)
				}
				if result != tt.expected {
					t.Errorf("Expected %v, got %v", tt.expected, result)
				}
			}
		})
	}
}

func TestParseOptionalTime(t *testing.T) {
	// Test N/A parsing
	result, err := parseOptionalTime("N/A")
	if err != nil {
		t.Errorf("Unexpected error parsing N/A: %v", err)
	}
	if !result.IsZero() {
		t.Error("Expected zero time for N/A")
	}

	// Test valid time parsing
	input := "2023-01-01 12:00:00 -0500"
	expected, _ := time.Parse(timeFormatLong, input)
	result, err = parseOptionalTime(input)
	if err != nil {
		t.Errorf("Unexpected error parsing valid time: %v", err)
	}
	if !result.Equal(expected) {
		t.Errorf("Expected %v, got %v", expected, result)
	}

	// Test invalid time
	_, err = parseOptionalTime("invalid-time")
	if err == nil {
		t.Error("Expected error for invalid time format")
	}
}

func TestStatusParseKV(t *testing.T) {
	status := &Status{}

	// Test valid key-value pair
	err := status.parseKV("STATUS   : ONLINE")
	if err != nil {
		t.Errorf("Unexpected error parsing STATUS: %v", err)
	}
	if status.Status != "ONLINE" {
		t.Errorf("Expected Status to be 'ONLINE', got '%s'", status.Status)
	}

	// Test invalid key-value pair
	err = status.parseKV("INVALID")
	if err == nil {
		t.Error("Expected error for invalid key-value pair")
	}
	if err != errInvalidKeyValuePair {
		t.Errorf("Expected errInvalidKeyValuePair, got %v", err)
	}
}

func TestNISReadWriteCloser(t *testing.T) {
	// Create a mock ReadWriteCloser
	buf := &mockReadWriteCloser{data: &bytes.Buffer{}}
	rwc := newNISReadWriteCloser(buf)

	// Test writing
	data := []byte("test message")
	n, err := rwc.Write(data)
	if err != nil {
		t.Errorf("Unexpected error writing: %v", err)
	}
	expectedLen := len(data) + 2 // 2 bytes for length prefix
	if n != len(data) {
		t.Errorf("Expected to write %d bytes of data, wrote %d", len(data), n)
	}

	// Check that length prefix was written correctly
	written := buf.data.Bytes()
	if len(written) != expectedLen {
		t.Errorf("Expected buffer length %d, got %d", expectedLen, len(written))
	}

	// Test reading (simulate response with length prefix)
	responseData := []byte("response data")
	responseBuf := &mockReadWriteCloser{data: &bytes.Buffer{}}
	responseRWC := newNISReadWriteCloser(responseBuf)

	// Write response data with length prefix
	_, _ = responseRWC.Write(responseData)

	// Now read it back
	readBuf := make([]byte, len(responseData))
	readN, err := responseRWC.Read(readBuf)
	if err != nil {
		t.Errorf("Unexpected error reading: %v", err)
	}
	if readN != len(responseData) {
		t.Errorf("Expected to read %d bytes, read %d", len(responseData), readN)
	}
	if string(readBuf) != string(responseData) {
		t.Errorf("Expected '%s', got '%s'", string(responseData), string(readBuf))
	}
}

func TestNewClient(t *testing.T) {
	// Test with a mock ReadWriteCloser
	buf := &mockReadWriteCloser{data: &bytes.Buffer{}}
	client := New(buf)

	if client == nil {
		t.Fatal("Expected client to be created")
	}
	if client.rwc == nil {
		t.Error("Expected rwc to be set")
	}
}

// Mock ReadWriteCloser for testing
type mockReadWriteCloser struct {
	data *bytes.Buffer
}

func (m *mockReadWriteCloser) Read(p []byte) (n int, err error) {
	return m.data.Read(p)
}

func (m *mockReadWriteCloser) Write(p []byte) (n int, err error) {
	return m.data.Write(p)
}

func (m *mockReadWriteCloser) Close() error {
	return nil
}

func TestClientStatus(t *testing.T) {
	// Create a simple test that just checks the client can be created
	// Full integration testing would require a real APCUPSD server
	buf := &mockReadWriteCloser{data: &bytes.Buffer{}}
	client := New(buf)

	if client == nil {
		t.Error("Expected client to be created")
	}

	// Test that Status() method exists and can be called (will fail due to mock)
	_, err := client.Status()
	if err == nil {
		t.Error("Expected error with mock ReadWriteCloser")
	}
}
