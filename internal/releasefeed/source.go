// Package releasefeed — источник выпусков панели: репозиторий GitHub, из
// которого панель и установщик читают список релизов, тела релизов и ассеты.
//
// Источник отделён от репозитория кода (SourceRepository): там остаются код,
// PR и статусы, а несовместимые с прежней панелью выпуски публикуются только
// здесь. Прежние установки читают адрес, зашитый в их сборку, и видят только
// совместимые выпуски; новая сборка читает только этот источник, без запасного
// пути. Имя записано одной строкой в файле repository рядом с пакетом и
// встраивается в бинарник; scripts/check-release-source.js сверяет его со всеми
// потребителями (update.go, setup.sh, конвейер выпусков, обвязка стенда).
package releasefeed

import (
	_ "embed"
	"errors"
	"fmt"
	"regexp"
	"strings"
)

// SourceRepository — репозиторий кода панели (модуль Go). Источник выпусков с
// ним совпадать не должен.
const SourceRepository = "shisui1511/xkeen-control-panel"

//go:embed repository
var repositoryFile string

// repoNameRe — owner/name в допустимом для GitHub виде.
var repoNameRe = regexp.MustCompile(`^[A-Za-z0-9](?:[A-Za-z0-9-]*[A-Za-z0-9])?/[A-Za-z0-9._-]+$`)

var (
	repository  string
	contractErr error
)

func init() {
	repository, contractErr = parseRepository(repositoryFile)
}

// parseRepository разбирает содержимое контракта: ровно одна непустая строка
// owner/name, не совпадающая с репозиторием кода.
func parseRepository(raw string) (string, error) {
	name := strings.TrimSpace(raw)
	switch {
	case name == "":
		return "", errors.New("release source: empty repository contract")
	case strings.ContainsAny(name, "\r\n\t "):
		return "", fmt.Errorf("release source: repository contract must be a single owner/name line, got %q", name)
	case !repoNameRe.MatchString(name):
		return "", fmt.Errorf("release source: invalid repository %q", name)
	case strings.EqualFold(name, SourceRepository):
		return "", fmt.Errorf("release source: repository %q is the source code repository", name)
	}
	return name, nil
}

// Check — ошибка встроенного контракта; nil — источник задан верно. Потребитель
// проверяет её до сетевого запроса: при ошибке обновление не проверяется и не
// ставится, другой источник не подставляется.
func Check() error {
	return contractErr
}

// Repository — owner/name источника выпусков; пусто при ошибке контракта.
func Repository() string {
	return repository
}

// APIReleasesURL — адрес списка релизов источника в API GitHub
// (…/releases; потребитель добавляет ?per_page= или /tags/<тег>).
func APIReleasesURL() string {
	if contractErr != nil {
		return ""
	}
	return "https://api.github.com/repos/" + repository + "/releases"
}

// DownloadURL — адрес ассета asset релиза tag (тег с префиксом v).
func DownloadURL(tag, asset string) string {
	if contractErr != nil {
		return ""
	}
	return "https://github.com/" + repository + "/releases/download/" + tag + "/" + asset
}
