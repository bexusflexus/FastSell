import { Panel } from '../components/Panel';

interface AdminStubPageProps {
  title: string;
  description: string;
}

export function AdminStubPage({ title, description }: AdminStubPageProps) {
  return (
    <Panel title={title} eyebrow="Admin">
      <p className="text-sm text-stone-300">{description}</p>
    </Panel>
  );
}
