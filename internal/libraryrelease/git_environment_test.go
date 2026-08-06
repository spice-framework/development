package libraryrelease

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spice-framework/development/internal/process"
)

func TestCommittedTreeIgnoresGitReplacementObjects(t *testing.T) {
	repository := t.TempDir()
	runGitTestCommand(t, repository, "init")
	runGitTestCommand(t, repository, "config", "user.name", "Spice Test")
	runGitTestCommand(t, repository, "config", "user.email", "spice@example.invalid")
	filename := filepath.Join(repository, "identity.txt")
	if err := os.WriteFile(filename, []byte("original\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runGitTestCommand(t, repository, "add", "identity.txt")
	runGitTestCommand(t, repository, "commit", "-m", "original")
	original := runGitTestCommand(t, repository, "rev-parse", "HEAD")
	if err := os.WriteFile(filename, []byte("replacement\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runGitTestCommand(t, repository, "commit", "-am", "replacement")
	replacement := runGitTestCommand(t, repository, "rev-parse", "HEAD")
	runGitTestCommand(t, repository, "replace", original, replacement)
	if got := runGitTestCommand(t, repository, "show", original+":identity.txt"); got != "replacement" {
		t.Fatalf("replacement fixture is inactive: git show = %q", got)
	}

	tree, err := readCommittedTree(t.Context(), repository, original)
	if err != nil {
		t.Fatal(err)
	}
	if got := string(tree.files["identity.txt"]); got != "original\n" {
		t.Fatalf("committed identity.txt = %q, want original object", got)
	}
}

func runGitTestCommand(t *testing.T, directory string, arguments ...string) string {
	t.Helper()
	command := exec.Command("git", arguments...)
	command.Dir = directory
	for _, entry := range process.IndependentEnvironment() {
		name, _, _ := strings.Cut(entry, "=")
		if !strings.EqualFold(name, "GIT_NO_REPLACE_OBJECTS") {
			command.Env = append(command.Env, entry)
		}
	}
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v: %s", strings.Join(arguments, " "), err, output)
	}
	return strings.TrimSpace(string(output))
}
