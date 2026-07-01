# speedtest

CLI speed test based on [Pishgaman Speed Test](https://speedtest.pishgaman.net) using [OpenSpeedTest](https://github.com/openspeedtest) protocol.

```
  Download ⠀⢀⣤⣴⣶⣶⣶⣿⣿⣿⣿⣿⣿⣿⣿⣿⣿⣿⣿⣿   134.4 Mbps  peak 134.4 Mbps
  Upload   ⠀⠀⠀⢀⣀⣤⣤⣴⣶⣶⣶⣶⣶⣶⣧⣤⣤⣀⣀⡀    12.3 Mbps  peak  12.5 Mbps
```

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
