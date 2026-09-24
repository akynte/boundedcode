// OpenCode 2.0.15 context hook. The Go ledger is authoritative; OpenCode's
// compaction summary is a disposable working note. Keep this adapter small so
// provider requests can be inspected against the matching OpenCode source tag.
import { appendFileSync, mkdirSync, readFileSync } from "node:fs"
import { join } from "node:path"

export default {
  id: "boundedcode.context",
  async setup(ctx) {
    await ctx.session.hook("prompt", async (event) => {
      if (!event.prompt.text) return
      const child = Bun.spawn(["bcode", "opencode", "record-prompt", "--session", String(event.sessionID), "--message", String(event.messageID)], {
        cwd: ctx.location.directory, stdin: "pipe", stdout: "pipe", stderr: "pipe", env: process.env,
      })
      child.stdin.write(event.prompt.text)
      child.stdin.end()
      const timeout = setTimeout(() => child.kill(), 3000)
      try {
        const [error, code] = await Promise.all([new Response(child.stderr).text(), child.exited])
        if (code !== 0) throw new Error(`BoundedCode could not preserve the raw user prompt: ${error.trim() || `exit ${code}`}`)
      } finally { clearTimeout(timeout) }
    })
    const taskState = async (sessionID) => {
      const command = ["bcode", "opencode", "context", "--session", String(sessionID)]
      const child = Bun.spawn(command, {
        cwd: ctx.location.directory,
        stdout: "pipe",
        stderr: "pipe", env: process.env,
      })
      const timeout = setTimeout(() => child.kill(), 3000)
      try {
        const [output, error, code] = await Promise.all([
          new Response(child.stdout).text(),
          new Response(child.stderr).text(),
          child.exited,
        ])
        if (code !== 0) {
          throw new Error(`BoundedCode task state unavailable: ${error.trim() || `exit ${code}`}`)
        }
        return output.trim()
      } finally {
        clearTimeout(timeout)
      }
    }
    await ctx.session.hook("context", async (event) => {
      if (event.model.providerID !== "boundedcode-local") return
      const state = await taskState(event.sessionID)
      if (state) event.system.push({ type: "text", text: state })
    })
    // Opt-in accounting observes the final OpenAI-compatible HTTP body, after
    // OpenCode has composed its own prompts, AGENTS, tools, and chat history.
    // Prism's exact tokenizer counts each category's serialized text. These
    // component counts are diagnostic; the chat template adds further tokens.
    await ctx.session.hook("http.request", async (event) => {
      if (process.env.BC_OPENCODE_BUDGET !== "1" || event.model.providerID !== "boundedcode-local" || event.kind !== "primary") return
      try {
        const request = await event.request.clone().json()
        const origin = new URL(event.request.url)
        if (!(["127.0.0.1", "localhost", "[::1]"].includes(origin.hostname))) return
        const groups = {
          open_code_system: [], agents_instructions: [], mcp_schemas: [], builtin_schemas: [],
          boundedcode_context: [], conversation_tail: [], tool_results: [],
          retrieved_evidence: [], compaction_checkpoint: [],
        }
        const rendered = (value) => typeof value === "string" ? value : JSON.stringify(value ?? "")
        const classifySystem = (content) => {
          const taskAt = content.indexOf("BoundedCode authoritative task state")
          const base = taskAt >= 0 ? content.slice(0, taskAt) : content
          if (taskAt >= 0) groups.boundedcode_context.push(content.slice(taskAt))
          const agentsAt = base.indexOf("Project intelligence (BoundedCode)")
          if (agentsAt >= 0) {
            groups.open_code_system.push(base.slice(0, agentsAt))
            groups.agents_instructions.push(base.slice(agentsAt))
          } else groups.open_code_system.push(base)
        }
        for (const message of request.messages ?? []) {
          const content = rendered(message.content)
          if (message.role === "system" || message.role === "developer") classifySystem(content)
          else if (content.includes("BoundedCode authoritative task state")) groups.boundedcode_context.push(content)
          else if (content.includes("BoundedCode supervised task") && content.includes("continues")) groups.compaction_checkpoint.push(content)
          else if (message.role === "tool" && (content.includes("source=file") || content.includes("evidence="))) groups.retrieved_evidence.push(content)
          else if (message.role === "tool") groups.tool_results.push(content)
          else groups.conversation_tail.push(JSON.stringify(message))
        }
        for (const tool of request.tools ?? []) {
          const name = tool.function?.name ?? tool.name ?? ""
          groups[name.startsWith("boundedcode_") ? "mcp_schemas" : "builtin_schemas"].push(JSON.stringify(tool))
        }
        const tokenize = async (value) => {
          if (!value) return 0
          const response = await fetch(`${origin.origin}/tokenize`, {
            method: "POST", headers: { "content-type": "application/json" },
            body: JSON.stringify({ content: value, add_special: false, parse_special: true }),
          })
          if (!response.ok) throw new Error(`Prism tokenizer returned ${response.status}`)
          const body = await response.json()
          if (!Array.isArray(body.tokens)) throw new Error("Prism tokenizer returned no tokens")
          return body.tokens.length
        }
        const entries = Object.entries(groups)
        const counts = Object.fromEntries(await Promise.all(entries.map(async ([name, texts]) => [name, await tokenize(texts.join("\n"))])))
        const totalJSON = await tokenize(JSON.stringify(request))
        const known = Object.values(counts).reduce((a, b) => a + b, 0)
        let declared = { context: null, output: null, buffer: null, keep: null }
        try {
          const cfg = JSON.parse(readFileSync(join(ctx.location.directory, "opencode.json"), "utf8"))
          const limit = cfg.providers?.[event.model.providerID]?.models?.[event.model.id]?.limit
          declared = { context: limit?.context ?? null, output: limit?.output ?? null,
            buffer: cfg.compaction?.buffer ?? null, keep: cfg.compaction?.keep?.tokens ?? null }
        } catch {}
        const record = {
          at: new Date().toISOString(), session_id: event.sessionID, model: request.model,
          tokenizer: "Prism /tokenize", categories: counts, full_request_json_tokens: totalJSON,
          serialization_difference: totalJSON - known,
          output_reserve: request.max_tokens ?? request.max_completion_tokens ?? declared.output,
          advertised_context: declared.context, compaction_buffer: declared.buffer,
          retained_tail: declared.keep,
          estimated_preflight_threshold: declared.context != null && declared.buffer != null ? declared.context - declared.buffer : null,
          note: "Component counts use the model tokenizer; chat-template and provider serialization are not exact per-category allocations.",
        }
        const dir = join(process.env.XDG_STATE_HOME ?? process.env.HOME ?? ".", "opencode")
        mkdirSync(dir, { recursive: true, mode: 0o700 })
        appendFileSync(join(dir, "boundedcode-budget.jsonl"), `${JSON.stringify(record)}\n`, { mode: 0o600 })
      } catch (error) {
        console.error(`BoundedCode request accounting failed: ${error}`)
      }
    })
    await ctx.session.hook("compaction", async (event) => {
      if (event.model.providerID !== "boundedcode-local") return
      const state = await taskState(event.sessionID)
      const taskID = state.match(/BoundedCode authoritative task state \(task ([^;]+);/i)?.[1]
      if (!taskID) return
      // The primary context hook restores the ledger card. OpenCode keeps a
      // short recent tail, so this checkpoint need not recursively summarize
      // earlier model-written summaries or submit a huge summary request.
      event.result = {
        summary: `BoundedCode supervised task ${taskID} continues. Its original objective, recorded user requirements, decisions, changed files and verification state are reconstructed from the durable ledger and checkout in the next request. Re-read repository evidence as needed.`,
      }
    })
  },
}
