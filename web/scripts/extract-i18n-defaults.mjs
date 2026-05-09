// One-shot extractor — round 2 of the i18n migration.
//
// Walks web/src/, finds every `t("key", { ..., defaultValue: "..." })`
// call with a STATIC string key + STATIC string defaultValue, and
// adds the key to es.json + en.json under the dotted path. Same
// rules as round 1 (commit a00bd9d):
//
//   - If the key is already present in es.json, DO NOT overwrite —
//     operator-curated copy wins.
//   - en.json receives the same value as a Spanish placeholder so
//     the i18n machinery resolves the key in either locale (a
//     translator can swap later in one PR).
//   - Skips keys with template literals, conditionals, or any
//     non-string-literal defaultValue. Those are surfaced in the
//     `skipped` summary.
//
// The defaultValue arg stays in code as belt-and-braces. We only
// touch the locale JSON files.
//
// Usage:  node scripts/extract-i18n-defaults.mjs

import fs from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";

const __dirname = path.dirname(fileURLToPath(import.meta.url));
const WEB_ROOT = path.resolve(__dirname, "..");
const SRC_DIR = path.join(WEB_ROOT, "src");
const ES_PATH = path.join(SRC_DIR, "i18n", "locales", "es.json");
const EN_PATH = path.join(SRC_DIR, "i18n", "locales", "en.json");

function walk(dir) {
  const out = [];
  for (const entry of fs.readdirSync(dir, { withFileTypes: true })) {
    const full = path.join(dir, entry.name);
    if (entry.isDirectory()) {
      out.push(...walk(full));
    } else if (/\.(t|j)sx?$/.test(entry.name)) {
      out.push(full);
    }
  }
  return out;
}

// Match: t("key.path", { ...optional stuff..., defaultValue: "value" }, ...)
// or with single quotes, and tolerating whitespace/newlines.
//
// Capturing groups:
//   1 = quote char around key (' or ")
//   2 = key
//   3 = quote char around defaultValue
//   4 = defaultValue
//
// We do NOT match template literals (backticks) — those are dynamic.
const T_REGEX =
  /\bt\(\s*(['"])([^'"]+)\1\s*,\s*\{[^}]*?defaultValue\s*:\s*(['"])((?:\\.|(?!\3).)*)\3[^}]*?\}/g;

function extractFromFile(filePath) {
  const src = fs.readFileSync(filePath, "utf8");
  const found = [];
  // Multi-line scan — regex is single-line but JS strings include \n,
  // and `[^}]*?` handles intermediate newlines. We don't run with
  // /s flag because we want `[^}]` to also exclude `}`, and the
  // engine already lets `.` match `\n` when the class allows it.
  let m;
  while ((m = T_REGEX.exec(src)) !== null) {
    const key = m[2];
    const value = m[4]
      // unescape \n, \", \\, \', \t for the parsed string
      .replace(/\\n/g, "\n")
      .replace(/\\t/g, "\t")
      .replace(/\\(['"\\])/g, "$1");
    found.push({ key, value, file: path.relative(WEB_ROOT, filePath) });
  }
  return found;
}

function getNested(obj, dotted) {
  const parts = dotted.split(".");
  let cur = obj;
  for (const p of parts) {
    if (cur === null || typeof cur !== "object" || !(p in cur)) return undefined;
    cur = cur[p];
  }
  return cur;
}

function setNested(obj, dotted, value) {
  const parts = dotted.split(".");
  let cur = obj;
  for (let i = 0; i < parts.length - 1; i++) {
    const p = parts[i];
    if (
      cur[p] === undefined ||
      cur[p] === null ||
      typeof cur[p] !== "object"
    ) {
      cur[p] = {};
    }
    cur = cur[p];
  }
  const last = parts[parts.length - 1];
  if (last in cur) return false; // don't overwrite
  cur[last] = value;
  return true;
}

function main() {
  const files = walk(SRC_DIR).filter(
    (f) =>
      !f.includes(`${path.sep}node_modules${path.sep}`) &&
      !/\.test\.[tj]sx?$/.test(f) &&
      !f.endsWith(".d.ts"),
  );

  const allFound = [];
  for (const f of files) {
    allFound.push(...extractFromFile(f));
  }

  // Dedupe by key — first occurrence wins. If the same key shows up
  // with different defaultValue strings, log it.
  const byKey = new Map();
  const conflicts = [];
  for (const item of allFound) {
    if (!byKey.has(item.key)) {
      byKey.set(item.key, item);
    } else {
      const existing = byKey.get(item.key);
      if (existing.value !== item.value) {
        conflicts.push({
          key: item.key,
          first: { value: existing.value, file: existing.file },
          second: { value: item.value, file: item.file },
        });
      }
    }
  }

  const es = JSON.parse(fs.readFileSync(ES_PATH, "utf8"));
  const en = JSON.parse(fs.readFileSync(EN_PATH, "utf8"));

  const addedEs = [];
  const addedEn = [];
  const alreadyPresent = [];
  for (const [key, item] of byKey) {
    const presentEs = getNested(es, key) !== undefined;
    const presentEn = getNested(en, key) !== undefined;
    if (presentEs && presentEn) {
      alreadyPresent.push(key);
      continue;
    }
    if (!presentEs) {
      setNested(es, key, item.value);
      addedEs.push(key);
    }
    if (!presentEn) {
      setNested(en, key, item.value);
      addedEn.push(key);
    }
  }

  fs.writeFileSync(ES_PATH, JSON.stringify(es, null, 2) + "\n");
  fs.writeFileSync(EN_PATH, JSON.stringify(en, null, 2) + "\n");

  console.log(`Total t() calls with static defaultValue: ${allFound.length}`);
  console.log(`Unique keys: ${byKey.size}`);
  console.log(`Already present in both locales: ${alreadyPresent.length}`);
  console.log(`Added to es.json: ${addedEs.length}`);
  console.log(`Added to en.json: ${addedEn.length}`);
  if (conflicts.length > 0) {
    console.log(`\nConflicts (same key, different defaultValue):`);
    for (const c of conflicts) {
      console.log(`  ${c.key}`);
      console.log(`    first  (${c.first.file}): ${JSON.stringify(c.first.value)}`);
      console.log(`    second (${c.second.file}): ${JSON.stringify(c.second.value)}`);
    }
  }
}

main();
