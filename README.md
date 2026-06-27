# speedtest

CLI speed test based on [Pishgaman Speed Test](https://speedtest.pishgaman.net) using [OpenSpeedTest](https://github.com/openspeedtest) protocol. Built with [Bubbletea](https://github.com/charmbracelet/bubbletea) TUI.

## Install

```bash
go install github.com/ogpourya/speedtest@latest
```

## Usage

### Default (download + upload)

```bash
speedtest
```

### Download only

```bash
speedtest --dl
```

### Upload only

```bash
speedtest --up
```

### Custom server

```bash
speedtest --server https://custom-server.com
```

### Connection mode

```bash
speedtest --single          # single connection
speedtest --threads 12      # 12 concurrent connections (default: 6)
```

### Help

```bash
speedtest --help
```

## Example

```
Testing download speed (6 conn)...
◌ Download: 124.53 Mbps
Testing upload speed (6 conn)...
◌ Upload:   12.34 Mbps

Press q to quit
```
