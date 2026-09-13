package acp

import (
	"encoding/json"
	"strings"
)

const maxPromptPayloadBytes = 8 << 20

// ContentBlock 保留 ACP/MCP ContentBlock 的开放字段，但在进入协议层前按 type 与
// initialize.promptCapabilities 做严格能力校验。动态 map 只停留在协议边界。
type ContentBlock map[string]any

func TextBlock(text string) ContentBlock {
	return ContentBlock{"type": "text", "text": text}
}

func validatePromptBlocks(process *agentProcess, blocks []ContentBlock) ([]ContentBlock, error) {
	if len(blocks) == 0 {
		return nil, newError("ACP_PROMPT_INVALID", "ACP prompt must contain at least one content block", false, nil, nil)
	}
	encoded, err := json.Marshal(blocks)
	if err != nil {
		return nil, newError("ACP_PROMPT_INVALID", "encode ACP prompt content", false, nil, err)
	}
	if len(encoded) > maxPromptPayloadBytes {
		return nil, newError("ACP_PROMPT_TOO_LARGE", "ACP prompt exceeds 8 MiB", false, map[string]any{"bytes": len(encoded)}, nil)
	}

	result := make([]ContentBlock, 0, len(blocks))
	for index, block := range blocks {
		if block == nil {
			return nil, newError("ACP_PROMPT_INVALID", "ACP prompt content block must be an object", false, map[string]any{"index": index}, nil)
		}
		blockType, _ := block["type"].(string)
		blockType = strings.TrimSpace(blockType)
		switch blockType {
		case "text":
			if _, ok := block["text"].(string); !ok {
				return nil, newError("ACP_PROMPT_INVALID", "ACP text content requires a string text field", false, map[string]any{"index": index}, nil)
			}
		case "resource_link":
			name, nameOK := block["name"].(string)
			uri, uriOK := block["uri"].(string)
			if !nameOK || strings.TrimSpace(name) == "" || !uriOK || strings.TrimSpace(uri) == "" {
				return nil, newError("ACP_PROMPT_INVALID", "ACP resource_link content requires non-empty name and uri", false, map[string]any{"index": index}, nil)
			}
		case "image":
			if !process.supportsPromptCapability("image") {
				return nil, capabilityError("promptCapabilities.image")
			}
			if _, ok := block["data"].(string); !ok {
				return nil, newError("ACP_PROMPT_INVALID", "ACP image content requires string data", false, map[string]any{"index": index}, nil)
			}
			if mimeType, ok := block["mimeType"].(string); !ok || strings.TrimSpace(mimeType) == "" {
				return nil, newError("ACP_PROMPT_INVALID", "ACP image content requires non-empty mimeType", false, map[string]any{"index": index}, nil)
			}
		case "audio":
			if !process.supportsPromptCapability("audio") {
				return nil, capabilityError("promptCapabilities.audio")
			}
			if _, ok := block["data"].(string); !ok {
				return nil, newError("ACP_PROMPT_INVALID", "ACP audio content requires string data", false, map[string]any{"index": index}, nil)
			}
			if mimeType, ok := block["mimeType"].(string); !ok || strings.TrimSpace(mimeType) == "" {
				return nil, newError("ACP_PROMPT_INVALID", "ACP audio content requires non-empty mimeType", false, map[string]any{"index": index}, nil)
			}
		case "resource":
			if !process.supportsPromptCapability("embeddedContext") {
				return nil, capabilityError("promptCapabilities.embeddedContext")
			}
			if _, ok := block["resource"].(map[string]any); !ok {
				return nil, newError("ACP_PROMPT_INVALID", "ACP embedded resource content requires a resource object", false, map[string]any{"index": index}, nil)
			}
		default:
			return nil, newError("ACP_PROMPT_INVALID", "unsupported ACP prompt content block type", false, map[string]any{"index": index, "type": blockType}, nil)
		}
		result = append(result, ContentBlock(cloneMap(map[string]any(block))))
	}
	return result, nil
}

func promptText(blocks []ContentBlock) (string, bool) {
	parts := make([]string, 0, len(blocks))
	for _, block := range blocks {
		if block["type"] != "text" {
			return "", false
		}
		text, _ := block["text"].(string)
		parts = append(parts, text)
	}
	return strings.TrimSpace(strings.Join(parts, "\n")), true
}
