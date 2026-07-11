---
name: code-debrief
description: Explain and teach the core implementation produced by vibecoding from real code, focusing on the main call chain, core code, state and data flow, design reasoning, and transferable engineering knowledge. Use when the user asks to explain, review, learn, debrief, or understand recent code changes, a commit, PR, diff, feature, module, or code path.
version: 0.1.2
---

Read the real code before explaining it. Do not guess paths, behavior, dependencies, runtime order, or design intent. If a fact can be established from the codebase, Git diff, configuration, or minimal runtime observation, inspect it instead of asking the user.

The goal is learning: help the user understand and retain control of code produced through vibecoding. Start from the current project and then extract transferable engineering knowledge. Do not turn the session into a generic code review, refactoring backlog, test audit, or project-management report.

## Scope

Determine the learning scope in this order:

1. An explicitly supplied PR, commit, commit range, diff, file, directory, symbol, or feature.
2. Current staged changes.
3. Current unstaged changes.
4. The most recent relevant commit.
5. The user's natural-language target.

Use the selected change as the center, then inspect only the upstream and downstream code needed to explain it: entry points, direct callers, direct dependencies, important types, data/state transitions, configuration that changes behavior, and critical failure paths.

Do not mechanically explain the whole repository or every changed file.

## Default interaction

Default to continuous explanation. Do not force questions after every section.

Only ask a single optional learning question when it materially helps verify a core mental model, and let the user skip it. If the user asks to be quizzed or taught interactively, switch to learning mode and ask one question at a time.

## Explanation order

Use this flexible learning skeleton, adapting the order to the code type and real execution path:

1. What problem or capability this code implements.
2. Where execution enters.
3. The main call chain or state flow.
4. The smallest set of core code that makes the behavior work.
5. Inputs, outputs, side effects, and state/data changes.
6. Important failure or alternate paths.
7. Why the code is organized this way.
8. The general engineering idea that can be transferred elsewhere.
9. Where to start when the user later wants to modify the behavior.

Start with the whole mental model, then deepen the core path. Explain by responsibility and logical block, not by repeating syntax line by line. Use line-by-line explanation only for genuinely difficult conditions, concurrency, state machines, data transformations, or implicit language/framework behavior.

## Evidence and code presentation

Ground important claims in concrete file paths and symbols. Prefer `path + symbol`, adding line numbers when stable and useful.

Show core code in fenced code blocks. Quote the minimum real source needed to support the explanation, usually one to three focused snippets per concept. Do not paste entire files.

When behavior changed, a before/after comparison is important, or a refactor changed responsibility, use a fenced `diff` block where possible.

Real source is evidence. Simplified code or pseudocode may be added for teaching, but must be explicitly labelled as a simplified model and never presented as the real implementation.

For complex cross-module flows, use Mermaid based only on verified code:

- `sequenceDiagram` for request, async, frontend/backend, or service call chains.
- `flowchart` for branching logic and failure paths.
- `stateDiagram-v2` for state transitions and lifecycles.
- `classDiagram` only when type/module relationships are central.

Each Mermaid diagram should explain one core idea and must be followed by code-based explanation. Do not invent missing components to make a diagram look complete.

## Facts, intent, and design knowledge

Keep these distinct:

- **Code fact**: behavior directly established by current code or minimal runtime verification.
- **Design interpretation**: a reason supported by structure, history, comments, or surrounding constraints.
- **Possible intent**: a plausible explanation not explicitly confirmed.

Never state inferred intent as fact.

Explain the real code first, then name a design principle or pattern only when the name improves learning. Do not force patterns onto the code. Bind every named concept to the exact code that demonstrates it.

When useful, briefly compare a common alternative implementation to reveal the design trade-off, but keep the focus on understanding rather than recommending a rewrite.

## Boundaries

By default, do not:

- analyze or teach test code;
- generate coverage reports or testing plans;
- prioritize refactors or produce a remediation roadmap;
- modify code;
- audit the whole codebase;
- provide a full tutorial for a third-party framework or library;
- expand unrelated infrastructure, deployment, or operations details.

Tests may be mentioned only when the user explicitly requests them or when one tiny test excerpt is the only clear evidence for an otherwise hidden behavioral boundary.

Third-party libraries should be explained only to the depth required to understand their role, inputs, outputs, control-flow effects, or state behavior in this project.

Static code reading is the default. Use minimal runtime observation only when static code cannot establish actual order or behavior, such as dynamic registration, plugin loading, runtime configuration, middleware order, event systems, or state restoration. Runtime observation is for accuracy, not for turning the task into testing or debugging.

## Depth

Infer a sensible depth unless the user specifies one:

- `quick`: goal, entry point, main call chain, and core code.
- `core`: also include state/data flow, critical alternate paths, and design reasoning.
- `deep`: also include difficult internals, important alternatives, and transferable architecture knowledge.

Prefer `core` for a complete feature. Keep simple changes short.

## Output style

Prefer an on-the-code walkthrough over a detached report. Use clear headings, concise prose, real code blocks, and Mermaid only when it improves the mental model.

End with a compact learning recap containing:

- the core mental model;
- the most important code locations;
- the main design idea learned;
- an optional single exercise based on the real code.

A full persistent document is optional and should be produced only when requested.
