import type { ReactNode } from "react";

// Qniform's OpenAPI descriptions are written in markdown, and they lean on
// inline code and bold to carry meaning — `pab` is a value, **requires** is a
// rule. Rendering them literally turns the target's own documentation into
// noise, so the two inline forms that actually appear are handled here.
//
// Deliberately not a markdown library: this renders a known, small subset of
// text whose source we control, and pulling in a parser to do it would be a
// dependency and an injection surface for no gain.
export function Inline({ text }: { text: string }) {
  return <>{parse(text)}</>;
}

function parse(text: string): ReactNode[] {
  const out: ReactNode[] = [];
  const re = /`([^`]+)`|\*\*([^*]+)\*\*/g;
  let last = 0;
  let m: RegExpExecArray | null;
  let k = 0;
  while ((m = re.exec(text)) !== null) {
    if (m.index > last) out.push(text.slice(last, m.index));
    if (m[1] !== undefined) {
      out.push(
        // No padding or background: a chip leaves a visible gap before any
        // punctuation that follows it, and these descriptions are full of
        // `code`-then-comma. Colour and face carry it well enough inline.
        <code key={k++} className="mono text-[0.95em] text-accent">
          {m[1]}
        </code>,
      );
    } else {
      out.push(
        <strong key={k++} className="font-semibold text-ink">
          {m[2]}
        </strong>,
      );
    }
    last = re.lastIndex;
  }
  if (last < text.length) out.push(text.slice(last));
  return out;
}

// A YAML folded-scalar description, as the seed catalogue and the target spec
// both write them: paragraphs separated by blank lines, with the occasional
// indented block that has to keep its own line breaks — a legacy account
// number's range table is the reason this exists, and it is unreadable as one
// run-on line.
//
// The indent is the whole signal, which is enough: YAML folding already
// preserves more-indented lines verbatim, so a block that survives the loader
// with its newlines intact is one the author meant to keep them.
export function Description({ text }: { text: string }) {
  const blocks = text.trim().split(/\n\s*\n/);
  return (
    <>
      {blocks.map((b, i) =>
        preformatted(b) ? (
          <pre
            key={i}
            className="mono my-2 overflow-x-auto rounded border border-line bg-surface p-2 text-[11px] leading-relaxed"
          >
            {dedent(b)}
          </pre>
        ) : (
          <p key={i} className={i > 0 ? "mt-2" : undefined}>
            <Inline text={b} />
          </p>
        ),
      )}
    </>
  );
}

// Every line indented means the author laid it out; one flush-left line means it
// is prose that merely wrapped.
function preformatted(block: string): boolean {
  const lines = block.split("\n").filter((l) => l.trim() !== "");
  return lines.length > 1 && lines.every((l) => /^\s\s/.test(l));
}

function dedent(block: string): string {
  const lines = block.split("\n");
  const indent = Math.min(
    ...lines.filter((l) => l.trim() !== "").map((l) => l.match(/^\s*/)![0].length),
  );
  return lines.map((l) => l.slice(indent)).join("\n");
}
