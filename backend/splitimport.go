package backend

// Импорт больших списков адресов для split-режима (`xwdtt ips import`).
//
// Форматы файла (JSON):
//
//	объект  {"claude.ai": ["1.2.3.4", ...], "cursor.com": [...], ...}
//	        — значения всех ключей собираются в один список (ключи-имена
//	        сервисов нужны только для читаемости файла);
//	массив  ["1.2.3.4", "5.6.7.8/32", "vk.com"]
//	        — просто список записей.
//
// Каждая запись нормализуется через NormalizeSplitEntry (IP → /32 и т.д.),
// дубли убираются. Некорректные записи не валят импорт — они пропускаются,
// их количество возвращается в skipped.

import (
	"bytes"
	"encoding/json"
	"fmt"
)

// ParseSplitIPList разбирает содержимое файла списка адресов.
// Возвращает нормализованный список (без дублей), число пропущенных
// некорректных записей и ошибку только если формат файла неизвестен.
func ParseSplitIPList(data []byte) (entries []string, skipped int, err error) {
	// UTF-8 BOM — частый артефакт Windows-редакторов; json.Unmarshal его не
	// принимает, и импорт падал с «неизвестный формат» без подсказки.
	data = bytes.TrimPrefix(data, []byte{0xEF, 0xBB, 0xBF})

	var raw []string

	// Пробуем формат «массив», затем «объект: сервис → IP».
	if uerr := json.Unmarshal(data, &raw); uerr != nil {
		var byService map[string][]string
		if oerr := json.Unmarshal(data, &byService); oerr != nil {
			return nil, 0, fmt.Errorf("неизвестный формат файла: нужен JSON-массив строк или объект {сервис: [ip...]}")
		}
		if len(byService) == 0 {
			return nil, 0, fmt.Errorf("в файле пустой объект — нечего импортировать")
		}
		for _, ips := range byService {
			raw = append(raw, ips...)
		}
	}

	// null/пустой массив — не «0 записей успешно», а ошибка ввода:
	// так опечатка в файле не маскируется под успешный импорт.
	if len(raw) == 0 {
		return nil, 0, fmt.Errorf("в файле нет записей (null или пустой массив)")
	}

	entries = make([]string, 0, len(raw))
	seen := make(map[string]bool, len(raw))
	for _, item := range raw {
		entry, nerr := NormalizeSplitEntry(item)
		if nerr != nil {
			skipped++
			continue
		}
		if seen[entry] {
			continue
		}
		seen[entry] = true
		entries = append(entries, entry)
	}
	return entries, skipped, nil
}
