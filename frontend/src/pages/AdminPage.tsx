import { Link } from 'react-router-dom';
import { Panel } from '../components/Panel';

const adminLinks = [
  { title: 'Inventory Groups', href: '/admin/inventory-groups', description: 'Manage required inventory item classification groups.' },
  { title: 'Container Types', href: '/admin/container-types', description: 'Manage structured container types, descriptions, and archive state.' },
  { title: 'Locations', href: '/admin/locations', description: 'Manage structured container locations, descriptions, and archive state.' },
  { title: 'Containers', href: '/admin/containers', description: 'Manage container records, archive state, and summaries.' },
  { title: 'Sell', href: '/admin/sell', description: 'Configure marketplace providers for future listing workflows.' },
  { title: 'Metrics', href: '/admin/metrics', description: 'Inventory value, counts, duplicate candidates, and top-value items.' },
  { title: 'Uploads', href: '/admin/uploads', description: 'Future upload session inspection and processing state.' },
  { title: 'System', href: '/admin/system', description: 'Read-only FastSell runtime health.' },
  { title: 'AI Configuration', href: '/admin/ai', description: 'Configure AI providers, active provider state, and connection tests.' },
];

export function AdminPage() {
  return (
    <div className="grid gap-6">
      <Panel title="Admin" eyebrow="FastSell operations">
        <p className="text-sm text-stone-300">Administrative tools for FastSell container intake and system operations.</p>
      </Panel>

      <div className="grid gap-4 sm:grid-cols-2">
        {adminLinks.map((link) => (
          <Link
            key={link.href}
            to={link.href}
            className="rounded-lg border border-rack-steel/30 bg-rack-soot/75 p-4 transition hover:border-copper-400/50 hover:bg-graphite-900"
          >
            <h2 className="text-lg font-semibold text-stone-100">{link.title}</h2>
            <p className="mt-2 text-sm text-stone-400">{link.description}</p>
          </Link>
        ))}
      </div>
    </div>
  );
}
