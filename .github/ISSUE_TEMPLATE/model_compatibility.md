---
name: Model compatibility report
about: How a local model behaved with BoundedCode on your hardware
labels: model-compatibility
---

**Model** (upstream repository and revision):

**Quantization / file**:

**llama.cpp version** (`llama-server --version`):

**Hardware** (CPU, GPU and VRAM, RAM):

**Context size and server settings** (or the profile YAML):

**Speed** — decode tokens/s at short and long context (`boundedcode bench infra` output if available):

**Tool-call reliability** — malformed tool calls, loops, thinking runaways:

**Tasks tried and outcomes** (TASK_VERIFIED / tests_green / failed):

**Observed failures**

<!-- Do not attach model weights. Remove private paths and proprietary code from logs. -->
