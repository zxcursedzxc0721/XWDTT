package core

// Ping (Этап 4.5): замер RTT до TURN-сервера через полный путь
// TURN Allocate (+ DTLS-хендшейк) — перенос RunPing из
// proxy-turn-vk-android/go_client/session.go.
//
// В raw-режиме DTLS не используется (сервер его не понимает): RTT меряем
// на запросе GETCONF_RAW — сервер обязан ответить RAWCONF/NOCONF.

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"strings"
	"sync"
	"time"

	"github.com/cbeuw/connutil"
	"github.com/pion/dtls/v3"
	"github.com/pion/dtls/v3/pkg/crypto/selfsign"
	"github.com/pion/turn/v5"
)

// RunPing выполняет TURN-аллокацию и замеряет RTT в мс:
//   - классический режим — DTLS-хендшейк поверх obfs-пайпа (как в референсе);
//   - raw/no-dtls — запрос конфига через RTP-obfs напрямую поверх relay
//     (сервер обязан ответить RAWCONF/NOCONF/AUTH-ответом, что и есть RTT).
//
// turnURLs — список TURN-адресов из GetCreds, берётся первый (Host/Port из
// tp переопределяют хост/порт, как и в RunSession). deviceID/password —
// реквизиты подключения к серверу (нужны прямому режиму).
func RunPing(
	ctx context.Context,
	tp *TurnParams,
	peer *net.UDPAddr,
	turnUser, turnPass string,
	turnURLs []string,
	deviceID, password string,
) (int64, error) {
	startPing := time.Now()

	if len(turnURLs) == 0 {
		return 0, fmt.Errorf("нет TURN URL")
	}
	selectedURL := turnURLs[0]

	urlhost, urlport, err := net.SplitHostPort(selectedURL)
	if err != nil {
		return 0, err
	}
	if tp.Host != "" {
		urlhost = tp.Host
	}
	if tp.Port != "" {
		urlport = tp.Port
	}
	turnAddr := net.JoinHostPort(urlhost, urlport)

	turnConn, err := dialTURNConn(turnAddr, tp.TCPTransport)
	if err != nil {
		return 0, err
	}
	defer turnConn.Close()

	var addrFamily turn.RequestedAddressFamily
	if peer.IP.To4() != nil {
		addrFamily = turn.RequestedAddressFamilyIPv4
	} else {
		addrFamily = turn.RequestedAddressFamilyIPv6
	}

	tc, err := turn.NewClient(&turn.ClientConfig{
		STUNServerAddr:         turnAddr,
		TURNServerAddr:         turnAddr,
		Conn:                   turnConn,
		Username:               turnUser,
		Password:               turnPass,
		RequestedAddressFamily: addrFamily,
		LoggerFactory:          &NullLoggerFactory{},
	})
	if err != nil {
		return 0, err
	}
	defer tc.Close()

	if err = tc.Listen(); err != nil {
		return 0, err
	}

	relay, err := tc.Allocate()
	if err != nil {
		return 0, fmt.Errorf("TURN Allocate: %w", err)
	}
	defer relay.Close()

	if (tp.RawMode || tp.NoDTLS) && len(tp.WrapKey) == wrapKeyLen {
		// ─── Прямой режим: RTT на запросе конфига через RTP-obfs ───
		return pingDirect(ctx, tp, peer, relay, deviceID, password)
	}

	// ─── Классический режим: DTLS-хендшейк поверх obfs-пайпа ───
	pipeA, pipeB := connutil.AsyncPacketPipe()
	defer pipeA.Close()
	defer pipeB.Close()

	sessCtx, sessCancel := context.WithCancel(ctx)
	defer sessCancel()

	useWrap := len(tp.WrapKey) == wrapKeyLen
	var obfsCfg *ObfsConfig
	var obfsWriteState *ObfsState
	if useWrap {
		obfsCfg = NewObfsConfig(tp.ObfsMode)
		obfsWriteState = NewObfsState()
	}

	var relayWg sync.WaitGroup
	relayWg.Add(2)

	// relay → pipeA
	go func() {
		defer relayWg.Done()
		defer sessCancel()
		buf := make([]byte, readBufSize+80)
		plain := make([]byte, readBufSize)
		var replay replayWindow
		for {
			n, _, readErr := relay.ReadFrom(buf)
			if readErr != nil {
				return
			}
			payload := buf[:n]
			if useWrap {
				if !obfsIsRTPPacket(payload) {
					continue
				}
				m, unwrapErr := obfsUnwrapPacket(tp.WrapKey, payload, plain)
				if unwrapErr != nil {
					continue
				}
				if !replay.accept(payload) {
					continue
				}
				payload = plain[:m]
			}
			_, _ = pipeA.WriteTo(payload, peer)
		}
	}()

	// pipeA → relay
	go func() {
		defer relayWg.Done()
		defer sessCancel()
		b := make([]byte, readBufSize)
		for {
			n, _, readErr := pipeA.ReadFrom(b)
			if readErr != nil {
				return
			}
			out := b[:n]
			if useWrap && obfsCfg != nil && obfsWriteState != nil {
				wrapped, wrapErr := obfsWrapPacket(tp.WrapKey, out, obfsCfg, obfsWriteState)
				if wrapErr != nil {
					return
				}
				out = wrapped
			}
			_, _ = relay.WriteTo(out, peer)
		}
	}()

	cert, certErr := selfsign.GenerateSelfSigned()
	if certErr != nil {
		return 0, certErr
	}

	dtlsCfg := &dtls.Config{
		Certificates:          []tls.Certificate{cert},
		InsecureSkipVerify:    true,
		ExtendedMasterSecret:  dtls.RequireExtendedMasterSecret,
		CipherSuites:          []dtls.CipherSuiteID{dtls.TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256},
		ConnectionIDGenerator: dtls.OnlySendCIDGenerator(),
		MTU:                   1100,
	}

	dtlsConn, dtlsErr := dtls.Client(pipeB, peer, dtlsCfg)
	if dtlsErr != nil {
		return 0, dtlsErr
	}
	defer dtlsConn.Close()

	hctx, hcancel := context.WithTimeout(sessCtx, 15*time.Second)
	defer hcancel()

	if hsErr := dtlsConn.HandshakeContext(hctx); hsErr != nil {
		return 0, hsErr
	}

	// Handshake completes -> we have a successful round trip!
	return time.Since(startPing).Milliseconds(), nil
}

// pingDirect — RTT в режиме без DTLS: пишем GETCONF_RAW (raw) или AUTH
// (no-dtls) через RTP-obfs напрямую поверх relay и ждём ответ.
func pingDirect(ctx context.Context, tp *TurnParams, peer *net.UDPAddr, relay net.PacketConn, deviceID, password string) (int64, error) {
	startPing := time.Now()

	cfg := NewObfsConfig(tp.ObfsMode)
	state := NewObfsState()

	// Простой симплекс: одна запись и одно чтение с дедлайном.
	_ = relay.SetDeadline(time.Now().Add(20 * time.Second))
	defer relay.SetDeadline(time.Time{})

	var req string
	if tp.RawMode {
		req = fmt.Sprintf("GETCONF_RAW:%s|%s", deviceID, password)
	} else {
		req = fmt.Sprintf("AUTH:%s|%s", deviceID, password)
	}
	wrapped, wrapErr := obfsWrapPacket(tp.WrapKey, []byte(req), cfg, state)
	if wrapErr != nil {
		return 0, fmt.Errorf("obfs wrap: %w", wrapErr)
	}
	if _, writeErr := relay.WriteTo(wrapped, peer); writeErr != nil {
		return 0, writeErr
	}

	// Ждём первый валидный ответ — этого достаточно для RTT.
	buf := make([]byte, readBufSize+80)
	plain := make([]byte, readBufSize)
	var replay replayWindow
	for {
		select {
		case <-ctx.Done():
			return 0, ctx.Err()
		default:
		}
		n, _, readErr := relay.ReadFrom(buf)
		if readErr != nil {
			return 0, readErr
		}
		payload := buf[:n]
		if !obfsIsRTPPacket(payload) {
			continue
		}
		if _, unwrapErr := obfsUnwrapPacket(tp.WrapKey, payload, plain); unwrapErr != nil {
			continue
		}
		if !replay.accept(payload) {
			continue
		}
		return time.Since(startPing).Milliseconds(), nil
	}
}

// DeriveWrapKey — экспорт deriveWrapKey для CLI (ping/check).
func DeriveWrapKey(password string) ([]byte, error) {
	return deriveWrapKey(password)
}

// ShortHash печатает хеш в сокращённом виде (первые 8 символов).
func ShortHash(h string) string {
	if len(h) <= 8 {
		return h
	}
	return strings.TrimSpace(h[:8]) + "…"
}
