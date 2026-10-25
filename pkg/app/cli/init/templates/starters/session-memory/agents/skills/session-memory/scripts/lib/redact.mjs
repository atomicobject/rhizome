// Redaction runs on full text before anything is shortened, written, or sent.
// It replaces credential-shaped values; it cannot recognize arbitrary private
// details, which is why promotion also asks a person to confirm publication.

const PATTERNS = [
  ["private-key", /-----BEGIN [A-Z ]*PRIVATE KEY-----[\s\S]*?-----END [A-Z ]*PRIVATE KEY-----/g],
  ["anthropic-key", /\bsk-ant-[A-Za-z0-9_-]{20,}/g],
  ["openai-key", /\bsk-(?:proj-|svcacct-)?[A-Za-z0-9_-]{20,}/g],
  ["github-token", /\b(?:gh[pousr]_[A-Za-z0-9]{30,}|github_pat_[A-Za-z0-9_]{40,})/g],
  ["aws-access-key", /\b(?:AKIA|ASIA)[0-9A-Z]{16}\b/g],
  ["slack-token", /\bxox[abprs]-[A-Za-z0-9-]{10,}/g],
  ["google-key", /\bAIza[0-9A-Za-z_-]{35}\b/g],
  ["stripe-key", /\b[rs]k_(?:live|test)_[0-9A-Za-z]{16,}/g],
  ["jwt", /\beyJ[A-Za-z0-9_-]{8,}\.[A-Za-z0-9_-]{8,}\.[A-Za-z0-9_-]{8,}/g],
];

// A name starts at a token boundary and its affixes are bounded, so scanning
// stays linear on text that repeats sensitive words.
const SENSITIVE_NAME = String.raw`(?<![A-Za-z0-9_.-])[A-Za-z0-9_.-]{0,40}?(?:api[_-]?key|access[_-]?key|secret|token|passw(?:or)?d|credential|private[_-]?key|auth)[A-Za-z0-9_.-]{0,40}`;
// A name may be quoted, as in `'password': ...` or `\"token\": ...`.
const KEY = String.raw`\\?["']?\b${SENSITIVE_NAME}\\?["']?`;

// name=value, name: value, "name": "value". A quoted value runs to the closing
// quote before a delimiter, so spaces and escaped quotes inside it are covered;
// bare values run to whitespace.
const QUOTED_ASSIGNMENT = new RegExp(
  String.raw`(${KEY}\s*[:=]\s*)(\\?")(.{0,2000}?)(\\?")(?=\s*(?:[,}\]\n]|$))`,
  "gi",
);
// A quoted value followed by more prose ends at the first closing quote that
// precedes whitespace or punctuation; it runs after the strict rule above.
const QUOTED_ASSIGNMENT_IN_PROSE = new RegExp(
  String.raw`(${KEY}\s*[:=]\s*)(\\?")(.{0,2000}?)(\\?")(?=[\s;:.)!?]|$)`,
  "gi",
);
// A quoted value that spans lines, is longer than the one-line rules below
// reach, or whose closing quote does not come within 8,000 characters, is
// redacted whole; other one-line values go to the rules below.
const MULTILINE_DOUBLE = new RegExp(String.raw`(${KEY}\s*[:=]\s*)"((?:[^"\\]|\\[\s\S]){0,8000})("?)`, "gi");
const MULTILINE_SINGLE = new RegExp(String.raw`(${KEY}\s*[:=]\s*)'((?:[^'\\]|\\[\s\S]){0,8000})('?)`, "gi");
const SINGLE_QUOTED_ASSIGNMENT = new RegExp(String.raw`(${KEY}\s*[:=]\s*)'([^'\n]*)'`, "gi");
const ASSIGNMENT = new RegExp(String.raw`(${KEY}\s*[:=]\s*)([^\s"'\\,;)}\]]{8,})`, "gi");
// YAML block scalars: `secret: |` (optionally with a comment) followed by
// indented lines.
const YAML_BLOCK = new RegExp(String.raw`(${KEY}\s*:\s*[|>][1-9]?[-+]?[1-9]?[ \t]*(?:#.*)?\n)((?:[ \t]+\S.*(?:\n|$)|[ \t]*\n)*[ \t]+\S.*(?:\n|$))`, "gi");
const BEARER = /(\b(?:Bearer|Basic|Token)\s+)[A-Za-z0-9._~+/=-]{16,}/g;
const URL_CREDENTIALS = /(\b[a-z][a-z0-9+.-]*:\/\/[^\s:/@]+:)[^\s@/]+(@)/gi;
// Dots are part of the token so a path ending in an extension is judged whole.
const LONG_TOKEN = /[A-Za-z0-9+/_=.-]{32,}/g;

function entropy(s) {
  const counts = new Map();
  for (const ch of s) counts.set(ch, (counts.get(ch) ?? 0) + 1);
  let h = 0;
  for (const n of counts.values()) {
    const p = n / s.length;
    h -= p * Math.log2(p);
  }
  return h;
}

// Hex digests (commits, content hashes) stay: they top out at 4 bits per
// character and are useful identifiers. Mixed-case base64-like runs above
// that are treated as secrets.
function looksSecret(raw) {
  // `NAME=/some/path` is judged by its value.
  const token = raw.replace(/^[A-Z][A-Z0-9_]*=/, "");
  if (/^[0-9a-f]+$/i.test(token)) return false;
  // Paths are not credentials: absolute paths, and slash-separated runs of
  // short word-like segments. Base64 secrets have long random runs instead.
  if (token.includes("/")) {
    const segments = token.split("/").filter(Boolean);
    if (token.startsWith("/") || (segments.length >= 2 && segments.every((seg) => seg.length <= 30 && /^[\w.@+-]+$/.test(seg)))) return false;
  }
  if (/^[\w-]+\.[A-Za-z]{1,6}$/.test(token)) return false;
  // Identifiers made of words (Go test names, CamelCase types) are not secrets.
  if (/^[A-Za-z_][A-Za-z0-9_]*$/.test(token) && (token.match(/[A-Z]?[a-z]{3,}/g) ?? []).join("").length >= token.length * 0.6) return false;
  if (!/[a-z]/.test(token) || !/[A-Z]/.test(token) || !/[0-9]/.test(token)) return false;
  return entropy(token) > 4.3;
}

export function redact(text) {
  if (!text) return { text: text ?? "", count: 0 };
  let count = 0;
  // CRLF and lone CR become LF so line-based rules see every line ending.
  let out = text.replace(/\r\n?/g, "\n");
  for (const [kind, re] of PATTERNS) {
    out = out.replace(re, () => {
      count++;
      return `[REDACTED:${kind}]`;
    });
  }
  out = out.replace(BEARER, (_, prefix) => {
    count++;
    return `${prefix}[REDACTED:bearer]`;
  });
  out = out.replace(URL_CREDENTIALS, (_, head, at) => {
    count++;
    return `${head}[REDACTED:password]${at}`;
  });
  out = out.replace(YAML_BLOCK, (_, head) => {
    count++;
    return `${head}  [REDACTED:assignment]\n`;
  });
  for (const [re, quote] of [[MULTILINE_DOUBLE, '"'], [MULTILINE_SINGLE, "'"]]) {
    out = out.replace(re, (match, head, value, close) => {
      if (close && !value.includes("\n") && value.length <= 2000) return match;
      count++;
      return `${head}${quote}[REDACTED:assignment]${close}`;
    });
  }
  out = out.replace(QUOTED_ASSIGNMENT, (match, head, open, value, close) => {
    if (value.length < 4 || value.startsWith("[REDACTED")) return match;
    count++;
    return `${head}${open}[REDACTED:assignment]${close}`;
  });
  out = out.replace(QUOTED_ASSIGNMENT_IN_PROSE, (match, head, open, value, close) => {
    if (value.length < 4 || value.startsWith("[REDACTED")) return match;
    count++;
    return `${head}${open}[REDACTED:assignment]${close}`;
  });
  out = out.replace(SINGLE_QUOTED_ASSIGNMENT, (match, head, value) => {
    if (value.length < 4 || value.startsWith("[REDACTED")) return match;
    count++;
    return `${head}'[REDACTED:assignment]'`;
  });
  out = out.replace(ASSIGNMENT, (match, head, value) => {
    if (value.startsWith("[REDACTED")) return match;
    count++;
    return `${head}[REDACTED:assignment]`;
  });
  out = out.replace(LONG_TOKEN, (token) => {
    if (!looksSecret(token)) return token;
    count++;
    return "[REDACTED:high-entropy]";
  });
  return { text: out, count };
}

// Keep the head and tail; the middle of a long message is the least useful
// part (pasted logs, generated plans) and the ends carry the request and the
// conclusion.
export function shorten(text, max) {
  if (!text || text.length <= max) return text ?? "";
  const keep = Math.max(0, max - 40);
  const head = Math.ceil(keep * 0.6);
  const tail = keep - head;
  const dropped = text.length - head - tail;
  return `${text.slice(0, head)}\n[… ${dropped} characters omitted …]\n${text.slice(text.length - tail)}`;
}
