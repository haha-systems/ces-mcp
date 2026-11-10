package engine

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

const maxRuntimeOutput = 8 << 20

// CommandRuntime starts one adapter process per dispatch. The subprocess gets
// only the dispatch manifest on stdin and no inherited environment by default.
// Its OS permissions are still controlled by the host.
type CommandRuntime struct {
	Executable string
	Args       []string
	Env        []string
}

func NewCommandRuntime(executable, argsText, envAllowlist string) (*CommandRuntime, error) {
	executable = strings.TrimSpace(executable)
	if executable == "" {
		return nil, nil
	}

	args := []string{}
	if strings.TrimSpace(argsText) != "" {
		args = strings.Fields(argsText)
	}

	allowed := map[string]struct{}{}

	for _, key := range strings.Split(envAllowlist, ",") {
		if key = strings.TrimSpace(key); key != "" {
			allowed[key] = struct{}{}
		}
	}

	env := []string{}

	for _, entry := range os.Environ() {
		key, _, ok := strings.Cut(entry, "=")
		if !ok {
			continue
		}

		if _, ok := allowed[key]; ok {
			env = append(env, entry)
		}
	}

	if _, err := exec.LookPath(executable); err != nil {
		return nil, fmt.Errorf("find runtime executable: %w", err)
	}

	return &CommandRuntime{Executable: executable, Args: args, Env: env}, nil
}

func (r *CommandRuntime) RunPhase(
	ctx context.Context,
	manifest DispatchManifest,
) (json.RawMessage, bool, error) {
	if r == nil {
		return nil, false, errors.New("runtime adapter is not configured")
	}

	input, err := json.Marshal(manifest)
	if err != nil {
		return nil, false, fmt.Errorf("encode dispatch manifest: %w", err)
	}

	command := exec.CommandContext(ctx, r.Executable, r.Args...)
	command.Env = append([]string(nil), r.Env...)
	command.Stdin = bytes.NewReader(input)

	var (
		output      boundedBuffer
		diagnostics boundedBuffer
	)

	output.Limit = maxRuntimeOutput
	diagnostics.Limit = 4096
	command.Stdout = &output

	command.Stderr = &diagnostics
	if err := command.Run(); err != nil {
		return nil, false, fmt.Errorf(
			"runtime command failed: %w (%s)",
			err,
			strings.TrimSpace(diagnostics.String()),
		)
	}

	artifact := bytes.TrimSpace(output.Bytes())
	if len(artifact) == 0 || !json.Valid(artifact) {
		return nil, false, errors.New("runtime command must write one JSON phase result to stdout")
	}

	return append(json.RawMessage(nil), artifact...), false, nil
}

type boundedBuffer struct {
	bytes.Buffer
	Limit int
}

func (b *boundedBuffer) Write(data []byte) (int, error) {
	written := len(data)

	remaining := b.Limit - b.Len()
	if remaining <= 0 {
		return written, nil
	}

	if len(data) > remaining {
		_, _ = b.Buffer.Write(data[:remaining])
		return written, nil
	}

	return b.Buffer.Write(data)
}
