"""The four benchmark queries, written once per engine. Each takes the source expression for the planned files.

q1_needle  free text (case-insensitive) inside one hour, newest 100
q2_attr    attribute filter user=ada over the day, newest 100
q3_levels  count per level over the day (a full scan of two columns)
q4_page    newest 100 events of one hour (the explorer's first page)
"""
import common as c

QUERIES = ["q1_needle", "q2_attr", "q3_levels", "q4_page"]
FULL_DAY = {"q2_attr", "q3_levels"}


def window_ms(query: str) -> tuple[int, int]:
    return (c.DAY_START_MS, c.DAY_END_MS) if query in FULL_DAY else c.WINDOW


def sql(engine: str, query: str, src: str) -> str:
    lo, hi = c.WINDOW_SQL
    if engine == "clickhouse":
        between = f"timestamp >= toDateTime64('{lo}', 3, 'UTC') AND timestamp < toDateTime64('{hi}', 3, 'UTC')"
        attr = "JSONExtractString(attributes, 'user') = 'ada'"
        count = "count()"
    elif engine == "duckdb":
        between = f"timestamp >= TIMESTAMPTZ '{lo}+00' AND timestamp < TIMESTAMPTZ '{hi}+00'"
        attr = "json_extract_string(attributes, '$.user') = 'ada'"
        count = "count(*)"
    else:  # datafusion: no JSON functions in the Python package, so the attribute filter is a substring match
        between = f"timestamp >= '{lo.replace(' ', 'T')}Z' AND timestamp < '{hi.replace(' ', 'T')}Z'"
        attr = "attributes LIKE '%\"user\":\"ada\"%'"
        count = "count(*)"
    project = f"project_id = '{c.PROJECT}'"
    newest = f"ORDER BY timestamp DESC, event_id DESC LIMIT {c.LIMIT}"
    if query == "q1_needle":
        return f"SELECT event_id FROM {src} WHERE {project} AND {between} AND lower(message) LIKE '%{c.NEEDLE}%' {newest}"
    if query == "q2_attr":
        return f"SELECT event_id FROM {src} WHERE {project} AND {attr} {newest}"
    if query == "q3_levels":
        return f"SELECT severity_text, {count} AS n FROM {src} WHERE {project} GROUP BY severity_text"
    return f"SELECT event_id FROM {src} WHERE {project} AND {between} {newest}"


def result_digest(query: str, rows: list) -> str:
    if query == "q3_levels":
        return c.digest(sorted((str(r[0]), int(r[1])) for r in rows))
    return c.digest([r[0] for r in rows])
