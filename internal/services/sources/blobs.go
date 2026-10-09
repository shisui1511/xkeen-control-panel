package sources

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"

	"github.com/shisui1511/xkeen-control-panel/internal/nodes"
	"github.com/shisui1511/xkeen-control-panel/internal/utils"
)

// Ошибки хранилища снимков. Пропавший или испорченный снимок — ошибка генератора,
// а не пустой набор: пустой набор удалил бы рабочие файлы ядра (Pitfall 7).
var (
	errSnapshotMissing = errors.New("sources: снимок узлов не найден")
	errSnapshotCorrupt = errors.New("sources: снимок узлов повреждён")
)

// hashRE — имя снимка: sha256 содержимого в hex. Путь файла строится только из
// значения, прошедшего эту проверку.
var hashRE = regexp.MustCompile(`^[0-9a-f]{64}$`)

// blobStore — снимки наборов узлов по хэшу: <dir>/<sha256>.json (0600). Каталог
// создаётся первой записью (0700); конструктор диска не трогает. Уборка снимков
// и проверка целостности по расписанию — план 145-17.
type blobStore struct {
	dir string
}

func (b blobStore) path(hash string) (string, error) {
	if !hashRE.MatchString(hash) {
		return "", fmt.Errorf("%w: недопустимое имя", errSnapshotCorrupt)
	}
	return filepath.Join(b.dir, hash+".json"), nil
}

// canonical — канонический JSON набора узлов: узлы отсортированы по ключу
// (идентичность и отпечаток транспорта, затем имя и исходный текст), ключи карт
// сортирует encoding/json. Один и тот же состав даёт один и тот же хэш независимо
// от порядка узлов в ответе провайдера.
func canonical(list []nodes.Node) ([]byte, error) {
	sorted := append([]nodes.Node(nil), list...)
	sort.SliceStable(sorted, func(i, j int) bool {
		a, c := sorted[i], sorted[j]
		if ka, kc := a.DupKey(), c.DupKey(); ka != kc {
			return ka < kc
		}
		if a.Name != c.Name {
			return a.Name < c.Name
		}
		return a.Text < c.Text
	})
	if sorted == nil {
		sorted = []nodes.Node{}
	}
	return json.Marshal(sorted)
}

// put записывает набор узлов и возвращает хэш содержимого. Существующий
// исправный снимок не переписывается.
func (b blobStore) put(list []nodes.Node) (string, error) {
	data, err := canonical(list)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(data)
	hash := hex.EncodeToString(sum[:])
	path, err := b.path(hash)
	if err != nil {
		return "", err
	}
	if cur, err := os.ReadFile(path); err == nil && bytes.Equal(cur, data) {
		return hash, nil
	}
	if err := os.MkdirAll(b.dir, 0o700); err != nil {
		return "", fmt.Errorf("sources: каталог снимков: %w", err)
	}
	if err := utils.AtomicWriteFile(path, data, 0o600); err != nil {
		return "", fmt.Errorf("sources: запись снимка: %w", err)
	}
	return hash, nil
}

// get читает набор узлов, сверяя sha256 содержимого с именем снимка.
func (b blobStore) get(hash string) ([]nodes.Node, error) {
	path, err := b.path(hash)
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, errSnapshotMissing
		}
		return nil, fmt.Errorf("sources: чтение снимка: %w", err)
	}
	sum := sha256.Sum256(data)
	if hex.EncodeToString(sum[:]) != hash {
		return nil, errSnapshotCorrupt
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	var list []nodes.Node
	if err := dec.Decode(&list); err != nil {
		return nil, fmt.Errorf("%w: %v", errSnapshotCorrupt, err)
	}
	return list, nil
}
