package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

// ANSI-цвета (включаются только для терминала).
const (
	cReset  = "\x1b[0m"
	cGray   = "\x1b[90m"
	cRed    = "\x1b[31m"
	cGreen  = "\x1b[32m"
	cYellow = "\x1b[33m"
	cCyan   = "\x1b[36m"
)

// statsSnapshot — последний снимок статистики ядра.
type statsSnapshot struct {
	Active int64
	Up     int64
	Down   int64
}

// printer печатает события ядра в консоль.
//
// В интерактивном режиме активная строка ввода (prompt) очищается перед
// каждой строкой события и перерисовывается после — частично введённый текст
// при этом визуально исчезает (буфер терминала сохраняется, команда
// выполняется корректно). В режиме connect та же роль у строки статистики,
// которая обновляется через \r без перевода строки.
type printer struct {
	mu          sync.Mutex
	out         io.Writer
	interactive bool
	tty         bool
	color       bool

	activeLine string // "" | "prompt" | "stats" — что сейчас нарисовано на строке

	hadError  bool
	lastStats statsSnapshot
	haveStats bool

	doneCh  chan struct{}
	doneOne sync.Once
}

func newPrinter(interactive bool) *printer {
	p := &printer{
		out:         os.Stdout,
		interactive: interactive,
		doneCh:      make(chan struct{}),
	}
	p.tty = isTerminal(os.Stdout)
	p.color = p.tty && os.Getenv("NO_COLOR") == ""
	if p.tty {
		enableVT()
	}
	return p
}

func isTerminal(f *os.File) bool {
	fi, err := f.Stat()
	if err != nil {
		return false
	}
	return fi.Mode()&os.ModeCharDevice != 0
}

func (p *printer) c(code, s string) string {
	if !p.color {
		return s
	}
	return code + s + cReset
}

func ts() string { return time.Now().Format("15:04:05") }

// clearLocked стирает текущую активную строку (stats/prompt).
func (p *printer) clearLocked() {
	if p.activeLine == "" {
		return
	}
	if p.tty {
		fmt.Fprint(p.out, "\r\x1b[2K")
	}
	p.activeLine = ""
}

// line печатает готовую строку. Если на экране была prompt-строка — она
// стирается, строка печатается, prompt перерисовывается.
func (p *printer) line(s string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	wasPrompt := p.activeLine == "prompt"
	p.clearLocked()
	fmt.Fprintln(p.out, s)
	if wasPrompt && p.interactive {
		p.showPromptLocked()
	}
}

// clearLine — стереть активную строку без печати (используется из обработчика Ctrl+C).
func (p *printer) clearLine() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.clearLocked()
}

func (p *printer) showPrompt() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.clearLocked()
	p.showPromptLocked()
}

func (p *printer) showPromptLocked() {
	fmt.Fprint(p.out, p.c(cCyan, "xwdtt> "))
	p.activeLine = "prompt"
}

// promptDone — пользователь нажал Enter: prompt больше не активен.
func (p *printer) promptDone() {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.activeLine == "prompt" {
		p.activeLine = ""
	}
}

// renderStats — обновляемая строка статистики (только режим connect, только терминал).
func (p *printer) renderStats(s statsSnapshot) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.interactive {
		return // в интерактиве автоматическая печать мешала бы промпту
	}
	text := fmt.Sprintf("воркеры: %d   ↓ %s   ↑ %s", s.Active, formatBytes(s.Up), formatBytes(s.Down))
	if p.tty {
		fmt.Fprintf(p.out, "\r\x1b[2K%s", text)
		p.activeLine = "stats"
	} else {
		fmt.Fprintln(p.out, text)
	}
}

func (p *printer) snapshot() statsSnapshot {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.lastStats
}

func (p *printer) hasStats() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.haveStats
}

func (p *printer) sawError() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.hadError
}

func (p *printer) printSnapshot() {
	p.mu.Lock()
	defer p.mu.Unlock()
	if !p.haveStats {
		fmt.Fprintln(p.out, "статистики пока нет — запустите connect.")
		return
	}
	s := p.lastStats
	fmt.Fprintf(p.out, "воркеры: %d   ↓ %s   ↑ %s\n", s.Active, formatBytes(s.Up), formatBytes(s.Down))
}

// closeDone закрывает канал завершения сессии (однократно).
func (p *printer) closeDone() {
	p.doneOne.Do(func() { close(p.doneCh) })
}

// ═══════════════════════════════════════════════════
// СОБЫТИЯ ЯДРА
// ═══════════════════════════════════════════════════

// OnEvent — приёмник событий Bridge (подключается через backend.NewApp).
func (p *printer) OnEvent(name string, args ...any) {
	switch name {
	case "state_changed":
		status, _ := args[0].(string)
		p.stateLine(status)
	case "log":
		if len(args) < 2 {
			return
		}
		level, _ := args[0].(string)
		msg, _ := args[1].(string)
		if strings.HasPrefix(msg, "[STATS]") {
			return // дубль события stats — у нас свой рендер
		}
		p.logLine(level, msg)
	case "stats":
		if len(args) > 0 {
			if m, ok := args[0].(map[string]any); ok {
				p.stats(m)
			}
		}
	case "error":
		msg, _ := args[0].(string)
		p.errorLine(msg)
	case "captcha_required":
		data, _ := args[0].(string)
		p.captchaLine(data)
	case "event":
		if len(args) > 0 {
			if n, ok := args[0].(string); ok {
				p.line(p.c(cGray, "["+ts()+"] событие: "+n))
			}
		}
	}
}

func (p *printer) stateLine(status string) {
	switch status {
	case "connecting":
		p.line(p.c(cYellow, "["+ts()+"] Подключение..."))
	case "connected":
		p.line(p.c(cGreen, "["+ts()+"] Туннель активен"))
	case "disconnected":
		p.line(p.c(cGray, "["+ts()+"] Туннель остановлен"))
		p.closeDone()
	default:
		p.line("[" + ts() + "] статус: " + status)
	}
}

func (p *printer) logLine(level, msg string) {
	prefix := p.c(cGray, "["+ts()+"]")
	var tag string
	switch strings.ToUpper(level) {
	case "WARN", "WARNING":
		tag = p.c(cYellow, "[WARN]")
	case "ERROR", "FATAL":
		tag = p.c(cRed, "[ERROR]")
	default:
		tag = p.c(cGray, "[INFO]")
	}
	p.line(fmt.Sprintf("%s %s %s", prefix, tag, msg))
}

func (p *printer) errorLine(msg string) {
	p.mu.Lock()
	p.hadError = true
	p.mu.Unlock()
	p.line(p.c(cRed, "["+ts()+"] [ОШИБКА] "+msg))
}

func (p *printer) captchaLine(data string) {
	text := "Требуется капча"
	var m map[string]string
	if err := json.Unmarshal([]byte(data), &m); err == nil {
		if mode := m["mode"]; mode != "" {
			text = "Капча (режим " + mode + ")"
		}
		if u := m["redirect_uri"]; u != "" {
			text += ": откройте " + u
		}
	}
	p.line(p.c(cYellow, "["+ts()+"] "+text))
}

func (p *printer) stats(m map[string]any) {
	s := statsSnapshot{
		Active: anyToInt(m["active"]),
		Up:     anyToInt(m["bytes_up"]),
		Down:   anyToInt(m["bytes_down"]),
	}
	p.mu.Lock()
	p.lastStats = s
	p.haveStats = true
	p.mu.Unlock()
	p.renderStats(s)
}

func anyToInt(v any) int64 {
	switch x := v.(type) {
	case int:
		return int64(x)
	case int32:
		return int64(x)
	case int64:
		return x
	case uint32:
		return int64(x)
	case uint64:
		return int64(x)
	case float64:
		return int64(x)
	default:
		n, _ := strconv.ParseInt(fmt.Sprint(v), 10, 64)
		return n
	}
}
