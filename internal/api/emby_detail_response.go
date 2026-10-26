package api

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"reflect"
	"sort"
	"strconv"
)

// A 200 status is not evidence of a valid JSON response. Marshal before sending
// headers so invalid floats or unsupported values cannot become empty 200s.
func embyWriteDetail(w http.ResponseWriter, r *http.Request, item map[string]any) {
	raw, err := json.Marshal(item)
	if err != nil {
		log.Printf("Emby detail encode_error=true error_type=%T", err)
		fail(w, http.StatusInternalServerError, "Cannot encode item details")
		return
	}
	// Log schema and safe protocol values, not titles, plot, image tags, URLs,
	// headers or session credentials embedded in MediaSources.
	schema := embyDetailShape(item, 0)
	report := map[string]any{"schema": schema}
	for _, key := range []string{"Type", "MediaType", "IsFolder", "PlayAccess", "MediaSourceCount", "RunTimeTicks", "ProductionYear", "VideoType"} {
		if value, ok := item[key]; ok {
			report[key] = value
		}
	}
	if id, ok := item["Id"].(string); ok {
		if _, err := strconv.ParseInt(id, 10, 64); err == nil {
			report["item_id"] = id
		}
	}
	info, _ := json.Marshal(report)
	log.Printf("Emby detail response_bytes=%d dto=%s", len(raw), info)
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(append(raw, '\n'))
}

// Include each object key and scalar JSON type, and an array's total length
// plus up to two element schemas. This keeps large metadata bounded.
func embyDetailShape(value any, depth int) any {
	if value == nil {
		return "null"
	}
	if depth > 7 {
		return "nested"
	}
	v := reflect.ValueOf(value)
	switch v.Kind() {
	case reflect.Map:
		out := map[string]any{}
		keys := v.MapKeys()
		sort.Slice(keys, func(i, j int) bool { return fmt.Sprint(keys[i].Interface()) < fmt.Sprint(keys[j].Interface()) })
		for i, key := range keys {
			if i >= 80 {
				out["_truncated"] = len(keys) - i
				break
			}
			out[fmt.Sprint(key.Interface())] = embyDetailShape(v.MapIndex(key).Interface(), depth+1)
		}
		return out
	case reflect.Slice, reflect.Array:
		samples := []any{}
		for i := 0; i < v.Len() && i < 2; i++ {
			samples = append(samples, embyDetailShape(v.Index(i).Interface(), depth+1))
		}
		return map[string]any{"length": v.Len(), "elements": samples}
	case reflect.String:
		return "string"
	case reflect.Bool:
		return "boolean"
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64, reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Float32, reflect.Float64:
		return "number"
	default:
		return "unsupported"
	}
}
