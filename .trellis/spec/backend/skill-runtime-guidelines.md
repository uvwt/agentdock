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
