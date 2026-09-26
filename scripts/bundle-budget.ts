// First-load JS and CSS of every prerendered shell, gzipped, against a
// budget. Each *.html lists its own <script type="module">,
// <link rel="modulepreload"> and <link rel="stylesheet">, so the sum of what
// they reference is what a visitor pays for on that route.
//
//   bun scripts/bundle-budget.ts <dist> [budgetKB]
import { gzipSync } from "node:zlib";
import { readdirSync, readFileSync } from "node:fs";
import { dirname, join } from "node:path";

const dist = process.argv[2] ?? "apps/web/dist";
const budgetKB = Number(process.argv[3] ?? 120);

const REF = /(?:src|href)="([^"]+\.(?:js|css))"/g;

// References are resolved as a browser would: absolute against the site root
// (dist), relative against the shell's own directory.
function firstLoad(html: string, shellDir: string): { bytes: number; files: number } {
  const seen = new Set<string>();
  for (const m of html.matchAll(REF)) {
    const ref = m[1]!;
    seen.add(ref.startsWith("/") ? join(dist, ref) : join(dist, shellDir, ref));
  }
  let bytes = 0;
  for (const path of seen) {
    try {
      bytes += gzipSync(readFileSync(path)).length;
    } catch {
      // A reference to something outside dist is not a first-load cost.
    }
  }
  return { bytes, files: seen.size };
}

// Recursive: a nested route prerenders to a nested shell.
const shells = readdirSync(dist, { recursive: true })
  .map(String)
  .filter((f) => f.endsWith(".html"));
if (shells.length === 0) {
  console.error(`no .html shells in ${dist}; run the build first`);
  process.exit(1);
}

let worst = { name: "", kb: 0 };
const over: string[] = [];
console.log(`first load, gzipped (budget ${budgetKB} kB)\n`);
for (const shell of shells.sort()) {
  const { bytes, files } = firstLoad(readFileSync(join(dist, shell), "utf8"), dirname(shell));
  const kb = bytes / 1024;
  if (kb > worst.kb) worst = { name: shell, kb };
  if (kb > budgetKB) over.push(shell);
  console.log(
    `  ${shell.padEnd(22)} ${kb.toFixed(1).padStart(7)} kB  ${String(files).padStart(3)} files  ${kb > budgetKB ? "OVER" : "ok"}`,
  );
}

console.log(`\nworst: ${worst.name} at ${worst.kb.toFixed(1)} kB`);
if (over.length > 0) {
  console.error(
    `${over.join(", ")} over budget; \`cd apps/web && bunx vite-bundle-visualizer\` shows what is in it`,
  );
  process.exit(1);
}
