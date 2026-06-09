# Skill Runtime 规范

> AgentDock 原生 Skill manifest、环境变量声明、安装和运行时注入的实现合同。

## Scenario: Manifest Env Declarations

### 1. Scope / Trigger

- Trigger: Skill package 需要声明 plain/secret 环境变量，并让 Env Manager、Nexus agent 和运行时注入共享同一份 manifest 合同。
- Applies to: `internal/skillruntime`, `internal/envregistry`, `internal/tools`, `internal/nexusagent`, `tests/skillruntime`。
- Goal: 新 Skill 不应长期依赖 AgentDock 核心中的 compat env 表来暴露 `BASE_URL`、`CONFIG_FILE`、`REDIRECT_URI` 等 plain env。

### 2. Signatures

- Manifest field:
  - `spec.permissions.env []EnvVar`
  - `EnvVar.Name string`
  - `EnvVar.Kind string`
- Runtime API:
  - `skillruntime.EnvDefinitionsForManifest(manifest Manifest) []EnvDefinition`
  - `envregistry.Store.EnvForSkill(skill string, definitions []Definition) (map[string]string, []string, error)`

### 3. Contracts

- `spec.permissions.env[].name` must be an environment variable name matching `^[A-Z_][A-Z0-9_]*$`.
- `spec.permissions.env[].kind` must be `plain` or `secret`.
- `spec.permissions.secrets` remains the legacy binding-secret declaration and is still discovered as secret env for compatibility.
- `spec.permissions.env` and `spec.permissions.secrets` must not declare the same name.
- Manifest definitions use `Source="manifest"`; compat table definitions use `Source="compat"`.
- Manifest definitions take precedence over compat definitions with the same `{skill,name}`.
- Secret values loaded from Env Registry files or process environment must be included in the redaction list returned to Skill Runtime.

### 4. Validation & Error Matrix

- Invalid env name -> manifest validation error at `spec.permissions.env[i].name`.
- Invalid kind -> manifest validation error at `spec.permissions.env[i].kind`.
- Duplicate env name within `permissions.env` -> manifest validation error.
- Duplicate name across `permissions.secrets` and `permissions.env` -> manifest validation error.
- No manifest env/secrets and no compat env definitions -> install requires `confirmed_no_env=true`.

### 5. Good/Base/Bad Cases

- Good: declare plain and secret env explicitly:
  ```yaml
  permissions:
    env:
      - name: OPENLIST_URL
        kind: plain
      - name: OPENLIST_TOKEN
        kind: secret
    secrets: []
  ```
- Base: old Skill with `permissions.secrets: [API_TOKEN]` still gets a secret manifest env definition and binding enforcement.
- Bad: declare `API_TOKEN` in both `permissions.secrets` and `permissions.env`; this is ambiguous and must fail manifest validation.

### 6. Tests Required

- Manifest parsing accepts `permissions.env` with plain and secret entries.
- Manifest validation rejects invalid kind and duplicate names.
- Install succeeds without `confirmed_no_env` when manifest env definitions exist.
- Runtime run injects manifest env definitions through `EnvProvider`.
- Secret manifest env values are redacted from stdout, stderr and structured output.
- Env Registry includes process-provided secret values in the redaction list.

### 7. Wrong vs Correct

#### Wrong

```yaml
permissions:
  secrets: [OPENLIST_TOKEN]
  env:
    - name: OPENLIST_TOKEN
      kind: secret
```

This mixes binding-secret semantics and Env Manager declaration for the same variable.

#### Correct

```yaml
permissions:
  env:
    - name: OPENLIST_TOKEN
      kind: secret
  secrets: []
```

Use `permissions.env` for Env Manager plain/secret declarations. Use `permissions.secrets` only when the Skill explicitly requires binding-based secret configuration.

## Scenario: Skill Source Validate Preflight

### 1. Scope / Trigger

- Trigger: `skill_manage` needs to validate a Skill source before installing it, so Skill authors can see manifest, entrypoint, dependency command and env-declaration issues without mutating installed Skill state.
- Applies to: `internal/skillruntime`, `internal/tools`, `internal/mcp`, `README.md`.
- Goal: validation should mirror install-time checks closely enough to be actionable, while staying read-only with respect to Skill state, Env Registry values and Skill execution.

### 2. Signatures

- Tool action:
  - `skill_manage action=validate`
- Input fields:
  - `source string` required: workspace/host path or HTTP(S) URL.
  - `digest string` optional: expected SHA-256 digest.
  - `max_bytes int` optional: package download/extract byte limit.
  - `confirmed_no_env bool` optional: confirms a Skill with no manifest/compat env declarations intentionally needs no Env Manager configuration.
- Runtime API:
  - `skillruntime.Runtime.Validate(ctx context.Context, req ValidateRequest) (ValidateResult, error)`
  - `ValidateRequest{Source, DigestSHA256, MaxBytes, ConfirmedNoEnv}`
  - `ValidateResult{Valid, Source, Digest, Manifest, Env, Commands, Issues, RequiresNoEnvConfirm}`

### 3. Contracts

- `validate` must not install, activate, roll back or write Skill state.
- `validate` must not run the Skill entrypoint and must not read real Env Registry secret values.
- `validate` reuses source preparation for local directories, zip packages and HTTP(S) downloads.
- Returned `source` must be safe for client display: HTTP(S) userinfo, query and fragment are omitted.
- Returned `env` contains declaration metadata only: `skill`, `name`, `kind`, `source`; it must not contain values.
- Returned `commands` contains one item per declared command with `command`, `found`, optional `path`, optional `error`.
- Returned `issues` contains structured `code`, `stage`, `message` entries.
- If manifest parsing succeeds, return `manifest` even when package-level checks fail, so agents can inspect and repair it.

### 4. Validation & Error Matrix

- Missing `source` input -> tool-level `VALIDATION_ERROR`.
- Source cannot be resolved at the tool boundary -> tool-level `SKILL_SOURCE_INVALID`.
- Source download/extract/digest preparation fails after runtime validation starts -> `valid=false`, issue code from `skillruntime.Error`.
- Digest mismatch -> `valid=false`, issue code `SKILL_DIGEST_MISMATCH`, stage `digest`.
- Manifest read/parse/manifest-schema failure -> `valid=false`, issue code `SKILL_MANIFEST_INVALID`, stage `manifest.read` or `manifest.parse`; omit `manifest` from tool response.
- Entrypoint missing, directory, incompatible platform/arch or package escape -> `valid=false`, issue from `ValidatePackageManifest`.
- No manifest/compat env declarations and `confirmed_no_env=false` -> `valid=false`, issue code `SKILL_MANIFEST_INVALID`, stage `manifest.env`, and `requires_no_env_confirm=true`.
- Declared command missing from `PATH` -> `valid=false`, issue code `SKILL_DEPENDENCY_MISSING`, stage `dependency`.

### 5. Good/Base/Bad Cases

- Good: local source with valid manifest, existing entrypoint, explicit env declarations and commands present returns `valid=true`, manifest, digest, env declarations, command checks and no issues.
- Base: source with no env declarations returns `valid=false` until caller passes `confirmed_no_env=true`.
- Bad: source with missing entrypoint and missing command returns `valid=false` with both package and dependency issues, without installing the package.

### 6. Tests Required

- Runtime/tool test for valid package asserting `valid=true`, digest exists, env declarations are returned and `skill_manage list` remains empty.
- Tool test for invalid package asserting structured issues include `manifest.entrypoint`, `manifest.env` and `dependency`.
- MCP schema test asserting `validate` is present in `skill_manage` action enum and output schema exposes `valid`, `source`, `digest`, `env`, `commands`, `issues`, `requires_no_env_confirm`.
- Full project gate: `make check`.

### 7. Wrong vs Correct

#### Wrong

```text
skill_manage action=install source=skill-sources/demo activate=false
```

Using install as a preflight still writes installed package state and can affect channel/version bookkeeping.

#### Correct

```text
skill_manage action=validate source=skill-sources/demo
```

Use validate for read-only install-time diagnostics, then install only after `valid=true` or after the user intentionally accepts remaining issues such as `confirmed_no_env`.
