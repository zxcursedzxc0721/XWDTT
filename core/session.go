package core

import (
	"context"
	"crypto/tls"
	"fmt"
	"log"
	"math/rand"
	"net"
	"strings"
	"sync"
	"time"

	"github.com/cbeuw/connutil"
	"github.com/pion/dtls/v3"
	"github.com/pion/dtls/v3/pkg/crypto/selfsign"
	"github.com/pion/turn/v5"
)

const (
	workerSendBuf      = 128
	sessionReadTimeout = 30 * time.Minute
	readBufSize        = 1600
	socketBufSize      = 625 * 1024
	keepaliveByte      = 0xFF // keepalive marker (DTLS-level или прямой obfs-кадр)

	// keepaliveInterval: 10с (как у референсного клиента) — агрессивнее держит
	// TURN permission/NAT-маппинг "тёплым" на каждом relay-сокете сессии,
	// чем прежние 15с.
	keepaliveInterval = 10 * time.Second

	// keepaliveMinSize/keepaliveMaxSize: keepalive-пакет не фиксированного
	// размера (было 1 байт постоянно) — случайная длина 25-44 байта имитирует
	// "тишину" OPUS в реальном звонке; постоянный размер через равные
	// интервалы — легко узнаваемый паттерн для DPI.
	keepaliveMinSize = 25
	keepaliveMaxSize = 20 // диапазон добавки к keepaliveMinSize (rand.Intn(20))

	// writeDeadline — дедлайн записи в транспорт. 3с, не sessionReadTimeout
	// (30 мин): запись пишет в UDP-сокет к TURN relay, а не читает
	// долгоживущее соединение; 30-минутный дедлайн означал, что зависшая
	// запись (переполненный сокет-буфер ОС, плохая сеть) держала бы Writer
	// колом полчаса, пока очередь SendCh/PrioCh этого воркера копится и/или
	// дропается диспетчером выше (см. readLoop в dispatcher.go).
	writeDeadline = 3 * time.Second
)

type connectedUDPConn struct{ *net.UDPConn }

func (c *connectedUDPConn) WriteTo(p []byte, _ net.Addr) (int, error) { return c.Write(p) }

func dialTURNConn(turnAddr string, tcp bool) (net.PacketConn, error) {
	if !tcp {
		resolved, err := net.ResolveUDPAddr("udp", turnAddr)
		if err != nil {
			return nil, fmt.Errorf("резолв TURN: %w", err)
		}
		c, err := net.DialUDP("udp", nil, resolved)
		if err != nil {
			return nil, fmt.Errorf("подключение TURN UDP: %w", err)
		}
		_ = c.SetReadBuffer(socketBufSize)
		_ = c.SetWriteBuffer(socketBufSize)
		return &connectedUDPConn{c}, nil
	}

	d := net.Dialer{Timeout: 10 * time.Second}
	c, err := d.Dial("tcp", turnAddr)
	if err != nil {
		return nil, fmt.Errorf("подключение TURN TCP: %w", err)
	}
	return turn.NewSTUNConn(c), nil
}

// SessionErrorType — тип ошибки сессии
type SessionErrorType string

const (
	// ADDRESS_DEAD — этот конкретный TURN-адрес не работает (квота, unreachable, timeout)
	SessionErrorAddressDead SessionErrorType = "ADDRESS_DEAD"
	// WRAP_TIMEOUT — DTLS-рукопожатие не прошло, нужно сменить обфускацию
	SessionErrorWrapTimeout SessionErrorType = "WRAP_TIMEOUT"
	// AUTH — TURN отклонил креды (401/протухшие); нужно обновить креды и повторить
	SessionErrorAuth SessionErrorType = "AUTH"
	// FATAL — невосстановимая ошибка (FATAL_AUTH, хеш мёртв и т.д.)
	SessionErrorFatal SessionErrorType = "FATAL"
)

// SessionError — структурированная ошибка сессии
type SessionError struct {
	Type    SessionErrorType
	Address string // TURN-адрес, на котором произошла ошибка (если применимо)
	Err     error
}

func (e *SessionError) Error() string {
	if e.Err != nil {
		return fmt.Sprintf("%s: %v (addr=%s)", e.Type, e.Err, e.Address)
	}
	return fmt.Sprintf("%s (addr=%s)", e.Type, e.Address)
}

func (e *SessionError) Unwrap() error { return e.Err }

// obfsDirectConn — net.Conn поверх TURN relay БЕЗ DTLS.
//
// RTP-obfs (ChaCha20-Poly1305 AEAD, obfs.go) уже даёт полноценное
// шифрование+аутентификацию каждого пакета. DTLS поверх него был чистым
// оверхедом: self-signed сертификат с InsecureSkipVerify не добавлял
// реальной защиты, зато удваивал AEAD-обработку на пакет и требовал
// отдельного хендшейка на каждую из 9 воркер-сессий.
// Используется только когда tp.NoDTLS=true И useWrap=true (иначе тут вообще
// нет шифрования — тогда обязателен DTLS).
// Перенос из proxy-turn-vk-android/go_client/session.go.
type obfsDirectConn struct {
	relay      net.PacketConn
	peer       net.Addr
	wrapKey    []byte
	cfg        *ObfsConfig
	writeState *ObfsState
	replay     replayWindow
}

func (c *obfsDirectConn) Read(b []byte) (int, error) {
	wire := make([]byte, len(b)+80) // RTP-заголовок(12) + AEAD tag + padding
	for {
		n, _, err := c.relay.ReadFrom(wire)
		if err != nil {
			return 0, err
		}
		if !obfsIsRTPPacket(wire[:n]) {
			continue
		}
		m, unwrapErr := obfsUnwrapPacket(c.wrapKey, wire[:n], b)
		if unwrapErr != nil {
			continue
		}
		if !c.replay.accept(wire[:n]) {
			continue // повтор/слишком старый кадр
		}
		return m, nil
	}
}

func (c *obfsDirectConn) Write(b []byte) (int, error) {
	wrapped, err := obfsWrapPacket(c.wrapKey, b, c.cfg, c.writeState)
	if err != nil {
		return 0, err
	}
	if _, err := c.relay.WriteTo(wrapped, c.peer); err != nil {
		return 0, err
	}
	return len(b), nil
}

func (c *obfsDirectConn) Close() error                       { return nil }
func (c *obfsDirectConn) LocalAddr() net.Addr                { return c.relay.LocalAddr() }
func (c *obfsDirectConn) RemoteAddr() net.Addr               { return c.peer }
func (c *obfsDirectConn) SetDeadline(t time.Time) error      { return c.relay.SetDeadline(t) }
func (c *obfsDirectConn) SetReadDeadline(t time.Time) error  { return c.relay.SetReadDeadline(t) }
func (c *obfsDirectConn) SetWriteDeadline(t time.Time) error { return c.relay.SetWriteDeadline(t) }

// handshakeSem ограничивает число одновременных DTLS-хендшейков (3),
// чтобы не выжигать CPU на одновременном старте всех воркеров.
var handshakeSem = make(chan struct{}, 3)

// RunSession устанавливает TURN-сессию с указанным адресом.
// В отличие от предыдущей версии, адрес передаётся явно, а не выбирается по индексу.
func RunSession(
	ctx context.Context,
	tp *TurnParams,
	peer *net.UDPAddr,
	d *Dispatcher,
	localPort string,
	getConfig bool,
	configCh chan<- string,
	sessionID int,
	turnAddr string, // конкретный TURN-адрес
	turnUser string, // username для TURN
	turnPass string, // password для TURN
	obfsMode string, // режим обфускации (пер-воркерский, может меняться на WRAP_TIMEOUT)
	cacheStreamID int, // для handleAuthError
	deviceID, password string,
	stats *Stats,
	allocateGate <-chan time.Time, // общий rate-limit Allocate на группу (может быть nil)
) (bool, *SessionError) {
	configDelivered := false

	if turnAddr == "" {
		return false, &SessionError{
			Type: SessionErrorFatal,
			Err:  fmt.Errorf("пустой TURN-адрес"),
		}
	}

	urlhost, urlport, err := net.SplitHostPort(turnAddr)
	if err != nil {
		return false, &SessionError{
			Type:    SessionErrorAddressDead,
			Address: turnAddr,
			Err:     fmt.Errorf("разбор TURN-адреса: %w", err),
		}
	}
	if tp.Host != "" {
		urlhost = tp.Host
	}
	if tp.Port != "" {
		urlport = tp.Port
	}
	turnAddrResolved := net.JoinHostPort(urlhost, urlport)

	turnConn, err := dialTURNConn(turnAddrResolved, tp.TCPTransport)
	if err != nil {
		return false, &SessionError{
			Type:    SessionErrorAddressDead,
			Address: turnAddr,
			Err:     err,
		}
	}
	defer turnConn.Close()

	if tp.TCPTransport {
		log.Printf("[СЕССИЯ #%d] TURN TCP (%s)", sessionID, turnAddr)
	} else {
		log.Printf("[СЕССИЯ #%d] TURN UDP (%s)", sessionID, turnAddr)
	}

	var addrFamily turn.RequestedAddressFamily
	if peer.IP.To4() != nil {
		addrFamily = turn.RequestedAddressFamilyIPv4
	} else {
		addrFamily = turn.RequestedAddressFamilyIPv6
	}

	tc, err := turn.NewClient(&turn.ClientConfig{
		STUNServerAddr:         turnAddrResolved,
		TURNServerAddr:         turnAddrResolved,
		Conn:                   turnConn,
		Username:               turnUser,
		Password:               turnPass,
		RequestedAddressFamily: addrFamily,
		LoggerFactory:          &NullLoggerFactory{},
	})
	if err != nil {
		return false, &SessionError{
			Type:    SessionErrorAddressDead,
			Address: turnAddr,
			Err:     fmt.Errorf("TURN клиент: %w", err),
		}
	}
	defer tc.Close()

	if err = tc.Listen(); err != nil {
		return false, &SessionError{
			Type:    SessionErrorAddressDead,
			Address: turnAddr,
			Err:     fmt.Errorf("TURN Listen: %w", err),
		}
	}

	// Глобальный rate-limit на сам момент TURN Allocate (не только на старт
	// горутины воркера, см. workerStartDelay в worker_group.go) — не более
	// одной новой аллокации за тик по всей группе. Без этого стаггеринг старта
	// не спасает: на нестабильной сети (ретраи/задержки Allocate) несколько
	// воркеров всё равно накладываются друг на друга и вместе выжигают
	// VK-квоту (error 486) быстрее, чем должны.
	if allocateGate != nil {
		select {
		case <-allocateGate:
		case <-ctx.Done():
			return false, &SessionError{
				Type:    SessionErrorFatal,
				Address: turnAddr,
				Err:     ctx.Err(),
			}
		}
	}

	relay, err := tc.Allocate()
	if err != nil {
		errStr := err.Error()
		errLower := strings.ToLower(errStr)

		// Проверяем на ошибки авторизации (кеш кредов)
		if isAuthError(err) {
			handleAuthError(cacheStreamID)
			// Креды протухли — это НЕ фатальная ошибка: воркер обновит
			// креды через GetCreds (кеш уже инвалидирован выше) и повторит
			return false, &SessionError{
				Type:    SessionErrorAuth,
				Address: turnAddr,
				Err:     fmt.Errorf("TURN Allocate: %w", err),
			}
		}

		// Квота, unreachable, timeout — всё это "адрес мёртв"
		if strings.Contains(errLower, "quota") ||
			strings.Contains(errLower, "486") ||
			strings.Contains(errLower, "unreachable") ||
			strings.Contains(errLower, "timeout") ||
			strings.Contains(errLower, "connection refused") ||
			strings.Contains(errLower, "no route to host") {
			return false, &SessionError{
				Type:    SessionErrorAddressDead,
				Address: turnAddr,
				Err:     fmt.Errorf("TURN Allocate: %w", err),
			}
		}

		// Остальные ошибки — фатальные
		return false, &SessionError{
			Type:    SessionErrorFatal,
			Address: turnAddr,
			Err:     fmt.Errorf("TURN Allocate: %w", err),
		}
	}
	defer relay.Close()

	getStreamCache(cacheStreamID).errorCount.Store(0)

	log.Printf("[СЕССИЯ #%d] Relay: %s", sessionID, relay.LocalAddr())

	sessCtx, sessCancel := context.WithCancel(ctx)
	// ВАЖНО: defer sessCancel() регистрируется НИЖЕ, рядом с defer stopRelay().
	// Defers работают LIFO, и sessCancel обязан выполниться первым — иначе
	// stopRelay снимет AfterFunc до его срабатывания, дедлайны не выставятся,
	// и горутины релея навсегда зависнут в ReadFrom (утечка на каждом
	// error-пути сессии).

	var sessionWg sync.WaitGroup
	sessionWg.Add(1)
	go func() {
		defer sessionWg.Done()
		t := time.NewTicker(10 * time.Second)
		defer t.Stop()
		for {
			select {
			case <-sessCtx.Done():
				return
			case <-t.C:
				tc.SendBindingRequest()
			}
		}
	}()

	var relayWg sync.WaitGroup

	useWrap := len(tp.WrapKey) == wrapKeyLen

	var obfsCfg *ObfsConfig
	var obfsWriteState *ObfsState
	if useWrap {
		obfsCfg = NewObfsConfig(obfsMode)
		obfsWriteState = NewObfsState()
	}

	// activeConn — транспорт сессии: либо прямой RTP-obfs поверх TURN
	// (NoDTLS/RawMode), либо классический DTLS поверх obfs-пайпа.
	var activeConn net.Conn

	if useWrap && (tp.NoDTLS || tp.RawMode) {
		// ─── Прямой режим: RTP-obfs AEAD прямо поверх TURN relay, без DTLS ───
		// DTLS поверх obfs был оверхедом: self-signed сертификат с
		// InsecureSkipVerify не добавлял защиты, зато удваивал AEAD-обработку
		// и требовал хендшейк на каждую из 9 воркер-сессий.
		activeConn = &obfsDirectConn{
			relay:      relay,
			peer:       peer,
			wrapKey:    tp.WrapKey,
			cfg:        obfsCfg,
			writeState: obfsWriteState,
		}
		defer sessCancel()
		log.Printf("[ВОРКЕР #%d] [ПРЯМОЙ] Без DTLS, только RTP-obfs AEAD ✓", sessionID)
	} else {
		// ─── Классический режим: DTLS поверх RTP-obfs (обратная совместимость) ───
		pipeA, pipeB := connutil.AsyncPacketPipe()
		// Порядок регистрируемых defer'ов (LIFO): закрытие пайпов →
		// stopRelay → sessCancel выполняется РАНЬШЕШЕ всех (см. комментарий выше).
		defer pipeA.Close()
		defer pipeB.Close()

		relayWg.Add(2)

		stopRelay := context.AfterFunc(sessCtx, func() {
			_ = relay.SetDeadline(time.Now())
			_ = pipeA.SetDeadline(time.Now())
		})
		defer stopRelay()
		defer sessCancel()

		go func() {
			defer relayWg.Done()
			defer sessCancel()

			readBufLen := readBufSize + 80
			buf := make([]byte, readBufLen)
			plain := make([]byte, readBufSize)
			// Replay window: отбрасываем повторно доставленные кадры (см. obfs.go).
			var replay replayWindow
			for {
				n, _, readErr := relay.ReadFrom(buf)
				if readErr != nil {
					return
				}
				payload := buf[:n]
				if useWrap {
					if !obfsIsRTPPacket(payload) {
						log.Printf("[СЕССИЯ #%d] OBFS unwrap: unexpected packet (n=%d)", sessionID, n)
						continue
					}
					m, wrapErr := obfsUnwrapPacket(tp.WrapKey, payload, plain)
					if wrapErr != nil {
						log.Printf("[СЕССИЯ #%d] OBFS unwrap: %v (n=%d)", sessionID, wrapErr, n)
						continue
					}
					if !replay.accept(payload) {
						continue // повтор/слишком старый кадр — не пропускаем
					}
					payload = plain[:m]
				}
				if _, writeErr := pipeA.WriteTo(payload, peer); writeErr != nil {
					return
				}
			}
		}()

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
				if useWrap {
					if obfsCfg != nil && obfsWriteState != nil {
						wrapped, wrapErr := obfsWrapPacket(tp.WrapKey, out, obfsCfg, obfsWriteState)
						if wrapErr != nil {
							log.Printf("[СЕССИЯ #%d] OBFS wrap: %v", sessionID, wrapErr)
							return
						}
						out = wrapped
					}
				}
				if _, writeErr := relay.WriteTo(out, peer); writeErr != nil {
					return
				}
			}
		}()

		cert, certErr := selfsign.GenerateSelfSigned()
		if certErr != nil {
			return false, &SessionError{
				Type:    SessionErrorFatal,
				Address: turnAddr,
				Err:     fmt.Errorf("генерация сертификата: %w", certErr),
			}
		}

		select {
		case handshakeSem <- struct{}{}:
		case <-sessCtx.Done():
			return false, &SessionError{
				Type:    SessionErrorFatal,
				Address: turnAddr,
				Err:     sessCtx.Err(),
			}
		}

		dtlsCfg := &dtls.Config{
			Certificates:          []tls.Certificate{cert},
			InsecureSkipVerify:    true,
			ExtendedMasterSecret:  dtls.RequireExtendedMasterSecret,
			CipherSuites:          []dtls.CipherSuiteID{dtls.TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256},
			ConnectionIDGenerator: dtls.OnlySendCIDGenerator(),
		}

		dtlsConn, dtlsErr := dtls.Client(pipeB, peer, dtlsCfg)
		if dtlsErr != nil {
			<-handshakeSem
			return false, &SessionError{
				Type:    SessionErrorAddressDead,
				Address: turnAddr,
				Err:     fmt.Errorf("DTLS клиент: %w", dtlsErr),
			}
		}

		hctx, hcancel := context.WithTimeout(sessCtx, 20*time.Second)
		log.Printf("[ВОРКЕР #%d] [DTLS] Рукопожатие (Handshake)...", sessionID)
		hsErr := dtlsConn.HandshakeContext(hctx)
		hcancel()
		<-handshakeSem

		if hsErr != nil {
			_ = dtlsConn.Close()
			errStr := strings.ToLower(hsErr.Error())
			if useWrap && (strings.Contains(errStr, "deadline") || strings.Contains(errStr, "timeout")) {
				return false, &SessionError{
					Type:    SessionErrorWrapTimeout,
					Address: turnAddr,
					Err:     fmt.Errorf("DTLS timeout, пароль/WRAP не подтверждён"),
				}
			}
			return false, &SessionError{
				Type:    SessionErrorAddressDead,
				Address: turnAddr,
				Err:     fmt.Errorf("DTLS хендшейк: %w", hsErr),
			}
		}
		log.Printf("[ВОРКЕР #%d] [DTLS] Соединение установлено ✓", sessionID)
		activeConn = dtlsConn
	}
	defer activeConn.Close()

	stats.ActiveConnections.Add(1)
	defer stats.ActiveConnections.Add(-1)

	// Прерывание ожидания конфига при отмене сессии: RequestRawConfig/
	// RequestConfig ставят ReadDeadline на 45 сек, и без этой подписки
	// Stop() переживают его целиком — стоп тянется полминуты, а быстрый
	// reconnect после disconnect падает с "already running".
	stopCfgWait := context.AfterFunc(sessCtx, func() {
		_ = activeConn.SetDeadline(time.Now())
	})

	if getConfig && configCh != nil && tp.RawMode {
		// RAW-режим: сервер отвечает RAWCONF:ip|dns|mtu вместо WG-конфига
		// (см. RequestRawConfig). Конфиг упаковывается в строку RAWCONF: и
		// уходит тем же каналом — обрабатывается в main.go.
		ip, dnsCSV, mtu, confErr := RequestRawConfig(activeConn, deviceID, password)
		if confErr != nil {
			errStr := confErr.Error()
			if strings.Contains(errStr, "FATAL_AUTH") {
				return false, &SessionError{
					Type:    SessionErrorFatal,
					Address: turnAddr,
					Err:     confErr,
				}
			}
			log.Printf("[ВОРКЕР #%d] Ошибка RAW-конфига: %v", sessionID, confErr)
			if strings.Contains(errStr, "i/o timeout") {
				log.Printf("[ВОРКЕР #%d] [ПОДСКАЗКА] сервер не ответил на GETCONF_RAW — туннель НЕ поднят (IP не изменится). Обычный режим работает без флагов; -raw требует, чтобы сервер был запущен с -listen-raw", sessionID)
			}
		} else if ip != "" {
			conf := fmt.Sprintf("RAWCONF:%s|%s|%d", ip, dnsCSV, mtu)
			select {
			case configCh <- conf:
				configDelivered = true
				log.Printf("[ВОРКЕР #%d] RAW-конфиг получен (ip=%s)", sessionID, ip)
			default:
				configDelivered = true
				log.Printf("[ВОРКЕР #%d] RAW-конфиг уже был доставлен другим воркером", sessionID)
			}
		} else {
			log.Printf("[ВОРКЕР #%d] Сервер ещё не назначил raw IP, повторим позже", sessionID)
		}
	} else if getConfig && configCh != nil {
		conf, confErr := RequestConfig(activeConn, localPort, deviceID, password)
		if confErr != nil {
			errStr := confErr.Error()
			if strings.Contains(errStr, "FATAL_AUTH") {
				return false, &SessionError{
					Type:    SessionErrorFatal,
					Address: turnAddr,
					Err:     confErr,
				}
			}
			log.Printf("[ВОРКЕР #%d] Ошибка конфига: %v", sessionID, confErr)
			if strings.Contains(errStr, "i/o timeout") {
				log.Printf("[ВОРКЕР #%d] [ПОДСКАЗКА] сервер не ответил на GETCONF — туннель НЕ поднят (IP не изменится). Обычный режим работает без флагов; -notls/-raw требует, чтобы сервер был запущен с -listen-direct/-listen-raw", sessionID)
			}
		} else if conf != "" {
			select {
			case configCh <- conf:
				configDelivered = true
				log.Printf("[ВОРКЕР #%d] Конфиг получен", sessionID)
			default:
				configDelivered = true
				log.Printf("[ВОРКЕР #%d] Конфиг уже был доставлен другим воркером", sessionID)
			}
		} else {
			log.Printf("[ВОРКЕР #%d] Сервер ещё не выдал WireGuard-конфиг, повторим позже", sessionID)
		}
	} else {
		if authErr := SendAuth(activeConn, deviceID, password); authErr != nil {
			log.Printf("[ВОРКЕР #%d] Ошибка авторизации: %v", sessionID, authErr)
		}
	}

	stopCfgWait() // конфиг получен/провалился — дедлайн больше не нужен

	emitReady()

	slot := &WorkerSlot{
		ID:     sessionID,
		SendCh: make(chan []byte, workerSendBuf),
		PrioCh: make(chan []byte, prioBuf),
	}
	d.Register(slot)
	defer d.Unregister(slot)

	var proxyWg sync.WaitGroup
	proxyWg.Add(3)

	stopDTLS := context.AfterFunc(sessCtx, func() {
		_ = activeConn.SetDeadline(time.Now())
	})
	defer stopDTLS()

	// Keepalive: пакет не пишется напрямую в dtlsConn (это была бы вторая
	// горутина, конкурирующая за conn с основным Writer'ом ниже) — кладётся
	// в slot.PrioCh неблокирующе, тем же путём, что и мелкие ACK-пакеты, и
	// уходит через единственную writer-горутину (порядок гарантирован).
	go func() {
		defer proxyWg.Done()
		t := time.NewTicker(keepaliveInterval)
		defer t.Stop()

		didBytes := make([]byte, 16)
		copy(didBytes, deviceID)

		for {
			select {
			case <-sessCtx.Done():
				return
			case <-t.C:
				size := keepaliveMinSize + rand.Intn(keepaliveMaxSize)
				pkt := getPktBuf(size)
				pkt[0] = keepaliveByte
				copy(pkt[1:17], didBytes)
				for i := 17; i < size; i++ {
					pkt[i] = keepaliveByte
				}
				select {
				case slot.PrioCh <- pkt:
				default:
					putPktBuf(pkt)
				}
			}
		}
	}()

	// Writer: dispatcher → dtlsConn. PrioCh (мелкие пакеты/ACK) всегда
	// проверяется первым, минуя SendCh с обычными данными — иначе ACK может
	// простоять в очереди за большим chunk'ом данных.
	go func() {
		defer proxyWg.Done()
		defer sessCancel()
		defer func() {
			// Осушаем обе очереди при выходе: буферы из пула не должны утекать
			for {
				select {
				case p := <-slot.PrioCh:
					putPktBuf(p)
				default:
					goto drainSend
				}
			}
		drainSend:
			for {
				select {
				case p := <-slot.SendCh:
					putPktBuf(p)
				default:
					return
				}
			}
		}()
		for {
			var pkt []byte
			var ok bool
			select {
			case pkt, ok = <-slot.PrioCh:
			default:
				select {
				case <-sessCtx.Done():
					return
				case pkt, ok = <-slot.PrioCh:
				case pkt, ok = <-slot.SendCh:
				}
			}
			if !ok {
				return
			}
			_ = activeConn.SetWriteDeadline(time.Now().Add(writeDeadline))
			_, writeErr := activeConn.Write(pkt)
			putPktBuf(pkt)
			if writeErr != nil {
				log.Printf("[ВОРКЕР #%d] Ошибка Writer: %v", sessionID, writeErr)
				return
			}
		}
	}()

	go func() {
		defer proxyWg.Done()
		defer sessCancel()
		for {
			pkt := getPktBuf(2048)
			_ = activeConn.SetReadDeadline(time.Now().Add(sessionReadTimeout))
			n, readErr := activeConn.Read(pkt)
			if readErr != nil {
				putPktBuf(pkt)
				if sessCtx.Err() != nil {
					return
				}
				if ne, ok := readErr.(net.Error); ok && ne.Timeout() {
					continue
				}
				log.Printf("[ВОРКЕР #%d] Ошибка Reader: %v", sessionID, readErr)
				return
			}

			if n == 1 && pkt[0] == keepaliveByte {
				putPktBuf(pkt)
				continue
			}

			pkt = pkt[:n]
			select {
			case d.ReturnCh <- pkt:
			case <-sessCtx.Done():
				putPktBuf(pkt)
				return
			default:
				// ReturnCh полон — пакет дропается, но эта горутина не
				// блокируется. Без default: если writeLoop не успевал
				// разгружать ReturnCh, Reader вставал колом и переставал
				// читать dtlsConn вообще — новые пакеты от сервера (включая
				// реальные ответы на пользовательский трафик) переставали
				// вычитываться из UDP-сокета ОС и терялись там. Один раз
				// заполнившийся канал убивал весь дальнейший приём для этого
				// воркера — select должен быть неблокирующим.
				putPktBuf(pkt)
			}
		}
	}()

	proxyWg.Wait()
	sessCancel()
	relayWg.Wait()
	sessionWg.Wait()
	// pipeA/pipeB закрываются defer'ом в DTLS-ветке (выше); в прямом режиме
	// их нет — relay закрывается defer relay.Close().
	log.Printf("[СЕССИЯ #%d] Завершена", sessionID)
	return configDelivered, nil
}
