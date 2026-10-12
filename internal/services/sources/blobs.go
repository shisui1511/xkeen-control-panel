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
	"strings"
	"sync"

	"github.com/shisui1511/xkeen-control-panel/internal/nodes"
	"github.com/shisui1511/xkeen-control-panel/internal/services/configlayer"
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

// diagKey — ключ диагностики источника в реестре «в полёте»: он не пересекается с
// хэшем снимка (хэш — 64 hex, id источника — 8 hex).
func diagKey(id string) string { return "diag:" + id }

// diagFileRE — имя файла диагностики: <id источника>.<ядро>.json.
var diagFileRE = regexp.MustCompile(`^([0-9a-f]{8})\.(?:xray|mihomo)\.json$`)

// blobRegistry — реестр данных «в полёте»: записанные, но ещё не зафиксированные
// в Live снимки и файлы диагностики. Уборка не удаляет то, что в нём числится. Один
// мьютекс охраняет и реестр, и удаление файлов: между проверкой и удалением запись
// не может вклиниться.
type blobRegistry struct {
	mu       sync.Mutex
	inflight map[string]int
}

func newBlobRegistry() *blobRegistry {
	return &blobRegistry{inflight: map[string]int{}}
}

// hold увеличивает счётчики ключей и возвращает идемпотентный release.
func (r *blobRegistry) hold(keys ...string) func() {
	keys = append([]string(nil), keys...)
	r.mu.Lock()
	for _, k := range keys {
		r.inflight[k]++
	}
	r.mu.Unlock()
	var once sync.Once
	return func() {
		once.Do(func() {
			r.mu.Lock()
			defer r.mu.Unlock()
			for _, k := range keys {
				if r.inflight[k] <= 1 {
					delete(r.inflight, k)
					continue
				}
				r.inflight[k]--
			}
		})
	}
}

// blobStore — снимки наборов узлов по хэшу: <dir>/<sha256>.json (0600). Каталог
// создаётся первой записью (0700); конструктор диска не трогает. diagDir —
// каталог диагностики ответов (его тоже убирает gc). reg общий для копий
// значения: blobStore копируется свободно.
type blobStore struct {
	dir     string
	diagDir string
	reg     *blobRegistry
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

// encode возвращает канонический JSON набора и его хэш.
func (b blobStore) encode(list []nodes.Node) (data []byte, hash string, err error) {
	data, err = canonical(list)
	if err != nil {
		return nil, "", err
	}
	sum := sha256.Sum256(data)
	return data, hex.EncodeToString(sum[:]), nil
}

// write записывает снимок; существующий исправный снимок не переписывается.
func (b blobStore) write(hash string, data []byte) error {
	path, err := b.path(hash)
	if err != nil {
		return err
	}
	if cur, err := os.ReadFile(path); err == nil && bytes.Equal(cur, data) {
		return nil
	}
	if err := os.MkdirAll(b.dir, 0o700); err != nil {
		return fmt.Errorf("sources: каталог снимков: %w", err)
	}
	if err := utils.AtomicWriteFile(path, data, 0o600); err != nil {
		return fmt.Errorf("sources: запись снимка: %w", err)
	}
	return nil
}

// put записывает набор узлов и возвращает хэш содержимого. Снимок не удержан:
// между put и записью ссылки в Live его может убрать уборка; для кандидатов
// обновления и предпросмотра — putHeld.
func (b blobStore) put(list []nodes.Node) (string, error) {
	data, hash, err := b.encode(list)
	if err != nil {
		return "", err
	}
	if err := b.write(hash, data); err != nil {
		return "", err
	}
	return hash, nil
}

// putHeld записывает набор узлов и помечает его «в полёте» до записи файла: окна
// «записан, но не удержан» нет. release (идемпотентный) снимает метку; после
// последнего release снимок подчиняется обычным правилам уборки.
func (b blobStore) putHeld(list []nodes.Node) (hash string, release func(), err error) {
	data, hash, err := b.encode(list)
	if err != nil {
		return "", nil, err
	}
	release = b.reg.hold(hash)
	if err := b.write(hash, data); err != nil {
		release()
		return "", nil, err
	}
	return hash, release, nil
}

// hold удерживает уже записанные снимки (счётчик на хэш). Повторные hold одного
// хэша складываются: снимок освобождается после последнего release.
func (b blobStore) hold(hashes ...string) (release func()) {
	return b.reg.hold(hashes...)
}

// holdDiag удерживает диагностику источников, пока их данные не попали в Live.
func (b blobStore) holdDiag(ids ...string) (release func()) {
	keys := make([]string, len(ids))
	for i, id := range ids {
		keys[i] = diagKey(id)
	}
	return b.reg.hold(keys...)
}

// gcKeep — что уборка обязана сохранить.
type gcKeep struct {
	// Snapshots — хэши снимков; Diag — id источников, чья диагностика нужна.
	Snapshots map[string]bool
	Diag      map[string]bool
}

// gc удаляет снимки и файлы диагностики вне keep и вне реестра «в полёте».
// keep вызывается под мьютексом реестра: всё, что попало в Live, в токены
// предпросмотра или в реестр до этого момента, видно уборке, а запись нового
// кандидата ждёт окончания уборки. Вторая возвращаемая величина keep «false» —
// набор собрать нельзя (слой выключен, живые данные не читаются): ничего не
// удаляется. Возвращает число удалённых снимков и файлов диагностики.
func (b blobStore) gc(keep func() (gcKeep, bool)) (snapshots, diags int) {
	b.reg.mu.Lock()
	defer b.reg.mu.Unlock()
	k, ok := keep()
	if !ok {
		return 0, 0
	}
	if entries, err := os.ReadDir(b.dir); err == nil {
		for _, e := range entries {
			hash, isJSON := strings.CutSuffix(e.Name(), ".json")
			if e.IsDir() || !isJSON || !hashRE.MatchString(hash) {
				continue
			}
			if k.Snapshots[hash] || b.reg.inflight[hash] > 0 {
				continue
			}
			if os.Remove(filepath.Join(b.dir, e.Name())) == nil {
				snapshots++
			}
		}
	}
	if b.diagDir == "" {
		return snapshots, 0
	}
	if entries, err := os.ReadDir(b.diagDir); err == nil {
		for _, e := range entries {
			m := diagFileRE.FindStringSubmatch(e.Name())
			if e.IsDir() || m == nil {
				continue
			}
			if k.Diag[m[1]] || b.reg.inflight[diagKey(m[1])] > 0 {
				continue
			}
			if os.Remove(filepath.Join(b.diagDir, e.Name())) == nil {
				diags++
			}
		}
	}
	return snapshots, diags
}

// collectGarbage убирает снимки и диагностику, которые больше никому не нужны.
// Сохраняется: снимки applied и pending из Live, снимки живых токенов предпросмотра
// и всё, что удержано в реестре «в полёте» (кандидаты обновления между записью и
// фиксацией). Диагностика сохраняется для источников из черновика, применённого
// состояния, Live и живых токенов. Слой выключен или Live не читается — уборка
// пропускается. Вызывается в конце Add, Preview, при старте сервиса, после
// каждого обновления источника и после фиксации живых данных («Применить»).
func (s *Service) collectGarbage() {
	s.blobs.gc(func() (gcKeep, bool) {
		st, err := s.d.Layer.ReadState()
		if err != nil {
			return gcKeep{}, false
		}
		live, err := readLive(st.Live)
		if err != nil {
			return gcKeep{}, false
		}
		k := gcKeep{Snapshots: map[string]bool{}, Diag: map[string]bool{}}
		for id, src := range live {
			k.Diag[id] = true
			for _, kl := range src.Kernels {
				if kl.Applied != nil {
					k.Snapshots[kl.Applied.Hash] = true
				}
				if kl.Pending != nil {
					k.Snapshots[kl.Pending.Hash] = true
				}
			}
		}
		for _, secs := range []configlayer.Sections{st.Draft, st.Applied} {
			for name := range secs {
				if id, ok := sourceID(name); ok {
					k.Diag[id] = true
				}
			}
		}
		s.previewKeep(&k)
		return k, true
	})
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
