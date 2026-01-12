package ups

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	// timeFormatLong is the package time format of long timestamps from a NIS.
	timeFormatLong = "2006-01-02 15:04:05 -0700"
)

var (
	// errInvalidKeyValuePair is returned when a message is not in the expected
	// "key : value" format.
	errInvalidKeyValuePair = errors.New("invalid key/value pair")

	// errInvalidDuration is returned when a value is not in the expected
	// duration format, e.g. "10 Seconds" or "2 minutes".
	errInvalidDuration = errors.New("invalid time duration")

	// errBufferTooLarge indicates that nisReadWriteCloser.Write was passed a
	// buffer that is too large to send to the NIS.
	errBufferTooLarge = errors.New("apcupsd: buffer too large; must be size of uint16 or less")
)

// Status is the status of an APC Uninterruptible Power Supply (UPS), as
// returned by a NIS.
type Status struct {
	// Header record indicating the STATUS format revision level, the number of records that follow the
	// APC statement, and the number of bytes that follow the record.
	APC string
	// The date and time that the information was last obtained from the UPS.
	Date time.Time
	// The name of the machine that collected the UPS data.
	Hostname string
	// The apcupsd release number, build date, and platform.
	Version string
	// The name of the UPS as stored in the EEPROM or in the UPSNAME directive in the configuration file.
	UPSName string
	// The cable as specified in the configuration file (UPSCABLE).
	Cable string
	// The driver being used to communicate with the UPS.
	Driver string
	// The mode in which apcupsd is operating as specified in the configuration file (UPSMODE)
	UPSMode string
	// The time/date that apcupsd was started.
	StartTime time.Time
	// The UPS model as derived from information from the UPS.
	Model string
	// The current status of the UPS (ONLINE, ONBATT, etc.)
	Status string
	// The current line voltage as returned by the UPS.
	LineVoltage float64
	// The percentage of load capacity as estimated by the UPS.
	LoadPercent float64
	// The percentage charge on the batteries.
	BatteryChargePercent float64
	// The remaining runtime left on batteries as estimated by the UPS.
	TimeLeft time.Duration
	// If the battery charge percentage (BCHARGE) drops below this value, apcupsd will shutdown your
	// system. Value is set in the configuration file (BATTERYLEVEL)
	MinimumBatteryChargePercent float64
	// apcupsd will shutdown your system if the remaining runtime equals or is below this point. Value is set
	// in the configuration file (MINUTES)
	MinimumTimeLeft time.Duration
	// apcupsd will shutdown your system if the time on batteries exceeds this value. A value of zero
	// disables the feature. Value is set in the configuration file (TIMEOUT)
	MaximumTime time.Duration
	// The sensitivity level of the UPS to line voltage fluctuations.
	Sense string
	// The line voltage below which the UPS will switch to batteries.
	LowTransferVoltage float64
	// The line voltage above which the UPS will switch to batteries.
	HighTransferVoltage float64
	// The delay period for the UPS alarm.
	AlarmDel time.Duration
	// Battery voltage as supplied by the UPS.
	BatteryVoltage float64
	// The reason for the last transfer to batteries.
	LastTransfer string
	// The number of transfers to batteries since apcupsd startup.
	NumberTransfers int
	// Time and date of last transfer to batteries, or N/A.
	XOnBattery time.Time
	// Time in seconds currently on batteries, or 0.
	TimeOnBattery time.Duration
	// Total (cumulative) time on batteries in seconds since apcupsd startup.
	CumulativeTimeOnBattery time.Duration
	// Time and date of last transfer from batteries, or N/A.
	XOffBattery time.Time
	// The interval in hours between automatic self tests.
	LastSelftest time.Time
	// The results of the last self test, and may have the following values:
	// • OK: self test indicates good battery
	// • BT: self test failed due to insufficient battery capacity
	// • NG: self test failed due to overload
	// • NO: No results (i.e. no self test performed in the last 5 minutes)
	Selftest bool
	// Status flag. English version is given by STATUS.
	StatusFlags string
	// The UPS serial number
	SerialNumber string
	// The date that batteries were last replaced
	BatteryDate string
	// The input voltage that the UPS is configured to expect.
	NominalInputVoltage float64
	// The nominal battery voltage.
	NominalBatteryVoltage float64
	// The maximum power in Watts that the UPS is designed to supply.
	NominalPower int
	// The firmware revision number as reported by the UPS.
	Firmware string
	// The time and date that the STATUS record was written.
	EndAPC time.Time
	// The ambient temperature as measured by the UPS.
	InternalTemp  float64
	OutputVoltage float64
	LineFrequency float64
	OutputAmps    float64
}

// A key is a field key for an apcupsd status line.
type key string

// List of keys sent by a NIS, used to map values to Status fields.
const (
	keyAlarmDel      key = "ALARMDEL"
	keyAPC           key = "APC"
	keyBattDate      key = "BATTDATE"
	keyBattV         key = "BATTV"
	keyBCharge       key = "BCHARGE"
	keyCable         key = "CABLE"
	keyCumOnBatt     key = "CUMONBATT"
	keyDate          key = "DATE"
	keyDriver        key = "DRIVER"
	keyEndAPC        key = "END APC"
	keyFirmware      key = "FIRMWARE"
	keyHiTrans       key = "HITRANS"
	keyHostname      key = "HOSTNAME"
	keyITemp         key = "ITEMP"
	keyLastStest     key = "LASTSTEST"
	keyLastXfer      key = "LASTXFER"
	keyLineFrequency key = "LINEFREQ"
	keyLineV         key = "LINEV"
	keyLoadPct       key = "LOADPCT"
	keyLoTrans       key = "LOTRANS"
	keyMaxTime       key = "MAXTIME"
	keyMBattChg      key = "MBATTCHG"
	keyMinTimeL      key = "MINTIMEL"
	keyModel         key = "MODEL"
	keyNomBattV      key = "NOMBATTV"
	keyNomInV        key = "NOMINV"
	keyNomPower      key = "NOMPOWER"
	keyNumXfers      key = "NUMXFERS"
	keyOutV          key = "OUTPUTV"
	keyOutputAmps    key = "OUTCURNT"
	keySelftest      key = "SELFTEST"
	keySense         key = "SENSE"
	keySerialNo      key = "SERIALNO"
	keyStartTime     key = "STARTTIME"
	keyStatFlag      key = "STATFLAG"
	keyStatus        key = "STATUS"
	keyTimeLeft      key = "TIMELEFT"
	keyTOnBatt       key = "TONBATT"
	keyUPSMode       key = "UPSMODE"
	keyUPSName       key = "UPSNAME"
	keyVersion       key = "VERSION"
	keyXOffBat       key = "XOFFBATT"
	keyXOnBat        key = "XONBATT"
)

// parseKV parses an input key/value string in "key : value" format, and sets
// the appropriate struct field from the input data.
func (s *Status) parseKV(kv string) error {
	sp := strings.SplitN(kv, ":", 2)
	if len(sp) != 2 {
		return errInvalidKeyValuePair
	}

	var (
		k = key(strings.TrimSpace(sp[0]))
		v = strings.TrimSpace(sp[1])
	)

	// Attempt to match various common data types.

	if match := s.parseKVString(k, v); match {
		return nil
	}

	if match, err := s.parseKVFloat(k, v); match {
		return err
	}

	if match, err := s.parseKVTime(k, v); match {
		return err
	}

	if match, err := s.parseKVDuration(k, v); match {
		return err
	}

	// Attempt to match uncommon data types.

	var err error
	switch k {
	case keyNumXfers:
		s.NumberTransfers, err = strconv.Atoi(v)
	case keyNomPower:
		f := strings.SplitN(v, " ", 2)
		s.NominalPower, err = strconv.Atoi(f[0])
	case keySelftest:
		s.Selftest = v == "YES"
	}

	return err
}

// parseKVString parses a simple string into the appropriate Status field. It
// returns true if a field was matched, and false if not.
func (s *Status) parseKVString(k key, v string) bool {
	switch k {
	case keyAPC:
		s.APC = v
	case keyHostname:
		s.Hostname = v
	case keyVersion:
		s.Version = v
	case keyUPSName:
		s.UPSName = v
	case keyCable:
		s.Cable = v
	case keyDriver:
		s.Driver = v
	case keyUPSMode:
		s.UPSMode = v
	case keyModel:
		s.Model = v
	case keyStatus:
		s.Status = v
	case keySense:
		s.Sense = v
	case keyLastXfer:
		s.LastTransfer = v
	case keyStatFlag:
		s.StatusFlags = v
	case keySerialNo:
		s.SerialNumber = v
	case keyBattDate:
		s.BatteryDate = v
	case keyFirmware:
		s.Firmware = v
	default:
		return false
	}

	return true
}

// parseKVFloat parses a float64 value into the appropriate Status field. It
// returns true if a field was matched, and false if not.
func (s *Status) parseKVFloat(k key, v string) (bool, error) {
	f := strings.SplitN(v, " ", 2)

	// Save repetition for function calls.
	parse := func() (float64, error) {
		return strconv.ParseFloat(f[0], 64)
	}

	var err error
	switch k {
	case keyLineV:
		s.LineVoltage, err = parse()
	case keyLoadPct:
		s.LoadPercent, err = parse()
	case keyBCharge:
		s.BatteryChargePercent, err = parse()
	case keyMBattChg:
		s.MinimumBatteryChargePercent, err = parse()
	case keyLoTrans:
		s.LowTransferVoltage, err = parse()
	case keyHiTrans:
		s.HighTransferVoltage, err = parse()
	case keyBattV:
		s.BatteryVoltage, err = parse()
	case keyNomInV:
		s.NominalInputVoltage, err = parse()
	case keyNomBattV:
		s.NominalBatteryVoltage, err = parse()
	case keyITemp:
		s.InternalTemp, err = parse()
	case keyOutV:
		s.OutputVoltage, err = parse()
	case keyLineFrequency:
		s.LineFrequency, err = parse()
	case keyOutputAmps:
		s.OutputAmps, err = parse()
	default:
		return false, nil
	}

	return true, err
}

// parseKVTime parses a time.Time value into the appropriate Status field. It
// returns true if a field was matched, and false if not.
func (s *Status) parseKVTime(k key, v string) (bool, error) {
	var err error
	switch k {
	case keyDate:
		s.Date, err = parseOptionalTime(v)
	case keyStartTime:
		s.StartTime, err = parseOptionalTime(v)
	case keyXOnBat:
		s.XOnBattery, err = parseOptionalTime(v)
	case keyXOffBat:
		s.XOffBattery, err = parseOptionalTime(v)
	case keyLastStest:
		s.LastSelftest, err = parseOptionalTime(v)
	case keyEndAPC:
		s.EndAPC, err = parseOptionalTime(v)
	default:
		return false, nil
	}

	return true, err
}

// parseKVDuration parses a time.Duration into the appropriate Status field. It
// returns true if a field was matched, and false if not.
func (s *Status) parseKVDuration(k key, v string) (bool, error) {
	// Save repetition for function calls.
	parse := func() (time.Duration, error) {
		return parseDuration(v)
	}

	var err error
	switch k {
	case keyTimeLeft:
		s.TimeLeft, err = parse()
	case keyMinTimeL:
		s.MinimumTimeLeft, err = parse()
	case keyMaxTime:
		s.MaximumTime, err = parse()
	case keyAlarmDel:
		// This field can take a variety of formats, so just ignore any error.
		s.AlarmDel, _ = parse()
		return true, nil
	case keyTOnBatt:
		s.TimeOnBattery, err = parse()
	case keyCumOnBatt:
		s.CumulativeTimeOnBattery, err = parse()
	default:
		return false, nil
	}

	return true, err
}

// parseDuration parses a duration value returned from a NIS as a time.Duration.
func parseDuration(d string) (time.Duration, error) {
	ss := strings.SplitN(d, " ", 2)
	if len(ss) != 2 {
		return 0, errInvalidDuration
	}

	var (
		num  = ss[0]
		unit = ss[1]
	)

	// Normalize units into ones that time.ParseDuration expects.
	switch strings.ToLower(unit) {
	case "minutes":
		unit = "m"
	case "seconds":
		unit = "s"
	}

	return time.ParseDuration(fmt.Sprintf("%s%s", num, unit))
}

// parseOptionalTime parses a time string but also accepts the special value
// "N/A" (which apcupsd reports for some values and conditions); this value is
// mapped to time.Time{}. The caller can check for this with time.IsZero().
func parseOptionalTime(value string) (time.Time, error) {
	if value == "N/A" {
		return time.Time{}, nil
	}

	if time, err := time.Parse(timeFormatLong, value); err == nil {
		return time, nil
	}

	return time.Time{}, fmt.Errorf("can't parse time: %q", value)
}

// Client is a client for the apcupsd Network Information Server (NIS).
type Client struct {
	rwc io.ReadWriteCloser
}

// Dial dials a connection to an NIS using the address on the named network, and
// creates a Client with the connection.
//
// Typically, network will be one of: "tcp", "tcp4", or "tcp6".
func Dial(network, addr string) (*Client, error) {
	return DialContext(context.Background(), network, addr)
}

// DialContext takes a context and dials a connection to an NIS using the
// address on the named network, and creates a Client with the connection.
//
// The provided Context must be non-nil. If the context expires before the
// connection is complete, an error is returned. Once successfully connected,
// any expiration of the context will not affect the connection.
//
// Typically, network will be one of: "tcp", "tcp4", or "tcp6".
func DialContext(ctx context.Context, network, addr string) (*Client, error) {
	var d net.Dialer
	c, err := d.DialContext(ctx, network, addr)
	if err != nil {
		return nil, err
	}

	return New(c), nil
}

// New wraps an existing io.ReadWriteCloser to create a Client for communication
// with an NIS. Client's Close method will close the io.ReadWriteCloser when
// called.
func New(rwc io.ReadWriteCloser) *Client {
	return &Client{rwc: newNISReadWriteCloser(rwc)}
}

// Close closes the connection to an NIS.
func (c *Client) Close() error { return c.rwc.Close() }

const (
	// maxString is the maximum string length for a NIS key/value pair. Value
	// copied from apcupsd source code, v3.14.14.
	maxString = 256
)

// Status retrieves the current UPS status from the NIS.
func (c *Client) Status() (*Status, error) {
	_, err := c.rwc.Write([]byte("status"))
	if err != nil {
		return nil, err
	}

	b := make([]byte, maxString)
	s := new(Status)

	// NIS server sends text lines containing key/value pairs, so must keep
	// iterating until EOF to parse them all.
	for {
		n, err := c.rwc.Read(b)
		if err == io.EOF {
			// Received key/value pair with length 0.
			break
		}
		if err != nil {
			return nil, err
		}

		// Parse key/value pair into appropriate struct field.
		if err := s.parseKV(string(b[:n])); err != nil {
			return nil, err
		}
	}

	return s, nil
}

var _ io.ReadWriteCloser = &nisReadWriteCloser{}

// newNISReadWriteCloser wraps an io.ReadWriteCloser.
func newNISReadWriteCloser(rwc io.ReadWriteCloser) *nisReadWriteCloser {
	return &nisReadWriteCloser{
		rwc:  rwc,
		lenb: make([]byte, 2),
	}
}

// An nisReadWriteCloser wraps an io.ReadWriteCloser with one that can encode
// and decode messages using the NIS's protocol.
type nisReadWriteCloser struct {
	mu   sync.Mutex
	rwc  io.ReadWriteCloser
	lenb []byte
}

// Read reads messages from the NIS using its protocol:
//   - 2 bytes: length of next message
//   - N bytes: data
func (rwc *nisReadWriteCloser) Read(b []byte) (int, error) {
	rwc.mu.Lock()
	defer rwc.mu.Unlock()

	// Read two byte length of next data.
	if _, err := io.ReadFull(rwc.rwc, rwc.lenb); err != nil {
		return 0, err
	}

	// When no more data returned from server, return io.EOF.
	length := binary.BigEndian.Uint16(rwc.lenb)
	if length == 0 {
		return 0, io.EOF
	}

	return io.ReadFull(rwc.rwc, b[:length])
}

// Write writes messages to the NIS using its protocol by prepending each
// message with its 2 byte length.
func (rwc *nisReadWriteCloser) Write(b []byte) (int, error) {
	// Cannot write more than math.MaxUint16 bytes.
	if len(b) > math.MaxUint16 {
		return 0, errBufferTooLarge
	}

	rwc.mu.Lock()
	defer rwc.mu.Unlock()

	// Two byte length of data
	binary.BigEndian.PutUint16(rwc.lenb, uint16(len(b)))

	// Send data and indicate the length of the body to caller
	n, err := rwc.rwc.Write(append(rwc.lenb, b...))
	n -= len(rwc.lenb)
	return n, err
}

// Close closes the underlying io.ReadWriteCloser.
func (rwc *nisReadWriteCloser) Close() error {
	rwc.mu.Lock()
	defer rwc.mu.Unlock()

	return rwc.rwc.Close()
}
