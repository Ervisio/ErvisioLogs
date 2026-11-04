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

test.each(['Giulia "Admin"', 'C:\\server\\logs', 'line one\nline two', 'value\\"quoted', '日本語 value'])('saved and clicked filters preserve %j', (value) => {
  const filters = [{ field: 'actor.name', op: 'eq', value }];
  expect(parseQuery(formatQuery('message', filters))).toEqual({ text: 'message', filters });
});

test('unquoted backslashes keep their literal meaning', () => {
  expect(parseQuery('path=C:\\server\\logs')).toEqual({ text: '', filters: [{ field: 'path', op: 'eq', value: 'C:\\server\\logs' }] });
});
