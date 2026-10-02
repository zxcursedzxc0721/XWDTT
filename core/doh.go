package core

// DoH (DNS-over-HTTPS) для резолва VK — перенос из
// proxy-turn-vk-android/go_client/doh.go (пакет main → core).
//
// Работает как подмена net.DefaultResolver: система ходит в DoH-эндпоинт
// POST-запросом (application/dns-message), поэтому DNS-трафик не виден DPI
// на порту 53. Bootstrap-соединение к самому эндпоинту идёт через обычный
// системный резолвер (PreferGo: false) — иначе была бы рекурсия.

import (
	"bytes"
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	neturl "net/url"
	"strings"
	"sync"
	"time"

	"golang.org/x/net/http2"
)

// goDNSIsDoH — является ли аргумент DoH-пресетом или произвольным URL.
func goDNSIsDoH(arg string) bool {
	arg = strings.ToLower(strings.TrimSpace(arg))
	return strings.HasPrefix(arg, "doh:") || strings.HasPrefix(arg, "doh-")
}

// goDoHEndpointsForPreset — эндпоинты для известных пресетов.
func goDoHEndpointsForPreset(preset string) []string {
	switch strings.ToLower(strings.TrimSpace(preset)) {
	case "doh-cloudflare":
		return []string{
			"https://1.1.1.1/dns-query",
			"https://cloudflare-dns.com/dns-query",
		}
	case "doh-google":
		return []string{
			"https://8.8.8.8/dns-query",
			"https://dns.google/dns-query",
		}
	default:
		return []string{
			"https://77.88.8.8/dns-query",
			"https://common.dot.dns.yandex.net/dns-query",
		}
	}
}

// goDoHEndpointsForArg — эндпоинты по аргументу: "doh:url1,url2" или пресет.
func goDoHEndpointsForArg(arg string) []string {
	arg = strings.TrimSpace(arg)
	if strings.HasPrefix(arg, "doh:") {
		raw := strings.TrimPrefix(arg, "doh:")
		var out []string
		for _, part := range strings.Split(raw, ",") {
			part = strings.TrimSpace(part)
			if part == "" {
				continue
			}
			if ep, ok := normalizeDoHEndpoint(part); ok {
				out = append(out, ep)
			}
		}
		if len(out) > 0 {
			return out
		}
		return goDoHEndpointsForPreset("doh-yandex")
	}
	return goDoHEndpointsForPreset(arg)
}

// normalizeDoHEndpoint — принимает только https://URL, путь по умолчанию /dns-query.
func normalizeDoHEndpoint(raw string) (string, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", false
	}
	if !strings.HasPrefix(strings.ToLower(raw), "https://") {
		return "", false
	}
	u, err := neturl.Parse(raw)
	if err != nil || u.Scheme != "https" || u.Host == "" {
		return "", false
	}
	path := strings.TrimSpace(u.Path)
	if path == "" || path == "/" {
		path = "/dns-query"
	}
	u.Path = path
	u.RawQuery = ""
	u.Fragment = ""
	return u.String(), true
}

// dohHostHeader — SNI/Host для эндоинта, заданного по IP (сертификат
// выпускается на доменное имя, IP-SNI не пройдёт проверку).
func dohHostHeader(endpoint string) string {
	u, err := neturl.Parse(endpoint)
	if err != nil {
		return ""
	}
	host := u.Hostname()
	if net.ParseIP(host) == nil {
		return ""
	}
	switch host {
	case "1.1.1.1", "1.0.0.1":
		return "cloudflare-dns.com"
	case "8.8.8.8", "8.8.4.4":
		return "dns.google"
	case "77.88.8.8", "77.88.8.1":
		return "common.dot.dns.yandex.net"
	default:
		return ""
	}
}

// formatDoHEndpoints — человекочитаемый список эндпоинтов для лога.
func formatDoHEndpoints(endpoints []string) string {
	seen := make(map[string]struct{})
	parts := make([]string, 0, len(endpoints))
	for _, ep := range endpoints {
		label := dohEndpointLabel(ep)
		if label == "" {
			continue
		}
		if _, ok := seen[label]; ok {
			continue
		}
		seen[label] = struct{}{}
		parts = append(parts, label)
	}
	return strings.Join(parts, ", ")
}

func dohEndpointLabel(endpoint string) string {
	if host := dohHostHeader(endpoint); host != "" {
		return host
	}
	u, err := neturl.Parse(endpoint)
	if err != nil {
		return endpoint
	}
	host := u.Hostname()
	if path := strings.Trim(u.Path, "/"); path != "" && path != "dns-query" {
		return host + "/" + path
	}
	return host
}

// newDoHBootstrapDialer — соединение к самому DoH-серверу идёт через
// системный DNS (не DoH) — иначе рекурсия при резолве DoH-эндпоинта.
func newDoHBootstrapDialer() *net.Dialer {
	return &net.Dialer{
		Timeout:   5 * time.Second,
		KeepAlive: 30 * time.Second,
		Resolver: &net.Resolver{
			PreferGo: false,
		},
	}
}

// newDoHHTTPClient — HTTP/2-клиент для DoH (h2 обязателен для
// cloudflare/google, http1.1 они тоже принимают, но медленнее).
func newDoHHTTPClient() *http.Client {
	bootstrap := newDoHBootstrapDialer()
	transport := &http2.Transport{
		DialTLSContext: func(ctx context.Context, network, addr string, cfg *tls.Config) (net.Conn, error) {
			host, _, err := net.SplitHostPort(addr)
			if err != nil {
				return nil, err
			}
			serverName := host
			if sni := dohHostHeader("https://" + host + "/"); sni != "" {
				serverName = sni
			}
			if cfg == nil {
				cfg = &tls.Config{}
			} else {
				cfg = cfg.Clone()
			}
			cfg.ServerName = serverName
			conn, err := bootstrap.DialContext(ctx, network, addr)
			if err != nil {
				return nil, err
			}
			tlsConn := tls.Client(conn, cfg)
			if err := tlsConn.HandshakeContext(ctx); err != nil {
				_ = conn.Close()
				return nil, err
			}
			return tlsConn, nil
		},
	}

	return &http.Client{
		Timeout:   10 * time.Second,
		Transport: transport,
	}
}

// stripDNSWireFrame — Go-резолвер пишет TCP DNS с длиновым префиксом
// (RFC 7766); для HTTP-запроса нужен голый wire-формат.
func stripDNSWireFrame(payload []byte) []byte {
	if len(payload) < 4 {
		return payload
	}
	ln := int(payload[0])<<8 | int(payload[1])
	if ln > 0 && ln+2 == len(payload) {
		return payload[2:]
	}
	return payload
}

// frameDNSForTCP — обратно навешиваем префикс длины для ответа резолверу.
func frameDNSForTCP(payload []byte) []byte {
	out := make([]byte, 2+len(payload))
	out[0] = byte(len(payload) >> 8)
	out[1] = byte(len(payload))
	copy(out[2:], payload)
	return out
}

// dohConn — фиктивный net.Conn: один Write = один DoH-обмен, ответ
// отдаётся через Read. Так Go-резолвер может использовать любой Dial.
type dohConn struct {
	mu        sync.Mutex
	endpoints []string
	client    *http.Client
	response  []byte
	readPos   int
	closed    bool
}

func newDohConn(endpoints []string, client *http.Client) *dohConn {
	return &dohConn{
		endpoints: append([]string(nil), endpoints...),
		client:    client,
	}
}

func (c *dohConn) Read(b []byte) (int, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return 0, net.ErrClosed
	}
	if len(c.response) == 0 {
		return 0, fmt.Errorf("doh: read before write")
	}
	if c.readPos >= len(c.response) {
		return 0, io.EOF
	}
	n := copy(b, c.response[c.readPos:])
	c.readPos += n
	return n, nil
}

func (c *dohConn) Write(b []byte) (int, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return 0, net.ErrClosed
	}

	// Go treats any custom net.Conn as TCP DNS (RFC 7766 length prefix).
	query := stripDNSWireFrame(b)
	var lastErr error
	for _, endpoint := range c.endpoints {
		body, err := c.exchange(endpoint, query)
		if err == nil {
			c.response = frameDNSForTCP(body)
			c.readPos = 0
			return len(b), nil
		}
		lastErr = err
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("no DoH endpoints available")
	}
	return 0, lastErr
}

func (c *dohConn) exchange(endpoint string, query []byte) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(query))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/dns-message")
	req.Header.Set("Content-Type", "application/dns-message")
	if host := dohHostHeader(endpoint); host != "" {
		req.Host = host
	}

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("doh: HTTP %d from %s", resp.StatusCode, endpoint)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 65535))
	if err != nil {
		return nil, err
	}
	if len(body) < 12 {
		return nil, fmt.Errorf("doh: short response from %s", endpoint)
	}
	return body, nil
}

func (c *dohConn) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.closed = true
	c.response = nil
	c.readPos = 0
	return nil
}

func (c *dohConn) LocalAddr() net.Addr              { return &net.TCPAddr{IP: net.IPv4zero, Port: 0} }
func (c *dohConn) RemoteAddr() net.Addr             { return &net.TCPAddr{IP: net.IPv4zero, Port: 443} }
func (c *dohConn) SetDeadline(time.Time) error      { return nil }
func (c *dohConn) SetReadDeadline(time.Time) error  { return nil }
func (c *dohConn) SetWriteDeadline(time.Time) error { return nil }

// setupDoHResolver — подмена net.DefaultResolver на DoH.
func setupDoHResolver(arg string, endpoints []string) {
	client := newDoHHTTPClient()

	log.Printf(
		"[КЛИЕНТ] DNS для VK: %s (%s) — DoH",
		goDNSLabel(arg),
		formatDoHEndpoints(endpoints),
	)

	net.DefaultResolver = &net.Resolver{
		PreferGo: true,
		Dial: func(ctx context.Context, network, address string) (net.Conn, error) {
			_ = ctx
			_ = network
			_ = address
			if len(endpoints) == 0 {
				return nil, fmt.Errorf("no DoH endpoints configured for %q", arg)
			}
			return newDohConn(endpoints, client), nil
		},
	}
}
