package legacyarchive

import (
	"encoding/json"
	"io"
	"os"

	"github.com/shisui1511/xkeen-control-panel/internal/services/configlayer"
)

// Status читает метаданные архива, не декодируя список адресов.
func (c *Cleanup) Status() configlayer.LegacySummary {
	var summary configlayer.LegacySummary
	_, _ = withDataMode(c.DataDir, lockNone, func(data archiveDir) (Result, error) {
		return inLegacy(data, false, func(legacy archiveDir) (Result, error) {
			m, err := legacy.readMarker()
			if err != nil {
				return Result{}, err
			}
			result := m.result(legacy.Name())
			summary.ArchivePath = result.ArchivePath
			summary.Pending = result.Complete && result.ArchivePath != ""
			if summary.Pending {
				summary.URLsAvailable = m.URLCount == 0
				if m.URLCount > 0 {
					if f, err := legacy.privateFile(urlsName); err == nil {
						summary.URLsAvailable = true
						f.Close()
					}
				}
			}
			return Result{}, nil
		})
	})
	return summary
}

// URLs читает отдельный секретный список. Ошибки не содержат его байты.
func (c *Cleanup) URLs() ([]string, error) {
	urls := []string{}
	_, err := withDataMode(c.DataDir, lockNone, func(data archiveDir) (Result, error) {
		return inLegacy(data, false, func(legacy archiveDir) (Result, error) {
			m, err := legacy.readMarker()
			if err != nil || m.State != complete {
				return Result{}, errMarker
			}
			if m.URLCount == 0 {
				return Result{}, nil
			}
			f, err := legacy.privateFile(urlsName)
			if err != nil {
				return Result{}, err
			}
			defer f.Close()
			dec := json.NewDecoder(f)
			if err := dec.Decode(&urls); err != nil || len(urls) != m.URLCount {
				return Result{}, errMarker
			}
			if _, err := dec.Token(); err != io.EOF {
				return Result{}, errMarker
			}
			return Result{}, nil
		})
	})
	if err != nil {
		return nil, errMarker
	}
	return urls, nil
}

// ForgetURLs убирает список из приватного каталога после записи закрытия.
// Занятый замок (идёт очистка) — ошибка: уборку повторит следующая сверка слоя.
func (c *Cleanup) ForgetURLs() error {
	_, err := withDataMode(c.DataDir, lockTry, func(data archiveDir) (Result, error) {
		return inLegacy(data, false, func(legacy archiveDir) (Result, error) {
			m, err := legacy.readMarker()
			if err != nil {
				return Result{}, err
			}
			if m.State != complete {
				return Result{}, errMarker
			}
			return Result{}, removePrivate(legacy, urlsName)
		})
	})
	if os.IsNotExist(err) {
		return nil
	}
	return err
}
