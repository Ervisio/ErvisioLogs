import { slugFor, slugify } from './slug';

test('folds accents and joins words with dashes', () => {
  expect(slugify('  Caffè  Città! ')).toBe('caffe-citta');
});

test('drops characters with no Latin form', () => {
  expect(slugify('日本語')).toBe('');
  expect(slugify('Shop 日本')).toBe('shop');
});

test('cuts at the length without leaving a trailing dash', () => {
  expect(slugify('abc def', 4)).toBe('abc');
});

test('falls back to a valid slug when nothing survives', () => {
  expect(slugFor('Acme', 'workspace')).toBe('acme');
  expect(slugFor('日本語', 'workspace')).toMatch(/^workspace-[a-z0-9]+$/);
});
