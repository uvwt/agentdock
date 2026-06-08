package skillruntime

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/uvwt/agentdock/internal/compatenv"
)

const (
	defaultMaxOutput  = 1 << 20
	defaultMaxCapture = 16 << 20
)

var envNamePattern = regexp.MustCompile(`^[A-Z_][A-Z0-9_]*$`)

type PermissionAuthorizer interface {
	Authorize(context.Context, Manifest, Operation) error
}

type PermissionAuthorizerFunc func(context.Context, Manifest, Operation) error

func (f PermissionAuthorizerFunc) Authorize(ctx context.Context, manifest Manifest, operation Operation) error {
	return f(ctx, manifest, operation)
}

func (r *Runtime) Run(ctx context.Context, req RunRequest) (RunResult, error) {
	started := time.Now().UTC()
	result := RunResult{RunID: req.RunID, Skill: req.Skill, Operation: req.Operation, StartedAt: started, ExitCode: -1}
	finish := func(err error) (RunResult, error) {
		result.CompletedAt = time.Now().UTC()
		result.DurationMS = result.CompletedAt.Sub(started).Milliseconds()
		if runtimeErr := new(Error); errors.As(err, &runtimeErr) {
			result.ErrorCode = runtimeErr.Code
			result.Error = runtimeErr.Error()
		} else if err != nil {
			result.ErrorCode = ErrExecutionFailed
			result.Error = err.Error()
		}
		if r.Reporter != nil {
			_ = r.Reporter.RunCompleted(ctx, result)
		}
		eventType := "run.completed"
		if err != nil {
			eventType = "run.failed"
		}
		r.emit(ctx, Event{
			Type:      eventType,
			RunID:     result.RunID,
			Skill:     result.Skill,
			Version:   result.Version,
			Operation: result.Operation,
			Timestamp: result.CompletedAt,
			Payload: map[string]any{
				"ok":          result.OK,
				"exit_code":   result.ExitCode,
				"error_code":  result.ErrorCode,
				"duration_ms": result.DurationMS,
			},
		})
		if err != nil {
			r.emit(ctx, Event{
				Type:      "observation.created",
				RunID:     result.RunID,
				Skill:     result.Skill,
				Version:   result.Version,
				Operation: result.Operation,
				Timestamp: result.CompletedAt,
				Payload: map[string]any{
					"error_code": result.ErrorCode,
					"stage":      errorStage(err),
					"message":    result.Error,
				},
			})
		}
		return result, err
	}

	packageDir, err := r.State.Resolve(req.Skill, req.Version, req.Channel)
	if err != nil {
		return finish(runtimeError(ErrOperationMissing, "resolve", err))
	}
	manifest, err := LoadManifest(packageDir)
	if err != nil {
		return finish(err)
	}
	result.Version = manifest.Metadata.Version
	operation, ok := findOperation(manifest, req.Operation)
	if !ok {
		return finish(runtimeError(ErrOperationMissing, "operation", fmt.Errorf("operation %q is not defined", req.Operation)))
	}
	r.emit(ctx, Event{Type: "run.started", RunID: req.RunID, Skill: req.Skill, Version: manifest.Metadata.Version, Operation: req.Operation, Timestamp: time.Now().UTC()})
	if err := ValidateJSON(operation.InputSchema, req.Input); err != nil {
		return finish(runtimeError(ErrInputInvalid, "input", err))
	}
	if r.Authorizer != nil {
		if err := r.Authorizer.Authorize(ctx, manifest, operation); err != nil {
			return finish(runtimeError(ErrPermissionDenied, "permission", err))
		}
	} else if err := conservativePermissionCheck(manifest); err != nil {
		return finish(runtimeError(ErrPermissionDenied, "permission", err))
	}

	binding, secrets, err := r.loadBinding(manifest, req.Binding)
	if err != nil {
		return finish(err)
	}
	envRegistryValues, envRegistrySecrets, err := r.loadEnvRegistry(manifest)
	if err != nil {
		return finish(err)
	}
	secrets = append(secrets, envRegistrySecrets...)
	sort.Slice(secrets, func(i, j int) bool { return len(secrets[i]) > len(secrets[j]) })
	entrypoint, err := safePackageJoin(packageDir, manifest.Spec.Entrypoint)
	if err != nil {
		return finish(runtimeError(ErrManifestInvalid, "entrypoint", err))
	}
	timeout := time.Duration(operation.TimeoutSeconds) * time.Second
	if req.Timeout > 0 && req.Timeout < timeout {
		timeout = req.Timeout
	}
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	maxOutput := req.MaxOutput
	if maxOutput <= 0 {
		maxOutput = defaultMaxOutput
	}
	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	cmd := exec.Command(entrypoint)
	configureProcessGroup(cmd)
	cmd.Dir = packageDir
	cmd.Stdin = bytes.NewReader(req.Input)
	cmd.Env = r.buildEnv(manifest, operation, binding, envRegistryValues)
	captureLimit := maxOutput
	if captureLimit < defaultMaxCapture {
		captureLimit = defaultMaxCapture
	}
	stdout := newCappedBuffer(captureLimit)
	stderr := newCappedBuffer(captureLimit)
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	if err := cmd.Start(); err != nil {
		return finish(runtimeError(ErrExecutionFailed, "start", err))
	}
	r.emit(ctx, Event{Type: "run.step.started", RunID: req.RunID, Skill: req.Skill, Version: manifest.Metadata.Version, Operation: req.Operation, Timestamp: time.Now().UTC(), Payload: map[string]any{"step": "execute"}})
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	var waitErr error
	select {
	case waitErr = <-done:
	case <-runCtx.Done():
		killProcessGroup(cmd)
		waitErr = <-done
	}
	result.ExitCode = exitCode(waitErr)
	stdoutBytes := stdout.Bytes()
	stderrBytes := stderr.Bytes()
	stdoutTruncated := stdout.Truncated() || stdout.TotalBytes() > int64(maxOutput)
	stderrTruncated := stderr.Truncated() || stderr.TotalBytes() > int64(maxOutput)
	result.StdoutBytes = stdout.TotalBytes()
	result.StderrBytes = stderr.TotalBytes()
	result.StdoutTruncated = stdoutTruncated
	result.StderrTruncated = stderrTruncated
	result.Truncated = stdoutTruncated || stderrTruncated
	result.Stdout = redactText(truncateText(stdoutBytes, maxOutput), secrets)
	result.Stderr = redactText(truncateText(stderrBytes, maxOutput), secrets)
	if errors.Is(runCtx.Err(), context.DeadlineExceeded) {
		return finish(runtimeError(ErrTimeout, "execute", fmt.Errorf("operation exceeded %s", timeout)))
	}
	if waitErr != nil {
		return finish(runtimeError(ErrExecutionFailed, "execute", waitErr))
	}
	trimmedOutput := bytes.TrimSpace(stdoutBytes)
	if len(trimmedOutput) == 0 {
		return finish(runtimeError(ErrOutputInvalid, "output", errors.New("stdout must contain final JSON")))
	}
	if stdout.Truncated() {
		result.Output = truncatedJSONFallback(trimmedOutput, stdout.TotalBytes(), maxOutput, secrets)
	} else {
		if err := ValidateJSON(operation.OutputSchema, trimmedOutput); err != nil {
			return finish(runtimeError(ErrOutputInvalid, "output", err))
		}
		redacted := redactJSON(trimmedOutput, secrets)
		if len(redacted) > maxOutput {
			result.Output = truncateJSONPreview(redacted, maxOutput)
		} else {
			result.Output = redacted
		}
	}
	result.OK = true
	r.emit(ctx, Event{Type: "run.evidence.created", RunID: req.RunID, Skill: req.Skill, Version: manifest.Metadata.Version, Operation: req.Operation, Timestamp: time.Now().UTC(), Payload: map[string]any{"kind": "operation_output", "exit_code": result.ExitCode, "truncated": result.Truncated}})
	return finish(nil)
}

func (r *Runtime) loadBinding(manifest Manifest, selected string) (Binding, []string, error) {
	if len(manifest.Spec.Bindings) == 0 && len(manifest.Spec.Permissions.Secrets) == 0 {
		return Binding{Env: map[string]string{}, Secrets: map[string]string{}}, nil, nil
	}
	if r.Bindings == nil {
		return Binding{}, nil, runtimeError(ErrBindingInvalid, "binding", errors.New("binding store is not configured"))
	}
	if selected == "" && len(manifest.Spec.Bindings) == 0 {
		return Binding{Env: map[string]string{}, Secrets: map[string]string{}}, nil, nil
	}
	binding, err := r.Bindings.Load(manifest.Metadata.Name, selected)
	if err != nil {
		return Binding{}, nil, err
	}
	if len(manifest.Spec.Bindings) > 0 && !contains(manifest.Spec.Bindings, binding.Name) {
		return Binding{}, nil, runtimeError(ErrBindingInvalid, "binding", fmt.Errorf("binding %q is not declared by skill", binding.Name))
	}
	allowed := make(map[string]struct{}, len(manifest.Spec.Permissions.Secrets))
	for _, name := range manifest.Spec.Permissions.Secrets {
		allowed[name] = struct{}{}
		value, ok := binding.Secrets[name]
		if !ok {
			return Binding{}, nil, runtimeError(ErrSecretMissing, "binding.secret", fmt.Errorf("secret %s is missing", name))
		}
		resolved, err := resolveSecret(value)
		if err != nil {
			return Binding{}, nil, runtimeError(ErrSecretMissing, "binding.secret", fmt.Errorf("%s: %w", name, err))
		}
		binding.Secrets[name] = resolved
	}
	for name := range binding.Secrets {
		if _, ok := allowed[name]; !ok {
			return Binding{}, nil, runtimeError(ErrBindingInvalid, "binding.secret", fmt.Errorf("undeclared secret %s", name))
		}
	}
	secrets := make([]string, 0, len(binding.Secrets))
	for _, value := range binding.Secrets {
		if value != "" {
			secrets = append(secrets, value)
		}
	}
	sort.Slice(secrets, func(i, j int) bool { return len(secrets[i]) > len(secrets[j]) })
	return binding, secrets, nil
}

func (r *Runtime) loadEnvRegistry(manifest Manifest) (map[string]string, []string, error) {
	if r.EnvProvider == nil {
		return map[string]string{}, nil, nil
	}
	definitions := EnvDefinitionsForManifest(manifest)
	values, secrets, err := r.EnvProvider.EnvForSkill(manifest.Metadata.Name, definitions)
	if err != nil {
		return nil, nil, runtimeError(ErrBindingInvalid, "env_registry", err)
	}
	return values, secrets, nil
}

func (r *Runtime) buildEnv(manifest Manifest, operation Operation, binding Binding, envRegistryValues map[string]string) []string {
	values := map[string]string{}
	for _, entry := range r.BaseEnv {
		if key, value, ok := strings.Cut(entry, "="); ok {
			values[key] = value
		}
	}
	values["AGENTDOCK_SKILL"] = manifest.Metadata.Name
	values["AGENTDOCK_SKILL_VERSION"] = manifest.Metadata.Version
	values["AGENTDOCK_OPERATION"] = operation.Name
	values["AGENTDOCK_BINDING"] = binding.Name
	for key, value := range envRegistryValues {
		if envNamePattern.MatchString(key) {
			values[key] = value
		}
	}
	for key, value := range binding.Env {
		if envNamePattern.MatchString(key) {
			values[key] = value
		}
	}
	for key, value := range binding.Secrets {
		values[key] = value
	}
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	result := make([]string, 0, len(keys))
	for _, key := range keys {
		result = append(result, key+"="+values[key])
	}
	return result
}

func EnvDefinitionsForManifest(manifest Manifest) []EnvDefinition {
	items := make([]EnvDefinition, 0, len(manifest.Spec.Permissions.Secrets)+len(manifest.Spec.Permissions.Env))
	seen := map[string]struct{}{}
	add := func(def EnvDefinition) {
		key := def.Skill + "\x00" + def.Name
		if _, ok := seen[key]; ok {
			return
		}
		seen[key] = struct{}{}
		items = append(items, def)
	}
	for _, name := range manifest.Spec.Permissions.Secrets {
		if envNamePattern.MatchString(name) {
			add(EnvDefinition{Skill: manifest.Metadata.Name, Name: name, Kind: "secret", Source: "manifest"})
		}
	}
	for _, env := range manifest.Spec.Permissions.Env {
		if envNamePattern.MatchString(env.Name) {
			add(EnvDefinition{Skill: manifest.Metadata.Name, Name: env.Name, Kind: strings.ToLower(strings.TrimSpace(env.Kind)), Source: "manifest"})
		}
	}
	for _, def := range compatEnvDefinitions(manifest.Metadata.Name) {
		add(def)
	}
	return items
}

func compatEnvDefinitions(skill string) []EnvDefinition {
	defs := compatenv.ForSkill(skill)
	items := make([]EnvDefinition, 0, len(defs))
	for _, def := range defs {
		items = append(items, EnvDefinition{Skill: def.Skill, Name: def.Name, Kind: def.Kind, Source: def.Source})
	}
	return items
}

func conservativePermissionCheck(manifest Manifest) error {
	if len(manifest.Spec.Permissions.Network) > 0 {
		return errors.New("network access requires an explicit PermissionAuthorizer")
	}
	for _, permission := range manifest.Spec.Permissions.Filesystem {
		if strings.HasPrefix(permission, "/") || strings.Contains(permission, "..") {
			return fmt.Errorf("filesystem permission %q requires an explicit PermissionAuthorizer", permission)
		}
	}
	return nil
}

func resolveSecret(reference string) (string, error) {
	name := ""
	switch {
	case strings.HasPrefix(reference, "env:"):
		name = strings.TrimPrefix(reference, "env:")
	case strings.HasPrefix(reference, "${") && strings.HasSuffix(reference, "}"):
		name = strings.TrimSuffix(strings.TrimPrefix(reference, "${"), "}")
	default:
		return reference, nil
	}
	if !envNamePattern.MatchString(name) {
		return "", fmt.Errorf("invalid environment reference %q", name)
	}
	value, ok := os.LookupEnv(name)
	if !ok {
		return "", fmt.Errorf("environment variable %s is not set", name)
	}
	return value, nil
}

func findOperation(manifest Manifest, name string) (Operation, bool) {
	for _, operation := range manifest.Spec.Operations {
		if operation.Name == name {
			return operation, true
		}
	}
	return Operation{}, false
}

func exitCode(err error) int {
	if err == nil {
		return 0
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return exitErr.ExitCode()
	}
	return -1
}

func redactText(value string, secrets []string) string {
	for _, secret := range secrets {
		if secret != "" {
			value = strings.ReplaceAll(value, secret, "[REDACTED]")
		}
	}
	return value
}

func redactJSON(raw []byte, secrets []string) json.RawMessage {
	var value any
	if err := json.Unmarshal(raw, &value); err != nil {
		return nil
	}
	value = redactJSONValue(value, secrets)
	encoded, err := json.Marshal(value)
	if err != nil {
		return nil
	}
	return encoded
}

func redactJSONValue(value any, secrets []string) any {
	switch typed := value.(type) {
	case string:
		return redactText(typed, secrets)
	case []any:
		for i := range typed {
			typed[i] = redactJSONValue(typed[i], secrets)
		}
		return typed
	case map[string]any:
		for key := range typed {
			typed[key] = redactJSONValue(typed[key], secrets)
		}
		return typed
	default:
		return value
	}
}

type cappedBuffer struct {
	mu         sync.Mutex
	buffer     bytes.Buffer
	limit      int
	totalBytes int64
	truncated  bool
}

func newCappedBuffer(limit int) *cappedBuffer { return &cappedBuffer{limit: limit} }

func (b *cappedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	written := len(p)
	b.totalBytes += int64(written)
	remaining := b.limit - b.buffer.Len()
	if remaining <= 0 {
		b.truncated = true
		return written, nil
	}
	if len(p) > remaining {
		p = p[:remaining]
		b.truncated = true
	}
	_, _ = b.buffer.Write(p)
	return written, nil
}

func (b *cappedBuffer) Bytes() []byte {
	b.mu.Lock()
	defer b.mu.Unlock()
	return append([]byte(nil), b.buffer.Bytes()...)
}

func (b *cappedBuffer) String() string { return string(b.Bytes()) }

func (b *cappedBuffer) Truncated() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.truncated
}

func (b *cappedBuffer) TotalBytes() int64 {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.totalBytes
}

func truncateText(data []byte, limit int) string {
	if limit <= 0 || len(data) <= limit {
		return string(data)
	}
	end := limit
	for end > 0 && !utf8.Valid(data[:end]) {
		end--
	}
	return string(data[:end])
}

func truncateJSONPreview(data []byte, limit int) json.RawMessage {
	if limit <= 0 || len(data) <= limit {
		return append(json.RawMessage(nil), data...)
	}
	var value any
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	if err := decoder.Decode(&value); err == nil {
		collectionLimits := []int{64, 32, 16, 8, 4, 2, 1}
		stringLimits := []int{2048, 1024, 512, 256, 128, 64}
		for _, collectionLimit := range collectionLimits {
			for _, stringLimit := range stringLimits {
				preview := pruneJSONValue(value, collectionLimit, stringLimit)
				encoded, err := json.Marshal(preview)
				if err == nil && len(encoded) <= limit {
					return encoded
				}
			}
		}
	}
	return truncatedJSONFallback(data, int64(len(data)), limit, nil)
}

func pruneJSONValue(value any, collectionLimit, stringLimit int) any {
	switch typed := value.(type) {
	case map[string]any:
		keys := make([]string, 0, len(typed))
		for key := range typed {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		if len(keys) > collectionLimit {
			keys = keys[:collectionLimit]
		}
		result := make(map[string]any, len(keys))
		for _, key := range keys {
			result[key] = pruneJSONValue(typed[key], collectionLimit, stringLimit)
		}
		return result
	case []any:
		limit := len(typed)
		if limit > collectionLimit {
			limit = collectionLimit
		}
		result := make([]any, 0, limit)
		for _, item := range typed[:limit] {
			result = append(result, pruneJSONValue(item, collectionLimit, stringLimit))
		}
		return result
	case string:
		if len(typed) <= stringLimit {
			return typed
		}
		return truncateText([]byte(typed), stringLimit)
	default:
		return value
	}
}

func truncatedJSONFallback(data []byte, originalBytes int64, limit int, secrets []string) json.RawMessage {
	previewLimit := limit / 2
	if previewLimit < 32 {
		previewLimit = 32
	}
	preview := redactText(truncateText(data, previewLimit), secrets)
	payload := map[string]any{
		"truncated":      true,
		"original_bytes": originalBytes,
		"preview":        preview,
	}
	encoded, _ := json.Marshal(payload)
	if limit > 0 && len(encoded) > limit {
		return json.RawMessage(`{"truncated":true}`)
	}
	return encoded
}

var _ io.Writer = (*cappedBuffer)(nil)
