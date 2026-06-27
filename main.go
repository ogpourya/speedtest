package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

const (
	defaultServer = "https://speedtest.pishgaman.net"
	dlPath        = "/downloading"
	upPath        = "/upload"
	testDuration  = 12 * time.Second
	threads       = 6
	uploadSize    = 30 * 1024 * 1024
)

var (
	serverFlag   = flag.String("server", defaultServer, "speed test server URL")
	downloadFlag = flag.Bool("dl", false, "only test download")
	uploadFlag   = flag.Bool("up", false, "only test upload")

	testCancel context.CancelFunc
)

func main() {
	flag.Parse()

	server := *serverFlag
	doDL := *downloadFlag || (!*downloadFlag && !*uploadFlag)
	doUP := *uploadFlag || (!*downloadFlag && !*uploadFlag)

	p := tea.NewProgram(initialModel(server, doDL, doUP))
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
	server string
	doDL   bool
	doUP   bool

	phase phase

	dlLive int64
	upLive int64
	dlDone bool
	upDone bool
	dlTime time.Time
	upTime time.Time

	dlResult float64
	upResult float64
	dlErr    error
	upErr    error

	spinner spinner.Model
	ready   bool
}

type tickMsg time.Time

func initialModel(server string, doDL, doUP bool) model {
	s := spinner.New()
	s.Spinner = spinner.Line
	s.Style = lipgloss.NewStyle().Foreground(lipgloss.Color("#c7a0ff"))

	m := model{
		server:  server,
		doDL:    doDL,
		doUP:    doUP,
		phase:   phaseDownload,
		spinner: s,
		dlTime:  time.Now(),
		upTime:  time.Now(),
	}

	if !doDL && doUP {
		m.phase = phaseUpload
	}

	return m
}

func (m model) Init() tea.Cmd {
	cmds := []tea.Cmd{m.spinner.Tick}

	ctx, cancel := context.WithCancel(context.Background())
	testCancel = cancel

	if m.doDL {
		cmds = append(cmds, runDownloadCmd(ctx, m.server, &m.dlLive, testDuration))
	} else if m.doUP {
		cmds = append(cmds, runUploadCmd(ctx, m.server, &m.upLive, testDuration))
	}

	cmds = append(cmds, tickEvery(time.Second/2))
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

	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		return m, cmd

	case tickMsg:
		return m, tickEvery(time.Second / 2)

	case dlDoneMsg:
		m.dlResult = msg.speed
		m.dlErr = msg.err
		m.dlDone = true

		if m.doUP {
			m.phase = phaseUpload
			ctx, cancel := context.WithCancel(context.Background())
			testCancel = cancel
			return m, tea.Batch(
				runUploadCmd(ctx, m.server, &m.upLive, testDuration),
				tickEvery(time.Second/2),
			)
		}
		m.phase = phaseDone
		return m, tea.Quit

	case upDoneMsg:
		m.upResult = msg.speed
		m.upErr = msg.err
		m.upDone = true
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

	s := m.spinner.View()

	var dlLine, upLine string

	if m.doDL {
		if m.dlErr != nil {
			dlLine = fmt.Sprintf("Download: error (%v)", m.dlErr)
		} else if m.dlDone {
			dlLine = fmt.Sprintf("Download: %s", fmtSpeed(m.dlResult))
		} else {
			live := speedMbps(atomic.LoadInt64(&m.dlLive), time.Since(m.dlTime))
			dlLine = fmt.Sprintf("%s Download: %s", s, fmtSpeed(live))
		}
	}

	if m.doUP {
		if m.upErr != nil {
			upLine = fmt.Sprintf("Upload:   error (%v)", m.upErr)
		} else if m.upDone {
			upLine = fmt.Sprintf("Upload:   %s", fmtSpeed(m.upResult))
		} else {
			live := speedMbps(atomic.LoadInt64(&m.upLive), time.Since(m.upTime))
			upLine = fmt.Sprintf("%s Upload:   %s", s, fmtSpeed(live))
		}
	}

	var status string
	switch m.phase {
	case phaseDownload:
		status = "Testing download speed..."
	case phaseUpload:
		status = "Testing upload speed..."
	case phaseDone:
		status = "Done!"
	}

	content := lipgloss.NewStyle().Padding(0, 1).Render(status)
	if dlLine != "" {
		content += "\n" + dlLine
	}
	if upLine != "" {
		content += "\n" + upLine
	}
	content += "\n\nPress q to quit"

	return content
}

func runDownloadCmd(ctx context.Context, server string, live *int64, dur time.Duration) tea.Cmd {
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

func runUploadCmd(ctx context.Context, server string, live *int64, dur time.Duration) tea.Cmd {
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

func fmtSpeed(mbps float64) string {
	if mbps >= 1000 {
		return fmt.Sprintf("%.2f Gbps", mbps/1000)
	}
	return fmt.Sprintf("%.2f Mbps", mbps)
}
