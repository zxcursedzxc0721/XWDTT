package backend

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
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

	// Сравниваем версии: latest > current?
	latest := strings.TrimPrefix(release.TagName, "v")
	log.Printf("[UPDATE] Release: %s, Current: %s, isNewer: %v", latest, currentVersion, isNewer(latest, currentVersion))
	if !isNewer(latest, currentVersion) {
		return &UpdateInfo{Available: false}, nil
	}

	// Ищем ссылку на скачивание для текущей ОС
	var downloadURL string
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
				break
			}
		}
		if downloadURL != "" {
			break
		}
	}

	return &UpdateInfo{
		Available: true,
		Version:   latest,
		URL:       downloadURL,
		Body:      release.Body,
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
