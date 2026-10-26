package core

// Тесты валидации протокольных ответов (дефект анализа: RAWCONF принимался
// без проверки ip/dns/mtu, а ответ конфига читался в буфер 4096).

import (
	"net"
	"strings"
	"testing"
)

// pipeWithResponse возвращает conn, с другой стороны которого «сервер»
// читает запрос и отвечает resp.
func pipeWithResponse(t *testing.T, resp string) net.Conn {
	t.Helper()
	server, client := net.Pipe()
	t.Cleanup(func() { server.Close(); client.Close() })
	go func() {
		buf := make([]byte, 65536)
		if _, err := server.Read(buf); err != nil {
			return
		}
		_, _ = server.Write([]byte(resp))
	}()
	return client
}

func TestRequestRawConfig_Validation(t *testing.T) {
	cases := []struct {
		name    string
		resp    string
		wantErr string // "" = без ошибки; "FATAL_AUTH" = ошибка авторизации
	}{
		{"валидный", "RAWCONF:10.66.0.11|1.1.1.1,8.8.8.8|1280", ""},
		{"пустой ip (сервер ещё не назначил)", "RAWCONF:|1.1.1.1|1280", ""},
		{"пустой dns", "RAWCONF:10.66.0.11||1280", ""},
		{"битый ip", "RAWCONF:not-an-ip|1.1.1.1|1280", "некорректный IP"},
		{"битый dns", "RAWCONF:10.66.0.11|nope,1.1.1.1|1280", "некорректный DNS"},
		{"mtu ноль", "RAWCONF:10.66.0.11|1.1.1.1|0", "MTU вне диапазона"},
		{"mtu отрицательный", "RAWCONF:10.66.0.11|1.1.1.1|-5", "MTU вне диапазона"},
		{"mtu слишком большой", "RAWCONF:10.66.0.11|1.1.1.1|99999", "MTU вне диапазона"},
		{"mtu не число", "RAWCONF:10.66.0.11|1.1.1.1|abc", "некорректный MTU"},
		{"мусор вместо RAWCONF", "BLABLA", "неожиданный ответ"},
		{"не хватает полей", "RAWCONF:10.66.0.11|1.1.1.1", "некорректный формат"},
		{"DENIED:wrong_password", "DENIED:wrong_password", "FATAL_AUTH"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			conn := pipeWithResponse(t, tc.resp)
			ip, dns, mtu, err := RequestRawConfig(conn, "dev", "pass")
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("неожиданная ошибка: %v", err)
				}
				_ = ip
				_ = dns
				_ = mtu
				return
			}
			if err == nil {
				t.Fatalf("ожидали ошибку %q, получили ip=%q dns=%q mtu=%d", tc.wantErr, ip, dns, mtu)
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Errorf("ошибка %q не содержит %q", err.Error(), tc.wantErr)
			}
		})
	}
}

func TestRequestRawConfig_ValidFields(t *testing.T) {
	conn := pipeWithResponse(t, "RAWCONF:10.66.0.11|1.1.1.1,8.8.8.8|1280")
	ip, dns, mtu, err := RequestRawConfig(conn, "dev", "pass")
	if err != nil {
		t.Fatalf("RequestRawConfig: %v", err)
	}
	if ip != "10.66.0.11" || dns != "1.1.1.1,8.8.8.8" || mtu != 1280 {
		t.Errorf("got ip=%q dns=%q mtu=%d", ip, dns, mtu)
	}
}

func TestRequestConfig_DeniedAndNoconf(t *testing.T) {
	for _, tc := range []struct {
		resp    string
		wantErr string
	}{
		{"NOCONF", ""},
		{"DENIED:expired", "FATAL_AUTH"},
	} {
		conn := pipeWithResponse(t, tc.resp)
		_, err := RequestConfig(conn, "12345", "dev", "pass")
		if tc.wantErr == "" {
			if err != nil {
				t.Errorf("NOCONF: %v", err)
			}
			continue
		}
		if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
			t.Errorf("got %v, want %q", err, tc.wantErr)
		}
	}
}

// TestRequestRawConfig_LongResponse: ответ в 64 КБ не обрезается
// (раньше буфер был 4096).
func TestRequestRawConfig_LongResponse(t *testing.T) {
	const prefix = "RAWCONF:10.66.0.11|"
	// DNS-поле с большим списком резолверов (реальный ответ сервера
	// теоретически может быть длиннее старого лимита 4096).
	var sb strings.Builder
	sb.WriteString(prefix)
	for i := 0; i < 300; i++ {
		if i > 0 {
			sb.WriteString(",")
		}
		sb.WriteString("2001:4860:4860::8888")
	}
	sb.WriteString("|1280")
	resp := sb.String()
	if len(resp) <= 4096 {
		t.Fatalf("тест бессмыслен: ответ %d байт <= 4096", len(resp))
	}

	conn := pipeWithResponse(t, resp)
	_, dns, _, err := RequestRawConfig(conn, "dev", "pass")
	if err != nil {
		t.Fatalf("длинный ответ: %v", err)
	}
	if got := len(strings.Split(dns, ",")); got != 300 {
		t.Errorf("обрезан ответ: резолверов %d, ожидалось 300", got)
	}
}
