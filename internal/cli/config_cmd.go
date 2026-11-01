package cli

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"doriyamedia/internal/config"
)

// configFromJSON decodes a JSON object into a typed Config, reporting nil when
// the bytes do not fit (which is how a wrong value type is caught).
func configFromJSON(raw []byte) *config.Config {
	cfg := config.Defaults()
	if err := json.Unmarshal(raw, cfg); err != nil {
		return nil
	}
	return cfg
}

// runConfig reads and edits config.json. `set` takes a dotted path so a single
// value can be changed without hand-editing JSON and without rewriting the file
// from a stale copy: it reloads, mutates, validates and saves.
func runConfig(a *App, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("用法: doriyamedia config <show|get|set|path> ...")
	}
	sub := args[0]
	rest := args[1:]

	switch sub {
	case "path":
		fmt.Fprintln(a.Out, a.ConfigPath)
		return nil

	case "show":
		cfg, err := a.loadConfig()
		if err != nil {
			return err
		}
		if a.JSON {
			return a.emit(cfg, nil)
		}
		raw, err := json.MarshalIndent(cfg, "", "  ")
		if err != nil {
			return err
		}
		// The secret and passwords are redacted in the human view only; --json
		// keeps the file's real content so a script can round-trip it.
		fmt.Fprintln(a.Out, redactSecrets(raw))
		return nil

	case "get":
		if len(rest) != 1 {
			return fmt.Errorf("用法: doriyamedia config get <key.path>")
		}
		cfg, err := a.loadConfig()
		if err != nil {
			return err
		}
		raw, err := json.Marshal(cfg)
		if err != nil {
			return err
		}
		var doc any
		if err := json.Unmarshal(raw, &doc); err != nil {
			return err
		}
		val, ok := lookupPath(doc, rest[0])
		if !ok {
			return fmt.Errorf("没有配置项 %q", rest[0])
		}
		if a.JSON {
			return a.emit(map[string]any{"key": rest[0], "value": val}, nil)
		}
		switch v := val.(type) {
		case string:
			fmt.Fprintln(a.Out, v)
		case float64:
			fmt.Fprintln(a.Out, strconv.FormatFloat(v, 'f', -1, 64))
		default:
			raw, _ := json.Marshal(v)
			fmt.Fprintln(a.Out, string(raw))
		}
		return nil

	case "set":
		if len(rest) != 2 {
			return fmt.Errorf("用法: doriyamedia config set <key.path> <value>")
		}
		cfg, err := a.loadConfig()
		if err != nil {
			return err
		}
		raw, err := json.Marshal(cfg)
		if err != nil {
			return err
		}
		var doc map[string]any
		if err := json.Unmarshal(raw, &doc); err != nil {
			return err
		}
		if err := setPath(doc, rest[0], rest[1]); err != nil {
			return err
		}
		// Round-trip through the typed config so a bad value (a port of "abc")
		// is refused here rather than at the server's next start.
		updated, err := json.Marshal(doc)
		if err != nil {
			return err
		}
		next := configFromJSON(updated)
		if next == nil {
			return fmt.Errorf("配置项 %q 的值类型不正确", rest[0])
		}
		if err := next.Save(a.ConfigPath); err != nil {
			return fmt.Errorf("写入 %s 失败: %w", a.ConfigPath, err)
		}
		if !a.Quiet {
			fmt.Fprintf(a.Out, "已设置 %s = %s\n", rest[0], rest[1])
			fmt.Fprintln(a.Out, "注意：正在运行的服务需要重启（或在该服务的设置页保存一次）后生效。")
		}
		return nil

	default:
		return fmt.Errorf("未知子命令: config %s", sub)
	}
}

// redactSecrets blanks the values that must not be printed to a terminal that
// may end up in a log or a screenshot.
func redactSecrets(raw []byte) string {
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		return string(raw)
	}
	redactKeys(doc)
	out, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return string(raw)
	}
	return string(out)
}

func redactKeys(doc map[string]any) {
	for k, v := range doc {
		switch val := v.(type) {
		case map[string]any:
			redactKeys(val)
		case string:
			switch k {
			case "secret", "password", "api_key", "token", "secret_key", "webdav_pass":
				if val != "" {
					doc[k] = "******"
				}
			}
		}
	}
}

// lookupPath walks a dotted path through decoded JSON ("playback.probe_size_mb").
func lookupPath(doc any, path string) (any, bool) {
	cur := doc
	for _, part := range strings.Split(path, ".") {
		obj, ok := cur.(map[string]any)
		if !ok {
			return nil, false
		}
		cur, ok = obj[part]
		if !ok {
			return nil, false
		}
	}
	return cur, true
}

// setPath writes a value at a dotted path, converting the text to the type the
// existing value has. A path that does not exist is an error: silently adding a
// misspelled key would leave the real setting unchanged.
func setPath(doc map[string]any, path, text string) error {
	parts := strings.Split(path, ".")
	cur := doc
	for i, part := range parts {
		if i == len(parts)-1 {
			old, ok := cur[part]
			if !ok {
				return fmt.Errorf("没有配置项 %q", path)
			}
			val, err := coerce(old, text)
			if err != nil {
				return fmt.Errorf("%s: %w", path, err)
			}
			cur[part] = val
			return nil
		}
		next, ok := cur[part].(map[string]any)
		if !ok {
			return fmt.Errorf("没有配置项 %q", path)
		}
		cur = next
	}
	return nil
}

// coerce converts text to the JSON type of the existing value.
func coerce(old any, text string) (any, error) {
	switch old.(type) {
	case bool:
		switch strings.ToLower(strings.TrimSpace(text)) {
		case "1", "true", "yes", "on":
			return true, nil
		case "0", "false", "no", "off":
			return false, nil
		}
		return nil, fmt.Errorf("需要 true 或 false")
	case float64:
		f, err := strconv.ParseFloat(strings.TrimSpace(text), 64)
		if err != nil {
			return nil, fmt.Errorf("需要数字")
		}
		return f, nil
	case []any:
		// A list is set from a comma-separated string or from JSON.
		text = strings.TrimSpace(text)
		if strings.HasPrefix(text, "[") {
			var arr []any
			if err := json.Unmarshal([]byte(text), &arr); err != nil {
				return nil, fmt.Errorf("JSON 数组格式错误")
			}
			return arr, nil
		}
		if text == "" {
			return []any{}, nil
		}
		var arr []any
		for _, item := range strings.Split(text, ",") {
			if item = strings.TrimSpace(item); item != "" {
				arr = append(arr, item)
			}
		}
		return arr, nil
	default:
		return text, nil
	}
}
