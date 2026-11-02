import { render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { MemoryRouter } from 'react-router-dom';
import App from './App';
import i18n from './i18n';

function mockReadyz(status: number, body: unknown) {
  vi.stubGlobal('fetch', vi.fn().mockResolvedValue({ status, json: async () => body }));
}
const renderApp = () =>
  render(
    <MemoryRouter>
      <App />
    </MemoryRouter>,
  );

beforeEach(async () => {
  localStorage.clear();
  await i18n.changeLanguage('en');
});
afterEach(() => vi.unstubAllGlobals());

test('shows all services ready', async () => {
  mockReadyz(200, {
    status: 'ready',
    checks: { postgres: 'ok', clickhouse: 'ok', migrations_postgres: 'ok', migrations_clickhouse: 'ok' },
  });
  renderApp();
  expect(await screen.findByText('All services are ready')).toBeInTheDocument();
  expect(screen.getAllByText('OK')).toHaveLength(4);
});

test('shows a failing service from a 503 body', async () => {
  mockReadyz(503, {
    status: 'not_ready',
    checks: { postgres: 'ok', clickhouse: 'fail', migrations_postgres: 'ok', migrations_clickhouse: 'ok' },
  });
  renderApp();
  expect(await screen.findByText('Some services are not ready')).toBeInTheDocument();
  expect(screen.getByText('Failing')).toBeInTheDocument();
});

test('reports an unreachable server', async () => {
  vi.stubGlobal('fetch', vi.fn().mockRejectedValue(new Error('network')));
  renderApp();
  expect(await screen.findByText('The server does not answer')).toBeInTheDocument();
});

test('switches language to Italian', async () => {
  mockReadyz(200, { status: 'ready', checks: {} });
  renderApp();
  await userEvent.selectOptions(screen.getByLabelText('Language'), 'it');
  await waitFor(() => expect(screen.getAllByText('Panoramica').length).toBeGreaterThan(0));
  expect(localStorage.getItem('lang')).toBe('it');
});

test('rail expands and collapses', async () => {
  mockReadyz(200, { status: 'ready', checks: {} });
  renderApp();
  const btn = screen.getByRole('button', { name: 'Expand' });
  expect(btn).toHaveAttribute('aria-expanded', 'false');
  await userEvent.click(btn);
  expect(screen.getByRole('button', { name: 'Collapse' })).toHaveAttribute('aria-expanded', 'true');
  expect(localStorage.getItem('rail')).toBe('wide');
});

test('theme toggle persists', async () => {
  mockReadyz(200, { status: 'ready', checks: {} });
  renderApp();
  await userEvent.click(screen.getByRole('button', { name: 'Switch theme' }));
  expect(document.documentElement.dataset.theme).toBe('light');
  expect(localStorage.getItem('theme')).toBe('light');
});
