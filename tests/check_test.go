package backend_test

import (
	"fmt"
	"testing"

	"pwdtt/cli"
)

// ═══════════════════════════════════════════════════
// classifyHashCheckError (Этап 4.5: servers check)
// ═══════════════════════════════════════════════════

func TestClassifyHashCheckError(t *testing.T) {
	cases := []struct {
		err    error
		status string
	}{
		{nil, "ok"},
		{fmt.Errorf("captcha_required"), "captcha"},
		{fmt.Errorf("CAPTCHA_WAIT_REQUIRED please"), "captcha"},
		{fmt.Errorf("call not found"), "dead"},
		{fmt.Errorf("missing turn_server"), "dead"},
		{fmt.Errorf("flood control"), "limited"},
		{fmt.Errorf("rate limit exceeded"), "limited"},
		{fmt.Errorf("net: lookup vk.com: i/o timeout"), "network"},
		{fmt.Errorf("connection deadline exceeded"), "network"},
		{fmt.Errorf("что-то неизвестное"), "error"},
	}
	for _, c := range cases {
		status, msg := cli.ClassifyHashCheckError(c.err)
		if status != c.status {
			t.Errorf("classify(%v) = %q, ожидалось %q", c.err, status, c.status)
		}
		if status != "ok" && msg == "" {
			t.Errorf("classify(%v): пустое сообщение при статусе %q", c.err, status)
		}
	}
}
