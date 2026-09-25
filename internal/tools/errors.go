package tools

import (
	"encoding/json"
	"fmt"
)

func accessDenied(path, message string) json.RawMessage {
	body, err := json.Marshal(map[string]string{
		"error":   "access_denied",
		"path":    path,
		"message": message,
	})
	if err != nil {
		return json.RawMessage(fmt.Sprintf(`{"error":"access_denied","path":%q,"message":%q}`, path, message))
	}
	return body
}
