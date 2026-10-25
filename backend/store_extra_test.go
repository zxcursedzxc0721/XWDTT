package backend

// Регрессионные тесты XW-02/05/07/11/23/24.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func testStore(t *testing.T) *Store {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("AppData", filepath.Join(dir, "AppData", "Roaming"))
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(dir, ".config"))
	return NewStore()
}

func TestLoadSettings_BOMIsAccepted(t *testing.T) {
	s := testStore(t)
	path := filepath.Join(s.baseDir, "config.json")
	bom := []byte{0xEF, 0xBB, 0xBF}
	body := []byte(`{"autoStart":false,"routeMode":"split","workers":3}`)
	if err := os.WriteFile(path, append(bom, body...), 0o600); err != nil {
		t.Fatal(err)
	}

	got := s.LoadSettings()
	if got.AutoStart {
		t.Error("BOM: настройки сброшены на дефолт (AutoStart=true)")
	}
	if got.RouteMode != RouteModeSplit {
		t.Errorf("BOM: RouteMode = %q, ожидалось split", got.RouteMode)
	}
	if got.Workers != 3 {
		t.Errorf("BOM: Workers = %d, ожидалось 3", got.Workers)
	}
}

func TestLoadSettings_BrokenFileIsKept(t *testing.T) {
	s := testStore(t)
	path := filepath.Join(s.baseDir, "config.json")
	if err := os.WriteFile(path, []byte(`{oops`), 0o600); err != nil {
		t.Fatal(err)
	}

	if got := s.LoadSettings(); !got.AutoStart {
		t.Error("на повреждённом конфиге ожидались дефолты")
	}
	data, err := os.ReadFile(path)
	if err != nil || string(data) != `{oops` {
		t.Errorf("повреждённый конфиг перезаписан: %q, err=%v", data, err)
	}
}

func TestAtomicWrite_NoTmpLeftOnError(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	// Непустой каталог на месте целевого файла: rename гарантированно упадёт.
	if err := os.MkdirAll(filepath.Join(path, "inner"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := atomicWrite(path, []byte("secret")); err == nil {
		t.Fatal("ожидалась ошибка записи")
	}
	if _, err := os.Stat(path + ".tmp"); !os.IsNotExist(err) {
		t.Error("после ошибки остался .tmp с содержимым")
	}
}

func TestProfileReservedNames(t *testing.T) {
	s := testStore(t)
	for _, name := range []string{"CON", "con", "PRN", "AUX", "NUL", "COM1", "LPT9"} {
		if err := s.SaveProfile(name, ProfileData{PeerAddr: "1.2.3.4:1"}); err == nil {
			t.Errorf("SaveProfile(%q) должен быть отклонён", name)
		}
		if _, err := s.LoadProfile(name); err == nil {
			t.Errorf("LoadProfile(%q) должен вернуть ошибку, а не висеть", name)
		}
		if s.ProfileExists(name) {
			t.Errorf("ProfileExists(%q) = true", name)
		}
	}
}

func TestSaveProfileNew_RefusesExisting(t *testing.T) {
	s := testStore(t)
	if err := s.SaveProfileNew("A 1", ProfileData{PeerAddr: "1.1.1.1:1", Password: "p1"}); err != nil {
		t.Fatalf("первое сохранение: %v", err)
	}
	err := s.SaveProfileNew("A 1", ProfileData{PeerAddr: "2.2.2.2:2", Password: "p2"})
	if err == nil {
		t.Fatal("SaveProfileNew перезаписал существующий профиль")
	}
	p, lerr := s.LoadProfile("A 1")
	if lerr != nil {
		t.Fatal(lerr)
	}
	if p.Password != "p1" {
		t.Errorf("профиль перезаписан: Password = %q", p.Password)
	}
}

func TestListBrokenProfiles(t *testing.T) {
	s := testStore(t)
	if err := s.SaveProfile("Ok 1", ProfileData{PeerAddr: "1.1.1.1:1"}); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(s.baseDir, "servers", "Bad 1.json"), []byte("{broken"), 0o600); err != nil {
		t.Fatal(err)
	}
	broken := s.ListBrokenProfiles()
	if len(broken) != 1 || broken[0] != "Bad 1" {
		t.Errorf("ListBrokenProfiles = %v, ожидалось [Bad 1]", broken)
	}
	if _, err := s.LoadProfile("Ok 1"); err != nil {
		t.Errorf("целый профиль перестал читаться: %v", err)
	}
}

func TestReadLatestLog_IgnoresNonLogs(t *testing.T) {
	s := testStore(t)
	dir := filepath.Join(s.baseDir, "logs")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	old := filepath.Join(dir, "2026-01-01_00-00-00.000.log")
	newer := filepath.Join(dir, "2026-02-02_00-00-00.000.log")
	os.WriteFile(old, []byte("old"), 0o600)
	os.WriteFile(newer, []byte("new"), 0o600)
	// «Позже по имени», но не лог.
	os.WriteFile(filepath.Join(dir, "zzz.tmp"), []byte("tmp"), 0o600)
	// Время модификации не должно перебивать имя.
	now := time.Now()
	os.Chtimes(newer, now.Add(-time.Hour), now.Add(-time.Hour))

	if got := s.ReadLatestLog(); got != "new" {
		t.Errorf("ReadLatestLog = %q, ожидалось %q", got, "new")
	}
}

func TestSplitImport_BOMNullEmpty(t *testing.T) {
	bom := []byte{0xEF, 0xBB, 0xBF}
	if _, _, err := ParseSplitIPList(append(bom, []byte(`["1.2.3.4"]`)...)); err != nil {
		t.Errorf("BOM: %v", err)
	}
	for _, data := range []string{"null", "{}", "[]"} {
		if _, _, err := ParseSplitIPList([]byte(data)); err == nil {
			t.Errorf("%q: ожидалась ошибка, а не «0 записей успешно»", data)
		}
	}
	if _, _, err := ParseSplitIPList(append(bom, []byte(`[{"value":"1.2.3.4"}]`)...)); err == nil ||
		!strings.Contains(err.Error(), "JSON-массив") {
		t.Errorf("массив объектов: err = %v", err)
	}
}
