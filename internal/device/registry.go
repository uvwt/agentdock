package device

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/uvwt/agentdock/internal/atomicfile"
)

// Device is a registered AgentDock instance.
type Device struct {
	ID       string    `json:"device_id"`
	Name     string    `json:"name"`
	MCPURL   string    `json:"mcp_url,omitempty"`
	Role     string    `json:"role,omitempty"` // builder | tester | reviewer | any
	Labels   []string  `json:"labels,omitempty"`
	LastSeen time.Time `json:"last_seen"`
	Online   bool      `json:"online"`
}

// Handoff is a soft assignment of a goal/work slice to a device.
type Handoff struct {
	ID         string    `json:"handoff_id"`
	GoalID     string    `json:"goal_id"`
	FromDevice string    `json:"from_device,omitempty"`
	ToDevice   string    `json:"to_device"`
	Role       string    `json:"role,omitempty"`
	Summary    string    `json:"summary,omitempty"`
	Status     string    `json:"status"` // pending | accepted | done | rejected
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
}

// Registry is a local multi-device directory. Full shared Goal Store sync is intentionally
// deferred; this enables handoff bookkeeping and discovery for Phase 7 scaffolding.
type Registry struct {
	path string
	mu   sync.Mutex
}

type stateFile struct {
	Devices  []Device  `json:"devices"`
	Handoffs []Handoff `json:"handoffs"`
}

func NewRegistry(agentDockHome string) (*Registry, error) {
	if strings.TrimSpace(agentDockHome) == "" {
		return nil, errors.New("agentdock home required")
	}
	dir := filepath.Join(agentDockHome, "devices")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	return &Registry{path: filepath.Join(dir, "registry.json")}, nil
}

func (r *Registry) load() (stateFile, error) {
	var st stateFile
	data, err := os.ReadFile(r.path)
	if err != nil {
		if os.IsNotExist(err) {
			return stateFile{}, nil
		}
		return st, err
	}
	if err := json.Unmarshal(data, &st); err != nil {
		return st, err
	}
	return st, nil
}

func (r *Registry) save(st stateFile) error {
	data, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return atomicfile.Write(r.path, data, 0o600)
}

func (r *Registry) UpsertDevice(d Device) (Device, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	st, err := r.load()
	if err != nil {
		return Device{}, err
	}
	d.ID = strings.TrimSpace(d.ID)
	d.Name = strings.TrimSpace(d.Name)
	if d.ID == "" || d.Name == "" {
		return Device{}, errors.New("device id and name are required")
	}
	d.LastSeen = time.Now().UTC()
	d.Online = true
	found := false
	for i := range st.Devices {
		if st.Devices[i].ID == d.ID {
			st.Devices[i] = d
			found = true
			break
		}
	}
	if !found {
		st.Devices = append(st.Devices, d)
	}
	if err := r.save(st); err != nil {
		return Device{}, err
	}
	return d, nil
}

func (r *Registry) ListDevices() ([]Device, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	st, err := r.load()
	if err != nil {
		return nil, err
	}
	out := append([]Device(nil), st.Devices...)
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

func (r *Registry) CreateHandoff(goalID, from, to, role, summary string) (Handoff, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	st, err := r.load()
	if err != nil {
		return Handoff{}, err
	}
	goalID = strings.TrimSpace(goalID)
	to = strings.TrimSpace(to)
	if goalID == "" || to == "" {
		return Handoff{}, errors.New("goal_id and to_device are required")
	}
	// ensure device exists (soft)
	exists := false
	for _, d := range st.Devices {
		if d.ID == to {
			exists = true
			break
		}
	}
	if !exists {
		return Handoff{}, fmt.Errorf("unknown device %q", to)
	}
	now := time.Now().UTC()
	h := Handoff{
		ID:         fmt.Sprintf("hnd_%d", now.UnixNano()),
		GoalID:     goalID,
		FromDevice: strings.TrimSpace(from),
		ToDevice:   to,
		Role:       strings.TrimSpace(role),
		Summary:    strings.TrimSpace(summary),
		Status:     "pending",
		CreatedAt:  now,
		UpdatedAt:  now,
	}
	st.Handoffs = append(st.Handoffs, h)
	if err := r.save(st); err != nil {
		return Handoff{}, err
	}
	return h, nil
}

func (r *Registry) UpdateHandoff(id, status string) (Handoff, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	st, err := r.load()
	if err != nil {
		return Handoff{}, err
	}
	status = strings.ToLower(strings.TrimSpace(status))
	switch status {
	case "pending", "accepted", "done", "rejected":
	default:
		return Handoff{}, fmt.Errorf("invalid handoff status %q", status)
	}
	for i := range st.Handoffs {
		if st.Handoffs[i].ID == id {
			st.Handoffs[i].Status = status
			st.Handoffs[i].UpdatedAt = time.Now().UTC()
			if err := r.save(st); err != nil {
				return Handoff{}, err
			}
			return st.Handoffs[i], nil
		}
	}
	return Handoff{}, fmt.Errorf("handoff not found: %s", id)
}

func (r *Registry) ListHandoffs(goalID string) ([]Handoff, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	st, err := r.load()
	if err != nil {
		return nil, err
	}
	out := make([]Handoff, 0, len(st.Handoffs))
	for _, h := range st.Handoffs {
		if goalID == "" || h.GoalID == goalID {
			out = append(out, h)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].UpdatedAt.After(out[j].UpdatedAt) })
	return out, nil
}
