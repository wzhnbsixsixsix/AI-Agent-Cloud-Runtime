package hook

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/imports/wasi_snapshot_preview1"
)

func (e *Engine) executeWASM(ctx context.Context, h Manifest, req Request) (Response, error) {
	if h.Path == "" {
		return Response{}, fmt.Errorf("hook %s: wasm path is required", h.ID)
	}
	code, err := readWASM(h.Path)
	if err != nil {
		return Response{}, err
	}
	payload, err := json.Marshal(req)
	if err != nil {
		return Response{}, err
	}
	stdout := &limitedBuffer{limit: e.MaxStdoutBytes}
	rt := wazero.NewRuntime(ctx)
	defer rt.Close(ctx)
	if _, err := wasi_snapshot_preview1.Instantiate(ctx, rt); err != nil {
		return Response{}, fmt.Errorf("hook %s: instantiate wasi: %w", h.ID, err)
	}
	cfg := wazero.NewModuleConfig().
		WithName(h.ID).
		WithStdin(bytes.NewReader(payload)).
		WithStdout(stdout).
		WithStderr(io.Discard)
	if _, err := rt.InstantiateWithConfig(ctx, code, cfg); err != nil {
		return Response{}, fmt.Errorf("hook %s: execute wasm: %w", h.ID, err)
	}
	raw := bytes.TrimSpace(stdout.Bytes())
	if len(raw) == 0 {
		return Response{Allowed: true}, nil
	}
	var resp Response
	if err := json.Unmarshal(raw, &resp); err != nil {
		return Response{}, fmt.Errorf("hook %s: parse stdout JSON: %w", h.ID, err)
	}
	return resp, nil
}

func readWASM(path string) ([]byte, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if strings.HasSuffix(path, ".b64") {
		decoded, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(raw)))
		if err != nil {
			return nil, err
		}
		return decoded, nil
	}
	return raw, nil
}

type limitedBuffer struct {
	buf   bytes.Buffer
	limit int
}

func (b *limitedBuffer) Write(p []byte) (int, error) {
	if b.limit > 0 && b.buf.Len()+len(p) > b.limit {
		return 0, fmt.Errorf("hook stdout exceeds %d bytes", b.limit)
	}
	return b.buf.Write(p)
}

func (b *limitedBuffer) Bytes() []byte {
	return b.buf.Bytes()
}
