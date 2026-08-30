package app

import (
	"context"

	toolrecall "github.com/uvwt/agentdock/internal/tool/recall"
)

func recallToolSpecs() []ToolSpec {
	return []ToolSpec{
		{Name: "recall_search", Title: "Search NexusDock Recall", Description: "Search NexusDock Recall Markdown documents and cards with lexical retrieval and optional semantic enhancement when embeddings are available. Use kind=all, markdown, or card; backend routing stays internal.", Availability: requiresNexus, Handler: typedToolHandler("recall_search", func(ctx context.Context, r *Runtime, request toolrecall.SearchRequest) (Result, error) {
			return r.recall.Search(ctx, request)
		})},
		{Name: "recall_read", Title: "Read NexusDock Recall entry", Description: "Read one Markdown document or card from the configured NexusDock Recall store by path.", Availability: requiresNexus, Handler: typedToolHandler("recall_read", func(ctx context.Context, r *Runtime, request toolrecall.ReadRequest) (Result, error) {
			return r.recall.Read(ctx, request)
		})},
		{Name: "recall_write", Title: "Write NexusDock Recall entry", Description: "Plan, create, replace, append, patch, update facts, diff, or delete NexusDock Recall content. The model must choose target=card/markdown and action explicitly.", Availability: requiresNexus, Handler: typedToolHandler("recall_write", func(ctx context.Context, r *Runtime, request toolrecall.WriteRequest) (Result, error) {
			return r.recall.Write(ctx, request)
		})},
		{Name: "recall_maintain", Title: "Maintain NexusDock Recall", Description: "Run NexusDock Recall maintenance actions such as list, lint, embedding_status, reindex, or reindex_cards.", Availability: requiresNexus, Handler: typedToolHandler("recall_maintain", func(ctx context.Context, r *Runtime, request toolrecall.MaintainRequest) (Result, error) {
			return r.recall.Maintain(ctx, request)
		})},
		{Name: "private_note_manage", Title: "Manage private notes", Description: "Explicit low-frequency NexusDock private note vault entrypoint. Do not use by default: use only when the user explicitly requests private note access or the content clearly contains sensitive secrets, credentials, or personal information. Search is metadata-only; plaintext is returned only by explicit read, and Git backups contain age ciphertext only. Actions: search, read, write, delete, status, or maintain.", Handler: typedToolHandler("private_note_manage", func(ctx context.Context, r *Runtime, request toolrecall.PrivateNoteRequest) (Result, error) {
			return r.recall.PrivateNoteManage(ctx, request)
		}), Availability: requiresNexus},
	}
}
