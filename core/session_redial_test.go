package core

// Регрессия к симптому «подключено, но трафика нет»: UDP-сокет к TURN,
// чей локальный адрес «протух» (Windows + Cloudflare WARP: пересоздание
// адаптера даёт wsasend EINVAL на каждой записи), должен пересоздаваться
// при ошибке записи, а не умирать до конца попытки Allocate.

import (
	"bytes"
	"net"
	"testing"
	"time"
)

func TestConnectedUDPConnRedialOnWriteError(t *testing.T) {
	ln, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("слушатель: %v", err)
	}
	defer ln.Close()

	dst, err := net.ResolveUDPAddr("udp", ln.LocalAddr().String())
	if err != nil {
		t.Fatalf("резолв: %v", err)
	}
	c, err := newConnectedUDP(dst)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer c.Close()

	// «Протухший» сокет: закрыт из-под нас (на Windows так ломается
	// connected-сокет при пересоздании VPN-адаптера).
	c.mu.Lock()
	if err := c.conn.Close(); err != nil {
		c.mu.Unlock()
		t.Fatalf("закрыть сокет: %v", err)
	}
	c.mu.Unlock()

	const payload = "redial-test"
	n, err := c.Write([]byte(payload))
	if err != nil {
		t.Fatalf("Write после «протухшего» сокета: %v", err)
	}
	if n != len(payload) {
		t.Fatalf("Write вернул %d, ожидалось %d", n, len(payload))
	}

	c.mu.Lock()
	gen := c.generation
	c.mu.Unlock()
	if gen != 1 {
		t.Fatalf("generation = %d, ожидалось 1 (одно пересоздание)", gen)
	}

	// Пакет дошёл по НОВОМУ сокету.
	_ = ln.SetReadDeadline(time.Now().Add(2 * time.Second))
	buf := make([]byte, 64)
	nr, _, err := ln.ReadFrom(buf)
	if err != nil {
		t.Fatalf("чтение у слушателя: %v", err)
	}
	if !bytes.Equal(buf[:nr], []byte(payload)) {
		t.Fatalf("получено %q, ожидалось %q", buf[:nr], payload)
	}

	// ReadFrom после пересоздания работает на новом сокете.
	errCh := make(chan error, 1)
	go func() {
		rb := make([]byte, 64)
		_, _, rerr := c.ReadFrom(rb)
		errCh <- rerr
	}()
	time.Sleep(50 * time.Millisecond)
	if _, err := ln.WriteTo([]byte("reply"), c.LocalAddr()); err != nil {
		t.Fatalf("ответ слушателя: %v", err)
	}
	select {
	case rerr := <-errCh:
		if rerr != nil {
			t.Fatalf("ReadFrom после пересоздания: %v", rerr)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("ReadFrom завис после пересоздания сокета")
	}
}

func TestConnectedUDPConnClosedWrite(t *testing.T) {
	dst, err := net.ResolveUDPAddr("udp", "127.0.0.1:9")
	if err != nil {
		t.Fatalf("резолв: %v", err)
	}
	c, err := newConnectedUDP(dst)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	if err := c.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if _, err := c.Write([]byte("x")); err == nil {
		t.Fatal("Write после Close должен возвращать ошибку")
	}
	if _, _, err := c.ReadFrom(make([]byte, 1)); err == nil {
		t.Fatal("ReadFrom после Close должен возвращать ошибку")
	}
}
