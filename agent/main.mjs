import { appendFile, mkdir, mkdtemp, writeFile } from "node:fs/promises";
import { join } from "node:path";
import { tmpdir } from "node:os";
import { inputs, runAgent } from "./runner.mjs";

const temp = process.env.RUNNER_TEMP || tmpdir();
await mkdir(temp, { recursive: true });
const outputDir = await mkdtemp(join(temp, "directions-result-"));
if (process.env.GITHUB_OUTPUT) await appendFile(process.env.GITHUB_OUTPUT,
  `artifact-directory=${outputDir}\nresponse-file=${join(outputDir, "response.md")}\nrun-file=${join(outputDir, "run.json")}\n`);
let hardLimit;
try {
  const config = inputs(process.env);
  // A trusted extension can ignore cancellation. Bound the action process too.
  hardLimit = setTimeout(() => {
    console.error("Directions agent exceeded its hard cleanup deadline.");
    process.exit(1);
  }, config.timeoutMs + 15000);
  await runAgent(config, outputDir);
  console.log("Directions agent completed. Results are available in the output artifacts.");
} catch {
  console.error("Directions agent failed. Inspect run.json when available; no provider payloads or credentials are logged.");
  try {
    const { access } = await import("node:fs/promises");
    await access(join(outputDir, "run.json"));
  } catch {
    await writeFile(join(outputDir, "run.json"), JSON.stringify({ status: "failed", error: "Invalid action configuration or setup failed" }), { mode: 0o600 });
  }
  process.exitCode = 1;
} finally { clearTimeout(hardLimit); }
