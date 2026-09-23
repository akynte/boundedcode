# Configure the reference inference stack

For a new installation use [Install the 8 GB reference](install.md), which
includes the exact model, runtime and server command. This page explains how
those settings connect.

## Two files, one server

`$BC_DATA/config/bcode.yaml` selects lifecycle and hardware budgets:

```yaml
profile: bonsai-2-27b-8gb-cuda
inference:
  mode: external
  base_url: http://127.0.0.1:8080
  port: 8080
```

`$BC_DATA/config/providers.yaml` selects the endpoint used for requests:

```yaml
default: local
providers:
  - name: local
    kind: llamacpp
    base_url: http://127.0.0.1:8080
    model: boundedcode-bonsai
    timeout_seconds: 900
roles: {}
```

Start the server with `--alias boundedcode-bonsai`. The single-provider file
above routes every generator role to one model. That is the reference
configuration: Bonsai stays resident and answers localization, planning,
editing and review.

`roles:` can send a role to a different provider, for example to try another
model for EDIT:

```yaml
roles:
    coding: my-editor
```

Each provider named there must be declared under `providers:`. On an 8 GB card
that holds one model at a time, two generators have to be managed providers
sharing one process slot, and every phase that changes model pays a model load.
A split like this is a new configuration: measure it before relying on it.
Verification commands execute in code; a role named `verification` is not what
runs the test suite.

External mode does not apply runtime flags to an already running server.
Keep its context, KV, thread and placement flags consistent with the profile.
[Reference profile](https://github.com/akynte/boundedcode/blob/main/profiles/bonsai-2-27b-8gb-cuda.yaml).

## Embedded mode

An advanced deployment can set `mode: embedded`, an absolute
`inference.binary` pointing to the **Prism** `llama-server`, and
`inference.model` to an absolute GGUF path or filename under
`$BC_DATA/models`. Set `inference.args: ["--parallel", "1", "--jinja",
"--alias", "boundedcode-bonsai"]`.

`bcode api` owns that child, derives runtime flags from the active profile, waits
for health, and restarts it within its process-manager budget. Operator arguments
are appended after profile flags. The default generated mode is `none`, so
initialization alone does not load a model.

## Check the configuration

```bash
bcode config show
bcode models health
bcode models conformance
```

Conformance makes real inference requests and may take minutes. It checks the
API contract; passing it does not establish coding quality.

`bcode models bench --json` records throughput. Read
[measurement caveats](choose-a-profile.md) before using `--write` to create a
profile; the generated profile does not retain all Bonsai-specific settings.

## Optional components

Jev is configured separately in `judgment.yaml`, not through generator role
routing. It is hosted, and it is required: a task run refuses to start without
a usable one.
[Use judgments](use-judgments.md).

The CPU embedding service is an experimental evaluation control, not required
for ordinary tasks. [Reference stack](../reference/model-stack.md).

Provider interfaces and explicit role overrides remain available to contributors.
Changing them creates a different setup requiring its own runtime and task
validation. [Provider implementation](add-a-provider.md).
