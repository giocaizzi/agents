package harness

// deepMerge merges src into dst: maps merge key by key, anything else in src
// replaces the value in dst.
func deepMerge(dst, src map[string]any) {
	for k, v := range src {
		sm, sok := v.(map[string]any)
		dm, dok := dst[k].(map[string]any)
		if sok && dok {
			deepMerge(dm, sm)
			continue
		}
		dst[k] = v
	}
}

// setIf sets m[key] to v unless v is empty.
func setIf(m map[string]any, key, v string) {
	if v != "" {
		m[key] = v
	}
}

func toAnySlice(in []string) []any {
	out := make([]any, len(in))
	for i, s := range in {
		out[i] = s
	}
	return out
}

func anyMap(in map[string]string) map[string]any {
	out := make(map[string]any, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}
