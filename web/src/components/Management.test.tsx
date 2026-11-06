import { act, render, renderHook, screen, waitFor } from '@testing-library/react';
import i18n from '../i18n';
import { Field, useAction, useRemote } from './Management';

beforeEach(async () => {
  await i18n.changeLanguage('en');
});

function pending<T>() {
  let resolve: (value: T) => void = () => {
    throw new Error('Promise not initialized');
  };
  const promise = new Promise<T>((done) => {
    resolve = done;
  });
  return { promise, resolve };
}

test('an action starts once when two submits occur before React renders busy state', async () => {
  const request = pending<void>();
  const action = vi.fn(() => request.promise);
  const success = vi.fn();
  const { result } = renderHook(() => useAction(success));
  let first: Promise<boolean> | undefined;
  let duplicate: Promise<boolean> | undefined;
  act(() => {
    first = result.current.run(action);
    duplicate = result.current.run(action);
  });
  expect(action).toHaveBeenCalledTimes(1);
  expect(await duplicate).toBe(false);
  await act(async () => {
    request.resolve();
    await first;
  });
  expect(success).toHaveBeenCalledTimes(1);
  expect(result.current.busy).toBe(false);
});

test('an action finishing after its form unmounts does not refresh another scope', async () => {
  const request = pending<void>();
  const success = vi.fn();
  const { result, unmount } = renderHook(() => useAction(success));
  let running: Promise<boolean> | undefined;
  act(() => {
    running = result.current.run(() => request.promise);
  });
  unmount();
  await act(async () => {
    request.resolve();
    await running;
  });
  expect(success).not.toHaveBeenCalled();
});

test('a response from the previous scope cannot replace the current result', async () => {
  const previous = pending<string>();
  const next = pending<string>();
  let oldSignal: AbortSignal | undefined;
  const oldLoad = vi.fn((signal: AbortSignal) => {
    oldSignal = signal;
    return previous.promise;
  });
  const newLoad = vi.fn(() => next.promise);
  const { result, rerender } = renderHook(({ load }) => useRemote(load), { initialProps: { load: oldLoad } });
  rerender({ load: newLoad });
  expect(oldSignal?.aborted).toBe(true);
  await act(async () => {
    next.resolve('current');
  });
  await waitFor(() => expect(result.current.data).toBe('current'));
  await act(async () => {
    previous.resolve('stale');
  });
  expect(result.current.data).toBe('current');
});

test('field labels exclude select options and explain hints separately', () => {
  render(
    <Field label="Saved view" hint="Choose a query">
      <select defaultValue="one">
        <option value="one">Failed invoices</option>
      </select>
    </Field>,
  );
  const control = screen.getByRole('combobox', { name: 'Saved view' });
  expect(control).toHaveAccessibleName('Saved view');
  expect(control).toHaveAccessibleDescription('Choose a query');
  expect(screen.getByText('Saved view').tagName).toBe('LABEL');
});

test('field associates its input when an adjacent action is present', () => {
  render(
    <Field label="Event color">
      <input type="color" defaultValue="#7798ff" />
      <button type="button">Clear color</button>
    </Field>,
  );
  const input = screen.getByLabelText('Event color');
  expect(input.tagName).toBe('INPUT');
  expect(input).toHaveAttribute('type', 'color');
  expect(screen.getByRole('button', { name: 'Clear color' })).not.toHaveAttribute('id', input.id);
});
