package web

import (
	"bytes"
	"compress/gzip"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"r0rpc/internal/rpc"
)

const payloadEncodingGzipBase64JSON = "gzip+base64+json"

func normalizeClientJobResult(result *rpc.JobResult) error {
	if result == nil {
		return nil
	}
	encoding := strings.TrimSpace(result.PayloadEncoding)
	if encoding == "" {
		return nil
	}
	switch encoding {
	case payloadEncodingGzipBase64JSON:
		decoded, err := decodeCompressedPayload(result.Payload)
		if err != nil {
			return err
		}
		result.Payload = decoded
		result.PayloadEncoding = ""
		return nil
	default:
		return fmt.Errorf("unsupported payloadEncoding: %s", encoding)
	}
}

func decodeCompressedPayload(raw json.RawMessage) (json.RawMessage, error) {
	var encoded string
	if err := json.Unmarshal(raw, &encoded); err != nil {
		return nil, fmt.Errorf("compressed payload must be base64 string: %w", err)
	}
	compressed, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return nil, fmt.Errorf("decode base64 payload: %w", err)
	}
	reader, err := gzip.NewReader(bytes.NewReader(compressed))
	if err != nil {
		return nil, fmt.Errorf("open gzip payload: %w", err)
	}
	defer reader.Close()
	plain, err := io.ReadAll(reader)
	if err != nil {
		return nil, fmt.Errorf("read gzip payload: %w", err)
	}
	trimmed := bytes.TrimSpace(plain)
	if len(trimmed) == 0 {
		return json.RawMessage("{}"), nil
	}
	if !json.Valid(trimmed) {
		return nil, fmt.Errorf("decoded payload is not valid json")
	}
	return json.RawMessage(trimmed), nil
}