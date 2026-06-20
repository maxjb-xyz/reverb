package download

import (
	"encoding/json"

	"github.com/maximusjb/crate/internal/core"
)

func jsonUnmarshal(s string, v any) error { return json.Unmarshal([]byte(s), v) }

func requestJSON(req core.DownloadRequest) string { b, _ := json.Marshal(req); return string(b) }
