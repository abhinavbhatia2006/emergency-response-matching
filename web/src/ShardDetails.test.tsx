import { render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, expect, it, vi } from "vitest";
import ShardDetails from "./ShardDetails";

const topology = { shards: [{ id: 0, nodes: [
  { id: "shard-0-primary", ready: true, last_heartbeat: "2026-11-18T12:00:00Z" },
  { id: "shard-0-replica", ready: false, last_heartbeat: null },
] }] };
afterEach(() => vi.unstubAllGlobals());

it("shows live shard identities, coordinator readiness and missing heartbeat honestly", async () => {
  vi.stubGlobal("fetch", vi.fn(async () => Response.json(topology)));
  render(<ShardDetails />);
  expect(screen.getByText("Loading shard details…")).toBeInTheDocument();
  expect(await screen.findByRole("heading", { name: "Shard 0" })).toBeInTheDocument();
  const primary = screen.getByText("shard-0-primary").closest("li")!;
  expect(within(primary).getByText("Ready")).toBeInTheDocument();
  const replica = screen.getByText("shard-0-replica").closest("li")!;
  expect(within(replica).getByText("Not ready")).toBeInTheDocument();
  expect(within(replica).getByText("No heartbeat recorded")).toBeInTheDocument();
  expect(fetch).toHaveBeenCalledWith("/development/shards", expect.any(Object));
});

it("clears stale ready nodes on failure and refreshes successfully", async () => {
  const get = vi.fn().mockResolvedValueOnce(Response.json(topology))
    .mockResolvedValueOnce(new Response("unavailable", { status: 503 }))
    .mockResolvedValueOnce(Response.json(topology));
  vi.stubGlobal("fetch", get);
  const user = userEvent.setup();
  render(<ShardDetails />);
  await screen.findByText("shard-0-primary");
  await user.click(screen.getByRole("button", { name: "Refresh shard details" }));
  expect(await screen.findByRole("alert")).toHaveTextContent("Shard details unavailable");
  expect(screen.queryByText("shard-0-primary")).not.toBeInTheDocument();
  await user.click(screen.getByRole("button", { name: "Refresh shard details" }));
  expect(await screen.findByText("shard-0-primary")).toBeInTheDocument();
  expect(screen.queryByRole("alert")).not.toBeInTheDocument();
});

it.each([{}, { shards: null }, { shards: [{ id: 0, nodes: [{ id: "node", ready: "true", last_heartbeat: null }] }] }])("rejects malformed topology instead of inventing readiness: %j", async (value) => {
  vi.stubGlobal("fetch", vi.fn(async () => Response.json(value)));
  render(<ShardDetails />);
  expect(await screen.findByRole("alert")).toHaveTextContent("Shard details unavailable");
});

it("reports an empty topology without inventing configured nodes", async () => {
  vi.stubGlobal("fetch", vi.fn(async () => Response.json({ shards: [] })));
  render(<ShardDetails />);
  expect(await screen.findByText("No configured shards were reported.")).toBeInTheDocument();
  await waitFor(() => expect(screen.getByRole("button", { name: "Refresh shard details" })).toBeEnabled());
});
