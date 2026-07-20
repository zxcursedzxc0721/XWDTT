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
// Репозиторий: luminescq/PWDTT.
// Возвращает ошибку если сеть недоступна или API недоступен.
func CheckUpdate(currentVersion string) (*UpdateInfo, error) {
	url := "https://api.github.com/repos/luminescq/PWDTT/releases/latest"

	client := &http.Client{Timeout: 8 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		// Сеть недоступна или таймаут
		return nil, fmt.Errorf("network error: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("github api status: %d", resp.StatusCode)
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

// isNewer проверяет что a > b (semver сравнение).
func isNewer(a, b string) bool {
	av := parseVersion(a)
	bv := parseVersion(b)
	for i := 0; i < 3; i++ {
		if av[i] > bv[i] {
			return true
		}
		if av[i] < bv[i] {
			return false
		}
	}
	return false
}

func parseVersion(v string) [3]int {
	var result [3]int
	v = strings.TrimPrefix(v, "v")
	parts := strings.SplitN(v, ".", 3)
	for i, p := range parts {
		fmt.Sscanf(p, "%d", &result[i])
	}
	return result
}
