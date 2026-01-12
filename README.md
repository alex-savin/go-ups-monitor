# go-ups-monitor

[![CI](https://github.com/alex-savin/go-ups-monitor/actions/workflows/ci.yml/badge.svg)](https://github.com/alex-savin/go-ups-monitor/actions/workflows/ci.yml)
[![Go Report Card](https://goreportcard.com/badge/github.com/alex-savin/go-ups-monitor)](https://goreportcard.com/report/github.com/alex-savin/go-ups-monitor)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](https://opensource.org/licenses/MIT)

A Go library and server for monitoring UPS (Uninterruptible Power Supply) devices via **apcupsd** and **NUT (Network UPS Tools)** protocols.

## Features

- 🔌 Support for both **apcupsd** and **NUT** protocols
- 📡 Real-time status updates via **WebSocket**
- 🌐 RESTful **HTTP API** for status queries and commands
- 📦 **Modular design** - use as a library or standalone server
- 🔄 Automatic reconnection with exponential backoff
- ⚡ Low resource footprint
- 🐳 Docker support

## Installation

### As a Library

```bash
go get github.com/alex-savin/go-ups-monitor
```

### From Source

```bash
git clone https://github.com/alex-savin/go-ups-monitor.git
cd go-ups-monitor
make build
```

### Docker

```bash
docker build -t go-ups-monitor .
docker run -p 8080:8080 -v ./config.yml:/app/config.yml go-ups-monitor
```

## Quick Start

### Standalone Server

1. Copy the sample configuration:
   ```bash
   cp config.sample.yml config.yml
   ```

2. Edit `config.yml` with your UPS details

3. Run the server:
   ```bash
   ./bin/go-ups-monitor
   ```

### As a Library

```go
package main

import (
    "context"
    "log"

    "github.com/alex-savin/go-ups-monitor/config"
    "github.com/alex-savin/go-ups-monitor/monitor"
    "github.com/alex-savin/go-ups-monitor/server"
)

func main() {
    // Load configuration
    cfg, err := config.LoadConfigFrom("config.yml")
    if err != nil {
        log.Fatal(err)
    }

    ctx := context.Background()

    // Create and start monitor
    mon := monitor.New(monitor.DefaultConfig())
    for _, device := range cfg.Devices {
        mon.StartDevice(ctx, device)
    }
    defer mon.StopAll()

    // Create and start server
    srv := server.New(server.DefaultConfig(), cfg, "config.yml", mon)
    if err := srv.Start(ctx); err != nil {
        log.Fatal(err)
    }
}
```

### One-Shot Status Fetch

```go
package main

import (
    "fmt"
    "log"

    "github.com/alex-savin/go-ups-monitor/config"
    "github.com/alex-savin/go-ups-monitor/monitor"
)

func main() {
    device := config.Device{
        Type: "nut",
        Name: "ups",
        Host: "localhost",
        Port: 3493,
    }

    status, err := monitor.FetchStatus(device)
    if err != nil {
        log.Fatal(err)
    }

    fmt.Printf("UPS Status: %s\n", status.Attributes["status"])
    fmt.Printf("Battery: %s%%\n", status.Attributes["battery_charge"])
}
```

## Configuration

```yaml
devices:
  - type: apcupsd          # or "nut"
    name: my-ups
    host: localhost
    port: 3551             # 3551 for apcupsd, 3493 for NUT
    username: ""           # Required for NUT with authentication
    password: ""
    selected_attributes:   # Optional: filter returned attributes
      - Status
      - BatteryChargePercent
      - TimeLeft
      - LoadPercent

logging:
  level: info              # debug, info, warn, error
  output: stdout           # stdout or stderr
  source: false            # Include source file in logs

server:
  host: 0.0.0.0
  port: 8080
```

## API Reference

### HTTP Endpoints

| Endpoint | Method | Description |
|----------|--------|-------------|
| `/health` | GET | Health check |
| `/api/status` | GET | Get status of all UPS devices |
| `/api/devices` | GET | List configured devices |
| `/api/device` | POST | Add a new device |
| `/api/device?name=xxx` | DELETE | Remove a device |
| `/api/device/test` | POST | Test connection to a device |
| `/api/commands?type=nut` | GET | List supported commands |
| `/api/command` | POST | Execute a command |

### WebSocket

Connect to `/ws` for real-time status updates. The server broadcasts status updates every 5 seconds.

```javascript
const ws = new WebSocket('ws://localhost:8080/ws');
ws.onmessage = (event) => {
    const statuses = JSON.parse(event.data);
    console.log(statuses);
};
```

### Example Responses

**GET /api/status**
```json
[
  {
    "device_name": "my-ups",
    "type": "apcupsd",
    "attributes": {
      "status": "ONLINE",
      "battery_charge": "100",
      "time_left": "3600",
      "load_percentage": "25",
      "input_voltage": "120.0"
    }
  }
]
```

**POST /api/command**
```json
{
  "device": "my-ups",
  "command": "test.battery.start"
}
```

## Supported Commands

### NUT
- `beeper.disable` / `beeper.enable` / `beeper.mute`
- `test.battery.start` / `test.battery.stop`
- `calibrate.start` / `calibrate.stop`
- `load.off` / `load.on`
- `shutdown.return` / `shutdown.stayoff`

### apcupsd
Commands are currently read-only for apcupsd devices.

## Package Structure

```
go-ups-monitor/
├── cmd/go-ups-monitor/  # CLI entry point
├── config/              # Configuration types and status management
├── monitor/             # Device polling and status fetching
├── server/              # HTTP/WebSocket server
└── ups/                 # Low-level apcupsd and NUT clients
```

## Development

```bash
# Run tests
make test

# Run tests with coverage
make test-coverage

# Format code
make fmt

# Run linter
make lint

# Build
make build

# Full pipeline
make all
```

## Home Assistant Integration

This project includes a custom Home Assistant integration in a separate repository. See the [ups_monitor integration](https://github.com/alex-savin/hassio-addon-ups-monitor-repo) for details.

## Contributing

Contributions are welcome! Please feel free to submit a Pull Request.

1. Fork the repository
2. Create your feature branch (`git checkout -b feature/amazing-feature`)
3. Commit your changes (`git commit -m 'Add amazing feature'`)
4. Push to the branch (`git push origin feature/amazing-feature`)
5. Open a Pull Request

## License

This project is licensed under the MIT License - see the [LICENSE](LICENSE) file for details.

## Acknowledgments

- [apcupsd](http://www.apcupsd.org/) - APC UPS daemon
- [NUT](https://networkupstools.org/) - Network UPS Tools
