package backend

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode"

	"github.com/google/uuid"
)

// maxProfileNameRunes — предел длины имени профиля (имя = имя файла)
const maxProfileNameRunes = 100

// Store — пассивный модуль хранения данных.
// Все записи атомарные (write to tmp → rename).
// Не инициирует ничего — только отвечает на запросы.
//
// Структура файлов:
//
//	~/.config/xwdtt/config.json        — AppSettings
//	~/.config/xwdtt/servers/<name>.json — ProfileData (серверы)
//	~/.config/xwdtt/logs/*.log          — логи сессий
type Store struct {
	baseDir string // ~/.config/xwdtt
}

// NewStore создаёт Store и создаёт директории если нужно.
func NewStore() *Store {
	dir := configDir()
	os.MkdirAll(filepath.Join(dir, "servers"), 0o700)
	os.MkdirAll(filepath.Join(dir, "logs"), 0o700)
	return &Store{baseDir: dir}
}

// ═══════════════════════════════════════════════════
// SETTINGS — настройки приложения
// ═══════════════════════════════════════════════════

// LoadSettings загружает настройки из config.json.
// Если файла нет — возвращает дефолтные настройки.
// Если obfsAccepted = false — obfsMode принудительно 'audio'.
func (s *Store) LoadSettings() AppSettings {
	defaults := AppSettings{AutoStart: true, ObfsMode: "audio", ObfsAccepted: false, DoH: "yandex", RouteMode: RouteModeFull}
	path := filepath.Join(s.baseDir, "config.json")
	data, err := os.ReadFile(path)
	if err != nil {
		return defaults
	}
	// UTF-8 BOM — частый артефакт Windows-редакторов. json.Unmarshal его не
	// принимает, и раньше настройки молча заменялись дефолтами.
	data = bytes.TrimPrefix(data, []byte{0xEF, 0xBB, 0xBF})
	var settings AppSettings
	if err := json.Unmarshal(data, &settings); err != nil {
		log.Printf("[CONFIG] %s: %v — используются настройки по умолчанию; файл НЕ перезаписан, исправьте его вручную", path, err)
		return defaults
	}
	if !settings.ObfsAccepted {
		settings.ObfsMode = "audio"
	}
	if settings.RouteMode != RouteModeSplit {
		settings.RouteMode = RouteModeFull // пусто/мусор → full (текущее поведение)
	}
	if settings.DoH == "" {
		settings.DoH = "yandex" // поведение до Этапа 4
	}
	return settings
}

// SaveSettings сохраняет настройки в config.json (атомарно).
func (s *Store) SaveSettings(settings AppSettings) error {
	data, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return err
	}
	return atomicWrite(filepath.Join(s.baseDir, "config.json"), data)
}

// ═══════════════════════════════════════════════════
// PROFILES — CRUD для серверов
// ═══════════════════════════════════════════════════

// sanitizeProfileName валидирует имя профиля: буквы и цифры (включая кириллицу),
// дефисы, подчёркивания, пробелы. Символы, опасные для файловой системы
// (\/ : * ? " < > | и управляющие), вырезаются.
// Имена устройств Windows: файл «CON.json» открывается как устройство CON,
// а не как файл — os.ReadFile на нём виснет навсегда.
var reservedProfileNames = map[string]bool{
	"CON": true, "PRN": true, "AUX": true, "NUL": true,
	"COM1": true, "COM2": true, "COM3": true, "COM4": true, "COM5": true,
	"COM6": true, "COM7": true, "COM8": true, "COM9": true,
	"LPT1": true, "LPT2": true, "LPT3": true, "LPT4": true, "LPT5": true,
	"LPT6": true, "LPT7": true, "LPT8": true, "LPT9": true,
}

func sanitizeProfileName(name string) string {
	var out []rune
	for _, r := range name {
		switch {
		case unicode.IsLetter(r), unicode.IsDigit(r), r == '-', r == '_', r == ' ':
			out = append(out, r)
		}
	}
	if len(out) > maxProfileNameRunes {
		out = out[:maxProfileNameRunes]
	}
	return strings.TrimSpace(string(out))
}

// validateProfileName проверяет имя после санитизации.
func validateProfileName(name string) error {
	if name == "" {
		return fmt.Errorf("пустое имя профиля")
	}
	if reservedProfileNames[strings.ToUpper(name)] {
		return fmt.Errorf("имя %q зарезервировано Windows (CON, PRN, AUX, NUL, COM1-9, LPT1-9)", name)
	}
	return nil
}

// profilePath возвращает путь к файлу профиля с проверкой имени.
func (s *Store) profilePath(name string) (string, error) {
	name = sanitizeProfileName(name)
	if err := validateProfileName(name); err != nil {
		return "", err
	}
	return filepath.Join(s.baseDir, "servers", name+".json"), nil
}

// ProfileExists — есть ли профиль с таким (санитизированным) именем.
// На Windows сравнение регистронезависимое: "Case 1" и "case 1" — один файл.
func (s *Store) ProfileExists(name string) bool {
	path, err := s.profilePath(name)
	if err != nil {
		return false
	}
	_, err = os.Stat(path)
	return err == nil
}

// LoadProfile загружает профиль сервера по имени.
func (s *Store) LoadProfile(name string) (*ProfileData, error) {
	path, err := s.profilePath(name)
	if err != nil {
		return nil, err
	}
	p, err := s.loadProfileAt(path)
	if err != nil {
		return nil, fmt.Errorf("профиль %q: %w", sanitizeProfileName(name), err)
	}
	return p, nil
}

func (s *Store) loadProfileAt(path string) (*ProfileData, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	data = bytes.TrimPrefix(data, []byte{0xEF, 0xBB, 0xBF})
	var p ProfileData
	if err := json.Unmarshal(data, &p); err != nil {
		return nil, fmt.Errorf("повреждённый JSON: %w", err)
	}
	return &p, nil
}

// SaveProfile сохраняет профиль сервера (атомарно), перезаписывая существующий.
func (s *Store) SaveProfile(name string, p ProfileData) error {
	path, err := s.profilePath(name)
	if err != nil {
		return err
	}
	return s.saveProfileAt(path, p)
}

// SaveProfileNew сохраняет НОВЫЙ профиль и отказывается перезаписывать
// существующий (в т.ч. отличающийся только регистром — на Windows это тот же
// файл). Без этой проверки servers add молча терял профиль.
func (s *Store) SaveProfileNew(name string, p ProfileData) error {
	path, err := s.profilePath(name)
	if err != nil {
		return err
	}
	if _, statErr := os.Stat(path); statErr == nil {
		return fmt.Errorf("профиль %q уже существует — выберите другое имя", sanitizeProfileName(name))
	}
	return s.saveProfileAt(path, p)
}

func (s *Store) saveProfileAt(path string, p ProfileData) error {
	if p.DeviceID == "" {
		if existing, err := s.loadProfileAt(path); err == nil && existing.DeviceID != "" {
			p.DeviceID = existing.DeviceID
		} else {
			p.DeviceID = uuid.New().String()
		}
	}
	data, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	return atomicWrite(path, data)
}

// DeleteProfile удаляет профиль сервера.
func (s *Store) DeleteProfile(name string) error {
	path, err := s.profilePath(name)
	if err != nil {
		return err
	}
	if err := os.Remove(path); err != nil {
		return fmt.Errorf("удалить профиль %q: %w", sanitizeProfileName(name), err)
	}
	return nil
}

// CanonicalProfileName — имя, под которым профиль хранится на диске
// (санитизация + обрезка). Нужно CLI, чтобы сравнивать имена до записи.
func CanonicalProfileName(name string) string { return sanitizeProfileName(name) }

// BackupProfile копирует файл профиля в backups/ — перед удалением
// повреждённого профиля, чтобы данные можно было восстановить вручную.
func (s *Store) BackupProfile(name string) (string, error) {
	src, err := s.profilePath(name)
	if err != nil {
		return "", err
	}
	data, err := os.ReadFile(src)
	if err != nil {
		return "", fmt.Errorf("прочитать профиль: %w", err)
	}
	dir := filepath.Join(s.baseDir, "backups")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	dst := filepath.Join(dir, fmt.Sprintf("%s-%s.broken.json",
		sanitizeProfileName(name), time.Now().Format("20060102-150405")))
	if err := os.WriteFile(dst, data, 0o600); err != nil {
		return "", err
	}
	return dst, nil
}

// ListBrokenProfiles возвращает имена профилей, которые есть на диске, но не
// читаются как JSON. Раньше они не показывались нигде, а rm удалял их без
// предупреждения — возможность ручного восстановления терялась.
func (s *Store) ListBrokenProfiles() []string {
	dir := filepath.Join(s.baseDir, "servers")
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var broken []string
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		name := strings.TrimSuffix(e.Name(), ".json")
		data, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			broken = append(broken, name)
			continue
		}
		data = bytes.TrimPrefix(data, []byte{0xEF, 0xBB, 0xBF})
		var p ProfileData
		if err := json.Unmarshal(data, &p); err != nil {
			broken = append(broken, name)
		}
	}
	return broken
}

// ListProfiles возвращает все профили серверов.
func (s *Store) ListProfiles() map[string]ProfileData {
	dir := filepath.Join(s.baseDir, "servers")
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	result := make(map[string]ProfileData)
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		name := strings.TrimSuffix(e.Name(), ".json")
		data, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			continue
		}
		// Файлы, сохранённые Windows-редакторами, могут начинаться с BOM —
		// без среза профиль «исчезает» из списка, хотя LoadProfile его читает
		// (там BOM срезается), а ListBrokenProfiles показывает его битым.
		data = bytes.TrimPrefix(data, []byte{0xEF, 0xBB, 0xBF})
		var p ProfileData
		if err := json.Unmarshal(data, &p); err != nil {
			continue
		}
		result[name] = p
	}
	return result
}

// CreateLogFile создаёт файл лога для сессии.
func (s *Store) CreateLogFile(peerIP string) (*os.File, error) {
	dir := filepath.Join(s.baseDir, "logs")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	return os.CreateTemp(dir, "*.log")
}

// ═══════════════════════════════════════════════════
// LOG FILE — запись полных логов в файл
// ═══════════════════════════════════════════════════

type LogFile struct {
	file    *os.File
	pending int // буферизованные строки до sync
}

// OpenLogFile открывает новый файл лога для сессии.
func (s *Store) OpenLogFile() *LogFile {
	dir := filepath.Join(s.baseDir, "logs")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil
	}
	// Миллисекунды + O_EXCL: два запуска в одну секунду больше не
	// перезаписывают лог друг друга. Коллизию в одну миллисекунду (быстрый
	// reconnect) разрешаем суффиксом — иначе вся сессия писала в никуда.
	name := time.Now().Format("2006-01-02_15-04-05.000") + ".log"
	path := filepath.Join(dir, name)
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	for suffix := 1; err != nil && os.IsExist(err) && suffix <= 100; suffix++ {
		path = filepath.Join(dir, time.Now().Format("2006-01-02_15-04-05.000")+fmt.Sprintf("-%02d", suffix)+".log")
		f, err = os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	}
	if err != nil {
		return nil
	}
	return &LogFile{file: f}
}

// Write пишет строку в файл лога.
// Sync каждые 50 строк чтобы не душить I/O.
func (lf *LogFile) Write(level, msg string) {
	if lf == nil || lf.file == nil {
		return
	}
	ts := time.Now().Format("15:04:05")
	fmt.Fprintf(lf.file, "[%s] [%s] %s\n", ts, level, msg)
	lf.pending++
	if lf.pending >= 50 {
		lf.file.Sync()
		lf.pending = 0
	}
}

// Close закрывает файл лога.
func (lf *LogFile) Close() {
	if lf != nil && lf.file != nil {
		lf.file.Sync()
		lf.file.Close()
	}
}

// ReadLatestLog читает содержимое последнего лог-файла.
func (s *Store) ReadLatestLog() string {
	dir := filepath.Join(s.baseDir, "logs")
	entries, err := os.ReadDir(dir)
	if err != nil {
		return ""
	}

	// Только .log-файлы: временные, desktop.ini и вложенные каталоги — не логи.
	// Время берём из имени; если имя не по шаблону — из mtime.
	var latestPath string
	var latestTime time.Time
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(strings.ToLower(e.Name()), ".log") {
			continue
		}
		info, ierr := e.Info()
		if ierr != nil {
			continue
		}
		when := logFileTime(e.Name(), info.ModTime())
		if latestPath == "" || when.After(latestTime) {
			latestPath, latestTime = filepath.Join(dir, e.Name()), when
		}
	}
	if latestPath == "" {
		return ""
	}
	data, err := os.ReadFile(latestPath)
	if err != nil {
		return ""
	}
	return string(data)
}

// logFileTime — время лога: из имени (формат OpenLogFile), иначе mtime.
func logFileTime(name string, mod time.Time) time.Time {
	base := strings.TrimSuffix(name, ".log")
	for _, layout := range []string{"2006-01-02_15-04-05.000", "2006-01-02_15-04-05"} {
		if t, err := time.ParseInLocation(layout, base, time.Local); err == nil {
			return t
		}
	}
	return mod
}

// ═══════════════════════════════════════════════════
// HELPERS
// ═══════════════════════════════════════════════════

// configDir возвращает каталог настроек (xwdtt).
// Перенос из легаси-каталога старого названия проекта выполняется
// НЕ здесь, а явно в main (MigrateLegacyConfigDir): side-effect в геттере
// выполнялся бы даже в тестах, вызывающих Store без подмены HOME/AppData,
// и трогал бы реальный каталог пользователя.
func configDir() string {
	base, err := os.UserConfigDir()
	if err != nil {
		base = os.Getenv("HOME")
	}
	return filepath.Join(base, "xwdtt")
}

// MigrateLegacyConfigDir переносит настройки из каталога старого названия
// проекта (pwdtt → xwdtt) одним rename: если новый каталог ещё не создан,
// а старый существует — переименовываем его целиком (настройки, профили
// серверов, логи), чтобы смена названия не теряла данные. Вызывается один
// раз из main. Ошибки не фатальны — при сбое старый каталог остаётся.
func MigrateLegacyConfigDir() {
	base, err := os.UserConfigDir()
	if err != nil {
		base = os.Getenv("HOME")
	}
	newDir := filepath.Join(base, "xwdtt")
	if _, err := os.Stat(newDir); !os.IsNotExist(err) {
		return // уже новый каталог — переносить нечего
	}
	legacy := filepath.Join(base, "pwdtt")
	if !isDir(legacy) {
		return
	}
	if err := os.Rename(legacy, newDir); err != nil {
		log.Printf("[CONFIG] не удалось перенести легаси-каталог %s: %v", legacy, err)
		return
	}
	log.Printf("[CONFIG] настройки перенесены из %s в %s", legacy, newDir)
}

func isDir(path string) bool {
	fi, err := os.Stat(path)
	return err == nil && fi.IsDir()
}

// atomicWrite записывает файл атомарно: tmp → rename.
// 0600: профили содержат пароль, config.json — настройки.
// При ошибке rename временный файл удаляется, чтобы копия секретов не
// оставалась на диске.
func atomicWrite(path string, data []byte) error {
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		os.Remove(tmp)
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		os.Remove(tmp)
		if fi, statErr := os.Stat(path); statErr == nil && fi.Mode().Perm()&0o200 == 0 {
			return fmt.Errorf("не удалось заменить %s (файл только для чтения): %w", path, err)
		}
		return err
	}
	return nil
}

// NewTestStore создаёт Store с временной директорией (для тестов).
// Подменяет HOME и, на Windows, AppData — os.UserConfigDir() читает именно их,
// иначе тесты пишут в реальный конфиг пользователя.
func NewTestStore(t interface{ TempDir() string; Setenv(string, string) }) *Store {
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("AppData", filepath.Join(dir, "AppData", "Roaming"))
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(dir, ".config"))
	return NewStore()
}
