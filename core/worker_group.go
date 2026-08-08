package core

import (
	"context"
	"log"
	"net"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

const (
	workersPerGroup  = 9
	defaultCycleSecs = 36000

	// maxAttemptsPerAddress — сколько раз пробуем один адрес, прежде чем забанить
	maxAttemptsPerAddress = 3

	// sleepWhenAllBanned — сколько спим, если все адреса забанены
	sleepWhenAllBanned = 60 * time.Second

	// sleepAfterAddressDead — спим после того как адрес умер (перед переходом к следующему)
	sleepAfterAddressDead = 500 * time.Millisecond

	// workerStartDelay — задержка между запуском воркеров
	workerStartDelay = 200 * time.Millisecond

	// workerBatchDelay — задержка между первой и второй волной воркеров (5 секунд)
	workerBatchDelay = 5 * time.Second

	// firstBatchSize — сколько воркеров запускаем в первой волне
	firstBatchSize = 5
)

func WorkerGroup(
	ctx context.Context,
	groupID int,
	hashIndex int,
	tp *TurnParams,
	peer *net.UDPAddr,
	d *Dispatcher,
	localPort string,
	getConfig bool,
	configCh chan<- string,
	turnIPsCh chan<- []string,
	workerIDs []int,
	pauseFlag *int32,
	deviceID, password string,
	stats *Stats,
	waitCreds <-chan struct{},
	signalCreds chan<- struct{},
	waitSpawn <-chan struct{},
	signalSpawn chan<- struct{},
) {

	if waitCreds != nil {
		select {
		case <-waitCreds:
		case <-ctx.Done():
			return
		}
	}

	var configSent int32
	if !getConfig {
		configSent = 1
	}

	for atomic.LoadInt32(pauseFlag) != 0 {
		if ctx.Err() != nil {
			return
		}
		time.Sleep(1 * time.Second)
	}

	hash := tp.Hashes[hashIndex%len(tp.Hashes)]
	shortHash := hash
	if len(shortHash) > 8 {
		shortHash = shortHash[:8]
	}
	log.Printf("[ГРУППА #%d] Запрос кредов (хеш: %s...)", groupID, shortHash)

	credStreamID := groupID * 100
	user, pass, turnURLs, err := GetCreds(ctx, hash, credStreamID)
	var creds *Credentials
	if err == nil {
		creds = &Credentials{User: user, Pass: pass, TurnURLs: turnURLs, CacheStreamID: credStreamID}
	} else {
		log.Printf("[ГРУППА #%d] Ошибка кредов: %v", groupID, err)
		return
	}

	log.Printf("[ГРУППА #%d] Креды OK, TURN: %v, %d воркеров", groupID, creds.TurnURLs, len(workerIDs))

	// Отправляем TURN IP для exclude routes (только первая группа)
	if groupID == 1 && turnIPsCh != nil && len(creds.TurnURLs) > 0 {
		var turnIPs []string
		for _, url := range creds.TurnURLs {
			host, _, err := net.SplitHostPort(url)
			if err != nil {
				host = url
			}
			if tp.Host != "" {
				host = tp.Host
			}
			resolved, err := net.LookupIP(host)
			if err != nil {
				turnIPs = append(turnIPs, host)
			} else {
				for _, ip := range resolved {
					turnIPs = append(turnIPs, ip.String())
				}
			}
		}
		// Дедупликация
		seen := make(map[string]bool)
		var unique []string
		for _, ip := range turnIPs {
			if !seen[ip] {
				seen[ip] = true
				unique = append(unique, ip)
			}
		}
		select {
		case turnIPsCh <- unique:
		default:
		}
	}

	var configRequestInFlight int32
	var wg sync.WaitGroup
	var credsMu sync.RWMutex

	if signalCreds != nil {
		go func() {
			time.Sleep(2 * time.Second)
			close(signalCreds)
		}()
	}

	if waitSpawn != nil {
		select {
		case <-waitSpawn:
		case <-ctx.Done():
			return
		}
	}

	// Запускаем воркеры с поэтапной задержкой
	for idx, wid := range workerIDs {
		wg.Add(1)

		// Определяем задержку перед запуском этого воркера
		var startDelay time.Duration
		if idx < firstBatchSize {
			// Первая волна: 5 воркеров с задержкой 200ms
			startDelay = time.Duration(idx) * workerStartDelay
		} else {
			// Вторая волна: задержка = (первая волна) + 5 секунд + (индекс во второй волне) * 200ms
			secondWaveIdx := idx - firstBatchSize
			startDelay = time.Duration(firstBatchSize)*workerStartDelay + workerBatchDelay + time.Duration(secondWaveIdx)*workerStartDelay
		}

		go func(wid int, delay time.Duration) {
			// Ждём свою задержку перед стартом
			select {
			case <-time.After(delay):
			case <-ctx.Done():
				wg.Done()
				return
			}

			defer wg.Done()

			shouldGetConfig := getConfig
			// Счётчик неудач на текущий адрес
			addressAttempts := 0
			// Храним текущий ObfsMode для этого воркера (может меняться при WRAP_TIMEOUT)
			workerObfsMode := tp.ObfsMode

			for {
				if ctx.Err() != nil {
					return
				}

				// Проверяем паузу
				for atomic.LoadInt32(pauseFlag) != 0 {
					if ctx.Err() != nil {
						return
					}
					time.Sleep(1 * time.Second)
				}

				// Берём свежие креды
				credsMu.RLock()
				urls := cloneStringSlice(creds.TurnURLs)
				credsMu.RUnlock()

				if len(urls) == 0 {
					log.Printf("[ВОРКЕР #%d] Нет TURN-адресов в кредах", wid)
					select {
					case <-time.After(5 * time.Second):
					case <-ctx.Done():
					}
					continue
				}

				// Фильтруем забаненные адреса
				var available []string
				for _, u := range urls {
					if !GlobalBlacklist.IsBanned(u) {
						available = append(available, u)
					}
				}

				// Если все адреса забанены — спим и пробуем заново
				if len(available) == 0 {
					log.Printf("[ВОРКЕР #%d] Все TURN-адреса забанены (всего %d), спим %v",
						wid, len(urls), sleepWhenAllBanned)
					select {
					case <-time.After(sleepWhenAllBanned):
					case <-ctx.Done():
					}
					continue
				}

				// Берём первый доступный адрес
				turnAddr := available[0]

				getConf := false
				if shouldGetConfig && atomic.LoadInt32(&configSent) == 0 {
					getConf = atomic.CompareAndSwapInt32(&configRequestInFlight, 0, 1)
				}
				var cc chan<- string
				if getConf {
					cc = configCh
				}

				// Передаём в RunSession конкретный адрес и креды
				configDelivered, sessErr := RunSession(
					ctx, tp, peer, d, localPort,
					getConf, cc, wid,
					turnAddr,       // конкретный TURN-адрес
					user,           // username из кредов (не меняется)
					pass,           // password из кредов (не меняется)
					credStreamID,   // для handleAuthError
					deviceID, password, stats,
				)

				if getConf {
					if configDelivered {
						atomic.StoreInt32(&configSent, 1)
					} else {
						atomic.StoreInt32(&configRequestInFlight, 0)
					}
				}

				// Обработка ошибок
				if sessErr != nil {
					if ctx.Err() != nil {
						return
					}

					log.Printf("[ВОРКЕР #%d] Ошибка сессии: %v (адрес=%s, тип=%s)",
						wid, sessErr.Err, sessErr.Address, sessErr.Type)

					switch sessErr.Type {

					case SessionErrorAddressDead:
						// Адрес мёртв — баним и пробуем следующий
						if sessErr.Address != "" {
							GlobalBlacklist.Ban(sessErr.Address)
							log.Printf("[ВОРКЕР #%d] TURN-адрес %s забанен на 5 минут", wid, sessErr.Address)
						}
						// Небольшая пауза перед переходом к следующему адресу
						select {
						case <-time.After(sleepAfterAddressDead):
						case <-ctx.Done():
						}
						// Продолжаем цикл — возьмём следующий доступный адрес
						continue

					case SessionErrorWrapTimeout:
						// DTLS-таймаут — пробуем сменить обфускацию
						log.Printf("[ВОРКЕР #%d] WRAP_TIMEOUT на адресе %s, пробуем сменить обфускацию",
							wid, sessErr.Address)

						// Меняем режим обфускации
						if workerObfsMode == "audio" {
							workerObfsMode = "video"
						} else {
							workerObfsMode = "audio"
						}
						tp.ObfsMode = workerObfsMode
						log.Printf("[ВОРКЕР #%d] Режим обфускации изменён на %s", wid, workerObfsMode)

						// Увеличиваем счётчик попыток на этом адресе
						addressAttempts++

						// Если слишком много попыток на одном адресе — баним его
						if addressAttempts >= maxAttemptsPerAddress {
							if sessErr.Address != "" {
								GlobalBlacklist.Ban(sessErr.Address)
								log.Printf("[ВОРКЕР #%d] TURN-адрес %s забанен после %d попыток",
									wid, sessErr.Address, addressAttempts)
							}
							addressAttempts = 0
							// Продолжаем цикл — возьмём следующий адрес
							continue
						}

						// Пробуем тот же адрес с новой обфускацией
						continue

					case SessionErrorFatal:
						// Фатальная ошибка — выходим
						log.Printf("[ВОРКЕР #%d] Фатальная ошибка: %v", wid, sessErr.Err)
						return

					default:
						// Неизвестный тип ошибки — спим и пробуем заново
						log.Printf("[ВОРКЕР #%d] Неизвестная ошибка: %v", wid, sessErr)
						select {
						case <-time.After(5 * time.Second):
						case <-ctx.Done():
						}
						continue
					}
				}

				// Успех — сбрасываем счётчики
				addressAttempts = 0
				// Возвращаем ObfsMode к исходному (если он менялся)
				if workerObfsMode != tp.ObfsMode {
					workerObfsMode = tp.ObfsMode
				}

				// После успешной сессии — небольшая пауза перед следующим циклом
				select {
				case <-time.After(100 * time.Millisecond):
				case <-ctx.Done():
					return
				}
			}
		}(wid, startDelay)
	}

	if signalSpawn != nil {
		close(signalSpawn)
	}

	wg.Wait()
	log.Printf("[ГРУППА #%d] Все воркеры группы завершились.", groupID)
}

func ParseHashes(raw string) []string {
	var result []string
	seen := make(map[string]struct{})
	for _, h := range strings.FieldsFunc(raw, func(r rune) bool {
		return r == ',' || r == ';' || r == '\n' || r == '\r' || r == '\t' || r == ' '
	}) {
		h = normalizeVKJoinHash(h)
		if h != "" {
			if _, exists := seen[h]; exists {
				continue
			}
			seen[h] = struct{}{}
			result = append(result, h)
		}
	}
	return result
}

func normalizeVKJoinHash(input string) string {
	s := strings.Trim(strings.TrimSpace(input), "<>\"'")
	if s == "" {
		return ""
	}

	lower := strings.ToLower(s)
	if idx := strings.Index(lower, "/call/join/"); idx >= 0 {
		s = s[idx+len("/call/join/"):]
	} else if strings.HasPrefix(lower, "http://") || strings.HasPrefix(lower, "https://") {
		return ""
	}

	if idx := strings.IndexAny(s, "?#/"); idx != -1 {
		s = s[:idx]
	}
	return strings.Trim(strings.TrimSpace(s), "/")
}

type TurnParams struct {
	Host     string
	Port     string
	Hashes   []string
	WrapKey  []byte
	ObfsMode string
}

type Credentials struct {
	User          string
	Pass          string
	TurnURLs      []string
	CacheStreamID int
}
