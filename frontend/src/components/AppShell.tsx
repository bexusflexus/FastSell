import { useEffect, useState, type PropsWithChildren } from 'react';
import { NavLink } from 'react-router-dom';
import { getAdminSystemHealth } from '../api/system';
import { WHOLE_SCENE_USER_LABEL } from '../wholeScenePresentation';
import type { AdminSystemHealthResponse } from '../types/system';

export function AppShell({ children }: PropsWithChildren) {
  const [isSystemLive, setIsSystemLive] = useState(false);

  useEffect(() => {
    let isMounted = true;

    const loadHealth = async () => {
      try {
        const health = await getAdminSystemHealth();
        if (isMounted) {
          setIsSystemLive(isFullyHealthy(health));
        }
      } catch {
        if (isMounted) {
          setIsSystemLive(false);
        }
      }
    };

    void loadHealth();
    const interval = window.setInterval(() => {
      void loadHealth();
    }, 30_000);

    return () => {
      isMounted = false;
      window.clearInterval(interval);
    };
  }, []);

  return (
    <div className="min-h-screen bg-graphite-950 text-stone-100">
      <div className="fixed inset-0 -z-10 bg-[radial-gradient(circle_at_18%_10%,rgba(240,82,42,0.18),transparent_28%),radial-gradient(circle_at_58%_0%,rgba(255,122,56,0.08),transparent_30%),linear-gradient(145deg,#050606_0%,#161a18_48%,#080908_100%)]" />
      <header className="border-b border-rack-trim/80 bg-graphite-950/72 backdrop-blur">
        <div className="mx-auto flex max-w-6xl items-center justify-between px-4 py-4 sm:px-6">
          <div>
            <div className="flex items-center gap-3">
              <span className="h-3 w-3 rounded-full bg-signal-red shadow-[0_0_18px_rgba(255,59,36,0.8)]" />
              <span className="text-xs font-semibold uppercase tracking-[0.28em] text-rack-glass">
                FastSell
              </span>
            </div>
            <h1 className="mt-2 text-2xl font-semibold text-stone-50 sm:text-3xl">Intake Console</h1>
          </div>
          <div className="flex flex-col items-end gap-3">
            <nav className="flex flex-wrap justify-end gap-2 text-sm">
              <NavLink to="/upload" className={({ isActive }) => navClass(isActive)}>
                Upload
              </NavLink>
              <NavLink to="/review" className={({ isActive }) => navClass(isActive)}>
                Review
              </NavLink>
              <NavLink to="/wholescene" className={({ isActive }) => navClass(isActive)}>
                {WHOLE_SCENE_USER_LABEL}
              </NavLink>
              <NavLink to="/inventory" className={({ isActive }) => navClass(isActive)}>
                Inventory
              </NavLink>
              <NavLink to="/admin" className={({ isActive }) => navClass(isActive)}>
                Admin
              </NavLink>
            </nav>
            <div className={`hidden rounded border px-3 py-2 text-xs font-semibold shadow-[0_0_14px_rgba(255,59,36,0.18)] sm:block ${isSystemLive ? 'border-signal-green/45 bg-signal-green/10 text-green-100' : 'border-signal-red/55 bg-red-950/25 text-signal-red'}`}>
              {isSystemLive ? 'System Live' : 'System Down'}
            </div>
          </div>
        </div>
      </header>
      <main className="mx-auto max-w-6xl px-4 py-6 sm:px-6 lg:py-8">{children}</main>
    </div>
  );
}

function isFullyHealthy(health: AdminSystemHealthResponse): boolean {
  return health.overall_status === 'ok';
}

function navClass(isActive: boolean) {
  return `rounded-md border px-3 py-2 transition ${
    isActive
      ? 'border-copper-500/45 bg-copper-500/12 text-amberline-100'
      : 'border-rack-steel/25 text-stone-300 hover:bg-rack-steel/12'
  }`;
}
