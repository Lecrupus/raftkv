import { expect, test } from "@playwright/test";
import { ClusterPage, healCluster } from "./cluster-page";

test.afterEach(async ({ request }) => healCluster(request));

/**
 * Critical flow 1 — the demo's whole point: kill the leader, a follower wins
 * an election, the failover is measured, and the guide proves the step.
 */
test("killing the leader elects a new one and measures the failover", async ({ page }) => {
  const ui = new ClusterPage(page);
  await ui.open();

  const oldLeader = await ui.leaderId();
  const oldTerm = Number(await ui.readout("term").textContent());

  await ui.power(oldLeader).click();

  // the killed node is drawn as down, with its power control flipped to Start
  await expect(ui.strip(oldLeader)).toHaveAttribute("data-state", "down");
  await expect(ui.power(oldLeader)).toHaveText("Start");

  // a different node takes over in a higher term
  await expect(ui.leaderStrip).toHaveCount(1);
  const newLeader = await ui.leaderId();
  expect(newLeader).not.toBe(oldLeader);
  await expect.poll(async () => Number(await ui.readout("term").textContent())).toBeGreaterThan(oldTerm);

  // the failover readout shows a measured time and the leader-killed cause
  await expect(ui.readout("failover")).toContainText("ms");
  await expect(page.getByTestId("readout-failover")).toContainText(`${oldLeader} → ${newLeader}`);

  // the spectrum lands a leader emission line; the journal and screen-reader narration agree
  await expect(page.locator('[data-testid="spectrum-event"][data-kind="leader"]').first()).toBeAttached();
  await expect(page.locator('[data-testid="journal-row"][data-kind="killed"]').first()).toContainText(oldLeader);
  await expect(page.getByTestId("announcer")).toContainText(`${newLeader} elected leader`);

  // the guide checks itself off from the server's failover record
  await expect(ui.guideStep("kill-leader")).toHaveAttribute("data-done", "true");

  // restart: the node replays its WAL and rejoins as a follower
  await ui.power(oldLeader).click();
  await expect(ui.strip(oldLeader)).toHaveAttribute("data-state", "follower", { timeout: 15_000 });
  await expect(page.getByTestId(`node-${oldLeader}-lag`)).toHaveText("in sync", { timeout: 15_000 });
});
