// Package process runs bounded, shell-free development commands.
package process

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

const maximumOutput = 2 << 20

type Runner interface {
	Run(context.Context, string, ...string) (string, error)
}

type ExecRunner struct{}

func (ExecRunner) Run(
	ctx context.Context,
	directory string,
	arguments ...string,
) (string, error) {
	if ctx == nil {
		return "", errors.New("process context must not be nil")
	}
	if len(arguments) == 0 || !allowedExecutable(arguments[0]) {
		return "", errors.New("development command executable is not allowed")
	}
	var output boundedBuffer
	// #nosec G204 -- allowedExecutable restricts the binary and CommandContext receives discrete arguments without a shell.
	command := exec.CommandContext(ctx, arguments[0], arguments[1:]...)
	command.Dir = directory
	command.Env = independentEnvironment()
	command.Stdout = &output
	command.Stderr = &output
	err := command.Run()
	text := output.String()
	if err != nil {
		return text, fmt.Errorf(
			"run %s: %w%s",
			strings.Join(arguments, " "),
			err,
			commandDetail(text),
		)
	}
	if output.truncated {
		return text, fmt.Errorf(
			"run %s: output exceeded %d bytes",
			strings.Join(arguments, " "),
			maximumOutput,
		)
	}
	return text, nil
}

type boundedBuffer struct {
	content   bytes.Buffer
	truncated bool
}

func (buffer *boundedBuffer) Write(content []byte) (int, error) {
	written := len(content)
	remaining := maximumOutput - buffer.content.Len()
	if remaining <= 0 {
		buffer.truncated = true
		return written, nil
	}
	if len(content) > remaining {
		content = content[:remaining]
		buffer.truncated = true
	}
	_, _ = buffer.content.Write(content)
	return written, nil
}

func (buffer *boundedBuffer) String() string {
	return strings.TrimSpace(buffer.content.String())
}

func allowedExecutable(name string) bool {
	return name == "git" || name == "go" || name == "gofmt"
}

func commandDetail(output string) string {
	if output == "" {
		return ""
	}
	return ":\n" + output
}

func independentEnvironment() []string {
	environment := os.Environ()
	result := make([]string, 0, len(environment)+1)
	for _, entry := range environment {
		name, _, _ := strings.Cut(entry, "=")
		if strings.EqualFold(name, "GOWORK") {
			continue
		}
		result = append(result, entry)
	}
	return append(result, "GOWORK=off")
}
