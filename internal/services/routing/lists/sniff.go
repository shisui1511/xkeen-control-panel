package lists

import (
	"bytes"
	"errors"
	"io"

	"google.golang.org/protobuf/encoding/protowire"
)

// Виды базы, которые возвращает SniffDat.
const (
	// SniffGeoSite — база geosite (домены).
	SniffGeoSite = "geosite"
	// SniffGeoIP — база geoip (сети).
	SniffGeoIP = "geoip"
)

// ErrNotDat — префикс не похож на начало dat-файла (GeoSiteList или GeoIPList).
var ErrNotDat = errors.New("префикс не похож на dat-файл")

// SniffDat определяет по первым байтам файла, что это dat, и вид базы. Префикс
// может обрываться на любом байте: обрыв внутри записи не ошибка, если до него
// удалось прочесть хотя бы один целый вложенный элемент. Ни одного целого
// элемента, повреждённая структура или тег записи не из печатных символов —
// ErrNotDat.
func SniffDat(prefix []byte) (kind string, err error) {
	found := ""
	_ = walkEntries(bytes.NewReader(prefix), func(code string, e *entryBody) error {
		if !sniffPlainCode(code) {
			return ErrNotDat
		}
		for {
			num, typ, terr := e.tag()
			if terr != nil {
				if errors.Is(terr, io.EOF) {
					return nil
				}
				return terr
			}
			if num == 2 && typ == protowire.BytesType {
				elem, berr := e.bytes(maxElemBytes)
				if berr != nil {
					return berr
				}
				k := kindOfElem(elem)
				if k == "" {
					return ErrNotDat
				}
				if _, ok := formatElem(k, elem); !ok {
					return ErrNotDat
				}
				found = k
				return errStopWalk
			}
			if serr := e.skip(typ); serr != nil {
				return serr
			}
		}
	})
	switch found {
	case KindSite:
		return SniffGeoSite, nil
	case KindIP:
		return SniffGeoIP, nil
	}
	return "", ErrNotDat
}

// sniffPlainCode — тег записи dat: непустая строка печатных ASCII-символов.
func sniffPlainCode(code string) bool {
	if code == "" {
		return false
	}
	for i := 0; i < len(code); i++ {
		if code[i] < 0x21 || code[i] > 0x7e {
			return false
		}
	}
	return true
}
