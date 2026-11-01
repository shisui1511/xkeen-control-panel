package configlayer

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
)

// OrderedObject — JSON-объект с сохранённым порядком ключей верхнего уровня.
// Значения остаются сырыми байтами (json.RawMessage): числа, порядок вложенных
// ключей и их запись не меняются. Обычный map[string]any порядок теряет, а
// порядок значим для YAML Mihomo (nameserver-policy, hosts) и читаем в diff.
type OrderedObject struct {
	keys []string
	vals map[string]json.RawMessage
}

// errNotObject — в разбор пришёл не JSON-объект.
var errNotObject = errors.New("не объект")

// NewOrderedObject возвращает пустой объект.
func NewOrderedObject() *OrderedObject {
	return &OrderedObject{vals: map[string]json.RawMessage{}}
}

// ParseOrderedObject разбирает JSON-объект с сохранением порядка ключей. Не
// объект, висячая запятая или лишние данные после объекта — ошибка. Повтор
// ключа оставляет последнее значение на месте первого вхождения (как
// encoding/json).
func ParseOrderedObject(data []byte) (*OrderedObject, error) {
	dec := json.NewDecoder(bytes.NewReader(data))
	if t, err := dec.Token(); err != nil || t != json.Delim('{') {
		return nil, errNotObject
	}
	o := NewOrderedObject()
	for dec.More() {
		kt, err := dec.Token()
		if err != nil {
			return nil, err
		}
		k, ok := kt.(string)
		if !ok {
			return nil, fmt.Errorf("ключ объекта: ожидалась строка")
		}
		var v json.RawMessage
		if err := dec.Decode(&v); err != nil {
			return nil, err
		}
		if _, dup := o.vals[k]; !dup {
			o.keys = append(o.keys, k)
		}
		o.vals[k] = v
	}
	if t, err := dec.Token(); err != nil || t != json.Delim('}') {
		if err == nil {
			err = errNotObject
		}
		return nil, err
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return nil, errors.New("данные после объекта")
	}
	return o, nil
}

// Keys возвращает ключи в сохранённом порядке (копия).
func (o *OrderedObject) Keys() []string {
	return append([]string(nil), o.keys...)
}

// Get возвращает значение ключа.
func (o *OrderedObject) Get(k string) (json.RawMessage, bool) {
	v, ok := o.vals[k]
	return v, ok
}

// Set задаёт значение: существующий ключ остаётся на своём месте, новый
// дописывается в конец.
func (o *OrderedObject) Set(k string, v json.RawMessage) {
	if _, ok := o.vals[k]; !ok {
		o.keys = append(o.keys, k)
	}
	o.vals[k] = v
}

// Delete удаляет ключ; порядок остальных не меняется.
func (o *OrderedObject) Delete(k string) {
	if _, ok := o.vals[k]; !ok {
		return
	}
	delete(o.vals, k)
	for i, key := range o.keys {
		if key == k {
			o.keys = append(o.keys[:i], o.keys[i+1:]...)
			break
		}
	}
}

// Len возвращает число ключей.
func (o *OrderedObject) Len() int { return len(o.keys) }

// MarshalJSON пишет объект с ключами в сохранённом порядке (компактно).
func (o *OrderedObject) MarshalJSON() ([]byte, error) {
	var buf bytes.Buffer
	buf.WriteByte('{')
	for i, k := range o.keys {
		if i > 0 {
			buf.WriteByte(',')
		}
		kb, err := marshalJSONString(k)
		if err != nil {
			return nil, err
		}
		buf.Write(kb)
		buf.WriteByte(':')
		v := o.vals[k]
		if len(bytes.TrimSpace(v)) == 0 {
			v = json.RawMessage("null")
		}
		if err := json.Compact(&buf, v); err != nil {
			return nil, fmt.Errorf("ключ %q: %w", k, err)
		}
	}
	buf.WriteByte('}')
	return buf.Bytes(), nil
}

// MarshalIndent пишет объект с отступами; порядок ключей сохраняется.
func (o *OrderedObject) MarshalIndent(indent string) ([]byte, error) {
	compact, err := o.MarshalJSON()
	if err != nil {
		return nil, err
	}
	var out bytes.Buffer
	if err := json.Indent(&out, compact, "", indent); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}

// marshalJSONString кодирует строку без HTML-экранирования (<, >, & остаются
// как есть, чтобы значения не менялись при переносе).
func marshalJSONString(s string) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(s); err != nil {
		return nil, err
	}
	return bytes.TrimRight(buf.Bytes(), "\n"), nil
}
