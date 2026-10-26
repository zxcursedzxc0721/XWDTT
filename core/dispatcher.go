package core

import (
	"context"
	"log"
	"net"
	"sync"
	"sync/atomic"
	"time"
)

var pktPool = sync.Pool{
	New: func() interface{} {
		return make([]byte, 2048)
	},
}

func getPktBuf(size int) []byte {
	b := pktPool.Get().([]byte)
	if cap(b) < size {
		b = make([]byte, size)
	}
	return b[:size]
}

func putPktBuf(b []byte) {
	if cap(b) < 2048 {
		return
	}
	pktPool.Put(b[:cap(b)])
}

const (
	// returnChBuf — глубина канала пакетов, готовых к записи в TUN. При RTT
	// ~50-60мс и целевой скорости 70-80 Мбит/с BDP ≈ 440-600КБ; при MTU~1300
	// это ~340-460 пакетов. 384 слота были впритык к этому потолку, отсюда
	// запас до 512 (тот же порядок, что использует референсный клиент).
	returnChBuf = 512
	prioBuf     = 32

	// maxDwellMS — сколько максимум миллисекунд подряд пакеты одного клиента
	// идут через один и тот же worker, даже если chunk по счётчику ещё не
	// закончился. Подстраховка на случай, если конкретный relay начал тормозить:
	// не ждём весь chunk, переключаемся раньше.
	maxDwellMS = 15

	// prioThreshold — пакеты размером до этого числа байт (в первую очередь
	// TCP ACK) идут через отдельный приоритетный канал (PrioCh) каждого
	// worker'а, минуя обычную chunk-очередь. Иначе ACK может застрять за
	// большим chunk'ом данных на медленном relay, и рост TCP-окна тормозится.
	prioThreshold = 128
)

// chunkSizeFor — сколько подряд пакетов такого размера отправлять в один
// worker, прежде чем переключиться на следующий.
//
// Зачем вообще chunk, а не round-robin по одному пакету: при round-robin
// каждый пакет летит через разный TURN relay с разным latency, что даёт
// reorder на другой стороне. TCP внутри туннеля интерпретирует reorder как
// потери → cwnd collapse → скорость single-flow падает до считанных KB/s.
//
// Почему размер зависит от размера пакета: крупные пакеты (объёмные данные)
// разумно группировать покрупнее — меньше переключений relay на мегабайт
// трафика. Мелкие пакеты (ACK, keepalive) — быстро переключать или вовсе
// уводить в приоритетный канал (см. prioThreshold), чтобы не накапливать
// задержку на управляющем трафике.
func chunkSizeFor(pktSize int) int {
	switch {
	case pktSize > 1100:
		return 64
	case pktSize >= 701:
		return 24
	case pktSize >= 301:
		return 8
	case pktSize >= 101:
		return 3
	default:
		return 1
	}
}

type WorkerSlot struct {
	ID     int
	SendCh chan []byte
	PrioCh chan []byte
}

// RawTUN — минимальный интерфейс TUN-устройства для raw-режима (сырые IP).
// На Android это fd от Java-стороны (*os.File), на ПК — обёртка над
// wintun (там нет файлового дескриптора, NativeTun.File() == nil).
type RawTUN interface {
	Read(p []byte) (int, error)
	Write(p []byte) (int, error)
	Close() error
}

type Dispatcher struct {
	localConn net.PacketConn
	tun       RawTUN // не nil в raw-режиме: сырые IP-пакеты вместо локального WG-loopback
	ready     chan struct{}
	readyOnce sync.Once // close(ready) ровно один раз: повторный AttachTUN не паникует
	clientAddr   atomic.Pointer[net.Addr]
	mu           sync.Mutex
	workers      []*WorkerSlot
	rrIndex      int
	rrCount      int   // сколько пакетов отправлено в текущий worker в рамках текущего chunk'а
	lastPktTime  int64 // unix millis последнего пакета — для сброса chunk'а после паузы
	chunkStartTs int64 // unix millis начала текущего chunk'а — для maxDwellMS
	ReturnCh     chan []byte
	ctx          context.Context
	cancel       context.CancelFunc
	wg           sync.WaitGroup
	stats        *Stats
	firstPktUp    uint32
	firstPktDown  uint32
	firstReadErr  uint32
	firstWriteErr uint32

	// Диагностика TUN-пути (rawtun): сколько пакетов реально прочитано из TUN,
	// сколько ушло в воркеры (SendCh/PrioCh) и сколько молча дропнуто из-за
	// перегрузки всех воркеров. Нужны, чтобы отличить "трафик из TUN не
	// читается вообще" от "читается, но дропается из-за перегруженных воркеров"
	// — оба выглядят одинаково снаружи (сервер не видит пакетов от клиента),
	// но чинятся по-разному.
	tunReadCount    uint64
	tunSentCount    uint64
	tunDroppedCount uint64
}

func NewDispatcher(ctx context.Context, localConn net.PacketConn, stats *Stats) *Dispatcher {
	dctx, dcancel := context.WithCancel(ctx)
	ready := make(chan struct{})
	close(ready) // localConn уже доступен — читаем/пишем сразу, как и раньше
	d := &Dispatcher{
		localConn: localConn,
		ready:     ready,
		ReturnCh:  make(chan []byte, returnChBuf),
		ctx:       dctx,
		cancel:    dcancel,
		stats:     stats,
	}

	d.wg.Add(2)
	go d.readLoop()
	go d.writeLoop()
	return d
}

// NewDispatcherPendingTUN — вариант для -mode rawtun: TUN-fd ещё не получен
// на момент старта воркеров (TUN поднимается только ПОСЛЕ того, как сервер
// назначит IP/DNS/MTU через RAWCONF). Горутины readLoop/writeLoop стартуют
// сразу, но ждут AttachTUN() прежде чем начать реальный ввод-вывод.
func NewDispatcherPendingTUN(ctx context.Context, stats *Stats) *Dispatcher {
	dctx, dcancel := context.WithCancel(ctx)
	d := &Dispatcher{
		ready:    make(chan struct{}),
		ReturnCh: make(chan []byte, returnChBuf),
		ctx:      dctx,
		cancel:   dcancel,
		stats:    stats,
	}

	d.wg.Add(2)
	go d.readLoop()
	go d.writeLoop()
	return d
}

// AttachTUN подключает полученный TUN-fd к уже запущенному диспетчеру и
// снимает блокировку с readLoop/writeLoop. close(ready) защищён sync.Once:
// повторный вызов (рестарт raw-режима, гонка) не паникует на двойном close.
// Запись d.tun строго ДО close(ready): циклы читают его в локальную
// переменную после < -d.ready — это даёт happens-before и убирает data race
// без синхронизации в горячем пути.
func (d *Dispatcher) AttachTUN(t RawTUN) {
	d.tun = t
	d.readyOnce.Do(func() { close(d.ready) })
}

func (d *Dispatcher) Shutdown() {
	d.cancel()
	d.wg.Wait()
}

func (d *Dispatcher) Register(w *WorkerSlot) {
	d.mu.Lock()
	d.workers = append(d.workers, w)
	count := len(d.workers)
	d.mu.Unlock()
	log.Printf("[ДИСП] Воркер #%d зарегистрирован (всего: %d)", w.ID, count)
}

func (d *Dispatcher) Unregister(slot *WorkerSlot) {
	d.mu.Lock()
	for i, w := range d.workers {
		if w == slot {
			d.workers = append(d.workers[:i], d.workers[i+1:]...)
			break
		}
	}
	remaining := len(d.workers)
	// Подстраховка: если текущий rrIndex вылез за границу после удаления
	if d.rrIndex >= remaining && remaining > 0 {
		d.rrIndex = d.rrIndex % remaining
	}
	d.rrCount = 0
	d.mu.Unlock()
	log.Printf("[ДИСП] Воркер #%d отключён (осталось: %d)", slot.ID, remaining)
}

// readLoop читает пакеты (из локального WG-loopback или TUN) и распределяет
// по workers адаптивными chunk'ами.
//
// Логика: отправляем chunkSizeFor(размер) подряд пакетов в один worker, потом
// переходим к следующему. Если текущий worker перегружен (канал полный) —
// немедленно ищем свободный worker и начинаем новый chunk на нём. Маленькие
// пакеты (вероятно ACK, см. prioThreshold) уходят через отдельный
// приоритетный канал, минуя очередь данных. maxDwellMS — предохранитель:
// если текущий relay начал тормозить, не ждём весь chunk целиком. Это
// гарантирует:
//   - В рамках chunk пакеты идут через один TURN relay → in-order delivery
//   - Между chunks — разные relay → максимальная агрегатная скорость
//   - ACK не застревают за большими chunk'ами данных на медленном relay
//   - Нет блокировки, нет буферизации сверх необходимого
func (d *Dispatcher) readLoop() {
	defer d.wg.Done()

	select {
	case <-d.ctx.Done():
		return
	case <-d.ready:
	}
	// Один снапшот после ready: запись в AttachTUN была до close(ready).
	tun := d.tun
	if tun != nil {
		rawDiagf("readLoop: разблокирован, начинаю читать из TUN")
	}

	// Адрес клиента (non-TUN режим) и его кэш: раньше `&addr` сохранялся в
	// atomic.Pointer на КАЖДЫЙ пакет — лишняя heap-аллокация в горячем пути.
	var (
		addr      net.Addr
		cachedUDP *net.UDPAddr // последний сохранённый адрес клиента
	)

	for {
		if err := d.ctx.Err(); err != nil {
			return
		}

		pkt := getPktBuf(2048)

		var n int
		var err error
		if tun != nil {
			n, err = tun.Read(pkt)
		} else {
			// addr объявлен вне цикла: раньше здесь был `var addr net.Addr`
			// на каждой итерации + `&addr` в Store — лишняя heap-аллокация
			// на КАЖДЫЙ пакет. Кэш нужен, чтобы не писать в atomic.Value
			// без изменений.
			n, addr, err = d.localConn.ReadFrom(pkt)
		}
		if err != nil {
			putPktBuf(pkt)
			if d.ctx.Err() != nil {
				return
			}
			if atomic.CompareAndSwapUint32(&d.firstReadErr, 0, 1) {
				src := "localConn"
				if tun != nil {
					src = "tunFile"
				}
				rawDiagf("readLoop: первая ошибка чтения из %s: %v", src, err)
			}
			time.Sleep(10 * time.Millisecond)
			continue
		}
		pkt = pkt[:n]

		if tun == nil && addr != nil {
			// Клиентский адрес кладём в atomic.Pointer только при СМЕНЕ —
			// Store на каждый пакет давал по одной аллокации на чтение.
			if ua, ok := addr.(*net.UDPAddr); ok {
				if cachedUDP == nil || ua.Port != cachedUDP.Port || !ua.IP.Equal(cachedUDP.IP) {
					box := addr
					d.clientAddr.Store(&box)
					cachedUDP = ua
				}
			} else {
				box := addr
				d.clientAddr.Store(&box)
			}
		}
		d.stats.TotalBytesUp.Add(int64(n))

		if tun != nil {
			c := atomic.AddUint64(&d.tunReadCount, 1)
			if c%200 == 0 {
				rawDiagf("readLoop: прочитано из TUN=%d отправлено=%d дропнуто=%d",
					c, atomic.LoadUint64(&d.tunSentCount), atomic.LoadUint64(&d.tunDroppedCount))
			}
		}

		if atomic.CompareAndSwapUint32(&d.firstPktUp, 0, 1) {
			if tun != nil {
				log.Printf("[ДИСП] [ДЕБАГ] Получен ПЕРВЫЙ пакет от TUN (%d байт)", n)
			} else {
				log.Printf("[ДИСП] [ДЕБАГ] Получен ПЕРВЫЙ пакет от локального WireGuard (%d байт) с адреса %s", n, addr.String())
			}
		}

		d.mu.Lock()
		nw := len(d.workers)
		if nw == 0 {
			d.mu.Unlock()
			d.stats.DroppedUp.Add(1)
			putPktBuf(pkt)
			continue
		}

		now := time.Now().UnixMilli()
		lastTime := d.lastPktTime
		d.lastPktTime = now
		if lastTime > 0 && now-lastTime > 10 {
			// Была пауза >10мс — предыдущий chunk уже не даёт выгоды от
			// affinity, начинаем новый со следующего worker'а.
			d.rrIndex = (d.rrIndex + 1) % nw
			d.rrCount = 0
			d.chunkStartTs = now
		}

		// Маленькие пакеты (вероятно ACK) — отдельный приоритетный канал с
		// фолбэком на любой другой worker, чтобы не застревать за большим
		// chunk'ом данных на текущем relay.
		if len(pkt) <= prioThreshold {
			idx := d.rrIndex % nw
			sentPrio := false
			select {
			case d.workers[idx].PrioCh <- pkt:
				sentPrio = true
			default:
				for i := 1; i < nw; i++ {
					alt := (idx + i) % nw
					select {
					case d.workers[alt].PrioCh <- pkt:
						sentPrio = true
					default:
					}
					if sentPrio {
						break
					}
				}
			}
			if sentPrio {
				if d.tun != nil {
					atomic.AddUint64(&d.tunSentCount, 1)
				}
				d.mu.Unlock()
				continue
			}
			// Все приоритетные каналы заняты — падаем в обычную очередь ниже.
		}

		chunk := chunkSizeFor(len(pkt))

		if d.chunkStartTs == 0 {
			d.chunkStartTs = now
		} else if now-d.chunkStartTs >= maxDwellMS {
			// Текущий relay слишком долго держит chunk — переключаемся,
			// не дожидаясь конца chunk'а по счётчику.
			d.rrIndex = (d.rrIndex + 1) % nw
			d.rrCount = 0
			d.chunkStartTs = now
		}

		sent := false
		idx := d.rrIndex % nw

		// Пробуем текущий worker (chunk affinity)
		w := d.workers[idx]
		select {
		case w.SendCh <- pkt:
			sent = true
			d.rrCount++
			if d.rrCount >= chunk {
				d.rrIndex = (idx + 1) % nw
				d.rrCount = 0
				d.chunkStartTs = now
			}
		default:
			// Текущий worker перегружен — ищем свободный, начинаем новый chunk
			for i := 1; i < nw; i++ {
				altIdx := (idx + i) % nw
				select {
				case d.workers[altIdx].SendCh <- pkt:
					sent = true
					d.rrIndex = altIdx
					d.rrCount = 1 // первый пакет нового chunk'а уже отправлен
					d.chunkStartTs = now
				default:
				}
				if sent {
					break
				}
			}
		}

		if sent {
			if tun != nil {
				atomic.AddUint64(&d.tunSentCount, 1)
			}
		} else {
			// Все workers перегружены — сдвигаем указатель, пакет дропается
			d.rrIndex = (idx + 1) % nw
			d.rrCount = 0
			putPktBuf(pkt)
			d.stats.DroppedUp.Add(1)
			if tun != nil {
				c := atomic.AddUint64(&d.tunDroppedCount, 1)
				if c == 1 || c%50 == 0 {
					rawDiagf("readLoop: пакет из TUN ДРОПНУТ — все воркеры перегружены (дропнуто всего=%d)", c)
				}
			}
		}
		d.mu.Unlock()
	}
}

func (d *Dispatcher) writeLoop() {
	defer d.wg.Done()

	select {
	case <-d.ctx.Done():
		return
	case <-d.ready:
	}
	// Один снапшот после ready (см. AttachTUN).
	tun := d.tun

	for {
		select {
		case <-d.ctx.Done():
			return
		case pkt := <-d.ReturnCh:
			if tun != nil {
				if atomic.CompareAndSwapUint32(&d.firstPktDown, 0, 1) {
					log.Printf("[ДИСП] [ДЕБАГ] Отправляем ПЕРВЫЙ пакет обратно в TUN (%d байт)", len(pkt))
				}
				if _, err := tun.Write(pkt); err != nil {
					if d.ctx.Err() != nil {
						putPktBuf(pkt)
						return
					}
					if atomic.CompareAndSwapUint32(&d.firstWriteErr, 0, 1) {
						rawDiagf("writeLoop: первая ошибка записи в tunFile: %v", err)
					}
					// Пакет не доставлен в TUN — дроп и без прироста байтов
					// (раньше TotalBytesDown рос даже при ошибке записи).
					d.stats.DroppedDown.Add(1)
					putPktBuf(pkt)
					continue
				}
				d.stats.TotalBytesDown.Add(int64(len(pkt)))
				putPktBuf(pkt)
				continue
			}

			addrPtr := d.clientAddr.Load()
			if addrPtr == nil {
				d.stats.DroppedDown.Add(1)
				putPktBuf(pkt)
				continue
			}
			addr := *addrPtr
			if atomic.CompareAndSwapUint32(&d.firstPktDown, 0, 1) {
				log.Printf("[ДИСП] [ДЕБАГ] Отправляем ПЕРВЫЙ пакет обратно локальному WireGuard (%d байт) на адрес %s", len(pkt), addr.String())
			}
			if _, err := d.localConn.WriteTo(pkt, addr); err != nil {
				if d.ctx.Err() != nil {
					putPktBuf(pkt)
					return
				}
				d.stats.DroppedDown.Add(1)
				putPktBuf(pkt)
				continue
			}
			d.stats.TotalBytesDown.Add(int64(len(pkt)))
			putPktBuf(pkt)
		}
	}
}
