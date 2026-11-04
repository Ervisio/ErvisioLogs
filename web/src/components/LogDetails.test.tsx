import { render, screen, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import LogDetails from './LogDetails';
import '../i18n';
import i18n from '../i18n';
import type { LogItem } from '../lib/api';

const item: LogItem = {
  id: 'event-1', tier: 'archive', timestamp: '2026-11-04T12:00:00Z', ingested_at: '2026-11-04T12:00:01Z',
  level: 'INFO', severity_number: 9, event: 'money.add', message: 'Cash +500', stream: 'economy', environment: 'production',
  resource: 'bank', service: '', host: '', trace_id: '', span_id: '',
  attributes: { actor: { type: 'player', id: 87, name: 'Giulia' }, amount: 500, allowed: false, context: { account: 'cash' } },
};

beforeEach(async () => { await i18n.changeLanguage('en'); });

test('filters existing actor and scalar attributes while preserving the original object', async () => {
  const user = userEvent.setup();
  const filter = vi.fn();
  render(<LogDetails item={item} onClose={vi.fn()} onFilter={filter} />);
  const dialog = screen.getByRole('dialog');
  expect(within(dialog).getByText('Cash +500')).toBeInTheDocument();
  await user.click(within(dialog).getByRole('button', { name: 'Filter by Actor · id' }));
  expect(filter).toHaveBeenLastCalledWith('actor.id', '87');
  await user.click(within(dialog).getByRole('button', { name: 'Filter by allowed' }));
  expect(filter).toHaveBeenLastCalledWith('allowed', 'false');
  const original = within(dialog).getByText('Show original').closest('details')!;
  expect(original).not.toHaveAttribute('open');
  await user.click(within(dialog).getByText('Show original'));
  expect(original).toHaveAttribute('open');
  expect(within(original).getByText(/"account": "cash"/)).toBeInTheDocument();
});

test('traps keyboard focus including the original disclosure and restores the opener', async () => {
  const user = userEvent.setup();
  const close = vi.fn();
  const opener = document.createElement('button');
  opener.textContent = 'Open event';
  document.body.append(opener);
  opener.focus();
  const rendered = render(<LogDetails item={item} onClose={close} onFilter={vi.fn()} />);
  expect(screen.getByRole('button', { name: 'Close' })).toHaveFocus();
  const summary = screen.getByText('Show original');
  summary.focus();
  await user.tab();
  expect(screen.getByRole('button', { name: 'Copy JSON' })).toHaveFocus();
  await user.keyboard('{Escape}');
  expect(close).toHaveBeenCalledOnce();
  rendered.unmount();
  expect(opener).toHaveFocus();
  opener.remove();
});

test('keeps malformed actor values visible as attributes without inventing an identity', () => {
  render(<LogDetails item={{ ...item, attributes: { actor: ['unstructured'], amount: 0 } }} onClose={vi.fn()} onFilter={vi.fn()} />);
  expect(screen.queryByRole('region', { name: 'Who' })).not.toBeInTheDocument();
  expect(screen.getByText('["unstructured"]')).toBeInTheDocument();
  expect(screen.getByRole('button', { name: 'Filter by amount' })).toHaveTextContent('0');
});
