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
