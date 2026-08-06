package libraryrelease

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func TestWritePublicKeyPublishesCanonicalPEMWithoutReplacing(t *testing.T) {
	privateKey := deterministicPrivateKey()
	privateKeyFile := filepath.Join(t.TempDir(), "release.key")
	if err := os.WriteFile(
		privateKeyFile,
		[]byte(base64.StdEncoding.EncodeToString(privateKey.Seed())),
		0o600,
	); err != nil {
		t.Fatal(err)
	}
	parent := t.TempDir()
	output := filepath.Join(parent, "release-public.pem")
	written, err := WritePublicKey(t.Context(), privateKeyFile, output)
	if err != nil {
		t.Fatal(err)
	}
	wantOutput, err := filepath.Abs(output)
	if err != nil {
		t.Fatal(err)
	}
	if written != wantOutput {
		t.Fatalf("WritePublicKey() output = %q, want %q", written, wantOutput)
	}
	content, err := os.ReadFile(output)
	if err != nil {
		t.Fatal(err)
	}
	want := canonicalPublicPEM(t, privateKey.Public().(ed25519.PublicKey))
	if !bytes.Equal(content, want) {
		t.Fatalf("public key = %q, want canonical PKIX PEM %q", content, want)
	}
	if _, err := parsePublicKey(content); err != nil {
		t.Fatalf("parse generated public key: %v", err)
	}
	if _, err := WritePublicKey(t.Context(), privateKeyFile, output); err == nil ||
		!strings.Contains(err.Error(), "already exists") {
		t.Fatalf("WritePublicKey(existing output) error = %v", err)
	}
	after, err := os.ReadFile(output)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(after, want) {
		t.Fatal("existing public key was replaced")
	}
	assertNoPublicKeyStaging(t, parent)
}

func TestWritePublicKeyRejectsUnsafeInputsAndOutputs(t *testing.T) {
	privateKey := deterministicPrivateKey()
	privateKeyFile := filepath.Join(t.TempDir(), "release.key")
	if err := os.WriteFile(
		privateKeyFile,
		[]byte(base64.StdEncoding.EncodeToString(privateKey.Seed())),
		0o600,
	); err != nil {
		t.Fatal(err)
	}
	if _, err := WritePublicKey(nil, privateKeyFile, filepath.Join(t.TempDir(), "nil.pem")); err == nil { //nolint:staticcheck // Intentional fail-closed boundary case.
		t.Fatal("WritePublicKey(nil context) error = nil")
	}
	canceled, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := WritePublicKey(canceled, privateKeyFile, filepath.Join(t.TempDir(), "canceled.pem")); !errors.Is(err, context.Canceled) {
		t.Fatalf("WritePublicKey(canceled) error = %v", err)
	}
	if _, err := WritePublicKey(t.Context(), "", filepath.Join(t.TempDir(), "missing.pem")); err == nil {
		t.Fatal("WritePublicKey(empty private key) error = nil")
	}
	if _, err := WritePublicKey(t.Context(), privateKeyFile, ""); err == nil {
		t.Fatal("WritePublicKey(empty output) error = nil")
	}
	missingParent := filepath.Join(t.TempDir(), "missing", "public.pem")
	if _, err := WritePublicKey(t.Context(), privateKeyFile, missingParent); err == nil {
		t.Fatal("WritePublicKey(missing output parent) error = nil")
	}

	existing := filepath.Join(t.TempDir(), "existing.pem")
	if err := os.WriteFile(existing, []byte("do not replace"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := WritePublicKey(t.Context(), privateKeyFile, existing); err == nil {
		t.Fatal("WritePublicKey(existing file) error = nil")
	}
	if content, err := os.ReadFile(existing); err != nil || string(content) != "do not replace" {
		t.Fatalf("existing output = %q, %v", content, err)
	}

	symlinkParent := t.TempDir()
	target := filepath.Join(symlinkParent, "target")
	link := filepath.Join(symlinkParent, "public.pem")
	if err := os.WriteFile(target, []byte("target"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, link); err == nil {
		if _, writeErr := WritePublicKey(t.Context(), privateKeyFile, link); writeErr == nil ||
			!strings.Contains(writeErr.Error(), "existing symlink") {
			t.Fatalf("WritePublicKey(symlink output) error = %v", writeErr)
		}
		if content, readErr := os.ReadFile(target); readErr != nil || string(content) != "target" {
			t.Fatalf("symlink target = %q, %v", content, readErr)
		}
	}

	realParent := t.TempDir()
	parentLink := filepath.Join(t.TempDir(), "redirect")
	if err := os.Symlink(realParent, parentLink); err == nil {
		redirected := filepath.Join(parentLink, "public.pem")
		if _, writeErr := WritePublicKey(t.Context(), privateKeyFile, redirected); writeErr == nil ||
			!strings.Contains(writeErr.Error(), "not a real directory") {
			t.Fatalf("WritePublicKey(symlink parent) error = %v", writeErr)
		}
		if _, statErr := os.Lstat(filepath.Join(realParent, "public.pem")); !os.IsNotExist(statErr) {
			t.Fatalf("symlinked parent was mutated: %v", statErr)
		}
	}
}

func TestWritePublicKeyConcurrentPublicationHasOneWinner(t *testing.T) {
	privateKey := deterministicPrivateKey()
	privateKeyFile := filepath.Join(t.TempDir(), "release.key")
	if err := os.WriteFile(
		privateKeyFile,
		[]byte(base64.StdEncoding.EncodeToString(privateKey)),
		0o600,
	); err != nil {
		t.Fatal(err)
	}
	parent := t.TempDir()
	output := filepath.Join(parent, "public.pem")
	start := make(chan struct{})
	errorsByCall := make([]error, 2)
	var wait sync.WaitGroup
	for index := range errorsByCall {
		wait.Add(1)
		go func() {
			defer wait.Done()
			<-start
			_, errorsByCall[index] = WritePublicKey(t.Context(), privateKeyFile, output)
		}()
	}
	close(start)
	wait.Wait()
	successes := 0
	failures := 0
	for _, err := range errorsByCall {
		if err == nil {
			successes++
		} else if strings.Contains(err.Error(), "already exists") {
			failures++
		} else {
			t.Fatalf("WritePublicKey() race error = %v", err)
		}
	}
	if successes != 1 || failures != 1 {
		t.Fatalf("concurrent results = %v", errorsByCall)
	}
	content, err := os.ReadFile(output)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := parsePublicKey(content); err != nil {
		t.Fatalf("parse concurrently published key: %v", err)
	}
	assertNoPublicKeyStaging(t, parent)
}

func assertNoPublicKeyStaging(t *testing.T, directory string) {
	t.Helper()
	entries, err := os.ReadDir(directory)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".spice-public-key-") {
			t.Fatalf("temporary public-key file remains: %q", entry.Name())
		}
	}
}
