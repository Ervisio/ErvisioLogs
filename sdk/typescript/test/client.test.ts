import { describe, expect, it } from "vitest";
import { ErvisioError, ErvisioLogs, parseRetryAfter, type ErvisioLogsOptions } from "../src/index.js";

interface Call {
  url: string;
  headers: Record<string, string>;
  size: number;
  body: { stream?: string; environment?: string; events: Array<Record<string, unknown>> };
}

type Reply =
  | { status: number; body?: unknown; headers?: Record<string, string> }
  | Error;

/** A fetch double: records every request and answers with `handler`, 202 by default. */
function setup(handler?: (call: Call, n: number) => Reply, extra: Partial<ErvisioLogsOptions> = {}) {
  const calls: Call[] = [];
  const sleeps: number[] = [];
  const errors: ErvisioError[] = [];
  const fetchMock = async (url: string, init: RequestInit): Promise<Response> => {
    const call: Call = {
      url,
      size: (init.body as string).length,
      headers: init.headers as Record<string, string>,
      body: JSON.parse(init.body as string),
    };
    calls.push(call);
    const reply: Reply = handler ? handler(call, calls.length - 1) : accept(call);
    if (reply instanceof Error) throw reply;
    const payload = reply.body === undefined ? "" : typeof reply.body === "string" ? reply.body : JSON.stringify(reply.body);
    return new Response(payload, { status: reply.status, headers: reply.headers ?? {} });
  };
  const client = new ErvisioLogs({
    baseUrl: "http://ingest.test/",
    token: "test-token-not-a-secret",
    flushIntervalMs: 0,
    fetch: fetchMock,
    sleep: async (ms) => {
      sleeps.push(ms);
    },
    random: () => 0.5,
    onError: (e) => errors.push(e),
    ...extra,
  });
  return { client, calls, sleeps, errors };
}

function accept(call: Call): Reply {
  return {
    status: 202,
    body: { request_id: "req_1", accepted: call.body.events.length, rejected: 0, status: "durably_queued", errors: [] },
  };
}

function sentNames(calls: Call[]): string[] {
  return calls.flatMap((c) => c.body.events.map((e) => String(e.event)));
}

describe("batching", () => {
  it("splits the queue into batches of maxBatchSize and sends the request shape the API expects", async () => {
    const { client, calls } = setup(undefined, { maxBatchSize: 3, stream: "app", environment: "prod", service: "api" });
    for (let i = 1; i <= 7; i++) client.info(`e${i}`, `message ${i}`, { n: i });
    const res = await client.flush();

    // Automatic flushes may have sent the first batches, so count through stats and requests.
    expect(res.failed).toBe(0);
    expect(calls.length).toBeGreaterThanOrEqual(3);
    for (const c of calls) {
      expect(c.url).toBe("http://ingest.test/api/v1/ingest/batch");
      expect(c.headers.Authorization).toBe("Bearer test-token-not-a-secret");
      expect(c.headers["Content-Type"]).toBe("application/json");
      expect(c.body.stream).toBe("app");
      expect(c.body.environment).toBe("prod");
      expect(c.body.events.length).toBeLessThanOrEqual(3);
    }
    expect(sentNames(calls)).toEqual(["e1", "e2", "e3", "e4", "e5", "e6", "e7"]);

    const first = calls[0]?.body.events[0] as Record<string, unknown>;
    expect(first.level).toBe("INFO");
    expect(first.service).toBe("api");
    expect(first.attributes).toEqual({ n: 1 });
    expect(typeof first.id).toBe("string");
    expect(new Date(first.timestamp as string).toISOString()).toBe(first.timestamp);
    // Defaults travel once per batch, not per event.
    expect(first.stream).toBeUndefined();
    expect(first.environment).toBeUndefined();

    const ids = new Set(calls.flatMap((c) => c.body.events.map((e) => e.id)));
    expect(ids.size).toBe(7);
    expect(client.stats()).toMatchObject({ queued: 0, sent: 7, rejected: 0, dropped: 0, retries: 0 });
  });

  it("keeps a request under the payload cap even when events are large", async () => {
    const { client, calls } = setup();
    const big = "x".repeat(30_000);
    for (let i = 0; i < 60; i++) client.info("big", big);
    await client.flush();
    expect(calls.length).toBeGreaterThan(1);
    for (const c of calls) expect(c.size).toBeLessThanOrEqual(900 * 1024);
    expect(sentNames(calls).length).toBe(60);
  });

  it("allows only one flush in flight and sends later events in a follow-up", async () => {
    let active = 0;
    let maxActive = 0;
    const { client, calls } = setup(undefined, {
      fetch: async (_url, init) => {
        active++;
        maxActive = Math.max(maxActive, active);
        await new Promise((r) => setTimeout(r, 5));
        active--;
        const body = JSON.parse(init.body as string) as Call["body"];
        calls.push({ url: "", size: 0, headers: {}, body });
        return new Response(
          JSON.stringify({ request_id: "r", accepted: body.events.length, rejected: 0, status: "durably_queued", errors: [] }),
          { status: 202 },
        );
      },
    });
    client.info("a");
    const f1 = client.flush();
    client.info("b");
    const f2 = client.flush();
    const f3 = client.flush();
    const [r1, r2] = await Promise.all([f1, f2, f3]);
    expect(maxActive).toBe(1);
    expect(r1.accepted).toBe(1);
    expect(r2.accepted).toBe(1);
    expect(sentNames(calls)).toEqual(["a", "b"]);
  });
});

describe("413 Payload Too Large", () => {
  it("splits the batch in half and retries the halves", async () => {
    const { client, calls } = setup((call) => (call.body.events.length > 2 ? { status: 413, body: { title: "too large" } } : accept(call)));
    for (let i = 1; i <= 5; i++) client.info(`e${i}`);
    const res = await client.flush();

    expect(res.accepted).toBe(5);
    expect(res.failed).toBe(0);
    expect(calls[0]?.body.events.length).toBe(5);
    expect(calls[1]?.body.events.length).toBe(3);
    // Every event was delivered exactly once in an accepted request.
    const accepted = calls.filter((c) => c.body.events.length <= 2).flatMap((c) => c.body.events.map((e) => e.event));
    expect(accepted).toEqual(["e1", "e2", "e3", "e4", "e5"]);
    expect(client.stats()).toMatchObject({ sent: 5, dropped: 0, retries: 0 });
  });

  it("drops a single event the server still finds too large", async () => {
    const { client, calls, errors } = setup(() => ({ status: 413 }));
    client.info("only");
    const res = await client.flush();
    expect(calls.length).toBe(1);
    expect(res.accepted).toBe(0);
    expect(client.stats().dropped).toBe(1);
    expect(errors.some((e) => e.kind === "dropped" && e.status === 413)).toBe(true);
  });
});

describe("retries", () => {
  it("honors Retry-After on 429", async () => {
    const { client, calls, sleeps } = setup((call, n) =>
      n === 0 ? { status: 429, headers: { "Retry-After": "3" }, body: { title: "slow down" } } : accept(call),
    );
    client.info("a");
    const res = await client.flush();
    expect(calls.length).toBe(2);
    expect(sleeps).toEqual([3000]);
    expect(res.accepted).toBe(1);
    expect(client.stats()).toMatchObject({ sent: 1, retries: 1, failed: 0 });
  });

  it("retries 503, using Retry-After when present and full-jitter backoff otherwise", async () => {
    const { client, calls, sleeps } = setup((call, n) => {
      if (n === 0) return { status: 503, headers: { "Retry-After": "2" } };
      if (n === 1) return { status: 503 };
      if (n === 2) return { status: 500 };
      return accept(call);
    });
    client.info("a");
    await client.flush();
    expect(calls.length).toBe(4);
    // random() = 0.5, base 500 ms: attempt 1 ceiling 1000 -> 500, attempt 2 ceiling 2000 -> 1000.
    expect(sleeps).toEqual([2000, 500, 1000]);
    expect(client.stats()).toMatchObject({ sent: 1, retries: 3 });
  });

  it("retries network errors", async () => {
    const { client, calls } = setup((call, n) => (n < 2 ? new TypeError("fetch failed") : accept(call)));
    client.info("a");
    const res = await client.flush();
    expect(calls.length).toBe(3);
    expect(res.accepted).toBe(1);
  });

  it("gives up after maxRetries and reports it", async () => {
    const { client, calls, errors } = setup(() => ({ status: 503 }), { maxRetries: 2 });
    client.info("a");
    client.info("b");
    const res = await client.flush();
    expect(calls.length).toBe(3);
    expect(res.failed).toBe(2);
    expect(client.stats()).toMatchObject({ sent: 0, failed: 2, retries: 2, queued: 0 });
    expect(errors.some((e) => e.kind === "retries_exhausted" && e.status === 503)).toBe(true);
  });

  it("does not retry 401", async () => {
    const { client, calls, errors } = setup(() => ({ status: 401, body: { title: "Unauthorized" } }));
    client.info("a");
    const res = await client.flush();
    expect(calls.length).toBe(1);
    expect(res.failed).toBe(1);
    expect(errors.some((e) => e.kind === "http" && e.status === 401)).toBe(true);
  });

  it("does not retry 422 and counts the events as rejected", async () => {
    const { client, calls, sleeps, errors } = setup(() => ({
      status: 422,
      body: {
        request_id: "req_9",
        accepted: 0,
        rejected: 2,
        status: "rejected",
        errors: [
          { index: 0, reason: "level must be one of ..." },
          { index: 1, reason: "stream does not exist" },
        ],
      },
    }));
    client.info("a");
    client.info("b");
    const res = await client.flush();
    expect(calls.length).toBe(1);
    expect(sleeps).toEqual([]);
    expect(res.rejected).toBe(2);
    expect(client.stats()).toMatchObject({ sent: 0, rejected: 2, retries: 0, failed: 0 });
    expect(errors[0]?.kind).toBe("rejected");
    expect(errors[0]?.requestId).toBe("req_9");
  });
});

describe("202 with rejected events", () => {
  it("counts accepted and rejected separately and never resends the rejected ones", async () => {
    const { client, calls, errors } = setup(() => ({
      status: 202,
      body: {
        request_id: "req_2",
        accepted: 2,
        rejected: 1,
        status: "durably_queued",
        errors: [{ index: 1, reason: "event must be at most 128 characters" }],
      },
    }));
    client.info("a");
    client.info("b");
    client.info("c");
    const res = await client.flush();
    const secondId = calls[0]?.body.events[1]?.id;

    expect(calls.length).toBe(1);
    expect(res.accepted).toBe(2);
    expect(res.rejected).toBe(1);
    expect(client.stats()).toMatchObject({ sent: 2, rejected: 1, retries: 0, queued: 0 });
    const err = errors.find((e) => e.kind === "rejected");
    expect(err?.rejected).toEqual([{ index: 1, id: secondId, reason: "event must be at most 128 characters" }]);
  });
});

describe("bounded queue", () => {
  it("drops the oldest events when full and counts them", async () => {
    const { client, calls, errors } = setup(undefined, { maxQueue: 3 });
    for (let i = 1; i <= 5; i++) client.info(`e${i}`);
    expect(client.stats()).toMatchObject({ queued: 3, dropped: 2 });
    expect(errors.filter((e) => e.kind === "dropped").length).toBe(2);

    await client.flush();
    expect(sentNames(calls)).toEqual(["e3", "e4", "e5"]);
    expect(client.stats()).toMatchObject({ queued: 0, sent: 3, dropped: 2 });
  });
});

describe("client-side validation", () => {
  it("truncates the message, rewrites bad attribute keys and reports through onError", async () => {
    const { client, calls, errors } = setup();
    client.info("My Event!", "é".repeat(20_000), { "a.b": 1, "ok-key_1": 2, "": 3, nested: { "x y": 4 } });
    await client.flush();
    const ev = calls[0]?.body.events[0] as Record<string, unknown>;

    expect(ev.event).toBe("My_Event_");
    expect(new TextEncoder().encode(ev.message as string).length).toBeLessThanOrEqual(32768);
    expect((ev.message as string).length).toBe(16384);
    expect(ev.attributes).toEqual({ a_b: 1, "ok-key_1": 2, _: 3, nested: { x_y: 4 } });
    expect(errors.length).toBe(1);
    expect(errors[0]?.kind).toBe("validation");
  });

  it("limits attribute depth, survives cycles and odd values", async () => {
    const { client, calls } = setup();
    const cyc: Record<string, unknown> = { name: "c" };
    cyc.self = cyc;
    let deep: Record<string, unknown> = { leaf: 1 };
    for (let i = 0; i < 12; i++) deep = { d: deep };
    client.info("odd", undefined, { cyc, deep, big: 10n, when: new Date(0), fn: () => 1, nan: NaN, err: new Error("boom") });
    await client.flush();
    const text = JSON.stringify(calls[0]?.body.events[0]);
    expect(text).toContain("[circular]");
    expect(text).toContain("[max depth]");
    expect(text).toContain("1970-01-01T00:00:00.000Z");
    expect(text).toContain("boom");
    expect(client.stats().sent).toBe(1);
  });

  it("applies overrides and normalizes the level", async () => {
    const { client, calls } = setup(undefined, { stream: "app" });
    client.log("warning", "x", "m", undefined, {
      id: "my-id",
      stream: "other",
      timestamp: new Date("2026-01-02T03:04:05.678Z"),
      trace_id: "0af7651916cd43dd8448eb211c80319c",
      span_id: "not hex",
    });
    await client.flush();
    const ev = calls[0]?.body.events[0] as Record<string, unknown>;
    expect(ev).toMatchObject({
      id: "my-id",
      stream: "other",
      level: "WARN",
      timestamp: "2026-01-02T03:04:05.678Z",
      trace_id: "0af7651916cd43dd8448eb211c80319c",
    });
    expect(ev.span_id).toBeUndefined();
  });

  it("never throws from log(), even when onError throws", () => {
    const { client } = setup(undefined, {
      onError: () => {
        throw new Error("callback bug");
      },
    });
    expect(() => client.log("nope" as never, "x", "m", { "a.b": 1 })).not.toThrow();
    expect(() => client.info(undefined as never)).not.toThrow();
    expect(() => client.info("x", { not: "a string" } as never, 5 as never)).not.toThrow();
  });
});

describe("close", () => {
  it("flushes what is queued, then drops later events", async () => {
    const { client, calls } = setup();
    client.info("a");
    client.info("b");
    await client.close();
    expect(sentNames(calls)).toEqual(["a", "b"]);
    client.info("late");
    expect(client.stats().dropped).toBe(1);
    await client.close();
    expect(calls.length).toBe(1);
  });
});

describe("helpers", () => {
  it("parses Retry-After as seconds or HTTP date", () => {
    expect(parseRetryAfter("7")).toBe(7000);
    expect(parseRetryAfter("Wed, 21 Oct 2015 07:28:10 GMT", Date.parse("Wed, 21 Oct 2015 07:28:00 GMT"))).toBe(10_000);
    expect(parseRetryAfter("garbage")).toBeUndefined();
    expect(parseRetryAfter(null)).toBeUndefined();
  });

  it("requires baseUrl and token", () => {
    expect(() => new ErvisioLogs({ baseUrl: "", token: "t" })).toThrow(TypeError);
    expect(() => new ErvisioLogs({ baseUrl: "http://x", token: "" })).toThrow(TypeError);
  });
});
