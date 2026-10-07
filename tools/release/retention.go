package main

import (
	"fmt"
	"sort"
	"strings"

	"github.com/uvwt/agentdock/internal/releaseversion"
)

type r2RetentionPlan struct {
	PreviousStable string   `json:"previous_stable,omitempty"`
	DeleteTags     []string `json:"delete_tags"`
}

// buildR2RetentionPlan 只对 release tool 能识别、且版本低于当前 Stable 的 prefix 做清理。
// 未知 prefix 和高于当前版本的 prefix 一律保留，避免旧 tag 重跑或异常对象导致误删。
func buildR2RetentionPlan(currentTag string, tags []string) (r2RetentionPlan, error) {
	current, err := releaseMetadataForTag(currentTag)
	if err != nil {
		return r2RetentionPlan{}, err
	}
	if current.Prerelease {
		return r2RetentionPlan{}, fmt.Errorf("R2 stable retention requires a stable current tag: %s", current.Tag)
	}

	unique := make(map[string]struct{}, len(tags))
	currentPresent := false
	for _, tag := range tags {
		tag = strings.TrimSpace(tag)
		if tag == "" {
			continue
		}
		unique[tag] = struct{}{}
		if tag == current.Tag {
			currentPresent = true
		}
	}
	if !currentPresent {
		return r2RetentionPlan{}, fmt.Errorf("current R2 release prefix is missing: releases/%s/", current.Tag)
	}

	previous := ""
	for tag := range unique {
		metadata, err := releaseMetadataForTag(tag)
		if err != nil || metadata.Prerelease {
			continue
		}
		comparison, ok := releaseversion.Compare(tag, current.Tag)
		if !ok || comparison >= 0 {
			continue
		}
		if previous == "" {
			previous = tag
			continue
		}
		if comparison, ok := releaseversion.Compare(tag, previous); ok && comparison > 0 {
			previous = tag
		}
	}

	deleteTags := make([]string, 0)
	for tag := range unique {
		metadata, err := releaseMetadataForTag(tag)
		if err != nil {
			continue
		}
		comparison, ok := releaseversion.Compare(tag, current.Tag)
		if !ok || comparison >= 0 {
			continue
		}
		if !metadata.Prerelease && tag == previous {
			continue
		}
		deleteTags = append(deleteTags, tag)
	}
	sort.Slice(deleteTags, func(i, j int) bool {
		comparison, ok := releaseversion.Compare(deleteTags[i], deleteTags[j])
		if !ok || comparison == 0 {
			return deleteTags[i] < deleteTags[j]
		}
		return comparison < 0
	})

	return r2RetentionPlan{
		PreviousStable: previous,
		DeleteTags:     deleteTags,
	}, nil
}
