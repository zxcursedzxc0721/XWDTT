package backend

// Миграция каталога при переименовании проекта (pwdtt → xwdtt).
// Вызывается из main, НЕ из configDir (side-effect в гетторе трогал бы
// реальный каталог пользователя в тестах, вызывающих Store без подмены env).

import (
	"os"
	"path/filepath"
	"testing"
)

// testUserConfigDir — база конфигурации с подменённым окружением.
func testUserConfigDir(t *testing.T) string {
	t.Helper()
	base := t.TempDir()
	t.Setenv("HOME", base)
	t.Setenv("AppData", filepath.Join(base, "AppData", "Roaming"))
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(base, ".config"))
	userCfg, err := os.UserConfigDir()
	if err != nil {
		t.Fatalf("UserConfigDir: %v", err)
	}
	return userCfg
}

func TestMigrateLegacyConfigDir(t *testing.T) {
	userCfg := testUserConfigDir(t)

	legacy := filepath.Join(userCfg, "pwdtt")
	if err := os.MkdirAll(filepath.Join(legacy, "servers"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(legacy, "config.json"), []byte(`{"routeMode":"split"}`), 0o644); err != nil {
		t.Fatal(err)
	}

	MigrateLegacyConfigDir()

	want := filepath.Join(userCfg, "xwdtt")
	if got := configDir(); got != want {
		t.Fatalf("configDir() = %s, ожидалось %s", got, want)
	}
	if _, err := os.Stat(filepath.Join(want, "config.json")); err != nil {
		t.Errorf("config.json не перенесён в новый каталог: %v", err)
	}
	if _, err := os.Stat(legacy); !os.IsNotExist(err) {
		t.Errorf("легаси-каталог должен быть переименован, но остался: %v", err)
	}
}

// Повторный вызов при уже существующем новом каталоге не трогает легаси.
func TestMigrateLegacyConfigDir_NoOpWhenNewExists(t *testing.T) {
	userCfg := testUserConfigDir(t)

	legacy := filepath.Join(userCfg, "pwdtt")
	newDir := filepath.Join(userCfg, "xwdtt")
	if err := os.MkdirAll(legacy, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(newDir, 0o755); err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(legacy, "keep.txt")
	if err := os.WriteFile(marker, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	MigrateLegacyConfigDir()

	if _, err := os.Stat(marker); err != nil {
		t.Errorf("легаси-каталог изменён при существующем новом: %v", err)
	}
}

// configDir() чистый: без подмены env и без side-effect — всегда новый путь.
func TestConfigDir_PureNoSideEffect(t *testing.T) {
	userCfg := testUserConfigDir(t)

	legacy := filepath.Join(userCfg, "pwdtt")
	if err := os.MkdirAll(legacy, 0o755); err != nil {
		t.Fatal(err)
	}

	got := configDir()
	if want := filepath.Join(userCfg, "xwdtt"); got != want {
		t.Errorf("configDir() = %s, ожидалось %s", got, want)
	}
	if _, err := os.Stat(legacy); err != nil {
		t.Errorf("configDir() не должен трогать легаси-каталог: %v", err)
	}
}
