package backend

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"runtime"
	"strings"
	"time"
)

// UpdateInfo — информация о доступном обновлении.
type UpdateInfo struct {
	Available bool   `json:"available"` // есть ли обновление
	Version   string `json:"version"`   // версия обновления
	URL       string `json:"url"`       // ссылка на скачивание
	Body      string `json:"body"`      // описание изменений (changelog)
	// AssetName — имя ассета под текущую ОС/архитектуру (для сверки с SHA256SUMS).
	AssetName string `json:"asset_name"`
	// SumsURL — ссылка на ассет SHA256SUMS релиза (пусто, если его нет).
	SumsURL string `json:"sums_url"`
}

// CheckUpdate проверяет наличие обновлений на GitHub.
// Репозиторий: zxcursedzxc0721/XWDTT.
// Возвращает ошибку если сеть недоступна или API недоступен.
func CheckUpdate(currentVersion string) (*UpdateInfo, error) {
	url := "https://api.github.com/repos/zxcursedzxc0721/XWDTT/releases/latest"

	client := &http.Client{Timeout: 8 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		// Сеть недоступна или таймаут
		return nil, fmt.Errorf("network error: %w", err)
	}
	defer resp.Body.Close()

	// 404 на /releases/latest — «в репозитории ещё нет релизов», а не сбой
	// API: GitHub отдаёт его всегда, когда релизов нет, и раньше любое
	// обновление падало с «github api status: 404».
	if resp.StatusCode == http.StatusNotFound {
		return &UpdateInfo{Available: false}, nil
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("github api: %s (status %d)", resp.Status, resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	var release struct {
		TagName string `json:"tag_name"`
		Body    string `json:"body"`
		Assets  []struct {
			BrowserDownloadURL string `json:"browser_download_url"`
			Name               string `json:"name"`
		} `json:"assets"`
	}
	if err := json.Unmarshal(body, &release); err != nil {
		return nil, fmt.Errorf("parse response: %w", err)
	}

	if release.TagName == "" {
		return &UpdateInfo{Available: false}, nil
	}

	latest := strings.TrimPrefix(release.TagName, "v")

	// Ищем ссылку на скачивание для текущей ОС и ассет SHA256SUMS.
	// Делаем ДО сравнения версий: verify-режиму нужен SumsURL даже когда
	// обновлений нет (сверка скачанного файла с последним релизом).
	var downloadURL, assetName, sumsURL string
	osName := runtime.GOOS // "linux", "windows" или "darwin"
	// Возможные синонимы ОС в имени ассета
	osAliases := []string{osName}
	if osName == "darwin" {
		osAliases = append(osAliases, "macos", "mac", "osx")
	}
	// Подходящие маркеры архитектуры (darwin собирается universal)
	archMarkers := []string{runtime.GOARCH}
	if osName == "darwin" {
		archMarkers = append(archMarkers, "universal", "arm64", "amd64")
	} else {
		archMarkers = append(archMarkers, "amd64", "x86_64")
	}
	for _, asset := range release.Assets {
		if strings.EqualFold(asset.Name, "SHA256SUMS") {
			sumsURL = asset.BrowserDownloadURL
			continue
		}
		name := strings.ToLower(asset.Name)
		osOK := false
		for _, a := range osAliases {
			if strings.Contains(name, a) {
				osOK = true
				break
			}
		}
		if !osOK {
			continue
		}
		for _, m := range archMarkers {
			if strings.Contains(name, m) {
				downloadURL = asset.BrowserDownloadURL
				assetName = asset.Name
				break
			}
		}
		if downloadURL != "" {
			break
		}
	}

	// Сравниваем версии: latest > current?
	log.Printf("[UPDATE] Release: %s, Current: %s, isNewer: %v", latest, currentVersion, isNewer(latest, currentVersion))
	if !isNewer(latest, currentVersion) {
		return &UpdateInfo{Version: latest, URL: downloadURL, AssetName: assetName, SumsURL: sumsURL}, nil
	}

	return &UpdateInfo{
		Available: true,
		Version:   latest,
		URL:       downloadURL,
		Body:      release.Body,
		AssetName: assetName,
		SumsURL:   sumsURL,
	}, nil
}

// isNewer проверяет что a > b (semver, с учётом pre-release).
func isNewer(a, b string) bool { return compareVersions(a, b) > 0 }

// compareVersions — semver-сравнение: -1, 0, 1.
// 1.6.1 > 1.6.1-beta > 1.6.1-alpha; build-метаданные (+build) игнорируются.
// Раньше pre-release считался равным релизу, и пользователь 1.6.1-beta не
// получал финальный 1.6.1.
func compareVersions(a, b string) int {
	ac, ap := splitVersion(a)
	bc, bp := splitVersion(b)
	for i := 0; i < 3; i++ {
		if ac[i] != bc[i] {
			if ac[i] > bc[i] {
				return 1
			}
			return -1
		}
	}
	// Финальный релиз новее любого pre-release.
	switch {
	case ap == "" && bp == "":
		return 0
	case ap == "" && bp != "":
		return 1
	case ap != "" && bp == "":
		return -1
	}
	return comparePrerelease(ap, bp)
}

// splitVersion разбирает "v1.6.1-beta.2+build" → ([3]int{1,6,1}, "beta.2").
func splitVersion(v string) ([3]int, string) {
	var core [3]int
	v = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(v), "v"))
	if i := strings.IndexByte(v, '+'); i != -1 {
		v = v[:i]
	}
	pre := ""
	if i := strings.IndexByte(v, '-'); i != -1 {
		pre, v = v[i+1:], v[:i]
	}
	for i, p := range strings.SplitN(v, ".", 3) {
		n := 0
		for j := 0; j < len(p) && p[j] >= '0' && p[j] <= '9'; j++ {
			n = n*10 + int(p[j]-'0')
		}
		core[i] = n
	}
	return core, pre
}

// comparePrerelease сравнивает dot-separated идентификаторы по semver §11.4:
// числовые — как числа, буквенные — лексикографически, числовой младше
// буквенного, меньшее число идентификаторов — младше.
func comparePrerelease(a, b string) int {
	as, bs := strings.Split(a, "."), strings.Split(b, ".")
	for i := 0; i < len(as) && i < len(bs); i++ {
		an, aNum := numericPart(as[i])
		bn, bNum := numericPart(bs[i])
		switch {
		case aNum && bNum:
			if an != bn {
				if an > bn {
					return 1
				}
				return -1
			}
		case aNum != bNum:
			if aNum {
				return -1
			}
			return 1
		default:
			if as[i] != bs[i] {
				if as[i] > bs[i] {
					return 1
				}
				return -1
			}
		}
	}
	switch {
	case len(as) > len(bs):
		return 1
	case len(as) < len(bs):
		return -1
	}
	return 0
}

func numericPart(s string) (int, bool) {
	if s == "" {
		return 0, false
	}
	n := 0
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return 0, false
		}
		n = n*10 + int(s[i]-'0')
	}
	return n, true
}

// ═══════════════════════════════════════════════════
// Проверка целостности скачанных файлов (SHA256SUMS)
// ═══════════════════════════════════════════════════

// FetchExpectedSHA256 скачивает SHA256SUMS релиза и возвращает ожидаемый
// SHA-256 для файла assetName. Ошибка — если ассета нет, сеть недоступна
// или нужной строки в файле нет.
func FetchExpectedSHA256(sumsURL, assetName string) (string, error) {
	if sumsURL == "" {
		return "", fmt.Errorf("в релизе нет ассета SHA256SUMS")
	}
	if assetName == "" {
		return "", fmt.Errorf("неизвестно имя файла для проверки")
	}
	client := &http.Client{Timeout: 8 * time.Second}
	resp, err := client.Get(sumsURL)
	if err != nil {
		return "", fmt.Errorf("download SHA256SUMS: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("SHA256SUMS: status %d", resp.StatusCode)
	}
	// SHA256SUMS — маленький текстовый файл; режем на 1 МБ на всякий случай.
	data, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return "", fmt.Errorf("read SHA256SUMS: %w", err)
	}
	sum, ok := parseSumsContent(string(data), assetName)
	if !ok {
		return "", fmt.Errorf("в SHA256SUMS нет строки для %q", assetName)
	}
	return sum, nil
}

// parseSumsContent ищет строку "<64 hex>  <имя файла>" для wantName.
// Поддерживаются разделители вида `hash  name`, `hash *name`, `hash\tname`.
func parseSumsContent(content, wantName string) (string, bool) {
	for _, line := range strings.Split(content, "\n") {
		line = strings.TrimRight(line, "\r")
		if len(line) < 65 {
			continue
		}
		hexPart := line[:64]
		if !isHex64(hexPart) {
			continue
		}
		name := strings.TrimLeft(line[64:], " \t*")
		if strings.EqualFold(name, wantName) {
			return strings.ToLower(hexPart), true
		}
	}
	return "", false
}

func isHex64(s string) bool {
	if len(s) != 64 {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f' || c >= 'A' && c <= 'F') {
			return false
		}
	}
	return true
}

// VerifyFileSHA256 хеширует файл по пути и сверяет с ожидаемым SHA-256
// (из SHA256SUMS релиза). Сравнение без учёта регистра.
func VerifyFileSHA256(path, expected string) (string, error) {
	expected = strings.ToLower(strings.TrimSpace(expected))
	if !isHex64(expected) {
		return "", fmt.Errorf("некорректный ожидаемый хеш %q", expected)
	}
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", fmt.Errorf("hash %s: %w", path, err)
	}
	got := hex.EncodeToString(h.Sum(nil))
	if got != expected {
		return got, fmt.Errorf("хеш НЕ совпадает: ожидался %s, получен %s", expected, got)
	}
	return got, nil
}
