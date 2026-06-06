package skillruntime

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type Binding struct {
	Name    string            `json:"name"`
	Secrets map[string]string `json:"secrets,omitempty"`
	Env     map[string]string `json:"env,omitempty"`
}

type BindingStore struct{ root string }

func NewBindingStore(root string) (*BindingStore, error) {
	if strings.TrimSpace(root) == "" {
		return nil, fmt.Errorf("binding root is required")
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(abs, 0o700); err != nil {
		return nil, fmt.Errorf("create binding root: %w", err)
	}
	return &BindingStore{root: abs}, nil
}

func (s *BindingStore) Load(skill, selected string) (Binding, error) {
	if !skillNamePattern.MatchString(skill) {
		return Binding{}, runtimeError(ErrBindingInvalid, "binding.name", fmt.Errorf("invalid skill name"))
	}
	path := filepath.Join(s.root, skill+".yaml")
	data, err := os.ReadFile(path)
	if err != nil {
		return Binding{}, runtimeError(ErrBindingInvalid, "binding.read", err)
	}
	var raw map[string]any
	trimmed := strings.TrimSpace(string(data))
	if strings.HasPrefix(trimmed, "{") {
		if err := json.Unmarshal(data, &raw); err != nil {
			return Binding{}, runtimeError(ErrBindingInvalid, "binding.parse", err)
		}
	} else {
		raw, err = parseYAML(data)
		if err != nil {
			return Binding{}, runtimeError(ErrBindingInvalid, "binding.parse", err)
		}
	}
	bindings, ok := raw["bindings"].(map[string]any)
	if !ok {
		return Binding{}, runtimeError(ErrBindingInvalid, "binding.parse", fmt.Errorf("bindings mapping is required"))
	}
	if selected == "" {
		selected, _ = raw["default"].(string)
	}
	if selected == "" && len(bindings) == 1 {
		for name := range bindings {
			selected = name
		}
	}
	if selected == "" {
		return Binding{}, runtimeError(ErrBindingInvalid, "binding.select", fmt.Errorf("binding name is required"))
	}
	value, ok := bindings[selected].(map[string]any)
	if !ok {
		return Binding{}, runtimeError(ErrBindingInvalid, "binding.select", fmt.Errorf("binding %q is not defined", selected))
	}
	encoded, _ := json.Marshal(value)
	var binding Binding
	if err := json.Unmarshal(encoded, &binding); err != nil {
		return Binding{}, runtimeError(ErrBindingInvalid, "binding.decode", err)
	}
	binding.Name = selected
	if binding.Secrets == nil {
		binding.Secrets = map[string]string{}
	}
	if binding.Env == nil {
		binding.Env = map[string]string{}
	}
	return binding, nil
}

func (s *BindingStore) Path(skill string) string { return filepath.Join(s.root, skill+".yaml") }
