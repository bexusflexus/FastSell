// @vitest-environment jsdom

import '@testing-library/jest-dom/vitest';
import { cleanup, render, screen, waitFor } from '@testing-library/react';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { MemoryRouter } from 'react-router-dom';
import { getAdminSystemHealth } from '../api/system';
import type { AdminSystemHealthResponse } from '../types/system';
import { AdminSystemPage } from './AdminSystemPage';

vi.mock('../api/system', () => ({
  getAdminSystemHealth: vi.fn(),
}));

afterEach(() => {
  cleanup();
  vi.resetAllMocks();
});

describe('AdminSystemPage image values', () => {
  it('keeps long candidate image references intact and enables wrapping', async () => {
    const image = 'ghcr.io/bexusflexus/fastsell:api-sha-8da0d26269d32da2af984715f3ee01154622f92e';
    vi.mocked(getAdminSystemHealth).mockResolvedValue(systemHealthWithImage(image));

    render(
      <MemoryRouter>
        <AdminSystemPage />
      </MemoryRouter>,
    );

    await waitFor(() => expect(screen.getByText(`Image: ${image}`)).toBeInTheDocument());
    expect(screen.getByText(`Image: ${image}`)).toHaveClass('min-w-0', '[overflow-wrap:anywhere]');
    expect(screen.getByText(`Image: ${image}`)).toHaveTextContent(image);
  });
});

function systemHealthWithImage(image: string): AdminSystemHealthResponse {
  return {
    overall_status: 'ok',
    generated_datetime: '2026-07-13T00:00:00Z',
    api: {
      status: 'ok',
      uptime_seconds: 1,
      server_time: '2026-07-13T00:00:00Z',
      data_root: '/app/data',
      image_root: '/app/data/images',
      intake_dir: '/app/data/intake/incoming',
    },
    database: {
      status: 'ok',
      reachable: true,
      migration_version: 1,
      migration_dirty: false,
      database_size_bytes: 1,
      container_count: 1,
      item_count: 1,
      image_asset_count: 1,
      upload_session_count: 1,
      upload_group_count: 1,
    },
    storage: {
      status: 'ok',
      path: '/app/data',
      total_bytes: 1,
      free_bytes: 1,
      used_bytes: 0,
      used_percent: 0,
    },
    paths: {
      status: 'ok',
      paths: [],
    },
    intake: {
      status: 'ok',
      pending_or_uploaded_image_count: 0,
      processing_image_count: 0,
      processed_image_count: 0,
      failed_image_count: 0,
      stuck_processing_image_count: 0,
      oldest_pending_datetime: null,
      latest_processed_datetime: null,
    },
    ai: {
      status: 'ok',
      ai_assist_enabled: false,
      active_provider_id: null,
      active_provider_name: null,
      active_provider_type: null,
      active_model_name: null,
      vision_enabled: null,
      last_test_status: null,
      last_test_datetime: null,
      last_error_message: null,
    },
    frontend: {
      status: 'ok',
      hosting_mode: 'nginx',
      public_url: 'http://localhost:8888',
      message: 'Frontend is healthy.',
    },
    docker: {
      status: 'ok',
      message: 'Docker is healthy.',
      generated_datetime: '2026-07-13T00:00:00Z',
      services: [
        {
          service_name: 'api',
          container_name: 'fastsell_api',
          image,
          state: 'running',
          health: 'healthy',
          restart_count: 0,
          started_at: null,
          finished_at: null,
          ports: [],
          status: 'ok',
        },
      ],
      alerts: [],
    },
    alerts: [],
  };
}
