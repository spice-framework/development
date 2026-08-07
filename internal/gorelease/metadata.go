package gorelease

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/spice-framework/development/internal/catalog"
	"github.com/spice-framework/development/internal/process"
)

const rendererIdentity = "github.com/spice-framework/development/cmd/spice-dev go-release renderer/v1"

type modMetadata struct {
	Module struct {
		Path string
	}
	Go        string
	Toolchain string
	Require   []struct {
		Path    string
		Version string
	}
	Replace []json.RawMessage
}

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
	Artifacts       []artifactFact `json:"artifacts"`
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

func requireModuleGraph(
	ctx context.Context,
	root string,
	repository catalog.Repository,
	runner process.Runner,
) ([]module, error) {
	modOutput, err := runner.Run(ctx, root, "go", "mod", "edit", "-json")
	if err != nil {
		return nil, fmt.Errorf("inspect Go release module: %w", err)
	}
	var metadata modMetadata
	if err := json.Unmarshal([]byte(modOutput), &metadata); err != nil {
		return nil, fmt.Errorf("parse Go release module: %w", err)
	}
	if metadata.Module.Path != repository.Module {
		return nil, fmt.Errorf("Go release module %q does not match catalog module %q", metadata.Module.Path, repository.Module)
	}
	if metadata.Go != "1.26.0" || metadata.Toolchain != "go1.26.5" {
		return nil, fmt.Errorf("Go release module requires go 1.26.0 and toolchain go1.26.5, got go %s and toolchain %s", metadata.Go, metadata.Toolchain)
	}
	if len(metadata.Replace) != 0 {
		return nil, errors.New("Go release module must not contain replace directives")
	}
	direct := make(map[string]string, len(metadata.Require))
	for _, requirement := range metadata.Require {
		if _, duplicate := direct[requirement.Path]; duplicate {
			return nil, fmt.Errorf("Go release module requires %s more than once", requirement.Path)
		}
		direct[requirement.Path] = requirement.Version
	}
	for _, required := range repository.Release.RequiredModules {
		version, found := direct[required]
		if !found || !catalog.ValidModuleVersion(version) {
			return nil, fmt.Errorf("Go release module must require %s at a canonical version", required)
		}
	}
	if _, err := runner.Run(ctx, root, "go", "list", "-mod=vendor", "./..."); err != nil {
		return nil, fmt.Errorf("validate committed vendor module graph: %w", err)
	}
	vendorContent, err := readBounded(filepath.Join(root, "vendor", "modules.txt"), 16<<20)
	if err != nil {
		return nil, fmt.Errorf("read committed vendor module graph: %w", err)
	}
	modules, err := parseVendorModules(vendorContent)
	if err != nil {
		return nil, err
	}
	selected := make(map[string]string, len(modules))
	for _, item := range modules {
		selected[item.Path] = item.Version
	}
	for modulePath, version := range direct {
		if selected[modulePath] != version {
			return nil, fmt.Errorf("committed vendor module %s is %q; go.mod requires %q", modulePath, selected[modulePath], version)
		}
	}
	return modules, nil
}

func parseVendorModules(content []byte) ([]module, error) {
	var modules []module
	seen := make(map[string]struct{})
	for line := range strings.SplitSeq(string(content), "\n") {
		if !strings.HasPrefix(line, "# ") || strings.HasPrefix(line, "## ") {
			continue
		}
		fields := strings.Fields(strings.TrimPrefix(line, "# "))
		if len(fields) != 2 || !catalog.ValidModuleVersion(fields[1]) {
			return nil, fmt.Errorf("committed vendor module header %q is malformed or uses a replacement", line)
		}
		if _, duplicate := seen[fields[0]]; duplicate {
			return nil, fmt.Errorf("committed vendor module graph repeats %s", fields[0])
		}
		seen[fields[0]] = struct{}{}
		modules = append(modules, module{Path: fields[0], Version: fields[1]})
	}
	slices.SortFunc(modules, func(left, right module) int { return strings.Compare(left.Path, right.Path) })
	return modules, nil
}

func renderArtifacts(ctx context.Context, prepared source, version string) (map[string][]byte, error) {
	archive, err := buildSourceArchive(ctx, prepared, version)
	if err != nil {
		return nil, err
	}
	base := prepared.repository.Name + "_" + strings.TrimPrefix(version, "v")
	archiveName := base + "_source.tar.gz"
	sbomName := base + "_sbom.spdx.json"
	metadataName := base + "_release.json"
	sbom, err := buildSBOM(prepared, version)
	if err != nil {
		return nil, err
	}
	metadata, err := buildReleaseMetadata(prepared, version, map[string][]byte{
		archiveName: archive,
		sbomName:    sbom,
	})
	if err != nil {
		return nil, err
	}
	artifacts := map[string][]byte{archiveName: archive, sbomName: sbom, metadataName: metadata}
	artifacts["checksums.txt"] = checksums(artifacts)
	return artifacts, nil
}

func buildReleaseMetadata(prepared source, version string, artifacts map[string][]byte) ([]byte, error) {
	facts := make([]artifactFact, 0, len(artifacts))
	for _, name := range sortedArtifactNames(artifacts) {
		digest := sha256.Sum256(artifacts[name])
		facts = append(facts, artifactFact{Name: name, SHA256: hex.EncodeToString(digest[:]), Size: len(artifacts[name])})
	}
	metadata := releaseMetadata{
		Schema: artifactSchema, Profile: catalog.ReleaseProfileGoModule,
		Repository: prepared.repository.Name, Module: prepared.repository.Module,
		Source: prepared.repository.CanonicalURL, Version: version, Commit: prepared.commit,
		SourceDateEpoch: prepared.epoch, Go: "1.26.5", Artifacts: facts,
	}
	return marshalDeterministic(metadata, "Go release metadata")
}

func buildSBOM(prepared source, version string) ([]byte, error) {
	rootID := packageID(prepared.repository.Module, version)
	packages := []spdxPackage{spdxPackageValue(prepared.repository.Module, version)}
	relationships := []spdxRelationship{{
		SPDXElementID: "SPDXRef-DOCUMENT", RelationshipType: "DESCRIBES", RelatedSPDXElement: rootID,
	}}
	for _, item := range prepared.modules {
		packages = append(packages, spdxPackageValue(item.Path, item.Version))
		relationships = append(relationships, spdxRelationship{
			SPDXElementID: rootID, RelationshipType: "DEPENDS_ON", RelatedSPDXElement: packageID(item.Path, item.Version),
		})
	}
	namespaceSeed := prepared.repository.Module + "@" + version + "@" + prepared.commit
	namespaceDigest := sha256.Sum256([]byte(namespaceSeed))
	document := spdxDocument{
		SPDXVersion: "SPDX-2.3", DataLicense: "CC0-1.0", SPDXID: "SPDXRef-DOCUMENT",
		Name: prepared.repository.Name + " " + version,
		DocumentNamespace: strings.TrimSuffix(prepared.repository.CanonicalURL, "/") + "/releases/" +
			version + "/spdx/v1/" + hex.EncodeToString(namespaceDigest[:]),
		CreationInfo: spdxCreationInfo{
			Created:  unixTime(prepared.epoch).Format(time.RFC3339),
			Creators: []string{"Organization: Spice Framework", "Tool: " + rendererIdentity},
		},
		Packages: packages, Relationships: relationships,
	}
	return marshalDeterministic(document, "Go release SBOM")
}

func spdxPackageValue(name string, version string) spdxPackage {
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

func marshalDeterministic(value any, label string) ([]byte, error) {
	content, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("encode %s: %w", label, err)
	}
	return append(content, '\n'), nil
}

func unixTime(epoch int64) time.Time { return time.Unix(epoch, 0).UTC() }
