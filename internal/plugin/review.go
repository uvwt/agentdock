package plugin

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"sort"
	"strings"
)

func emptyReview() Review {
	return Review{
		Skills: []SkillComponent{}, MCP: []MCPReview{}, Unsupported: []string{}, Warnings: []string{}, Executables: []string{}, Issues: []string{},
		Compatibility: Compatibility{Format: "unknown", Supported: []string{}, Unsupported: []string{}, Warnings: []string{}},
	}
}

type reviewTokenMaterial struct {
	PackageDigest string        `json:"package_digest"`
	Compatibility Compatibility `json:"compatibility"`
	Unsupported   []string      `json:"unsupported,omitempty"`
	Warnings      []string      `json:"warnings,omitempty"`
	Executables   []string      `json:"executables,omitempty"`
}

func buildReview(pkg Package) Review {
	review := Review{
		Name:          pkg.Manifest.Name,
		Version:       pkg.Manifest.Version,
		Description:   pkg.Manifest.Description,
		PackageDigest: pkg.PackageDigest,
		Provenance:    pkg.Manifest.Provenance,
		Skills:        append([]SkillComponent(nil), pkg.Components.Skills...),
		MCP:           reviewMCPComponents(pkg.Components.MCP),
		Unsupported:   append([]string(nil), pkg.Unsupported...),
		Warnings:      append([]string(nil), pkg.Warnings...),
		Executables:   append([]string(nil), pkg.Executables...),
		Compatibility: pkg.Compatibility,
	}
	review.ReviewToken = makeReviewToken(pkg)
	if len(pkg.Unsupported) > 0 {
		review.Issues = append(review.Issues, "Plugin contains unsupported components: "+strings.Join(pkg.Unsupported, ", "))
	}
	review.Valid = len(review.Issues) == 0
	return review
}

func makeReviewToken(pkg Package) string {
	material := reviewTokenMaterial{
		PackageDigest: pkg.PackageDigest,
		Compatibility: pkg.Compatibility,
		Unsupported:   append([]string(nil), pkg.Unsupported...),
		Warnings:      append([]string(nil), pkg.Warnings...),
		Executables:   append([]string(nil), pkg.Executables...),
	}
	sort.Strings(material.Unsupported)
	sort.Strings(material.Warnings)
	sort.Strings(material.Executables)
	data, _ := json.Marshal(material)
	sum := sha256.Sum256(data)
	return "review-v1:sha256:" + hex.EncodeToString(sum[:])
}

func verifyReviewToken(pkg Package, token string) error {
	token = strings.TrimSpace(token)
	if token == "" {
		return pluginError("PLUGIN_REVIEW_REQUIRED", "review", errors.New("validate the Plugin and submit the returned review_token"))
	}
	if token != makeReviewToken(pkg) {
		return pluginError("PLUGIN_REVIEW_CHANGED", "review", errors.New("Plugin candidate changed after review; validate again and confirm the new review_token"))
	}
	return nil
}
