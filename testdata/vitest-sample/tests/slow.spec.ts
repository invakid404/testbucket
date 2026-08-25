import { test, expect } from "vitest";
const sleep = (ms: number) => new Promise((r) => setTimeout(r, ms));
test("slow io", async () => { await sleep(180); expect(true).toBe(true); });
test("slow calc", async () => { await sleep(120); expect(2 * 2).toBe(4); });
