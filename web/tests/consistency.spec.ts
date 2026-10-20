import { expect, test } from "@playwright/test";
import { ClusterPage, healCluster } from "./cluster-page";

test.afterEach(async ({ request }) => healCluster(request));

/**
 * Critical flow 2 — consistency over availability: with one node down a
 * majority still accepts writes; with two down, writes stall instead of
 * returning wrong data; once a majority is back, the committed write is there.
 */
test("writes survive one failure and stall without a majority", async ({ page }) => {
  const ui = new ClusterPage(page);
  await ui.open();
  const key = `e2e-${Date.now()}`;

  // one follower down: 2 of 3 is still a majority, so the write commits
  const [f1, f2] = await ui.followerIds();
  await ui.power(f1!).click();
  await expect(ui.downStrips).toHaveCount(1);
  expect(await ui.kv("put", key, "committed")).toBe(200);
  await expect(ui.guideStep("write-while-down")).toHaveAttribute("data-done", "true");

  // the second follower down: no majority, the quorum readout says writes stall
  await ui.power(f2!).click();
  await expect(ui.downStrips).toHaveCount(2);
  await expect(page.getByTestId("quorum-verdict")).toContainText("writes stall");
  const stalled = await ui.kv("put", key, "unconfirmed");
  expect(stalled).toBe(503);
  // a timed-out write is "not confirmed", never reported as done (its outcome is unknown)
  await expect(page.getByTestId("kv-result")).toContainText("not confirmed");
  await expect(page.getByTestId("kv-note")).toBeVisible();

  // majority back: reads work again and return one of the two writes — never anything else.
  // (The leader may still commit the unconfirmed write: that is correct Raft behaviour.)
  await ui.power(f1!).click();
  await expect(ui.leaderStrip).toHaveCount(1, { timeout: 15_000 });
  await expect(page.getByTestId("quorum-verdict")).toContainText("writes accepted");
  expect(await ui.kv("get", key)).toBe(200);
  await expect(page.getByTestId("kv-result")).toContainText(/"value": "(committed|unconfirmed)"/);
});

test("partitioning the leader makes it step down — no split brain", async ({ page }) => {
  const ui = new ClusterPage(page);
  await ui.open();
  const leader = await ui.leaderId();

  await ui.network(leader).click();
  await expect(ui.strip(leader)).toHaveAttribute("data-state", "partition");
  await expect(ui.network(leader)).toHaveText("Heal");

  // exactly one leader, and it is not the partitioned node
  await expect(ui.leaderStrip).toHaveCount(1, { timeout: 15_000 });
  expect(await ui.leaderId()).not.toBe(leader);
  await expect(ui.guideStep("partition-leader")).toHaveAttribute("data-done", "true");
});

test("stale reads are labelled as such @mobile", async ({ page }) => {
  const ui = new ClusterPage(page);
  await ui.open();
  expect(await ui.kv("put", "stale-demo", "v1")).toBe(200);
  expect(await ui.kv("get-stale", "stale-demo")).toBe(200);
  await expect(page.getByTestId("kv-result")).toContainText("stale read");
});
