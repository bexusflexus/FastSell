// @vitest-environment jsdom

import '@testing-library/jest-dom/vitest';
import { cleanup, render, screen, waitFor } from '@testing-library/react';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { MemoryRouter } from 'react-router-dom';
import { AppShell } from './AppShell';
import { getAdminSystemHealth, getSystemVersion } from '../api/system';

vi.mock('../api/system', () => ({
  getAdminSystemHealth: vi.fn(),
  getSystemVersion: vi.fn(),
}));

afterEach(() => {
  cleanup();
  vi.resetAllMocks();
});

describe('AppShell system status', () => {
  it('renders the installed version and available update', async () => {
    vi.mocked(getAdminSystemHealth).mockResolvedValue({ overall_status: 'ok' } as never);
    vi.mocked(getSystemVersion).mockResolvedValue({
      installed_version: 'v0.1.3',
      latest_version: 'v0.1.4',
      update_available: true,
    });

    renderShell();

    await waitFor(() => expect(screen.getByText('System Live v0.1.3')).toBeInTheDocument());
    expect(screen.getByText('New Version v0.1.4 available')).toBeInTheDocument();
  });

  it('renders candidate versions and hides the alert without an update', async () => {
    vi.mocked(getAdminSystemHealth).mockResolvedValue({ overall_status: 'ok' } as never);
    vi.mocked(getSystemVersion).mockResolvedValue({
      installed_version: 'candidate-a1b2c3d',
      latest_version: 'v0.1.3',
      update_available: false,
    });

    renderShell();

    await waitFor(() => expect(screen.getByText('System Live candidate-a1b2c3d')).toBeInTheDocument());
    expect(screen.queryByText(/New Version/)).not.toBeInTheDocument();
  });
});

function renderShell() {
  render(
    <MemoryRouter>
      <AppShell>
        <div>Content</div>
      </AppShell>
    </MemoryRouter>,
  );
}
