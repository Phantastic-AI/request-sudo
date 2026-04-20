package execution

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"lease-broker-successor/internal/core"
)

type Executor interface {
	Name() string
	Execute(ctx context.Context, req core.Request) (core.ExecutionResult, error)
}

type LocalExecutor struct{}

func (LocalExecutor) Name() string {
	return "local-exec"
}

func DefaultSanitizedEnv() map[string]string {
	return map[string]string{
		"PATH":   "/usr/sbin:/usr/bin:/sbin:/bin",
		"LANG":   "C",
		"LC_ALL": "C",
	}
}

func BuildExecutionSpec(argv []string, cwd string) core.ExecutionSpec {
	return core.ExecutionSpec{
		Argv: append([]string(nil), argv...),
		Env:  DefaultSanitizedEnv(),
		Cwd:  normalizeCwd(cwd),
	}
}

func (LocalExecutor) Execute(ctx context.Context, req core.Request) (core.ExecutionResult, error) {
	started := time.Now().UTC()
	if len(req.Spec.Argv) == 0 {
		return core.ExecutionResult{StartedAt: started, FinishedAt: time.Now().UTC(), ExitCode: -1, Stderr: "empty argv"}, fmt.Errorf("empty argv")
	}

	cmd := exec.CommandContext(ctx, req.Spec.Argv[0], req.Spec.Argv[1:]...)
	cmd.Dir = normalizeCwd(req.Spec.Cwd)
	cmd.Env = envSlice(req.Spec.Env)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	finished := time.Now().UTC()
	exitCode := 0
	if err != nil {
		exitCode = -1
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		}
	}
	return core.ExecutionResult{
		StartedAt:  started,
		FinishedAt: finished,
		ExitCode:   exitCode,
		Stdout:     stdout.String(),
		Stderr:     stderr.String(),
	}, err
}

func envSlice(env map[string]string) []string {
	keys := make([]string, 0, len(env))
	for key := range env {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	out := make([]string, 0, len(keys))
	for _, key := range keys {
		out = append(out, fmt.Sprintf("%s=%s", key, env[key]))
	}
	return out
}

func normalizeCwd(cwd string) string {
	if cwd == "" {
		return "/"
	}
	if !filepath.IsAbs(cwd) {
		return "/"
	}
	clean := filepath.Clean(cwd)
	if strings.TrimSpace(clean) == "" {
		return "/"
	}
	return clean
}
