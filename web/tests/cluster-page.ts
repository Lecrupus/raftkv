import { expect, type APIRequestContext, type Locator, type Page } from "@playwright/test";

/**
 * Page object for the RaftKV console. Every locator is a data-testid or a
 * data-* state attribute, never copy or CSS classes, so a restyle or a copy
 * edit cannot break the suite.
 */
export class ClusterPage {
  readonly page: Page;
  constructor(page: Page) {
    this.page = page;
  }

  async open() {
    await this.page.goto("/");
    await expect(this.page.getByTestId("cluster")).toBeVisible();
    await expect(this.leaderStrip).toHaveCount(1); // wait for an elected leader
  }

  strip(id: string): Locator {
    return this.page.getByTestId(`node-strip-${id}`);
  }
  power(id: string): Locator {
    return this.page.getByTestId(`node-${id}-power`);
  }
  network(id: string): Locator {
    return this.page.getByTestId(`node-${id}-network`);
  }
  get leaderStrip(): Locator {
    return this.page.locator('[data-testid^="node-strip-"][data-state="leader"]');
  }
  get downStrips(): Locator {
    return this.page.locator('[data-testid^="node-strip-"][data-state="down"]');
  }
  readout(name: "leader" | "term" | "quorum" | "commit" | "failover"): Locator {
    return this.page.getByTestId(`readout-${name}-value`);
  }
  guideStep(id: string): Locator {
    return this.page.getByTestId(`guide-step-${id}`);
  }

  async leaderId(): Promise<string> {
    const id = await this.leaderStrip.getAttribute("data-node-id");
    if (!id) throw new Error("no leader strip");
    return id;
  }

  async followerIds(): Promise<string[]> {
    return this.page.locator('[data-testid^="node-strip-"][data-state="follower"]').evaluateAll((els) =>
      els.map((e) => e.getAttribute("data-node-id") ?? ""),
    );
  }

  /** Fill the keyspace form and run one operation; resolves with the HTTP status shown. */
  async kv(op: "put" | "get" | "get-stale" | "delete", key: string, value?: string): Promise<number> {
    await this.page.getByTestId("kv-key").fill(key);
    if (value !== undefined) await this.page.getByTestId("kv-value").fill(value);
    const result = this.page.getByTestId("kv-result");
    const before = await result.textContent();
    await this.page.getByTestId(`kv-${op}`).click();
    await expect(this.page.getByTestId(`kv-put`)).toBeEnabled({ timeout: 20_000 }); // busy state cleared
    await expect(result).not.toHaveText(before ?? "");
    return Number(await result.getAttribute("data-status"));
  }
}

/** Restore every node through the control API so each test starts from a healthy cluster. */
export async function healCluster(request: APIRequestContext) {
  const view = await (await request.get("/api/cluster")).json();
  for (const n of view.nodes as Array<{ id: string; up: boolean; status?: { isolated: boolean } }>) {
    if (!n.up) await request.post(`/api/nodes/${n.id}/start`);
    else if (n.status?.isolated) await request.post(`/api/nodes/${n.id}/heal`);
  }
  await expect
    .poll(async () => (await (await request.get("/api/cluster")).json()).leader, { timeout: 15_000 })
    .not.toBe("");
}
