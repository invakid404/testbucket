import { test, expect } from "vitest";
const sleep = (ms: number) => new Promise((r) => setTimeout(r, ms));
test("medium a", async () => { await sleep(60); expect(1).toBe(1); });
test("medium b", async () => { await sleep(40); expect(1).toBe(1); });
