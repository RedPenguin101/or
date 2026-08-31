import type { ReactNode } from "react";

export function Panel({
  title,
  aside,
  children,
}: {
  title?: ReactNode;
  aside?: ReactNode;
  children: ReactNode;
}) {
  return (
    <section className="rounded-lg border border-line bg-raised">
      {title && (
        <header className="flex items-baseline justify-between gap-4 rounded-t-lg border-b border-line bg-chrome-soft px-4 py-2.5">
          <h2 className="text-sm font-semibold tracking-tight">{title}</h2>
          {aside && <div className="text-xs text-ink-soft">{aside}</div>}
        </header>
      )}
      <div className="p-4">{children}</div>
    </section>
  );
}

const toneClass = {
  good: "bg-good-soft text-good",
  warn: "bg-warn-soft text-warn",
  bad: "bg-bad-soft text-bad",
  flat: "bg-accent-soft text-accent",
} as const;

export function Pill({
  tone = "flat",
  children,
}: {
  tone?: keyof typeof toneClass;
  children: ReactNode;
}) {
  return (
    <span
      className={`inline-block rounded-full px-2 py-0.5 text-[11px] font-medium ${toneClass[tone]}`}
    >
      {children}
    </span>
  );
}

export function Stat({
  label,
  value,
  tone,
}: {
  label: string;
  value: ReactNode;
  tone?: "good" | "warn" | "bad";
}) {
  const c = tone
    ? { good: "text-good", warn: "text-warn", bad: "text-bad" }[tone]
    : "";
  return (
    <div>
      <div className={`text-2xl font-semibold tabular-nums ${c}`}>{value}</div>
      <div className="text-xs text-ink-soft">{label}</div>
    </div>
  );
}

export function Empty({ children }: { children: ReactNode }) {
  return <p className="py-6 text-center text-sm text-ink-soft">{children}</p>;
}

export function ErrorNote({ error }: { error: unknown }) {
  const msg = error instanceof Error ? error.message : String(error);
  return (
    <div className="rounded-md border border-bad bg-bad-soft px-3 py-2 text-sm text-bad">
      {msg}
    </div>
  );
}
