import { formatQuery, parseQuery } from './query';

test('splits key=value filters from free text', () => {
  expect(parseQuery('user=ada boom')).toEqual({ text: 'boom', filters: [{ field: 'user', op: 'eq', value: 'ada' }] });
});

test('maps every operator', () => {
  const ops = parseQuery('a!=1 b~x c>2 d>=3 e<4 f<=5 has:g').filters.map((f) => f.op);
  expect(ops).toEqual(['neq', 'contains', 'gt', 'gte', 'lt', 'lte', 'exists']);
});

test('quoted values keep their spaces', () => {
  expect(parseQuery('msg="card declined" "two words"')).toEqual({
    text: 'two words',
    filters: [{ field: 'msg', op: 'eq', value: 'card declined' }],
  });
});

test('a term without a value, or a bare has:, stays text', () => {
  expect(parseQuery('user= has:')).toEqual({ text: 'user= has:', filters: [] });
});


test('round-trips tags with attribute paths and quoted values', () => {
  const filters = [{ field: 'actor.name', op: 'eq', value: 'Player Seven' }, { field: 'amount', op: 'gte', value: '42' }];
  const formatted = formatQuery('transfer completed', filters, ['economy', 'player:trade']);
  expect(parseQuery(formatted)).toEqual({ text: 'transfer completed', filters, tags: ['economy', 'player:trade'] });
});

test('deduplicates valid tags while preserving malformed terms as message text', () => {
  expect(parseQuery('tag:economy tag:economy tag:trade tag:UPPER tag:')).toEqual({ text: 'tag:UPPER tag:', filters: [], tags: ['economy', 'trade'] });
});

test('accepts a forty-character tag and preserves a longer token as text', () => {
  const valid = 't'.repeat(40); const invalid = 't'.repeat(41);
  expect(parseQuery(`tag:${valid} tag:${invalid}`)).toEqual({ text: `tag:${invalid}`, filters: [], tags: [valid] });
});

test('round-trips literal quotes and backslashes in attribute values', () => {
  const value = 'Player "Seven" \\ unit';
  const filters = [{ field: 'actor.name', op: 'eq', value }];
  expect(parseQuery(formatQuery('transfer', filters, ['economy']))).toEqual({ text: 'transfer', filters, tags: ['economy'] });
});
