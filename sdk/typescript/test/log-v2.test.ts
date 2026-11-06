import { describe, expect, it } from "vitest";
import { buildEvent } from "../src/validate.js";

describe("log v2", () => {
  it("normalizes tags and keeps actor and target in attributes", () => {
    const built = buildEvent({}, "INFO", "trade", "Trade completed", { amount: 42 }, {
      tags: ["Trade", "trade", "bad tag", "x".repeat(41)],
      actor: { type: "player", id: 7 }, target: { type: "player", id: 8, name: "Player 8" },
    });
    expect(JSON.parse(built.json)).toMatchObject({ tags: ["trade"], attributes: {
      amount: 42, actor: { type: "player", id: 7 }, target: { type: "player", id: 8, name: "Player 8" },
    } });
    expect(built.problems).toHaveLength(1);
  });
  it("limits tags to twenty", () => {
    const built = buildEvent({}, "INFO", "x", "", {}, { tags: Array.from({ length: 21 }, (_, i) => `tag${i}`) });
    expect(JSON.parse(built.json).tags).toHaveLength(20);
    expect(built.problems).toHaveLength(1);
  });
});
