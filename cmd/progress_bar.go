package cmd

import (
	"bytes"
	"fmt"
	"io"
	"log"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/atomicobject/rhizome/pkg/logging"
	"golang.org/x/term"
)

type terminalHint interface {
	IsTerminal() bool
}

// progressBar renders a lightweight progress indicator to stderr/stdout.
type progressBar struct {
	out        io.Writer
	ownedTTY   *os.File
	width      int
	lastLen    int
	active     bool
	label      string
	showCounts bool
	tty        bool
	lastLog    time.Time
	lastPct    int
	lastDone   int
	lastTotal  int
	lastRender time.Time
	mu         sync.Mutex
}

const progressBarTailGap = 18

func newProgressBar(out io.Writer) *progressBar {
	return &progressBar{out: out, width: 28, label: "Indexing", showCounts: true}
}

var openProgressTTY = func() *os.File {
	f, err := os.OpenFile("/dev/tty", os.O_WRONLY, 0)
	if err != nil {
		return nil
	}
	if !term.IsTerminal(int(f.Fd())) {
		_ = f.Close()
		return nil
	}
	return f
}

func newCLIProgressBar(primary io.Writer) *progressBar {
	if tty := openProgressTTY(); tty != nil {
		// Cobra often wraps stderr in a writer that looks non-terminal. For the
		// default CLI UX we prefer the real tty when available so the progress bar
		// can render in place instead of degrading into line-by-line updates.
		bar := newProgressBar(tty)
		bar.ownedTTY = tty
		bar.tty = true
		bar.showCounts = false
		bar.lastPct = -1
		return bar
	}
	bar := newProgressBarWithLabel(preferredProgressWriter(primary, os.Stderr), "Indexing")
	bar.showCounts = false
	return bar
}

func preferredProgressWriter(primary, fallback io.Writer) io.Writer {
	if isTerminalWriter(primary) {
		return primary
	}
	if isTerminalWriter(fallback) {
		return fallback
	}
	if primary != nil {
		return primary
	}
	return fallback
}

func newProgressBarWithLabel(out io.Writer, label string) *progressBar {
	bar := newProgressBar(out)
	if strings.TrimSpace(label) != "" {
		bar.label = label
	}
	bar.tty = isTerminalWriter(out)
	bar.lastPct = -1
	return bar
}

type progressAwareWriter struct {
	bar *progressBar
	out io.Writer
}

func (p *progressBar) WrapWriter(out io.Writer) io.Writer {
	if p == nil || out == nil {
		return out
	}
	return &progressAwareWriter{bar: p, out: out}
}

type progressLogFilterWriter struct {
	out     io.Writer
	verbose bool
}

func withProgressLogOutput(bar *progressBar, out io.Writer, verbose bool) func() {
	if bar == nil {
		return func() {}
	}
	prevFlags := log.Flags()
	prevPrefix := log.Prefix()
	prevWriter := log.Writer()
	if out != nil {
		var writer io.Writer = &progressLogFilterWriter{
			out:     bar.WrapWriter(out),
			verbose: verbose,
		}
		if logging.StandardInstalled() {
			writer = io.MultiWriter(prevWriter, writer)
		}
		log.SetOutput(writer)
	}
	return func() {
		log.SetOutput(prevWriter)
		log.SetFlags(prevFlags)
		log.SetPrefix(prevPrefix)
	}
}

func (p *progressBar) Println(msg string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.clearLine()
	fmt.Fprintln(p.out, msg)
}

func (p *progressBar) SetLabel(label string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	label = strings.TrimSpace(label)
	if label == "Refreshing ontology" {
		label = "Ontology graph refresh"
	}
	if label == "" || label == p.label {
		return
	}
	p.label = label
	if !p.active {
		return
	}
	if !p.tty {
		p.lastLog = time.Time{}
	}
	p.renderLocked()
}

func (p *progressBar) Update(done, total int) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if total < 0 {
		total = 0
	}
	if done < 0 {
		done = 0
	} else if total > 0 && done > total {
		done = total
	}
	p.lastDone = done
	p.lastTotal = total

	if !p.tty {
		p.updateNonTTY(done, total)
		return
	}
	if !p.shouldRenderTTY(done, total) {
		return
	}

	p.renderLocked()
}

func (p *progressBar) Close() {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.active && p.lastLen > 0 {
		if p.tty {
			fmt.Fprint(p.out, "\n")
		}
	}
	p.active = false
	p.lastLen = 0
	if p.ownedTTY != nil {
		_ = p.ownedTTY.Close()
		p.ownedTTY = nil
	}
}

func (p *progressBar) clearLine() {
	if !p.active || p.lastLen == 0 {
		return
	}
	p.eraseLocked()
	p.active = false
	p.lastLen = 0
}

func (p *progressBar) updateNonTTY(done, total int) {
	if total <= 0 {
		// Avoid spamming logs when we don't know the total.
		if time.Since(p.lastLog) >= 5*time.Second || p.lastLog.IsZero() {
			if p.showCounts {
				fmt.Fprintf(p.out, "%s %d/?\n", p.label, done)
			} else {
				fmt.Fprintf(p.out, "%s\n", p.label)
			}
			p.lastLog = time.Now()
		}
		p.active = true
		return
	}

	if done > total {
		done = total
	}
	pct := 0
	if total > 0 {
		pct = done * 100 / total
	}

	shouldLog := false
	switch {
	case p.lastLog.IsZero():
		shouldLog = true
	case done == total:
		shouldLog = true
	case pct != p.lastPct && pct%5 == 0:
		shouldLog = true
	case time.Since(p.lastLog) >= 10*time.Second:
		shouldLog = true
	}

	if shouldLog {
		if p.showCounts {
			fmt.Fprintf(p.out, "%s %d/%d (%d%%)\n", p.label, done, total, pct)
		} else {
			fmt.Fprintf(p.out, "%s %d%%\n", p.label, pct)
		}
		p.lastLog = time.Now()
		p.lastPct = pct
	}
	p.active = done < total
}

func (p *progressBar) shouldRenderTTY(done, total int) bool {
	if p.lastRender.IsZero() || !p.active {
		return true
	}
	if total > 0 && done >= total {
		return true
	}
	if done == 0 && total == 0 {
		return time.Since(p.lastRender) >= 250*time.Millisecond
	}
	return time.Since(p.lastRender) >= 80*time.Millisecond
}

func (p *progressBar) renderWidth(suffixLen int) int {
	width := p.width
	if cols := terminalWidth(p.out); cols > 0 {
		width = cols - len(p.label) - suffixLen - 4 - progressBarTailGap
	}
	if width < 10 {
		width = 10
	}
	return width
}

func (p *progressBar) eraseLocked() {
	fmt.Fprintf(p.out, "\r%s\r", strings.Repeat(" ", p.lastLen))
}

func (p *progressBar) renderLocked() {
	line := p.renderLineLocked()
	if len(line) < p.lastLen {
		line += strings.Repeat(" ", p.lastLen-len(line))
	}
	fmt.Fprint(p.out, line)
	p.lastLen = len(line)
	p.active = true
	p.lastRender = time.Now()
}

func (p *progressBar) renderLineLocked() string {
	if p.lastTotal <= 0 {
		if p.showCounts {
			return fmt.Sprintf("\r%s [?] %d/?", p.label, p.lastDone)
		}
		return fmt.Sprintf("\r%s [?]", p.label)
	}
	percent := p.lastDone * 100 / p.lastTotal
	suffix := fmt.Sprintf(" %3d%%", percent)
	if p.showCounts {
		suffix = fmt.Sprintf(" %d/%d %3d%%", p.lastDone, p.lastTotal, percent)
	}
	barWidth := p.renderWidth(len(suffix))
	filled := barWidth * p.lastDone / p.lastTotal
	if filled > barWidth {
		filled = barWidth
	}
	empty := barWidth - filled

	var bar strings.Builder
	switch {
	case barWidth <= 0:
	case p.lastDone >= p.lastTotal:
		bar.WriteString(strings.Repeat("=", barWidth))
	default:
		if filled > 0 {
			bar.WriteString(strings.Repeat("=", filled))
		}
		if empty > 0 {
			bar.WriteString(">")
			if empty > 1 {
				bar.WriteString(strings.Repeat(" ", empty-1))
			}
		}
	}
	return fmt.Sprintf("\r%s [%s]%s", p.label, bar.String(), suffix)
}

func shouldSuppressProgressLogLine(line string) bool {
	line = strings.TrimSpace(line)
	if line == "" {
		return false
	}
	lower := strings.ToLower(line)
	if strings.Contains(lower, "warning") || strings.Contains(lower, "error") || strings.Contains(lower, "failed") {
		return false
	}
	// Use Contains so patterns match even when the log timestamp prefix is present
	// (e.g. "2026/04/07 11:20:58 [index] ..." still contains "[index] ").
	switch {
	case strings.Contains(line, "[csharp index]"):
		return true
	case strings.Contains(line, "RebuildCallEdgesForPaths: "):
		return true
	case strings.Contains(line, "RebuildAllCallEdges: "):
		return true
	case strings.Contains(line, "[index] ") && strings.Contains(line, " writes="):
		return true
	case strings.Contains(line, "watcher: computing embeddings"):
		return true
	case strings.Contains(line, "watcher: embeddings complete"):
		return true
	case strings.Contains(line, "watcher: detected change"):
		return true
	case strings.Contains(line, "codeanchor: watcher ready"):
		return true
	default:
		return false
	}
}

func (w *progressLogFilterWriter) Write(p []byte) (int, error) {
	if w == nil || w.out == nil || len(p) == 0 {
		return 0, nil
	}
	if w.verbose {
		return w.out.Write(p)
	}

	var kept bytes.Buffer
	for line := range bytes.Lines(p) {
		if !shouldSuppressProgressLogLine(string(line)) {
			kept.Write(line)
		}
	}
	if kept.Len() == 0 {
		return len(p), nil
	}

	n, err := w.out.Write(kept.Bytes())
	if n >= kept.Len() {
		return len(p), err
	}
	if err == nil {
		err = io.ErrShortWrite
	}
	// Translate the retained-byte count back to the caller's input so retries
	// resume after both written bytes and intentionally suppressed lines.
	consumed := 0
	for line := range bytes.Lines(p) {
		if !shouldSuppressProgressLogLine(string(line)) {
			if n < len(line) {
				return consumed + n, err
			}
			n -= len(line)
		}
		consumed += len(line)
	}
	return consumed, err
}

func (w *progressAwareWriter) Write(p []byte) (int, error) {
	if w == nil || w.out == nil || len(p) == 0 {
		return 0, nil
	}
	if w.bar == nil || !w.bar.tty {
		return w.out.Write(p)
	}

	w.bar.mu.Lock()
	shouldRestore := w.bar.active && w.bar.lastLen > 0
	if shouldRestore {
		// Clear the live bar before any non-bar output so timing/debug logs do not
		// get glued onto the progress line. If the write emitted a newline, repaint
		// the bar afterwards so long-running phases stay visually steady.
		w.bar.eraseLocked()
	}
	w.bar.mu.Unlock()

	n, err := w.out.Write(p)
	if shouldRestore && n > 0 && bytes.IndexAny(p[:n], "\r\n") >= 0 {
		w.bar.mu.Lock()
		if w.bar.lastTotal > 0 || w.bar.lastDone > 0 {
			w.bar.renderLocked()
		}
		w.bar.mu.Unlock()
	}
	return n, err
}

func isTerminalWriter(w io.Writer) bool {
	if hint, ok := w.(terminalHint); ok {
		return hint.IsTerminal()
	}
	if f, ok := w.(*os.File); ok {
		return term.IsTerminal(int(f.Fd()))
	}
	if fd, ok := w.(interface{ Fd() uintptr }); ok {
		return term.IsTerminal(int(fd.Fd()))
	}
	return false
}

func terminalWidth(w io.Writer) int {
	if f, ok := w.(*os.File); ok {
		if width, _, err := term.GetSize(int(f.Fd())); err == nil {
			return width
		}
	}
	if fd, ok := w.(interface{ Fd() uintptr }); ok {
		if width, _, err := term.GetSize(int(fd.Fd())); err == nil {
			return width
		}
	}
	return 0
}
