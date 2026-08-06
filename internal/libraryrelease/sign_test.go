package libraryrelease

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/spice-framework/development/internal/catalog"
)

func TestSignCreatesDeterministicAuthenticatedProductionRelease(t *testing.T) {
	repository, plan, value, files, publicKey := productionSigningFixture(t)
	firstOutput := filepath.Join(t.TempDir(), "first")
	secondOutput := filepath.Join(t.TempDir(), "second")
	first, err := Sign(t.Context(), repository, firstOutput, plan, value, files)
	if err != nil {
		t.Fatal(err)
	}
	second, err := Sign(t.Context(), repository, secondOutput, plan, value, files)
	if err != nil {
		t.Fatal(err)
	}
	wantFiles := artifactNames(plan.Repository, plan.Version, true)
	if !slices.Equal(first.Files, wantFiles) || !slices.Equal(second.Files, wantFiles) {
		t.Fatalf("Sign() files = %v and %v, want %v", first.Files, second.Files, wantFiles)
	}
	if !equalStringMap(directoryHashes(t, firstOutput), directoryHashes(t, secondOutput)) {
		t.Fatal("repeated production signing produced different bytes")
	}

	checksums := readSigningTestFile(t, firstOutput, "checksums.txt")
	signature := readSigningTestFile(t, firstOutput, "checksums.txt.sig")
	publicPEM := readSigningTestFile(t, firstOutput, "checksums.txt.pem")
	if len(signature) != ed25519.SignatureSize || !ed25519.Verify(publicKey, checksums, signature) {
		t.Fatal("production checksum signature is invalid")
	}
	trustedPEM, err := os.ReadFile(files.TrustedPublicKey)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(publicPEM, trustedPEM) {
		t.Fatal("emitted public key differs from independent trust anchor")
	}
	archiveName := plan.Repository + "_1.2.3_source.tar.gz"
	sbomName := plan.Repository + "_1.2.3_sbom.spdx.json"
	wantChecksums, err := artifactChecksums(map[string][]byte{
		archiveName: readSigningTestFile(t, firstOutput, archiveName),
		sbomName:    readSigningTestFile(t, firstOutput, sbomName),
	})
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(checksums, wantChecksums) || bytes.Contains(checksums, []byte("checksums.txt.")) {
		t.Fatalf("signed checksums = %q", checksums)
	}
	if _, err := Render(t.Context(), repository, filepath.Join(t.TempDir(), "unsigned"), plan, value); err == nil ||
		!strings.Contains(err.Error(), "requires a rehearsal plan") {
		t.Fatalf("Render(production plan) error = %v", err)
	}
}

func TestSignAcceptsCanonicalPrivateKeyFormats(t *testing.T) {
	for _, format := range []string{"seed", "private", "pkcs8"} {
		t.Run(format, func(t *testing.T) {
			repository, plan, value, files, _ := productionSigningFixture(t)
			privateKey := deterministicPrivateKey()
			var content []byte
			switch format {
			case "seed":
				content = []byte(base64.StdEncoding.EncodeToString(privateKey.Seed()))
			case "private":
				content = []byte(base64.StdEncoding.EncodeToString(privateKey))
			case "pkcs8":
				encoded, err := x509.MarshalPKCS8PrivateKey(privateKey)
				if err != nil {
					t.Fatal(err)
				}
				content = pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: encoded})
			}
			if err := os.WriteFile(files.PrivateKey, content, 0o600); err != nil {
				t.Fatal(err)
			}
			if _, err := Sign(
				t.Context(),
				repository,
				filepath.Join(t.TempDir(), "release"),
				plan,
				value,
				files,
			); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestPrivateAndPublicKeyParsingRejectsNoncanonicalOrInvalidInput(t *testing.T) {
	privateKey := deterministicPrivateKey()
	pkcs8, err := x509.MarshalPKCS8PrivateKey(privateKey)
	if err != nil {
		t.Fatal(err)
	}
	privatePEM := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: pkcs8})
	publicPEM := canonicalPublicPEM(t, privateKey.Public().(ed25519.PublicKey))
	otherPrivate, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	otherPKCS8, err := x509.MarshalPKCS8PrivateKey(otherPrivate)
	if err != nil {
		t.Fatal(err)
	}
	otherPublic, err := x509.MarshalPKIXPublicKey(&otherPrivate.PublicKey)
	if err != nil {
		t.Fatal(err)
	}
	inconsistent := slices.Clone(privateKey)
	inconsistent[len(inconsistent)-1] ^= 1
	for name, content := range map[string][]byte{
		"empty":                nil,
		"invalid base64":       []byte("not a key"),
		"wrong base64 length":  []byte(base64.StdEncoding.EncodeToString([]byte("short"))),
		"base64 whitespace":    append([]byte(base64.StdEncoding.EncodeToString(privateKey.Seed())), '\n'),
		"inconsistent private": []byte(base64.StdEncoding.EncodeToString(inconsistent)),
		"malformed PKCS8":      pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: []byte("bad")}),
		"non Ed25519 PKCS8":    pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: otherPKCS8}),
		"wrong PEM type":       pem.EncodeToMemory(&pem.Block{Type: "ED25519 PRIVATE KEY", Bytes: pkcs8}),
		"trailing PEM data":    append(slices.Clone(privatePEM), '\n'),
	} {
		t.Run("private "+name, func(t *testing.T) {
			if key, parseErr := parsePrivateKey(content); parseErr == nil {
				clear(key)
				t.Fatal("parsePrivateKey() error = nil")
			}
		})
	}
	for name, content := range map[string][]byte{
		"empty":                nil,
		"malformed PKIX":       pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: []byte("bad")}),
		"non Ed25519 PKIX":     pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: otherPublic}),
		"wrong PEM type":       pem.EncodeToMemory(&pem.Block{Type: "ED25519 PUBLIC KEY", Bytes: []byte("bad")}),
		"trailing PEM data":    append(slices.Clone(publicPEM), '\n'),
		"oversized public key": make([]byte, maximumSigningKeyBytes+1),
	} {
		t.Run("public "+name, func(t *testing.T) {
			if _, parseErr := parsePublicKey(content); parseErr == nil {
				t.Fatal("parsePublicKey() error = nil")
			}
		})
	}
}

func TestSigningMaterialFailsClosedWhenIncomplete(t *testing.T) {
	if _, _, err := (signingMaterial{}).sign([]byte("checksums")); err == nil {
		t.Fatal("sign(incomplete material) error = nil")
	}
	if _, err := validatedPrivateKey(make(ed25519.PrivateKey, ed25519.SeedSize)); err == nil {
		t.Fatal("validatedPrivateKey(short key) error = nil")
	}
	var nilMaterial *signingMaterial
	nilMaterial.clear()
	material := signingMaterial{
		privateKey: slices.Clone(deterministicPrivateKey()),
		publicKey:  make(ed25519.PublicKey, ed25519.PublicKeySize),
		publicPEM:  []byte("public"),
	}
	material.clear()
	if material.privateKey != nil || material.publicKey != nil || material.publicPEM != nil {
		t.Fatalf("clear() retained signing material: %#v", material)
	}
}

func TestSignRejectsUntrustedProductionStateAndSigningInputs(t *testing.T) {
	for name, configure := range map[string]func(
		*testing.T,
		string,
		*Plan,
		*SigningFiles,
		*string,
	){
		"rehearsal plan": func(_ *testing.T, _ string, plan *Plan, _ *SigningFiles, _ *string) {
			plan.Mode = "rehearsal"
			plan.Artifacts = artifactNames(plan.Repository, plan.Version, false)
		},
		"dirty tracked": func(t *testing.T, repository string, _ *Plan, _ *SigningFiles, _ *string) {
			if err := os.WriteFile(filepath.Join(repository, "oidc.go"), []byte("package oidc\n// dirty\n"), 0o600); err != nil {
				t.Fatal(err)
			}
		},
		"dirty untracked": func(t *testing.T, repository string, _ *Plan, _ *SigningFiles, _ *string) {
			if err := os.WriteFile(filepath.Join(repository, "secret.txt"), []byte("secret"), 0o600); err != nil {
				t.Fatal(err)
			}
		},
		"missing exact tag": func(t *testing.T, repository string, plan *Plan, _ *SigningFiles, _ *string) {
			provenanceGitCommand(t, repository, "tag", "--delete", plan.Version)
		},
		"HEAD moved": func(t *testing.T, repository string, _ *Plan, _ *SigningFiles, _ *string) {
			provenanceGitCommand(t, repository, "commit", "--allow-empty", "-m", "after plan")
		},
		"epoch changed": func(_ *testing.T, _ string, plan *Plan, _ *SigningFiles, _ *string) {
			plan.SourceDateEpoch++
		},
		"output inside repository": func(_ *testing.T, repository string, _ *Plan, _ *SigningFiles, output *string) {
			*output = filepath.Join(repository, "dist")
		},
		"private key inside repository": func(t *testing.T, repository string, _ *Plan, files *SigningFiles, _ *string) {
			content, err := os.ReadFile(files.PrivateKey)
			if err != nil {
				t.Fatal(err)
			}
			inside := filepath.Join(repository, ".private-key")
			if err := os.WriteFile(inside, content, 0o600); err != nil {
				t.Fatal(err)
			}
			exclude := filepath.Join(repository, ".git", "info", "exclude")
			if err := os.WriteFile(exclude, []byte(".private-key\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			files.PrivateKey = inside
		},
		"mismatched trusted key": func(t *testing.T, _ string, _ *Plan, files *SigningFiles, _ *string) {
			other := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{0x19}, ed25519.SeedSize))
			outside := filepath.Join(t.TempDir(), "other-release.pub")
			if err := os.WriteFile(
				outside,
				canonicalPublicPEM(t, other.Public().(ed25519.PublicKey)),
				0o600,
			); err != nil {
				t.Fatal(err)
			}
			files.TrustedPublicKey = outside
		},
		"existing output": func(t *testing.T, _ string, _ *Plan, _ *SigningFiles, output *string) {
			if err := os.Mkdir(*output, 0o750); err != nil {
				t.Fatal(err)
			}
		},
	} {
		t.Run(name, func(t *testing.T) {
			repository, plan, value, files, _ := productionSigningFixture(t)
			output := filepath.Join(t.TempDir(), "release")
			configure(t, repository, &plan, &files, &output)
			if _, err := Sign(t.Context(), repository, output, plan, value, files); err == nil {
				t.Fatal("Sign() error = nil")
			}
			if name != "existing output" {
				if _, err := os.Lstat(output); !os.IsNotExist(err) {
					t.Fatalf("failed signing output state: %v", err)
				}
			}
		})
	}

	repository, plan, value, files, _ := productionSigningFixture(t)
	canceled, cancel := context.WithCancel(t.Context())
	cancel()
	output := filepath.Join(t.TempDir(), "canceled")
	if _, err := Sign(canceled, repository, output, plan, value, files); err == nil {
		t.Fatal("Sign(canceled) error = nil")
	}
	if _, err := os.Lstat(output); !os.IsNotExist(err) {
		t.Fatalf("canceled signing output state: %v", err)
	}
	if _, err := Sign( //nolint:staticcheck // Intentional fail-closed boundary case.
		nil,
		repository,
		filepath.Join(t.TempDir(), "nil-context"),
		plan,
		value,
		files,
	); err == nil {
		t.Fatal("Sign(nil context) error = nil")
	}
}

func TestReadSigningFileRejectsUnsafeFiles(t *testing.T) {
	directory := t.TempDir()
	if _, err := readSigningFile("", maximumSigningKeyBytes, false); err == nil {
		t.Fatal("readSigningFile(empty path) error = nil")
	}
	if _, err := readSigningFile(
		filepath.Join(directory, "missing"),
		maximumSigningKeyBytes,
		false,
	); err == nil {
		t.Fatal("readSigningFile(missing path) error = nil")
	}
	if _, err := readSigningFile(directory, maximumSigningKeyBytes, false); err == nil {
		t.Fatal("readSigningFile(directory) error = nil")
	}
	large := filepath.Join(directory, "large")
	if err := os.WriteFile(large, make([]byte, maximumSigningKeyBytes+1), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := readSigningFile(large, maximumSigningKeyBytes, false); err == nil {
		t.Fatal("readSigningFile(large) error = nil")
	}
	target := filepath.Join(directory, "target")
	link := filepath.Join(directory, "link")
	if err := os.WriteFile(target, []byte("key"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	if _, err := readSigningFile(link, maximumSigningKeyBytes, false); err == nil {
		t.Fatal("readSigningFile(symlink) error = nil")
	}
	if runtime.GOOS != "windows" {
		permissive := filepath.Join(directory, "permissive")
		if err := os.WriteFile(permissive, []byte("key"), 0o644); err != nil {
			t.Fatal(err)
		}
		if _, err := readSigningFile(permissive, maximumSigningKeyBytes, true); err == nil {
			t.Fatal("readSigningFile(permissive private key) error = nil")
		}
	}
}

func TestRequireOutsideRepositoryResolvesDirectorySymlinks(t *testing.T) {
	repository := t.TempDir()
	link := filepath.Join(t.TempDir(), "repository-link")
	if err := os.Symlink(repository, link); err != nil {
		t.Skipf("directory symlink unavailable: %v", err)
	}
	if err := requireOutsideRepository(
		repository,
		filepath.Join(link, "release.key"),
		"release private key",
	); err == nil {
		t.Fatal("requireOutsideRepository(path through repository symlink) error = nil")
	}
	if err := requireOutsideRepository(
		repository,
		filepath.Join(t.TempDir(), "release.key"),
		"release private key",
	); err != nil {
		t.Fatalf("requireOutsideRepository(external path) error = %v", err)
	}
}

func TestCommitReleaseArtifactsRemovesStagingOnLateValidationFailure(t *testing.T) {
	parent := t.TempDir()
	output := filepath.Join(parent, "release")
	plan := Plan{Artifacts: []string{"artifact"}}
	wantErr := errors.New("checkout changed")
	if _, err := commitReleaseArtifacts(
		output,
		plan,
		map[string][]byte{"artifact": []byte("complete")},
		func() error { return wantErr },
	); !errors.Is(err, wantErr) {
		t.Fatalf("commitReleaseArtifacts() error = %v", err)
	}
	if _, err := os.Lstat(output); !os.IsNotExist(err) {
		t.Fatalf("late validation output state: %v", err)
	}
	entries, err := os.ReadDir(parent)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("late validation retained staging entries: %v", entries)
	}
}

func productionSigningFixture(
	t *testing.T,
) (string, Plan, catalog.Catalog, SigningFiles, ed25519.PublicKey) {
	t.Helper()
	fixture := loadProvenanceFixture(t)
	repository := committedProvenanceFixture(t, fixture, "starter-oidc")
	privateKey := deterministicPrivateKey()
	publicKey := privateKey.Public().(ed25519.PublicKey)
	publicKeyPath := filepath.Join(repository, "security", "release", "ed25519-public.pem")
	if err := os.MkdirAll(filepath.Dir(publicKeyPath), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(publicKeyPath, canonicalPublicPEM(t, publicKey), 0o600); err != nil {
		t.Fatal(err)
	}
	provenanceGitCommand(t, repository, "add", "security/release/ed25519-public.pem")
	provenanceGitCommand(t, repository, "commit", "-m", "add release trust anchor")
	commit := strings.TrimSpace(provenanceGitCommand(t, repository, "rev-parse", "HEAD"))
	provenanceGitCommand(t, repository, "tag", fixture.Plan.Version)
	plan := fixture.Plan
	plan.Mode = "production"
	plan.Commit = commit
	epoch, err := strconv.ParseInt(
		strings.TrimSpace(provenanceGitCommand(t, repository, "show", "-s", "--format=%ct", commit)),
		10,
		64,
	)
	if err != nil {
		t.Fatal(err)
	}
	plan.SourceDateEpoch = epoch
	plan.Artifacts = artifactNames(plan.Repository, plan.Version, true)
	value := catalogForRenderFixture(t, plan.CompatibilityCurrent)
	keyDirectory := t.TempDir()
	files := SigningFiles{
		PrivateKey:       filepath.Join(keyDirectory, "release.key"),
		TrustedPublicKey: publicKeyPath,
	}
	if err := os.WriteFile(
		files.PrivateKey,
		[]byte(base64.StdEncoding.EncodeToString(privateKey.Seed())),
		0o600,
	); err != nil {
		t.Fatal(err)
	}
	return repository, plan, value, files, publicKey
}

func deterministicPrivateKey() ed25519.PrivateKey {
	return ed25519.NewKeyFromSeed(bytes.Repeat([]byte{0x42}, ed25519.SeedSize))
}

func canonicalPublicPEM(t *testing.T, publicKey ed25519.PublicKey) []byte {
	t.Helper()
	encoded, err := x509.MarshalPKIXPublicKey(publicKey)
	if err != nil {
		t.Fatal(err)
	}
	return pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: encoded})
}

func readSigningTestFile(t *testing.T, directory string, name string) []byte {
	t.Helper()
	content, err := os.ReadFile(filepath.Join(directory, name))
	if err != nil {
		t.Fatal(err)
	}
	return content
}
