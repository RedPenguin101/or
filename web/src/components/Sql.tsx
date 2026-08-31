import type { ReactNode } from "react";

// Syntax highlighting for the SQL a rule is made of.
//
// Hand-written, for the same reason Prose.tsx is: this renders one dialect we
// control, and a highlighting library would be a dependency, a bundle, and a
// theme to fight — its colours come from its own stylesheet, not from the oklch
// tokens the rest of the app is built on.
//
// This is a lexer, not a parser. Five classes are enough, and one of them earns
// its place for a domain reason: a double-quoted identifier here is a *legacy
// source field* ("REP-ID", "ACCT-TYPE-CD"), so telling it apart from a target
// field name is the distinction a reader of these files most needs.
//
// A comment gets colour too, deliberately understated. The seed's SQL carries
// none — a rule's prose belongs to the rule, in its manifest, where the pages
// that survey many rules can read it — but an authored revision may well add
// one, and it should recede rather than shout.
export default function Sql({ src }: { src: string }) {
  return (
    <pre className="mono overflow-x-auto rounded-md border border-line bg-surface p-3 text-[11px] leading-relaxed">
      <code>{lex(src).map((t, i) => paint(t, i))}</code>
    </pre>
  );
}

type Kind = "comment" | "string" | "ident" | "keyword" | "plain";
type Token = { kind: Kind; text: string };

const className: Record<Kind, string> = {
  comment: "text-syn-comment italic",
  string: "text-syn-string",
  ident: "text-syn-ident",
  keyword: "text-syn-keyword",
  plain: "",
};

function paint(t: Token, i: number): ReactNode {
  if (t.kind === "plain") return t.text;
  return (
    <span key={i} className={className[t.kind]}>
      {t.text}
    </span>
  );
}

// DuckDB's dialect, restricted to what these rules actually use. A keyword that
// is missing renders as plain text, which is a smaller failure than colouring
// something that is not one.
const KEYWORDS = new Set(
  (
    "select from where group by having order limit offset as on join left right inner outer full " +
    "cross union all distinct case when then else end and or not null is in like between exists " +
    "with recursive cast try_cast asc desc nulls first last coalesce over partition filter " +
    "true false interval using natural except intersect qualify"
  )
    .toUpperCase()
    .split(" "),
);

const WORD_START = /[A-Za-z_]/;
const WORD = /[A-Za-z0-9_]/;
const DIGIT = /[0-9]/;

// lex walks the source once and emits every character exactly once, in order:
// the concatenation of the tokens is the input. Nothing here may drop a
// character, because the output is the file a user is reading.
function lex(src: string): Token[] {
  const out: Token[] = [];
  const push = (kind: Kind, text: string) => {
    if (!text) return;
    // Runs of plain text are merged so React renders far fewer nodes.
    const last = out[out.length - 1];
    if (kind === "plain" && last?.kind === "plain") last.text += text;
    else out.push({ kind, text });
  };

  let i = 0;
  while (i < src.length) {
    const c = src[i];

    if (c === "-" && src[i + 1] === "-") {
      const nl = src.indexOf("\n", i);
      const end = nl < 0 ? src.length : nl;
      push("comment", src.slice(i, end));
      i = end;
      continue;
    }
    if (c === "/" && src[i + 1] === "*") {
      const close = src.indexOf("*/", i + 2);
      const end = close < 0 ? src.length : close + 2;
      push("comment", src.slice(i, end));
      i = end;
      continue;
    }
    // Quoted runs share one scanner: '' and "" are the escapes in both.
    if (c === "'" || c === '"') {
      let j = i + 1;
      while (j < src.length) {
        if (src[j] !== c) {
          j++;
        } else if (src[j + 1] === c) {
          j += 2;
        } else {
          j++;
          break;
        }
      }
      push(c === "'" ? "string" : "ident", src.slice(i, j));
      i = j;
      continue;
    }
    if (WORD_START.test(c)) {
      let j = i;
      while (j < src.length && WORD.test(src[j])) j++;
      const word = src.slice(i, j);
      push(KEYWORDS.has(word.toUpperCase()) ? "keyword" : "plain", word);
      i = j;
      continue;
    }
    if (DIGIT.test(c)) {
      let j = i;
      while (j < src.length && /[0-9.]/.test(src[j])) j++;
      push("string", src.slice(i, j));
      i = j;
      continue;
    }

    // Operators and whitespace, up to whatever starts a token above.
    let j = i;
    while (j < src.length) {
      const d = src[j];
      if (d === "'" || d === '"' || WORD_START.test(d) || DIGIT.test(d)) break;
      if (d === "-" && src[j + 1] === "-") break;
      if (d === "/" && src[j + 1] === "*") break;
      j++;
    }
    if (j === i) j++; // every branch must advance, or the page hangs
    push("plain", src.slice(i, j));
    i = j;
  }
  return out;
}

export { lex };
