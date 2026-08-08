package distributionrelease

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/spice-framework/development/internal/catalog"
)

const rendererIdentity = "github.com/spice-framework/development/cmd/spice-dev distribution-release renderer/v1"

type releaseMetadata struct {
	Schema          int            `json:"schema"`
	Profile         string         `json:"profile"`
	Repository      string         `json:"repository"`
	Module          string         `json:"module"`
	Source          string         `json:"source"`
	Version         string         `json:"version"`
	Commit          string         `json:"commit"`
	SourceDateEpoch int64          `json:"source_date_epoch"`
	Go              string         `json:"go"`
	Toolchain       string         `json:"toolchain"`
	Build           buildFact      `json:"build"`
	Targets         []targetFact   `json:"targets"`
	Payloads        []artifactFact `json:"payloads"`
	Artifacts       []artifactFact `json:"artifacts"`
}

type buildFact struct {
	ModuleMode     string            `json:"module_mode"`
	CGOEnabled     bool              `json:"cgo_enabled"`
	Trimpath       bool              `json:"trimpath"`
	BuildVCS       bool              `json:"build_vcs"`
	BuildID        string            `json:"build_id"`
	Environment    string            `json:"environment"`
	CacheIsolation bool              `json:"cache_isolation"`
	Source         string            `json:"source"`
	GOAMD64        string            `json:"goamd64"`
	GOARM64        string            `json:"goarm64"`
	Identity       buildIdentityFact `json:"identity"`
}

type buildIdentityFact struct {
	VersionSymbol string `json:"version_symbol"`
	VersionValue  string `json:"version_value"`
	CommitSymbol  string `json:"commit_symbol"`
	CommitValue   string `json:"commit_value"`
}

type targetFact struct {
	GOOS     string   `json:"goos"`
	GOARCH   string   `json:"goarch"`
	Archive  string   `json:"archive"`
	Binaries []string `json:"binaries"`
}

type artifactFact struct {
	Name   string `json:"name"`
	SHA256 string `json:"sha256"`
	Size   int    `json:"size"`
}

type spdxDocument struct {
	SPDXVersion       string             `json:"spdxVersion"`
	DataLicense       string             `json:"dataLicense"`
	SPDXID            string             `json:"SPDXID"`
	Name              string             `json:"name"`
	DocumentNamespace string             `json:"documentNamespace"`
	CreationInfo      spdxCreationInfo   `json:"creationInfo"`
	Packages          []spdxPackage      `json:"packages"`
	Relationships     []spdxRelationship `json:"relationships"`
}

type spdxCreationInfo struct {
	Created  string   `json:"created"`
	Creators []string `json:"creators"`
}

type spdxPackage struct {
	Name             string `json:"name"`
	SPDXID           string `json:"SPDXID"`
	VersionInfo      string `json:"versionInfo"`
	DownloadLocation string `json:"downloadLocation"`
	FilesAnalyzed    bool   `json:"filesAnalyzed"`
	LicenseConcluded string `json:"licenseConcluded"`
	LicenseDeclared  string `json:"licenseDeclared"`
	CopyrightText    string `json:"copyrightText"`
}

type spdxRelationship struct {
	SPDXElementID      string `json:"spdxElementId"`
	RelationshipType   string `json:"relationshipType"`
	RelatedSPDXElement string `json:"relatedSpdxElement"`
}

func renderArtifacts(ctx context.Context, prepared preparedRelease) (map[string][]byte, error) {
	payloads, err := loadPayloads(ctx, prepared)
	if err != nil {
		return nil, err
	}
	artifacts := make(map[string][]byte, len(prepared.repository.Release.Targets)+3)
	targets := make([]targetFact, 0, len(prepared.repository.Release.Targets))
	for _, target := range prepared.repository.Release.Targets {
		binaries, err := buildTarget(ctx, prepared, target)
		if err != nil {
			return nil, err
		}
		archive, err := buildArchive(prepared, target, binaries, payloads)
		if err != nil {
			return nil, err
		}
		name := targetBase(prepared.repository.Name, prepared.repository.Release.Version, target)
		if target.GOOS == "windows" {
			name += ".zip"
		} else {
			name += ".tar.gz"
		}
		if err := requireArtifactSize(name, len(archive)); err != nil {
			return nil, err
		}
		artifacts[name] = archive
		binaryNames := make([]string, 0, len(binaries))
		for binaryName := range binaries {
			binaryNames = append(binaryNames, binaryName)
		}
		slices.Sort(binaryNames)
		targets = append(targets, targetFact{
			GOOS: target.GOOS, GOARCH: target.GOARCH, Archive: name, Binaries: binaryNames,
		})
	}
	base := prepared.repository.Name + "_" + strings.TrimPrefix(prepared.repository.Release.Version, "v")
	sbomName := base + "_sbom.spdx.json"
	sbom, err := buildSBOM(prepared)
	if err != nil {
		return nil, err
	}
	artifacts[sbomName] = sbom
	metadataName := base + "_release.json"
	metadata, err := buildReleaseMetadata(prepared, targets, payloads, artifacts)
	if err != nil {
		return nil, err
	}
	artifacts[metadataName] = metadata
	artifacts["checksums.txt"] = artifactChecksums(artifacts)
	return artifacts, nil
}

func requireArtifactSize(name string, size int) error {
	if size < 0 || int64(size) > maximumArtifactBytes {
		return fmt.Errorf("distribution artifact %q exceeds %d bytes", name, maximumArtifactBytes)
	}
	return nil
}

func buildReleaseMetadata(
	prepared preparedRelease,
	targets []targetFact,
	payloads map[string][]byte,
	artifacts map[string][]byte,
) ([]byte, error) {
	identity := prepared.repository.Release.BuildIdentity
	metadata := releaseMetadata{
		Schema: artifactSchema, Profile: catalog.ReleaseProfileDistribution,
		Repository: prepared.repository.Name, Module: prepared.repository.Module,
		Source: prepared.repository.CanonicalURL, Version: prepared.repository.Release.Version,
		Commit: prepared.commit, SourceDateEpoch: prepared.epoch,
		Go: "1.26.5", Toolchain: "go1.26.5",
		Build: buildFact{
			ModuleMode: "vendor", CGOEnabled: false, Trimpath: true, BuildVCS: false, BuildID: "",
			Environment: "closed", CacheIsolation: true, Source: "materialized-tagged-commit",
			GOAMD64: "v1", GOARM64: "v8.0",
			Identity: buildIdentityFact{
				VersionSymbol: identity.VersionSymbol,
				VersionValue:  strings.TrimPrefix(prepared.repository.Release.Version, "v"),
				CommitSymbol:  identity.CommitSymbol,
				CommitValue:   prepared.commit,
			},
		},
		Targets: targets, Payloads: facts(payloads), Artifacts: facts(artifacts),
	}
	return marshal(metadata, "distribution release metadata")
}

func facts(values map[string][]byte) []artifactFact {
	result := make([]artifactFact, 0, len(values))
	for _, name := range artifactNames(values) {
		digest := sha256.Sum256(values[name])
		result = append(result, artifactFact{Name: name, SHA256: hex.EncodeToString(digest[:]), Size: len(values[name])})
	}
	return result
}

func buildSBOM(prepared preparedRelease) ([]byte, error) {
	version := prepared.repository.Release.Version
	rootID := packageID(prepared.repository.Module, version)
	packages := []spdxPackage{newSPDXPackage(prepared.repository.Module, version)}
	relationships := []spdxRelationship{{
		SPDXElementID: "SPDXRef-DOCUMENT", RelationshipType: "DESCRIBES", RelatedSPDXElement: rootID,
	}}
	for _, item := range prepared.modules {
		packages = append(packages, newSPDXPackage(item.Path, item.Version))
		relationships = append(relationships, spdxRelationship{
			SPDXElementID: rootID, RelationshipType: "DEPENDS_ON", RelatedSPDXElement: packageID(item.Path, item.Version),
		})
	}
	namespaceDigest := sha256.Sum256([]byte(prepared.repository.Module + "@" + version + "@" + prepared.commit))
	document := spdxDocument{
		SPDXVersion: "SPDX-2.3", DataLicense: "CC0-1.0", SPDXID: "SPDXRef-DOCUMENT",
		Name: prepared.repository.Name + " " + version,
		DocumentNamespace: strings.TrimSuffix(prepared.repository.CanonicalURL, "/") + "/releases/" +
			version + "/spdx/distribution-v1/" + hex.EncodeToString(namespaceDigest[:]),
		CreationInfo: spdxCreationInfo{
			Created:  time.Unix(prepared.epoch, 0).UTC().Format(time.RFC3339),
			Creators: []string{"Organization: Spice Framework", "Tool: " + rendererIdentity},
		},
		Packages: packages, Relationships: relationships,
	}
	return marshal(document, "distribution SPDX SBOM")
}

func newSPDXPackage(name string, version string) spdxPackage {
	return spdxPackage{
		Name: name, SPDXID: packageID(name, version), VersionInfo: version,
		DownloadLocation: "NOASSERTION", FilesAnalyzed: false,
		LicenseConcluded: "NOASSERTION", LicenseDeclared: "NOASSERTION", CopyrightText: "NOASSERTION",
	}
}

func packageID(name string, version string) string {
	digest := sha256.Sum256([]byte(name + "@" + version))
	return "SPDXRef-Package-" + hex.EncodeToString(digest[:8])
}

func marshal(value any, label string) ([]byte, error) {
	content, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("encode %s: %w", label, err)
	}
	return append(content, '\n'), nil
}
