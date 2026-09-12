package main

import (
	"crypto/sha256"
	"debug/buildinfo"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

type component struct {
	Type       string     `json:"type"`
	Name       string     `json:"name"`
	Version    string     `json:"version,omitempty"`
	Hashes     []hash     `json:"hashes,omitempty"`
	Properties []property `json:"properties,omitempty"`
}

type hash struct {
	Algorithm string `json:"alg"`
	Content   string `json:"content"`
}

type property struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

type bom struct {
	BOMFormat   string      `json:"bomFormat"`
	SpecVersion string      `json:"specVersion"`
	Version     int         `json:"version"`
	Metadata    metadata    `json:"metadata"`
	Components  []component `json:"components"`
}

type metadata struct {
	Timestamp string    `json:"timestamp"`
	Component component `json:"component"`
}

func main() {
	if len(os.Args) != 5 {
		fmt.Fprintln(os.Stderr, "usage: release_sbom BINARY PACKAGE_LOCK VERSION TIMESTAMP")
		os.Exit(2)
	}
	binaryPath, lockPath, appVersion, timestamp := os.Args[1], os.Args[2], os.Args[3], os.Args[4]
	if _, err := time.Parse(time.RFC3339, timestamp); err != nil {
		fatal(err)
	}
	contents, err := os.ReadFile(binaryPath)
	if err != nil {
		fatal(err)
	}
	digest := sha256.Sum256(contents)
	application := component{
		Type: "application", Name: "forms", Version: appVersion,
		Hashes: []hash{{Algorithm: "SHA-256", Content: hex.EncodeToString(digest[:])}},
	}
	components := make([]component, 0)
	info, err := buildinfo.ReadFile(binaryPath)
	if err != nil {
		fatal(err)
	}
	for _, dependency := range info.Deps {
		modulePath, moduleVersion := dependency.Path, dependency.Version
		if dependency.Replace != nil {
			modulePath, moduleVersion = dependency.Replace.Path, dependency.Replace.Version
		}
		components = append(components, component{
			Type: "library", Name: modulePath, Version: moduleVersion,
			Properties: []property{{Name: "forms:ecosystem", Value: "go"}},
		})
	}
	lockContents, err := os.ReadFile(lockPath)
	if err != nil {
		fatal(err)
	}
	var lock struct {
		Packages map[string]struct {
			Version string `json:"version"`
		} `json:"packages"`
	}
	if err := json.Unmarshal(lockContents, &lock); err != nil {
		fatal(err)
	}
	seenNPM := make(map[string]bool)
	for packagePath, value := range lock.Packages {
		marker := "node_modules/"
		index := strings.LastIndex(packagePath, marker)
		if index < 0 || value.Version == "" {
			continue
		}
		name := packagePath[index+len(marker):]
		key := name + "@" + value.Version
		if seenNPM[key] {
			continue
		}
		seenNPM[key] = true
		components = append(components, component{
			Type: "library", Name: name, Version: value.Version,
			Properties: []property{{Name: "forms:ecosystem", Value: "npm"}},
		})
	}
	sort.Slice(components, func(i, j int) bool {
		if components[i].Name == components[j].Name {
			return components[i].Version < components[j].Version
		}
		return components[i].Name < components[j].Name
	})
	result := bom{
		BOMFormat: "CycloneDX", SpecVersion: "1.5", Version: 1,
		Metadata: metadata{Timestamp: timestamp, Component: application}, Components: components,
	}
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(result); err != nil {
		fatal(err)
	}
}

func fatal(err error) {
	fmt.Fprintf(os.Stderr, "%s: %v\n", filepath.Base(os.Args[0]), err)
	os.Exit(1)
}
