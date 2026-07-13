package mcpserver

import (
	"net/url"
	"strconv"
	"strings"
)

// putPtr sets m[key] = *v when v is non-nil. Used to build PATCH bodies where
// only provided fields should be sent (nil pointer = leave unchanged).
func putPtr[T any](m map[string]any, key string, v *T) {
	if v != nil {
		m[key] = *v
	}
}

// putStr sets m[key] = v only when v is a non-empty string. Avoids sending empty
// enum values (severity/priority/type) that the database would reject.
func putStr(m map[string]any, key, v string) {
	if strings.TrimSpace(v) != "" {
		m[key] = v
	}
}

// putList sets m[key] = v only when the slice is non-empty.
func putList(m map[string]any, key string, v []string) {
	if len(v) > 0 {
		m[key] = v
	}
}

// query builds a url.Values from string/int pairs, skipping empty/zero values.
func query() url.Values { return url.Values{} }

func setStr(q url.Values, key, v string) {
	if strings.TrimSpace(v) != "" {
		q.Set(key, v)
	}
}

func setInt(q url.Values, key string, v int) {
	if v > 0 {
		q.Set(key, strconv.Itoa(v))
	}
}
