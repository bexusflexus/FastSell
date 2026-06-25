import { Panel } from '../components/Panel';

interface PlaceholderPageProps {
  title: string;
}

export function PlaceholderPage({ title }: PlaceholderPageProps) {
  return (
    <Panel title={title} eyebrow="Coming next">
      <p className="text-sm text-stone-300">This page is intentionally stubbed for now.</p>
    </Panel>
  );
}
