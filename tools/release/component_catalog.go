package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/uvwt/agentdock/internal/component"
)

const componentCatalogSourcePath = "internal/component/catalog-v1.json"

func loadComponentCatalog() (component.Catalog, error) {
	var data []byte
	var err error
	for _, candidate := range []string{
		componentCatalogSourcePath,
		filepath.Join("..", "..", componentCatalogSourcePath),
	} {
		data, err = os.ReadFile(candidate)
		if err == nil {
			break
		}
	}
	if err != nil {
		return component.Catalog{}, fmt.Errorf("read component catalog source: %w", err)
	}
	catalog, err := component.ParseCatalog(data)
	if err != nil {
		return component.Catalog{}, fmt.Errorf("validate component catalog source: %w", err)
	}
	return catalog, nil
}

func currentCloudflaredCatalogEntry(agentdockVersion string) (component.CatalogComponent, error) {
	catalog, err := loadComponentCatalog()
	if err != nil {
		return component.CatalogComponent{}, err
	}
	entry, err := component.SelectCloudflaredEntry(catalog, agentdockVersion)
	if err != nil {
		return component.CatalogComponent{}, fmt.Errorf("select cloudflared for AgentDock %s: %w", agentdockVersion, err)
	}
	return entry, nil
}

func writeCloudflaredComponentCatalog(stdout io.Writer) error {
	catalog, err := loadComponentCatalog()
	if err != nil {
		return err
	}
	encoder := json.NewEncoder(stdout)
	encoder.SetIndent("", "  ")
	return encoder.Encode(catalog)
}
