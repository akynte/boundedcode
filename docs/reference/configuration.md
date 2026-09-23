# Configuration reference

Configuration lives under `$BC_DATA/config` (`/data/config` when `BC_DATA` is
unset). Run `bcode config init` to create initial files and `bcode config show` to
inspect effective settings. Initialization does not install weights or select
the Bonsai profile: use the [reference install](../how-to/install.md).

| File | Responsibility |
|---|---|
| `bcode.yaml` | Inference lifecycle, profile, API, sandbox, index and egress |
| `providers.yaml` | Inference endpoint and model identifier |
| `profiles/*.yaml` | Optional overrides of embedded hardware profiles |
| `judgment.yaml` | Hosted Jev decision plane. Required: a task run refuses to start without a usable one |

## `bcode.yaml`

Minimal reference override; omitted fields retain their defaults:

```yaml
profile: bonsai-2-27b-8gb-cuda
inference:
  mode: external
  base_url: http://127.0.0.1:8080
api:
  addr: 127.0.0.1:7777
sandbox:
  mode: auto
egress:
  enabled: false
```

Key settings, from [the loader](https://github.com/akynte/boundedcode/blob/main/internal/config/config.go):

| Setting | Meaning / actual default |
|---|---|
| `profile` | Fresh config still names the older `reference-8gb-cuda-64gb-ram`; explicitly select Bonsai |
| `inference.mode` | `none` by default; `external` connects to an operator-owned server; `embedded` lets `bcode api` start one |
| `inference.binary`, `args` | Embedded server executable and appended CLI arguments |
| `inference.port` | 8080; not the status API port |
| `inference.start_timeout_seconds` | 300; cold model load deadline |
| `api.addr` | `127.0.0.1:7777`; no authentication |
| `api.shutdown_grace_seconds` | 30; coordinate with an outer process manager |
| `sandbox.mode` | `auto`, `landlock`, `bwrap`, or container fallback `none` |
| `sandbox.read_only_paths` | Toolchain/library paths child commands may read; include your actual Go installation |
| `sandbox.allowed_tcp_connect` | Explicit additional TCP ports; not an HTTP/host allowlist |
| `index.max_file_bytes` | 1,048,576 |
| `index.chunk_lines` | 60 |
| `index.watch_enabled` | true; watcher requires the service lifecycle, not just a one-shot index |
| `index.excludes` | Repository walk exclusions |
| `gates` | Defaults enable breaking-change, out-of-scope and apply gates; plan gate is off |
| `oracle.dir` | Empty by default. Absolute directory of [hidden acceptance checks](../how-to/add-hidden-acceptance-checks.md), outside every repository; `--oracle` overrides it |
| `oracle.feedback_rounds` | 3. Distinct failing candidates a task's model may learn hidden verdicts about; the next one ends the task |

For an embedded server, the profile contributes runtime flags and `inference.args`
are appended. Explicit arguments must match the measured profile. Do not apply
the old CPU-MoE flags to Bonsai.

### Environment overrides

`BC_DATA` chooses the data directory. `BC_API_ADDR`, `BC_PROFILE`,
`BC_INFERENCE_MODE` and `BC_INFERENCE_BASE_URL` override their
file settings. An old exported value can explain why editing YAML has no effect.

### Provisioning

`egress.enabled` enables separate dependency/documentation proxy lanes on
`egress.deps_port` (7780) and `egress.docs_port` (7781). Allowlist rules carry
`host`, optional `lanes`, and a required `why`. Exact hosts and a leading
`*.` wildcard are accepted; bare `*` is rejected. Proxy destinations are
restricted to ports 80/443. This does not remove non-proxy network routes.

See [dependency provisioning](../how-to/fetch-dependencies.md) and
[network limits](../explanation/isolation-model.md#network-limitations).

## `providers.yaml`

The reference server is launched with `--alias boundedcode-bonsai`:

```yaml
default: local
providers:
  - name: local
    kind: llamacpp
    base_url: http://127.0.0.1:8080
    model: boundedcode-bonsai
    timeout_seconds: 900
```

All generative roles resolve to this provider: role names divide responsibility,
not GPU residency. Native verification is executed by Go/recipes, not delegated
to a second “verification LLM.”

The adapter supports additional configuration for internal experiments, including
role routing, explicit capabilities and a checksum-pinned managed process. These
are advanced extension surfaces, not a different recommended model stack.
`api_key_env` names an environment variable; never store its secret value in YAML.
See [provider implementation](../how-to/add-a-provider.md).

## Profiles

The [Bonsai profile](https://github.com/akynte/boundedcode/blob/main/profiles/bonsai-2-27b-8gb-cuda.yaml) is embedded in
the binary. A same-named YAML file in `$BC_DATA/config/profiles` overrides it.
Profiles carry context and phase budgets, output reserves, tool caps, sampling,
and llama.cpp arguments.

`max_packet_tokens + reserved_output_tokens` must not exceed
`context_tokens`. The configured server window must agree with the profile.

The Bonsai profile has 32,768 context tokens, a 12,000-token packet cap,
8,192 reserved output tokens, Q8 KV caches and one intended inference slot.
Its measured-peak fields are zero: they are not measurements.
`max_concurrent_tasks` is not a machine-wide GPU scheduler.

The benchmark-generated profile is a heuristic starting point and does not
preserve all Bonsai runtime/phase settings. Keep the shipped profile intact until
a measured override has been reviewed. See [profile tuning](../how-to/choose-a-profile.md).
