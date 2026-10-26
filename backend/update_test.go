package backend

// XW-09: сравнение версий с pre-release.

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestIsNewer_Semver(t *testing.T) {
	cases := []struct {
		a, b string
		want bool
	}{
		{"1.6.2", "1.6.1", true},
		{"1.10.0", "1.9.9", true},
		{"v1.7.0", "1.6.0", true},
		{"1.6.1", "1.6.1", false},
		{"v1.6.1", "1.6.1", false},
		{"1.6.1", "1.6.1-beta", true},  // финал новее pre-release
		{"1.6.1-beta", "1.6.1", false}, // и наоборот — не «новее»
		{"1.6.1-rc1", "1.6.1-beta", true},
		{"1.6.1-beta.2", "1.6.1-beta.1", true},
		{"1.6.1-beta", "1.6.0", true},
		{"1.6.0", "1.6.1-beta", false},
		{"2.0.0+build5", "2.0.0", false}, // build-метаданные не влияют
	}
	for _, c := range cases {
		if got := isNewer(c.a, c.b); got != c.want {
			t.Errorf("isNewer(%q, %q) = %v, ожидалось %v", c.a, c.b, got, c.want)
		}
	}
}

// ═══ SHA256SUMS: парсинг и сверка файлов ═══

func TestParseSumsContent(t *testing.T) {
	const (
		win = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
		lin = "BBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBB"
	)
	content := win + "  xwdtt-windows-amd64.exe\n" +
		lin + " *xwdtt-linux-amd64\r\n" +
		"ccccc  короткая-строка\n" +
		"zzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzz  битый-хеш\n"

	cases := []struct {
		want string
		name string
		sum  string
		ok   bool
	}{
		{"Windows", "xwdtt-windows-amd64.exe", win, true},
		{"Linux", "xwdtt-linux-amd64", strings.ToLower(lin), true},
		{"без регистра", "XWDTT-WINDOWS-AMD64.EXE", win, true},
		{"нет файла", "XWDTT-macos.zip", "", false},
	}
	for _, c := range cases {
		sum, ok := parseSumsContent(content, c.name)
		if ok != c.ok || sum != c.sum {
			t.Errorf("%s: parseSumsContent(...) = (%q, %v), ожидалось (%q, %v)", c.want, sum, ok, c.sum, c.ok)
		}
	}
}

func TestVerifyFileSHA256(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "f.bin")
	if err := os.WriteFile(path, []byte("hello xwdtt"), 0o600); err != nil {
		t.Fatal(err)
	}
	// sha256("hello xwdtt")
	const good = "1cf0e3b0b1a5a2b0476a2c5ef1a3f6a86c3f24b8f7c70ec1e0d1c5e7f46ea2b1"

	got, err := VerifyFileSHA256(path, good)
	if err == nil {
		t.Fatalf("ожидалась ошибка при несовпадении хеша, got=%s", got)
	}

	if _, err := VerifyFileSHA256(path, "не-хеш"); err == nil {
		t.Error("некорректный хеш должен отклоняться")
	}
	if _, err := VerifyFileSHA256(filepath.Join(dir, "нет.bin"), good); err == nil {
		t.Error("несуществующий файл должен отклоняться")
	}

	// Посчитаем настоящий хеш и убедимся в совпадении (регистр не важен).
	h := sha256.Sum256([]byte("hello xwdtt"))
	real := hex.EncodeToString(h[:])
	if sum, err := VerifyFileSHA256(path, strings.ToUpper(real)); err != nil || sum != real {
		t.Errorf("VerifyFileSHA256 = (%q, %v), ожидалось (%q, nil)", sum, err, real)
	}
}
