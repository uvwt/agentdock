package taskstate

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

type TemplateStatus string

const (
	TemplateDraft     TemplateStatus = "draft"
	TemplateValidated TemplateStatus = "validated"
	TemplateActive    TemplateStatus = "active"
	TemplateRetired   TemplateStatus = "retired"
)

type MatchRule struct {
	Keywords  []string `json:"keywords,omitempty"`
	Devices   []string `json:"devices,omitempty"`
	TaskTypes []string `json:"task_types,omitempty"`
	Priority  int      `json:"priority,omitempty"`
}

type TemplateStep struct {
	ID                         string   `json:"id"`
	Title                      string   `json:"title"`
	Phase                      Phase    `json:"phase"`
	Required                   bool     `json:"required"`
	DependsOn                  []string `json:"depends_on,omitempty"`
	SuggestedCommands          []string `json:"suggested_commands,omitempty"`
	Substitution               string   `json:"substitution,omitempty"`
	SubstitutionReasonRequired bool     `json:"substitution_reason_required,omitempty"`
}

type Template struct {
	ID                   string         `json:"id"`
	Version              string         `json:"version"`
	Title                string         `json:"title"`
	Description          string         `json:"description,omitempty"`
	Status               TemplateStatus `json:"status"`
	Match                MatchRule      `json:"match,omitempty"`
	CompletionConditions []string       `json:"completion_conditions"`
	Steps                []TemplateStep `json:"steps"`
	Hash                 string         `json:"hash,omitempty"`
	PublishedAt          *time.Time     `json:"published_at,omitempty"`
	RetiredAt            *time.Time     `json:"retired_at,omitempty"`
}

type TemplateCandidate struct {
	ID      string `json:"id"`
	Version string `json:"version"`
	Score   int    `json:"score"`
	Reason  string `json:"reason"`
}

type TemplateSelection struct {
	ID             string              `json:"id"`
	Version        string              `json:"version"`
	Hash           string              `json:"hash"`
	SelectedReason string              `json:"selected_reason"`
	Candidates     []TemplateCandidate `json:"candidates,omitempty"`
	Snapshot       Template            `json:"snapshot"`
}

type StepEvidence struct {
	Type        string    `json:"type"`
	Source      string    `json:"source"`
	Result      string    `json:"result"`
	Summary     string    `json:"summary"`
	ArtifactRef string    `json:"artifact_ref,omitempty"`
	SHA256      string    `json:"sha256,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
}

type TaskStep struct {
	ID                 string         `json:"id"`
	Title              string         `json:"title"`
	Phase              Phase          `json:"phase"`
	Required           bool           `json:"required"`
	DependsOn          []string       `json:"depends_on,omitempty"`
	SuggestedCommands  []string       `json:"suggested_commands,omitempty"`
	Substitution       string         `json:"substitution,omitempty"`
	Status             string         `json:"status"`
	Substituted        bool           `json:"substituted,omitempty"`
	SubstitutionReason string         `json:"substitution_reason,omitempty"`
	SkipReason         string         `json:"skip_reason,omitempty"`
	Evidence           []StepEvidence `json:"evidence"`
	UpdatedAt          time.Time      `json:"updated_at"`
}

func (s *Store) WorkflowRoot() string { return filepath.Join(filepath.Dir(s.root), "workflows") }

func (s *Store) ensureWorkflowDirs() error {
	for _, dir := range []string{filepath.Join(s.WorkflowRoot(), "drafts"), filepath.Join(s.WorkflowRoot(), "published")} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return err
		}
		if err := os.Chmod(dir, 0o700); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) SaveTemplateDraft(t Template) (Template, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.ensureWorkflowDirs(); err != nil {
		return Template{}, err
	}
	t.Status = TemplateDraft
	t.Hash = ""
	t.PublishedAt = nil
	t.RetiredAt = nil
	if err := validateTemplate(t); err != nil {
		return Template{}, err
	}
	path := s.templatePath("drafts", t.ID, t.Version)
	if _, err := os.Stat(s.templatePath("published", t.ID, t.Version)); err == nil {
		return Template{}, errors.New("published template version is immutable; create a new version")
	}
	if err := writeJSONAtomic(path, t); err != nil {
		return Template{}, err
	}
	return t, nil
}

func (s *Store) ValidateTemplate(id, version string) (Template, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	t, err := s.loadTemplateLocked("drafts", id, version)
	if err != nil {
		return Template{}, err
	}
	if err := validateTemplate(t); err != nil {
		return Template{}, err
	}
	t.Status = TemplateValidated
	if err := writeJSONAtomic(s.templatePath("drafts", t.ID, t.Version), t); err != nil {
		return Template{}, err
	}
	return t, nil
}

func (s *Store) PublishTemplate(id, version string) (Template, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.ensureWorkflowDirs(); err != nil {
		return Template{}, err
	}
	t, err := s.loadTemplateLocked("drafts", id, version)
	if err != nil {
		return Template{}, err
	}
	if err := validateTemplate(t); err != nil {
		return Template{}, err
	}
	published := s.templatePath("published", id, version)
	if _, err := os.Stat(published); err == nil {
		return Template{}, errors.New("published template version already exists and cannot be overwritten")
	}
	now := time.Now().UTC()
	t.Status = TemplateActive
	t.PublishedAt = &now
	t.Hash = templateHash(t)
	if err := writeJSONAtomic(published, t); err != nil {
		return Template{}, err
	}
	return t, nil
}

func (s *Store) RetireTemplate(id, version string) (Template, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	t, err := s.loadTemplateLocked("published", id, version)
	if err != nil {
		return Template{}, err
	}
	if t.Status != TemplateActive {
		return Template{}, errors.New("only active templates can be retired")
	}
	now := time.Now().UTC()
	t.Status = TemplateRetired
	t.RetiredAt = &now
	if err := writeJSONAtomic(s.templatePath("published", id, version), t); err != nil {
		return Template{}, err
	}
	return t, nil
}

func (s *Store) GetTemplate(id, version string) (Template, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, area := range []string{"published", "drafts"} {
		t, err := s.loadTemplateLocked(area, id, version)
		if err == nil {
			return t, nil
		}
	}
	return Template{}, fmt.Errorf("template %s@%s not found", id, version)
}

func (s *Store) ListTemplates(status TemplateStatus) ([]Template, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.ensureWorkflowDirs(); err != nil {
		return nil, err
	}
	var out []Template
	for _, area := range []string{"drafts", "published"} {
		entries, err := os.ReadDir(filepath.Join(s.WorkflowRoot(), area))
		if err != nil {
			return nil, err
		}
		for _, entry := range entries {
			if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
				continue
			}
			data, err := os.ReadFile(filepath.Join(s.WorkflowRoot(), area, entry.Name()))
			if err != nil {
				return nil, err
			}
			var t Template
			if err := json.Unmarshal(data, &t); err != nil {
				return nil, err
			}
			if status == "" || t.Status == status {
				out = append(out, t)
			}
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].ID == out[j].ID {
			return out[i].Version > out[j].Version
		}
		return out[i].ID < out[j].ID
	})
	return out, nil
}

func (s *Store) MatchTemplates(goal, device, taskType string) ([]TemplateCandidate, error) {
	templates, err := s.ListTemplates(TemplateActive)
	if err != nil {
		return nil, err
	}
	goalLower := strings.ToLower(goal)
	var out []TemplateCandidate
	for _, t := range templates {
		score := t.Match.Priority
		var reasons []string
		for _, keyword := range t.Match.Keywords {
			if strings.Contains(goalLower, strings.ToLower(strings.TrimSpace(keyword))) {
				score += 10
				reasons = append(reasons, "keyword:"+keyword)
			}
		}
		if containsFold(t.Match.Devices, device) && device != "" {
			score += 30
			reasons = append(reasons, "device:"+device)
		}
		if containsFold(t.Match.TaskTypes, taskType) && taskType != "" {
			score += 20
			reasons = append(reasons, "task_type:"+taskType)
		}
		if score > 0 {
			out = append(out, TemplateCandidate{ID: t.ID, Version: t.Version, Score: score, Reason: strings.Join(reasons, ", ")})
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Score == out[j].Score {
			return out[i].ID < out[j].ID
		}
		return out[i].Score > out[j].Score
	})
	return out, nil
}

func validateTemplate(t Template) error {
	if !validTemplateToken(t.ID) || !validTemplateToken(t.Version) {
		return errors.New("template id and version must contain only letters, numbers, dot, dash, or underscore")
	}
	if strings.TrimSpace(t.Title) == "" {
		return errors.New("template title is required")
	}
	if len(normalizeTexts(t.CompletionConditions)) == 0 {
		return errors.New("template requires at least one completion condition")
	}
	if len(t.Steps) == 0 {
		return errors.New("template requires at least one step")
	}
	ids := map[string]TemplateStep{}
	for _, step := range t.Steps {
		if !validTemplateToken(step.ID) || strings.TrimSpace(step.Title) == "" {
			return fmt.Errorf("invalid template step %q", step.ID)
		}
		if !validPhase(step.Phase) {
			return fmt.Errorf("step %s has invalid phase %q", step.ID, step.Phase)
		}
		if _, exists := ids[step.ID]; exists {
			return fmt.Errorf("duplicate step id %q", step.ID)
		}
		if step.Substitution != "" && step.Substitution != "allowed" && step.Substitution != "forbidden" {
			return fmt.Errorf("step %s substitution must be allowed or forbidden", step.ID)
		}
		ids[step.ID] = step
	}
	for _, step := range t.Steps {
		for _, dep := range step.DependsOn {
			depStep, ok := ids[dep]
			if !ok {
				return fmt.Errorf("step %s depends on unknown step %s", step.ID, dep)
			}
			if phaseIndex(depStep.Phase) > phaseIndex(step.Phase) {
				return fmt.Errorf("step %s depends on later phase step %s", step.ID, dep)
			}
		}
	}
	return nil
}

func (s *Store) templatePath(area, id, version string) string {
	return filepath.Join(s.WorkflowRoot(), area, id+"@"+version+".json")
}

func (s *Store) loadTemplateLocked(area, id, version string) (Template, error) {
	if !validTemplateToken(id) || !validTemplateToken(version) {
		return Template{}, errors.New("invalid template id or version")
	}
	data, err := os.ReadFile(s.templatePath(area, id, version))
	if err != nil {
		return Template{}, err
	}
	var t Template
	if err := json.Unmarshal(data, &t); err != nil {
		return Template{}, err
	}
	return t, nil
}

func writeJSONAtomic(path string, value any) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	tmp, err := os.CreateTemp(filepath.Dir(path), ".tmp-*.json")
	if err != nil {
		return err
	}
	name := tmp.Name()
	defer os.Remove(name)
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(name, path); err != nil {
		return err
	}
	return os.Chmod(path, 0o600)
}

func templateHash(t Template) string {
	t.Hash = ""
	t.PublishedAt = nil
	t.RetiredAt = nil
	data, _ := json.Marshal(t)
	sum := sha256.Sum256(data)
	return "sha256:" + hex.EncodeToString(sum[:])
}

func validTemplateToken(v string) bool {
	if strings.TrimSpace(v) == "" {
		return false
	}
	for _, r := range v {
		if !((r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '.' || r == '-' || r == '_') {
			return false
		}
	}
	return true
}

func validPhase(p Phase) bool { return phaseIndex(p) >= 0 }
func phaseIndex(p Phase) int {
	for i, value := range phaseOrder {
		if p == value {
			return i
		}
	}
	return -1
}
func containsFold(values []string, value string) bool {
	for _, item := range values {
		if strings.EqualFold(strings.TrimSpace(item), strings.TrimSpace(value)) {
			return true
		}
	}
	return false
}
