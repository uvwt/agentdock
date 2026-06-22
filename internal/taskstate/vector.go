package taskstate

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"sort"
	"strings"
	"time"
)

const (
	defaultTaskVectorMinScore     = 0.55
	defaultTaskVectorTimeoutMS    = 10000
	maxTemplateVectorCandidates   = 3
	maxTemplateVectorTextRunes    = 900
	minTemplateVectorLexicalScore = 0.02
)

// EmbeddingProvider returns one vector per input text. Implementations should
// preserve input order and should not treat empty strings as errors.
type EmbeddingProvider interface {
	EmbedTexts(ctx context.Context, texts []string) ([][]float64, error)
}

type StoreOptions struct {
	TaskVectorSearch    bool
	EmbeddingEndpoint   string
	EmbeddingToken      string
	EmbeddingModel      string
	TaskVectorTimeoutMS int
	TaskVectorMinScore  float64

	EmbeddingProvider EmbeddingProvider
}

func newStore(root string, opts StoreOptions) *Store {
	var provider EmbeddingProvider
	if opts.TaskVectorSearch {
		provider = opts.EmbeddingProvider
	}
	if provider == nil && opts.TaskVectorSearch && strings.TrimSpace(opts.EmbeddingEndpoint) != "" {
		provider = &httpEmbeddingProvider{
			endpoint: strings.TrimSpace(opts.EmbeddingEndpoint),
			token:    strings.TrimSpace(opts.EmbeddingToken),
			model:    strings.TrimSpace(opts.EmbeddingModel),
			timeout:  taskVectorTimeout(opts.TaskVectorTimeoutMS),
			client:   &http.Client{},
		}
	}
	minScore := opts.TaskVectorMinScore
	if minScore <= 0 || minScore > 1 {
		minScore = defaultTaskVectorMinScore
	}
	return &Store{
		root:           root,
		vectorProvider: provider,
		vectorMinScore: minScore,
		vectorCache:    map[string][]float64{},
	}
}

func taskVectorTimeout(ms int) time.Duration {
	if ms <= 0 {
		ms = defaultTaskVectorTimeoutMS
	}
	return time.Duration(ms) * time.Millisecond
}

type httpEmbeddingProvider struct {
	endpoint string
	token    string
	model    string
	timeout  time.Duration
	client   *http.Client
}

func (p *httpEmbeddingProvider) EmbedTexts(ctx context.Context, texts []string) ([][]float64, error) {
	if strings.TrimSpace(p.endpoint) == "" {
		return nil, errors.New("embedding endpoint is empty")
	}
	if len(texts) == 0 {
		return nil, nil
	}
	ctx, cancel := context.WithTimeout(ctx, p.timeout)
	defer cancel()
	payload := map[string]any{"input": texts}
	if strings.TrimSpace(p.model) != "" {
		payload["model"] = p.model
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	if p.token != "" {
		req.Header.Set("Authorization", "Bearer "+p.token)
	}
	client := p.client
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("embedding endpoint returned HTTP %d: %s", resp.StatusCode, truncateVectorError(data))
	}
	var parsed struct {
		Data []struct {
			Embedding []float64 `json:"embedding"`
			Index     int       `json:"index"`
		} `json:"data"`
	}
	if err := json.Unmarshal(data, &parsed); err != nil {
		return nil, err
	}
	if len(parsed.Data) != len(texts) {
		return nil, fmt.Errorf("embedding response count=%d, want %d", len(parsed.Data), len(texts))
	}
	vectors := make([][]float64, len(texts))
	for position, item := range parsed.Data {
		index := item.Index
		if index < 0 || index >= len(texts) || vectors[index] != nil {
			index = position
		}
		vectors[index] = item.Embedding
	}
	for i, vector := range vectors {
		if len(vector) == 0 {
			return nil, fmt.Errorf("embedding %d is empty", i)
		}
	}
	return vectors, nil
}

func truncateVectorError(data []byte) string {
	text := strings.TrimSpace(string(data))
	if len(text) > 300 {
		return text[:300] + "..."
	}
	return text
}

func (s *Store) templateVectorScores(goal, taskType string, templates []Template) map[string]float64 {
	if s.vectorProvider == nil || strings.TrimSpace(goal) == "" || len(templates) == 0 {
		return nil
	}
	queryText := strings.TrimSpace(strings.Join([]string{goal, taskType}, " "))
	candidates := shortlistTemplateVectorCandidates(queryText, templates)
	if len(candidates) == 0 {
		return nil
	}

	keys := make([]string, 0, len(candidates))
	missingKeys := []string{}
	missingTexts := []string{}
	textByKey := map[string]string{}
	for _, candidate := range candidates {
		key := templateVectorCacheKey(candidate.template)
		keys = append(keys, key)
		textByKey[key] = candidate.text
	}

	cached := map[string][]float64{}
	s.vectorMu.Lock()
	for _, key := range keys {
		if vector := s.vectorCache[key]; len(vector) > 0 {
			cached[key] = append([]float64{}, vector...)
		} else {
			missingKeys = append(missingKeys, key)
			missingTexts = append(missingTexts, textByKey[key])
		}
	}
	s.vectorMu.Unlock()

	input := append([]string{queryText}, missingTexts...)
	vectors, err := s.vectorProvider.EmbedTexts(context.Background(), input)
	if err != nil || len(vectors) != len(input) {
		return nil
	}
	queryVector := vectors[0]
	if len(queryVector) == 0 {
		return nil
	}
	if len(missingKeys) > 0 {
		s.vectorMu.Lock()
		for i, key := range missingKeys {
			vector := vectors[i+1]
			if len(vector) > 0 {
				s.vectorCache[key] = append([]float64{}, vector...)
				cached[key] = append([]float64{}, vector...)
			}
		}
		s.vectorMu.Unlock()
	}

	scores := map[string]float64{}
	for _, key := range keys {
		if score := cosineSimilarity(queryVector, cached[key]); score > 0 {
			scores[key] = score
		}
	}
	return scores
}

func templateVectorCacheKey(t Template) string {
	if strings.TrimSpace(t.Hash) != "" {
		return t.ID + "@" + t.Version + "#" + t.Hash
	}
	return t.ID + "@" + t.Version + "#" + templateHash(t)
}

func templateVectorText(t Template) string {
	parts := []string{t.ID, t.Title, t.Description}
	parts = append(parts, t.Match.Keywords...)
	parts = append(parts, t.Match.TaskTypes...)
	parts = append(parts, t.CompletionConditions...)
	for _, step := range t.Steps {
		parts = append(parts, step.ID, step.Title)
		parts = append(parts, step.SuggestedCommands...)
	}
	return truncateTemplateVectorText(strings.Join(normalizeTexts(parts), "\n"))
}

type templateVectorCandidate struct {
	template Template
	text     string
	score    float64
}

func shortlistTemplateVectorCandidates(query string, templates []Template) []templateVectorCandidate {
	items := make([]templateVectorCandidate, 0, len(templates))
	for _, t := range templates {
		text := templateVectorText(t)
		score := lexicalVectorPrefilterScore(query, text)
		if score >= minTemplateVectorLexicalScore {
			items = append(items, templateVectorCandidate{template: t, text: text, score: score})
		}
	}
	sort.SliceStable(items, func(i, j int) bool {
		if items[i].score == items[j].score {
			return items[i].template.ID < items[j].template.ID
		}
		return items[i].score > items[j].score
	})
	if len(items) > maxTemplateVectorCandidates {
		items = items[:maxTemplateVectorCandidates]
	}
	return items
}

func lexicalVectorPrefilterScore(query, text string) float64 {
	queryTerms := vectorPrefilterTerms(query)
	if len(queryTerms) == 0 {
		return 0
	}
	textMatch := templateMatchText(text)
	if textMatch == "" {
		return 0
	}
	matches := 0
	weighted := 0
	for term := range queryTerms {
		if strings.Contains(textMatch, term) {
			matches++
			weighted += len([]rune(term))
		}
	}
	if matches == 0 {
		return 0
	}
	return float64(matches)/float64(len(queryTerms)) + float64(weighted)/200
}

func vectorPrefilterTerms(value string) map[string]struct{} {
	normalized := templateMatchText(value)
	terms := map[string]struct{}{}
	runes := []rune(normalized)
	for n := 2; n <= 4; n++ {
		if len(runes) < n {
			continue
		}
		for i := 0; i <= len(runes)-n; i++ {
			term := string(runes[i : i+n])
			if weakTemplateKeyword(term) {
				continue
			}
			terms[term] = struct{}{}
		}
	}
	return terms
}

func truncateTemplateVectorText(text string) string {
	runes := []rune(text)
	if len(runes) <= maxTemplateVectorTextRunes {
		return text
	}
	return string(runes[:maxTemplateVectorTextRunes])
}

func templateVectorScoreBonus(score, minScore float64) int {
	if score < minScore {
		return 0
	}
	denominator := 1 - minScore
	if denominator <= 0 {
		return 70
	}
	bonus := 35 + int(math.Round((score-minScore)/denominator*35))
	if bonus < 35 {
		return 35
	}
	if bonus > 70 {
		return 70
	}
	return bonus
}

func cosineSimilarity(a, b []float64) float64 {
	if len(a) == 0 || len(a) != len(b) {
		return 0
	}
	var dot, normA, normB float64
	for i := range a {
		dot += a[i] * b[i]
		normA += a[i] * a[i]
		normB += b[i] * b[i]
	}
	if normA == 0 || normB == 0 {
		return 0
	}
	return dot / (math.Sqrt(normA) * math.Sqrt(normB))
}
