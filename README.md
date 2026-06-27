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

### Help

```bash
speedtest --help
```

## Example

```
Testing download speed...
◌ Download: 124.53 Mbps
◌ Upload:   12.34 Mbps

Press q to quit
```
