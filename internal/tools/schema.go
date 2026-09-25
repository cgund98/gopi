package tools

import (
	"encoding/json"

	"github.com/invopop/jsonschema"
)

const maxReadBytes = 32 * 1024

func schemaFor(v any) json.RawMessage {
	reflector := jsonschema.Reflector{DoNotReference: true}
	body, err := json.Marshal(reflector.Reflect(v))
	if err != nil {
		panic(err)
	}
	return body
}
