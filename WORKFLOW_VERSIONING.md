# Workflow Versioning & Deterministic Replay (Fluxo)

## Goals

1. **Versioned workflow definitions**
    - Multiple versions of the same workflow name can coexist (e.g. `order@v1`, `order@v2`).
2. **Deterministic replay guarantee**
    - A workflow instance must always resume using **the same workflow version** and **the same workflow fingerprint**
      it started with.
3. **Safe deploys**
    - Deploying new code must not silently change the behavior of already-started workflow instances.

## Terminology

- **Workflow Name**: Logical identifier, e.g. `order-approval`.
- **Workflow Version**: Opaque string, e.g. `v1`, `2025-12-16`, `1.0.3`.
- **Fingerprint (Definition Hash)**: A stable hash derived from the workflow definition’s deterministic metadata.
    - Includes: workflow name, version, step names, retry policy configuration, and other deterministic config fields.
    - Excludes: function pointers / closures (non-serializable, non-hashable).

## API Expectations

### Defining a workflow version

`WorkflowDefinition` adds:

- `Version string` (default: `"v1"` if empty)
- `Fingerprint string` (optional; if empty Fluxo computes it)

### Registering workflows

- `RegisterWorkflow(def)` registers `(def.Name, def.Version)`.
- Duplicate registration of the same `(name, version)` is an error.

### Running workflows

- `Run(ctx, name, input)`:
    - If exactly one version of `name` is registered, run it.
    - If multiple versions are registered, return an error telling the caller to use `RunVersion`.
- `RunVersion(ctx, name, version, input)` runs a specific version.

## Replay / Resume Rules

When an instance starts, Fluxo persists:

- `workflow_name`
- `workflow_version`
- `workflow_fingerprint`

When resuming (e.g., `GetInstance`, `Signal`, worker resume, crash recovery):

1. Look up the workflow definition by `(name, version)`.
2. Compute fingerprint for the registered definition.
3. Compare to the instance’s persisted fingerprint.
    - If mismatch: fail fast with `ErrWorkflowDefinitionMismatch`.
    - Rationale: continuing would violate determinism.

## Migration / Compatibility

- Existing persisted instances without version/fingerprint are treated as:
    - `workflow_version = "v1"`
    - `workflow_fingerprint = ""` (unknown legacy)
- Engines may optionally support a “strict mode”:
    - Strict mode forbids resuming instances with empty fingerprint.

## Non-Goals (for this milestone)

- Automatic workflow definition migration between versions
- Compatibility layers or step-by-step schema migration DSL
- Distributed workflow execution

## Testing Strategy

Must cover:

1. Multiple versions register & resolve correctly
2. Instances persist `(name, version, fingerprint)` on start
3. Resume uses persisted version even if newer version exists
4. Resume fails with mismatch if definition changed for same `(name, version)`
