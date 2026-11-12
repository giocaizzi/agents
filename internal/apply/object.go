// Package apply writes managed keys into harness-native config files. It
// knows nothing about harnesses or configuration: callers describe each file
// as a Target and the Engine merges, compares and writes it.
package apply

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"reflect"
	"sort"
	"strings"
	"time"
)

// Object is a JSON object that keeps its key order.
type Object struct {
	Keys   []string
	Values map[string]any
}

// NewObject returns an empty Object.
func NewObject() *Object {
	return &Object{Values: map[string]any{}}
}

// Get returns the value stored under key.
func (o *Object) Get(key string) (any, bool) {
	v, ok := o.Values[key]
	return v, ok
}

// Set stores value under key; a new key is appended, an existing one keeps its position.
func (o *Object) Set(key string, value any) {
	if o.Values == nil {
		o.Values = map[string]any{}
	}
	if _, ok := o.Values[key]; !ok {
		o.Keys = append(o.Keys, key)
	}
	o.Values[key] = value
}

// Delete removes key and reports whether it was present.
func (o *Object) Delete(key string) bool {
	if _, ok := o.Values[key]; !ok {
		return false
	}
	delete(o.Values, key)
	for i, k := range o.Keys {
		if k == key {
			o.Keys = append(o.Keys[:i], o.Keys[i+1:]...)
			break
		}
	}
	return true
}

// decodeJSON decodes a JSON document into Objects, []any and scalars (numbers as json.Number).
func decodeJSON(data []byte) (any, error) {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	v, err := decodeValue(dec)
	if err != nil {
		return nil, err
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return nil, errors.New("unexpected data after the top-level value")
	}
	return v, nil
}

func decodeValue(dec *json.Decoder) (any, error) {
	tok, err := dec.Token()
	if err != nil {
		return nil, fmt.Errorf("decode json: %w", err)
	}
	delim, ok := tok.(json.Delim)
	if !ok {
		return tok, nil
	}
	var v any
	switch delim {
	case '{':
		v, err = decodeObject(dec)
	case '[':
		v, err = decodeArray(dec)
	default:
		return nil, fmt.Errorf("decode json: unexpected delimiter %q", delim)
	}
	if err != nil {
		return nil, err
	}
	if _, err := dec.Token(); err != nil { // the closing delimiter
		return nil, fmt.Errorf("decode json: %w", err)
	}
	return v, nil
}

func decodeObject(dec *json.Decoder) (*Object, error) {
	obj := NewObject()
	for dec.More() {
		kt, err := dec.Token()
		if err != nil {
			return nil, fmt.Errorf("decode json: %w", err)
		}
		key, _ := kt.(string)
		v, err := decodeValue(dec)
		if err != nil {
			return nil, err
		}
		obj.Set(key, v)
	}
	return obj, nil
}

func decodeArray(dec *json.Decoder) ([]any, error) {
	arr := []any{}
	for dec.More() {
		v, err := decodeValue(dec)
		if err != nil {
			return nil, err
		}
		arr = append(arr, v)
	}
	return arr, nil
}

// normalize converts any supported Go value into the canonical document model:
// *Object, []any, string, bool, nil, json.Number and time values.
func normalize(v any) (any, error) {
	switch t := v.(type) {
	case nil, string, bool, json.Number, time.Time:
		return t, nil
	case *Object:
		if t == nil {
			return NewObject(), nil
		}
		return normalizeObject(t.Keys, t.Values)
	case Object:
		return normalizeObject(t.Keys, t.Values)
	case map[string]any:
		keys := make([]string, 0, len(t))
		for k := range t {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		return normalizeObject(keys, t)
	case []any:
		out := make([]any, len(t))
		for i, e := range t {
			n, err := normalize(e)
			if err != nil {
				return nil, err
			}
			out[i] = n
		}
		return out, nil
	default:
		if isTOMLTime(v) {
			return v, nil
		}
		raw, err := marshalPlain(v)
		if err != nil {
			return nil, fmt.Errorf("normalize %T: %w", v, err)
		}
		return decodeJSON(raw)
	}
}

func normalizeObject(keys []string, values map[string]any) (*Object, error) {
	out := NewObject()
	for _, k := range keys {
		n, err := normalize(values[k])
		if err != nil {
			return nil, err
		}
		out.Set(k, n)
	}
	return out, nil
}

func isTOMLTime(v any) bool {
	name := reflect.TypeOf(v).String()
	return strings.HasPrefix(name, "toml.Local")
}

func clone(v any) any {
	switch t := v.(type) {
	case *Object:
		out := NewObject()
		for _, k := range t.Keys {
			out.Set(k, clone(t.Values[k]))
		}
		return out
	case []any:
		out := make([]any, len(t))
		for i, e := range t {
			out[i] = clone(e)
		}
		return out
	default:
		return v
	}
}

// equal compares two normalized documents semantically: object key order is ignored and numbers compare by value.
func equal(a, b any) bool {
	switch x := a.(type) {
	case *Object:
		y, ok := b.(*Object)
		if !ok || len(x.Keys) != len(y.Keys) {
			return false
		}
		for _, k := range x.Keys {
			yv, ok := y.Values[k]
			if !ok || !equal(x.Values[k], yv) {
				return false
			}
		}
		return true
	case []any:
		y, ok := b.([]any)
		if !ok || len(x) != len(y) {
			return false
		}
		for i := range x {
			if !equal(x[i], y[i]) {
				return false
			}
		}
		return true
	case json.Number:
		y, ok := b.(json.Number)
		return ok && numEqual(x, y)
	default:
		return reflect.DeepEqual(a, b)
	}
}

func numEqual(a, b json.Number) bool {
	if a == b {
		return true
	}
	ra, ok1 := new(big.Rat).SetString(string(a))
	rb, ok2 := new(big.Rat).SetString(string(b))
	return ok1 && ok2 && ra.Cmp(rb) == 0
}

// canon renders v as compact JSON with sorted keys and normalized numbers, so equal values give equal text.
func canon(v any) string {
	var sb strings.Builder
	writeCanon(&sb, v)
	return sb.String()
}

func writeCanon(sb *strings.Builder, v any) {
	switch t := v.(type) {
	case *Object:
		keys := append([]string(nil), t.Keys...)
		sort.Strings(keys)
		sb.WriteByte('{')
		for i, k := range keys {
			if i > 0 {
				sb.WriteByte(',')
			}
			sb.WriteString(plainString(k))
			sb.WriteByte(':')
			writeCanon(sb, t.Values[k])
		}
		sb.WriteByte('}')
	case []any:
		sb.WriteByte('[')
		for i, e := range t {
			if i > 0 {
				sb.WriteByte(',')
			}
			writeCanon(sb, e)
		}
		sb.WriteByte(']')
	case json.Number:
		if r, ok := new(big.Rat).SetString(string(t)); ok {
			sb.WriteString(r.RatString())
		} else {
			sb.WriteString(string(t))
		}
	default:
		sb.WriteString(plainString(v))
	}
}

func marshalPlain(v any) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return nil, fmt.Errorf("marshal json: %w", err)
	}
	return bytes.TrimRight(buf.Bytes(), "\n"), nil
}

func plainString(v any) string {
	raw, err := marshalPlain(v)
	if err != nil {
		return "null"
	}
	return string(raw)
}

// encodeJSON renders a normalized document with two-space indent and a trailing newline.
func encodeJSON(v any) ([]byte, error) {
	var buf bytes.Buffer
	if err := writeJSON(&buf, v, 0); err != nil {
		return nil, err
	}
	buf.WriteByte('\n')
	return buf.Bytes(), nil
}

func writeJSON(buf *bytes.Buffer, v any, depth int) error {
	switch t := v.(type) {
	case *Object:
		return writeObject(buf, t, depth)
	case []any:
		return writeArray(buf, t, depth)
	default:
		raw, err := marshalPlain(v)
		if err != nil {
			return err
		}
		buf.Write(raw)
		return nil
	}
}

func writeObject(buf *bytes.Buffer, o *Object, depth int) error {
	if len(o.Keys) == 0 {
		buf.WriteString("{}")
		return nil
	}
	pad := strings.Repeat("  ", depth+1)
	buf.WriteString("{\n")
	for i, k := range o.Keys {
		buf.WriteString(pad + plainString(k) + ": ")
		if err := writeJSON(buf, o.Values[k], depth+1); err != nil {
			return err
		}
		writeSeparator(buf, i, len(o.Keys))
	}
	buf.WriteString(strings.Repeat("  ", depth) + "}")
	return nil
}

func writeArray(buf *bytes.Buffer, a []any, depth int) error {
	if len(a) == 0 {
		buf.WriteString("[]")
		return nil
	}
	pad := strings.Repeat("  ", depth+1)
	buf.WriteString("[\n")
	for i, e := range a {
		buf.WriteString(pad)
		if err := writeJSON(buf, e, depth+1); err != nil {
			return err
		}
		writeSeparator(buf, i, len(a))
	}
	buf.WriteString(strings.Repeat("  ", depth) + "]")
	return nil
}

func writeSeparator(buf *bytes.Buffer, i, n int) {
	if i < n-1 {
		buf.WriteByte(',')
	}
	buf.WriteByte('\n')
}
