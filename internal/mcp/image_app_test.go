package mcp

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"testing"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
	protocol "github.com/uvwt/agentdock-protocol"
	"github.com/uvwt/agentdock-protocol/mcpapps"
	"github.com/uvwt/agentdock/internal/config"
)

func TestDirectImageAppUsesSharedRendererAndPreservesPixels(t *testing.T) {
	var original []byte
	for _, mode := range []config.MCPAppsMode{
		config.MCPAppsModeOff,
		config.MCPAppsModeCompact,
		config.MCPAppsModeFull,
	} {
		t.Run(string(mode), func(t *testing.T) {
			root := t.TempDir()
			img := image.NewRGBA(image.Rect(0, 0, 2, 2))
			img.Set(0, 0, color.RGBA{R: 255, A: 255})
			var buf bytes.Buffer
			if err := png.Encode(&buf, img); err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(root, "image.png")
			if err := os.WriteFile(path, buf.Bytes(), 0600); err != nil {
				t.Fatal(err)
			}

			h := newMCPAppTestHarnessWithMode(t, config.Config{
				AgentDockDefaultDir: root,
				AgentDockHome:       filepath.Join(root, ".agentdock"),
			}, mode)
			var found *mcpsdk.Tool
			for tool, err := range h.session.Tools(t.Context(), nil) {
				if err != nil {
					t.Fatal(err)
				}
				if tool.Name == "view_image" {
					found = tool
				}
			}
			if found == nil || found.OutputSchema == nil {
				t.Fatal("missing image contract")
			}

			hasImageApp := mode != config.MCPAppsModeOff
			if hasImageApp {
				assertToolUIResource(t, found, protocol.ImageUIResourceURI)
				var imageResource *mcpsdk.Resource
				for resource, err := range h.session.Resources(t.Context(), nil) {
					if err != nil {
						t.Fatal(err)
					}
					if resource.URI == protocol.ImageUIResourceURI {
						imageResource = resource
						break
					}
				}
				if imageResource == nil || imageResource.Description != "Display the requested image and, after user confirmation, provide it to the model when supported." {
					t.Fatalf("image resource description = %#v", imageResource)
				}
			} else if found.Meta["ui"] != nil {
				t.Fatal("Apps disabled")
			}

			result, err := h.session.CallTool(t.Context(), &mcpsdk.CallToolParams{
				Name:      "view_image",
				Arguments: map[string]any{"path": path, "format": "png"},
			})
			if err != nil || result.IsError {
				t.Fatalf("image failed: %v %#v", err, result)
			}
			pixels, ok := result.Content[0].(*mcpsdk.ImageContent)
			if !ok || len(pixels.Data) == 0 || result.StructuredContent == nil {
				t.Fatal("lost image or metadata")
			}
			if mode == config.MCPAppsModeOff {
				original = append([]byte(nil), pixels.Data...)
			} else if !bytes.Equal(original, pixels.Data) {
				t.Fatal("Apps changed image bytes")
			}

			if hasImageApp {
				if result.Meta["ui"] == nil {
					t.Fatal("missing result binding")
				}
				read, err := h.session.ReadResource(t.Context(), &mcpsdk.ReadResourceParams{URI: protocol.ImageUIResourceURI})
				if err != nil || len(read.Contents) != 1 || read.Contents[0].Text != mcpapps.HTML("view_image", "Image") {
					t.Fatalf("not using shared renderer: %v", err)
				}
			} else if result.Meta["ui"] != nil || len(result.Content) != 1 {
				t.Fatal("disabled result decorated")
			}

			failed, err := h.session.CallTool(t.Context(), &mcpsdk.CallToolParams{
				Name:      "view_image",
				Arguments: map[string]any{"path": filepath.Join(root, "missing.png")},
			})
			if err != nil || !failed.IsError || failed.Meta["ui"] != nil {
				t.Fatalf("failure acquired image widget: %v %#v", err, failed)
			}
		})
	}
}
