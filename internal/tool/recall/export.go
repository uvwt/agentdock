package recall

import "context"

func (s *Service) MemoryBootstrap(ctx context.Context, args map[string]any) (Result, error) {
	return s.memoryBootstrap(ctx, args)
}
func (s *Service) MemoryRead(ctx context.Context, args map[string]any) (Result, error) {
	return s.memoryRead(ctx, args)
}
func (s *Service) MemoryDiff(ctx context.Context, args map[string]any) (Result, error) {
	return s.memoryDiff(ctx, args)
}
func (s *Service) MemoryPatch(ctx context.Context, args map[string]any) (Result, error) {
	return s.memoryPatch(ctx, args)
}
func (s *Service) MemoryUpdateFact(ctx context.Context, args map[string]any) (Result, error) {
	return s.memoryUpdateFact(ctx, args)
}
func (s *Service) MemoryLint(ctx context.Context, args map[string]any) (Result, error) {
	return s.memoryLint(ctx, args)
}
func (s *Service) MemoryCardCapture(ctx context.Context, args map[string]any) (Result, error) {
	return s.memoryCardCapture(ctx, args)
}
func (s *Service) MemoryCardWrite(ctx context.Context, args map[string]any) (Result, error) {
	return s.memoryCardWrite(ctx, args)
}
func (s *Service) NotesSearch(ctx context.Context, args map[string]any) (Result, error) {
	return s.notesSearch(ctx, args)
}
func (s *Service) NotesCapture(ctx context.Context, args map[string]any) (Result, error) {
	return s.notesCapture(ctx, args)
}
func (s *Service) NotesWrite(ctx context.Context, args map[string]any) (Result, error) {
	return s.notesWrite(ctx, args)
}
