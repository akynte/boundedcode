# Measure and tune the 8 GB profile

Use `profile: bonsai-2-27b-8gb-cuda` for the reference installation.
Its context and runtime settings are documented in the
[8 GB runtime guide](../explanation/8gb-runtime.md).

The setup TUI selects the reference profile for the detected hardware. Other
embedded profiles remain experimental starting configurations; their names are
not proof of measured support.

<!-- test:run -->
```console
$ bcode config profiles
…
* is the active profile. Shipped profiles are starting points, not measurements:
…
```

On-disk profiles under `$BC_DATA/config/profiles/` override embedded profiles
with the same name. `bcode config show` prints the effective values.

## Measure without changing the profile

```bash
bcode models bench --iterations 3 --prompt-tokens 2000 --output-tokens 256 --json
```

This sends real synthetic requests. Prefer an idle inference server and record
its flags, model hash, runtime revision and other GPU users.

The implementation uses server phase timings when present and labels estimates
when they are absent. It measures cache reuse across a stable synthetic prefix.
Its `ttft_*` fields are currently request duration, its RAM field is the client
process's RSS, and its GPU field is a whole-device post-request sample.
None is a complete task resource profile.

## Saving measurements

```bash
bcode models bench --context 32768 --profile-name measured-bonsai --write
```

This writes a proposed profile; it does not activate it. Do not combine
`--json` and `--write`: the current CLI returns after printing JSON.

The proposed settings are heuristic and do not preserve the reference's phase
budgets or every runtime knob. Compare with
[the shipped Bonsai profile](https://github.com/akynte/boundedcode/blob/main/profiles/bonsai-2-27b-8gb-cuda.yaml), merge
the measured fields intentionally, and inspect the resulting configuration
before switching `bcode.yaml`.

`bcode doctor` compares recorded memory with host capacity, but this is diagnostic,
not live allocation control. `max_concurrent_tasks` is metadata; the runtime
does not enforce a GPU admission queue from it. Keep one generation workload
active on the reference card.

## Useful tuning boundaries

- `context_tokens` must agree with the server's per-slot window.
- `max_packet_tokens + reserved_output_tokens` must fit that window.
- Structured `phase_budgets` separately reserve context and output.
- `runtime` settings affect embedded startup; external servers need equivalent
  command-line changes.
- A larger packet can increase prefill cost without fixing a retrieval miss.
  Inspect `bcode graph search "your symbol" --json` before increasing it.
