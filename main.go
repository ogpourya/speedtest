package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"flag"
	"fmt"
	"io"
	"log"
	"math"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

const (
	defaultServer = "https://speedtest.pishgaman.net"
	dlPath        = "/downloading"
	upPath        = "/upload"
	testDuration  = 12 * time.Second
	uploadSize    = 30 * 1024 * 1024
	sparkWidth    = 20
)

var (
	serverFlag   = flag.String("server", defaultServer, "speed test server URL")
	downloadFlag = flag.Bool("dl", false, "only test download")
	uploadFlag   = flag.Bool("up", false, "only test upload")
	singleFlag   = flag.Bool("single", false, "single connection mode")
	threadsFlag  = flag.Int("threads", 6, "number of concurrent connections")

	testCancel context.CancelFunc
)

var (
	accentSty = lipgloss.NewStyle().Foreground(lipgloss.Color("#2EF8BB"))
	dimSty    = lipgloss.NewStyle().Foreground(lipgloss.Color("#626262"))
	boldSty   = lipgloss.NewStyle().Bold(true)
	labelSty  = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#2EF8BB"))
	errSty    = lipgloss.NewStyle().Foreground(lipgloss.Color("#FF6B6B"))
	unitSty   = lipgloss.NewStyle().Foreground(lipgloss.Color("#626262"))
	baseSty   = lipgloss.NewStyle().Padding(1, 2)
)

func main() {
	flag.Parse()

	server := *serverFlag
	doDL := *downloadFlag || (!*downloadFlag && !*uploadFlag)
	doUP := *uploadFlag || (!*downloadFlag && !*uploadFlag)
	threads := *threadsFlag
	if *singleFlag {
		threads = 1
	}
	if threads < 1 {
		threads = 1
	}

	p := tea.NewProgram(initialModel(server, doDL, doUP, threads))
	if _, err := p.Run(); err != nil {
		log.Fatal(err)
	}
}

type phase int

const (
	phaseDownload phase = iota
	phaseUpload
	phaseDone
)

type model struct {
	server  string
	doDL    bool
	doUP    bool
	threads int

	phase phase

	dlLive *int64
	upLive *int64
	dlDone bool
	upDone bool
	dlTime time.Time
	upTime time.Time

	dlResult float64
	upResult float64
	dlErr    error
	upErr    error

	dlSamples []float64
	upSamples []float64
	dlPeak    float64
	upPeak    float64

	ready bool
}

type tickMsg time.Time

func initialModel(server string, doDL, doUP bool, threads int) model {
	m := model{
		server:  server,
		doDL:    doDL,
		doUP:    doUP,
		threads: threads,
		phase:   phaseDownload,
		dlLive:  new(int64),
		upLive:  new(int64),
		dlTime:  time.Now(),
		upTime:  time.Now(),
	}

	if !doDL && doUP {
		m.phase = phaseUpload
	}

	return m
}

func (m model) Init() tea.Cmd {
	ctx, cancel := context.WithCancel(context.Background())
	testCancel = cancel

	cmds := []tea.Cmd{tickEvery(200 * time.Millisecond)}

	if m.doDL {
		cmds = append(cmds, runDownloadCmd(ctx, m.server, m.dlLive, testDuration, m.threads))
	} else if m.doUP {
		cmds = append(cmds, runUploadCmd(ctx, m.server, m.upLive, testDuration, m.threads))
	}

	return tea.Batch(cmds...)
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.ready = true

	case tea.KeyMsg:
		switch msg.String() {
		case "q", "ctrl+c":
			if testCancel != nil {
				testCancel()
			}
			return m, tea.Quit
		}

	case tickMsg:
		if !m.dlDone && m.doDL {
			speed := speedMbps(atomic.LoadInt64(m.dlLive), time.Since(m.dlTime))
			m.dlSamples = append(m.dlSamples, speed)
			if speed > m.dlPeak {
				m.dlPeak = speed
			}
		}
		if !m.upDone && m.doUP {
			speed := speedMbps(atomic.LoadInt64(m.upLive), time.Since(m.upTime))
			m.upSamples = append(m.upSamples, speed)
			if speed > m.upPeak {
				m.upPeak = speed
			}
		}
		return m, tickEvery(200 * time.Millisecond)

	case dlDoneMsg:
		m.dlResult = msg.speed
		m.dlErr = msg.err
		m.dlDone = true
		if msg.speed > m.dlPeak {
			m.dlPeak = msg.speed
		}

		if m.doUP {
			m.phase = phaseUpload
			m.upTime = time.Now()
			ctx, cancel := context.WithCancel(context.Background())
			testCancel = cancel
			return m, tea.Batch(
				runUploadCmd(ctx, m.server, m.upLive, testDuration, m.threads),
				tickEvery(200*time.Millisecond),
			)
		}
		m.phase = phaseDone
		return m, tea.Quit

	case upDoneMsg:
		m.upResult = msg.speed
		m.upErr = msg.err
		m.upDone = true
		if msg.speed > m.upPeak {
			m.upPeak = msg.speed
		}
		m.phase = phaseDone
		return m, tea.Quit
	}

	return m, nil
}

type dlDoneMsg struct {
	speed float64
	err   error
}
type upDoneMsg struct {
	speed float64
	err   error
}

func (m model) View() string {
	if !m.ready {
		return ""
	}

	var lines []string

	if m.doDL {
		var spd float64
		if m.dlDone {
			spd = m.dlResult
		} else {
			spd = speedMbps(atomic.LoadInt64(m.dlLive), time.Since(m.dlTime))
		}
		lines = append(lines, testLine("Download", m.dlSamples, spd, m.dlPeak, m.dlErr))
	}

	if m.doUP {
		var spd float64
		if m.upDone {
			spd = m.upResult
		} else {
			spd = speedMbps(atomic.LoadInt64(m.upLive), time.Since(m.upTime))
		}
		lines = append(lines, testLine("Upload", m.upSamples, spd, m.upPeak, m.upErr))
	}

	content := lipgloss.JoinVertical(lipgloss.Left, lines...)

	if m.phase != phaseDone {
		content += "\n" + dimSty.Render("q: quit")
	}

	return baseSty.Render(content)
}

func testLine(name string, samples []float64, speed, peak float64, testErr error) string {
	if testErr != nil {
		return fmt.Sprintf("%s %s",
			labelSty.Render(fmt.Sprintf("%-8s", name)),
			errSty.Render(fmt.Sprintf("error (%v)", testErr)),
		)
	}

	label := labelSty.Render(fmt.Sprintf("%-8s", name))

	spark := accentSty.Render(sparkline(samples, sparkWidth))

	speedNum := boldSty.Render(fmt.Sprintf("%8.1f", speed))
	speedUnit := unitSty.Render(fmtSpeed(speed, true))
	speedStr := speedNum + speedUnit

	var peakStr string
	if peak > 0 {
		p := boldSty.Render(fmt.Sprintf("%5.1f", peak))
		u := unitSty.Render(fmtSpeed(peak, true))
		peakStr = dimSty.Render(" peak ") + p + u
	}

	return fmt.Sprintf("%s %s  %s%s", label, spark, speedStr, peakStr)
}

func sparkline(data []float64, width int) string {
	if len(data) == 0 || width == 0 {
		return ""
	}

	bars := []rune("▁▂▃▄▅▆▇█")
	maxVal := 0.0
	for _, v := range data {
		if v > maxVal {
			maxVal = v
		}
	}
	if maxVal == 0 {
		maxVal = 1
	}

	n := len(data)
	runes := make([]rune, width)

	for i := range width {
		idx := i * n / width
		if idx >= n {
			idx = n - 1
		}
		level := int(math.Round(data[idx] / maxVal * 7))
		if level < 0 {
			level = 0
		}
		if level > 7 {
			level = 7
		}
		runes[i] = bars[level]
	}

	return string(runes)
}

func runDownloadCmd(ctx context.Context, server string, live *int64, dur time.Duration, threads int) tea.Cmd {
	return func() tea.Msg {
		tr := &http.Transport{
			MaxIdleConnsPerHost: threads,
			MaxConnsPerHost:     threads,
		}
		client := &http.Client{Transport: tr}
		defer client.CloseIdleConnections()

		var wg sync.WaitGroup
		done := make(chan struct{})

		for range threads {
			wg.Add(1)
			go func() {
				defer wg.Done()
				buf := make([]byte, 64*1024)
				for {
					select {
					case <-done:
						return
					case <-ctx.Done():
						return
					default:
					}
					req, err := http.NewRequest("GET", server+dlPath, nil)
					if err != nil {
						return
					}
					q := req.URL.Query()
					q.Set("n", fmt.Sprintf("%d", time.Now().UnixNano()))
					req.URL.RawQuery = q.Encode()

					resp, err := client.Do(req)
					if err != nil {
						continue
					}
					for {
						n, err := resp.Body.Read(buf)
						if n > 0 {
							atomic.AddInt64(live, int64(n))
						}
						if err != nil {
							break
						}
					}
					resp.Body.Close()
				}
			}()
		}

		select {
		case <-ctx.Done():
			close(done)
			wg.Wait()
			return dlDoneMsg{err: ctx.Err()}
		case <-time.After(dur):
			close(done)
			wg.Wait()
		}

		total := atomic.LoadInt64(live)
		if total == 0 {
			return dlDoneMsg{err: fmt.Errorf("no data received")}
		}
		return dlDoneMsg{speed: speedMbps(total, dur)}
	}
}

func runUploadCmd(ctx context.Context, server string, live *int64, dur time.Duration, threads int) tea.Cmd {
	return func() tea.Msg {
		data := make([]byte, uploadSize)
		if _, err := rand.Read(data); err != nil {
			return upDoneMsg{err: fmt.Errorf("failed to generate data: %w", err)}
		}

		tr := &http.Transport{
			MaxIdleConnsPerHost: threads,
			MaxConnsPerHost:     threads,
		}
		client := &http.Client{Transport: tr}
		defer client.CloseIdleConnections()

		var wg sync.WaitGroup
		done := make(chan struct{})

		for range threads {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for {
					select {
					case <-done:
						return
					case <-ctx.Done():
						return
					default:
					}
					body := &countingReader{r: bytes.NewReader(data), total: live}
					req, err := http.NewRequest("POST", server+upPath, body)
					if err != nil {
						continue
					}
					q := req.URL.Query()
					q.Set("n", fmt.Sprintf("%d", time.Now().UnixNano()))
					req.URL.RawQuery = q.Encode()
					req.Header.Set("Content-Type", "application/octet-stream")
					req.ContentLength = int64(len(data))

					resp, err := client.Do(req)
					if err != nil {
						continue
					}
					io.Copy(io.Discard, resp.Body)
					resp.Body.Close()
				}
			}()
		}

		select {
		case <-ctx.Done():
			close(done)
			wg.Wait()
			return upDoneMsg{err: ctx.Err()}
		case <-time.After(dur):
			close(done)
			wg.Wait()
		}

		total := atomic.LoadInt64(live)
		if total == 0 {
			return upDoneMsg{err: fmt.Errorf("no data sent")}
		}
		return upDoneMsg{speed: speedMbps(total, dur)}
	}
}

type countingReader struct {
	r     io.Reader
	total *int64
}

func (c *countingReader) Read(p []byte) (int, error) {
	n, err := c.r.Read(p)
	if n > 0 {
		atomic.AddInt64(c.total, int64(n))
	}
	return n, err
}

func tickEvery(d time.Duration) tea.Cmd {
	return tea.Tick(d, func(t time.Time) tea.Msg {
		return tickMsg(t)
	})
}

func speedMbps(total int64, elapsed time.Duration) float64 {
	s := elapsed.Seconds()
	if s <= 0 {
		return 0
	}
	return float64(total) * 8 / s / 1_000_000
}

func fmtSpeed(mbps float64, short bool) string {
	if mbps >= 1000 {
		if short {
			return " Gbps"
		}
		return fmt.Sprintf("%.2f Gbps", mbps/1000)
	}
	if short {
		return " Mbps"
	}
	return fmt.Sprintf("%.2f Mbps", mbps)
}
