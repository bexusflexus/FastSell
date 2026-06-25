import type { PropsWithChildren } from 'react';

interface PanelProps extends PropsWithChildren {
  title: string;
  eyebrow?: string;
  action?: React.ReactNode;
}

export function Panel({ title, eyebrow, action, children }: PanelProps) {
  return (
    <section className="rounded-lg border border-rack-steel/30 bg-[linear-gradient(180deg,rgba(22,26,24,0.96),rgba(8,9,8,0.96))] shadow-panel">
      <div className="flex flex-col gap-3 border-b border-rack-steel/25 bg-[linear-gradient(90deg,rgba(36,42,38,0.74),rgba(13,15,14,0.68))] px-4 py-4 sm:flex-row sm:items-center sm:justify-between">
        <div>
          {eyebrow ? <p className="text-xs font-semibold uppercase tracking-[0.22em] text-rack-glass">{eyebrow}</p> : null}
          <h2 className="mt-1 text-lg font-semibold text-stone-50">{title}</h2>
        </div>
        {action}
      </div>
      <div className="p-4">{children}</div>
    </section>
  );
}
