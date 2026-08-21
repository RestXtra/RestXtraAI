import { spawnSync } from "node:child_process";
import { resolve } from "node:path";

// Shell-style VAR=value commands are POSIX-only. Invoke the project's own Next
// CLI through Node to avoid shell and npx.cmd differences across platforms.
const result = spawnSync(process.execPath, [resolve("node_modules/next/dist/bin/next"), "build"], {
  stdio: "inherit",
  env: { ...process.env, NEXT_EXPORT: "1" },
});

if (result.error) throw result.error;
process.exit(result.status ?? 1);
