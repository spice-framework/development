package verify

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/spice-framework/development/internal/catalog"
	"github.com/spice-framework/development/internal/process"
)

const maximumCompatibilityMetadata = 64 << 10

type moduleFile struct {
	Require []moduleRequirement `json:"Require"`
}

type moduleRequirement struct {
	Path     string `json:"Path"`
	Version  string `json:"Version"`
	Indirect bool   `json:"Indirect"`
}

func verifyStarterCompatibility(
	ctx context.Context,
	directory string,
	policy catalog.StarterCompatibilityPolicy,
	runner process.Runner,
) (string, bool, error) {
	content, err := readCompatibilityMetadata(directory, policy.MetadataFile)
	if err != nil {
		return "", false, err
	}
	metadata, err := catalog.ParseStarterCompatibility(content, policy)
	if err != nil {
		return "", false, err
	}
	output, err := runner.Run(ctx, directory, "go", "mod", "edit", "-json")
	if err != nil {
		return output, true, fmt.Errorf("inspect go.mod: %w", err)
	}
	if err := requireDirectCoreVersion(output, policy.CoreModule, metadata.Minimum); err != nil {
		return "", true, err
	}
	return "", true, nil
}

func readCompatibilityMetadata(directory string, name string) (_ []byte, resultErr error) {
	root, err := os.OpenRoot(directory)
	if err != nil {
		return nil, fmt.Errorf("open starter checkout: %w", err)
	}
	defer func() { resultErr = errors.Join(resultErr, root.Close()) }()
	file, err := root.Open(name)
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", name, err)
	}
	defer func() { resultErr = errors.Join(resultErr, file.Close()) }()
	info, err := file.Stat()
	if err != nil {
		return nil, fmt.Errorf("inspect %s: %w", name, err)
	}
	if !info.Mode().IsRegular() || info.Size() < 0 || info.Size() > maximumCompatibilityMetadata {
		return nil, fmt.Errorf(
			"%s is not a regular file bounded to %d bytes",
			name,
			maximumCompatibilityMetadata,
		)
	}
	content, err := io.ReadAll(io.LimitReader(file, maximumCompatibilityMetadata+1))
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", name, err)
	}
	if len(content) > maximumCompatibilityMetadata {
		return nil, fmt.Errorf(
			"%s exceeds %d bytes",
			name,
			maximumCompatibilityMetadata,
		)
	}
	return content, nil
}

func requireDirectCoreVersion(content string, module string, minimum string) error {
	decoder := json.NewDecoder(strings.NewReader(content))
	var file moduleFile
	if err := decoder.Decode(&file); err != nil {
		return fmt.Errorf("decode go.mod metadata: %w", err)
	}
	var trailing json.RawMessage
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err == nil {
			return errors.New("go.mod metadata has trailing JSON values")
		}
		return fmt.Errorf("decode trailing go.mod metadata: %w", err)
	}
	matching := make([]moduleRequirement, 0, 1)
	for _, requirement := range file.Require {
		if requirement.Path == module {
			matching = append(matching, requirement)
		}
	}
	if len(matching) == 0 {
		return fmt.Errorf("go.mod must directly require core module %q", module)
	}
	if len(matching) != 1 {
		return fmt.Errorf("go.mod has %d requirements for core module %q", len(matching), module)
	}
	requirement := matching[0]
	if requirement.Indirect {
		return fmt.Errorf("go.mod requirement for core module %q must be direct", module)
	}
	if requirement.Version != minimum {
		return fmt.Errorf(
			"starter compatibility minimum %q does not match direct go.mod requirement %q",
			minimum,
			requirement.Version,
		)
	}
	return nil
}
