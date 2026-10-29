import type { ExtensionAPI } from "@earendil-works/pi-coding-agent";

interface DirectionsContext {
  root: string;
  content: string;
  warnings: string[];
}

function parseContext(stdout: string): DirectionsContext {
  const value: unknown = JSON.parse(stdout);
  if (
    !value || typeof value !== "object" ||
    typeof (value as DirectionsContext).root !== "string" ||
    typeof (value as DirectionsContext).content !== "string" ||
    !Array.isArray((value as DirectionsContext).warnings) ||
    !(value as DirectionsContext).warnings.every((warning) => typeof warning === "string") ||
    Buffer.byteLength((value as DirectionsContext).content, "utf8") > 64 * 1024
  ) throw new Error("Invalid response from directions context");
  return value as DirectionsContext;
}

/** Context only: native AGENTS discovery and host runtime settings are untouched. */
export default function directions(pi: ExtensionAPI) {
  pi.registerFlag("directions-bin", {
    description: "Trusted Directions executable (default: directions on host PATH)",
    type: "string",
    default: "directions",
  });

  pi.registerFlag("directions-allow-urls", {
    description: "Whitespace-separated exact HTTPS guidance references approved by the host",
    type: "string",
    default: "",
  });

  let loaded: DirectionsContext | undefined;
  let lastWarning = "";

  async function refresh(ctx: { cwd: string; hasUI: boolean; ui: { notify(message: string, type: "warning"): void } }) {
    // Clear before attempting a refresh; failed reads must not retain stale rules.
    loaded = undefined;
    try {
      const binary = pi.getFlag("directions-bin");
      const approved = pi.getFlag("directions-allow-urls");
      const args = ["context", ctx.cwd];
      if (typeof approved === "string") for (const url of approved.split(/\s+/).filter(Boolean)) args.push("--allow-url", url);
      const result = await pi.exec(typeof binary === "string" ? binary : "directions", args, {
        cwd: ctx.cwd,
        timeout: 10_000,
      });
      if (result.code !== 0) throw new Error(result.stderr.trim() || "directions context failed");
      loaded = parseContext(result.stdout);
      if (loaded.warnings.length) warn(loaded.warnings.join("\n"), ctx);
      else lastWarning = "";
    } catch (error) {
      const message = error instanceof Error ? error.message : String(error);
      loaded = {
        root: "",
        content: "Directions context could not be loaded. Do not assume repository policies were checked.\n" + message.slice(0, 1000),
        warnings: [],
      };
      warn("Directions: " + message.slice(0, 1000), ctx);
    }
  }

  function warn(message: string, ctx: { hasUI: boolean; ui: { notify(message: string, type: "warning"): void } }) {
    if (ctx.hasUI && message !== lastWarning) ctx.ui.notify(message, "warning");
    lastWarning = message;
  }

  pi.on("session_start", async (_event, ctx) => {
    lastWarning = "";
    await refresh(ctx);
  });

  pi.on("before_agent_start", async (event, ctx) => {
    // Fresh config and diff once per prompt, not per tool turn. This also
    // reapplies after /new, /resume, /reload and compaction without saved messages.
    await refresh(ctx);
    const notes = loaded?.warnings.length ? "\n\nDirections adapter notes:\n" + loaded.warnings.join("\n") : "";
    if (loaded?.content) event.systemPromptOptions.sections.directions = loaded.content + notes;
    else delete event.systemPromptOptions.sections.directions;
  });
}
