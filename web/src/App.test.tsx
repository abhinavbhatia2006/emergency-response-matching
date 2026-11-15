import { render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import App from "./App";
const request = {
  id: "req-123",
  type: "water",
  location: "delhi",
  details: "Drinking water for a family",
  status: "pending",
  created_at: "2026-11-15T12:00:00Z",
};
let rows: (typeof request)[];
let resources: object[];
let ready: boolean;
let matchFails: boolean;
beforeEach(() => {
  rows = [];
  resources = [];
  ready = true;
  matchFails = false;
  vi.stubGlobal(
    "fetch",
    vi.fn(async (input: string, init?: RequestInit) => {
      if (input === "/readyz")
        return new Response("{}", { status: ready ? 200 : 503 });
      if (input === "/requests" && init?.method === "POST") {
        const body = JSON.parse(String(init.body));
        if (body.type !== "water" || body.location !== "delhi" || !body.details)
          return new Response('{"error":"bad input"}', { status: 400 });
        rows = [{ ...request, ...body }];
        return Response.json(rows[0], { status: 201 });
      }
      if (input === "/requests") return Response.json(rows);
      if (input === "/resources" && init?.method === "POST") {
        const body = JSON.parse(String(init.body));
        resources = [
          {
            id: "res-123",
            ...body,
            status: body.availability ? "available" : "unavailable",
          },
        ];
        return Response.json(resources[0], { status: 201 });
      }
      if (input === "/resources") return Response.json(resources);
      if (input === "/requests/req-123") return Response.json(rows[0]);
      if (input === "/match/req-123") {
        if (matchFails)
          return new Response('{"error":"no suitable resource available"}', {
            status: 409,
          });
        rows = [{ ...request, status: "matched" }];
        return Response.json({ ...rows[0], resource_id: "res-123" });
      }
      if (input === "/requests/req-123/status") {
        rows = [{ ...request, status: JSON.parse(String(init?.body)).status }];
        return Response.json(rows[0]);
      }
      return new Response('{"error":"Request not found"}', { status: 404 });
    }),
  );
});
afterEach(() => vi.unstubAllGlobals());
it.each(["New request", "Add resource", "Register your first resource"])(
  "clears a completed request notification when opening %s",
  async (buttonName) => {
    rows = [{ ...request, status: "matched" }];
    const user = userEvent.setup();
    render(<App />);
    await user.click(await screen.findByRole("button", { name: "View request req-123" }));
    await user.click(screen.getByRole("button", { name: "Mark completed" }));
    expect(await screen.findByText("Request marked completed.")).toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: "Close dialog" }));
    if (buttonName === "Register your first resource") {
      await user.click(screen.getByRole("tab", { name: /Resources/ }));
    }
    await user.click(screen.getByRole("button", { name: buttonName }));
    expect(screen.getByRole("dialog")).toBeInTheDocument();
    expect(screen.queryByText("Request marked completed.")).not.toBeInTheDocument();
  },
);
it("shows honest empty data and backend readiness", async () => {
  ready = false;
  render(<App />);
  expect(await screen.findByText("No requests yet")).toBeInTheDocument();
  expect(await screen.findByText("Service unavailable")).toBeInTheDocument();
  expect(screen.getByLabelText("Pending requests")).toHaveTextContent("0");
});
it("creates a request with the public payload and displays the saved result", async () => {
  const user = userEvent.setup();
  render(<App />);
  await screen.findByText("No requests yet");
  await user.click(screen.getByRole("button", { name: "New request" }));
  const dialog = screen.getByRole("dialog");
  await user.selectOptions(
    within(dialog).getByLabelText("Assistance type"),
    "water",
  );
  await user.type(within(dialog).getByLabelText("Location"), "delhi");
  await user.type(
    within(dialog).getByLabelText("Request details"),
    "Drinking water for a family",
  );
  await user.click(
    within(dialog).getByRole("button", { name: "Create request" }),
  );
  expect(
    await screen.findByRole("heading", { name: "Request details" }),
  ).toBeInTheDocument();
  expect(screen.getByText("Drinking water for a family")).toBeInTheDocument();
  expect(screen.getByLabelText("Pending requests")).toHaveTextContent("1");
});
it("matches a pending request and allows completion only after matching", async () => {
  rows = [request];
  const user = userEvent.setup();
  render(<App />);
  await user.click(
    await screen.findByRole("button", { name: "View request req-123" }),
  );
  expect(
    screen.queryByRole("button", { name: "Mark completed" }),
  ).not.toBeInTheDocument();
  await user.click(screen.getByRole("button", { name: "Find a match" }));
  expect(await screen.findByText("res-123")).toBeInTheDocument();
  await user.click(screen.getByRole("button", { name: "Mark completed" }));
  await waitFor(() =>
    expect(
      screen.queryByRole("button", { name: "Find a match" }),
    ).not.toBeInTheDocument(),
  );
  expect(
    await screen.findByText("Request marked completed."),
  ).toBeInTheDocument();
});
it("looks up an unknown request without inventing a result", async () => {
  const user = userEvent.setup();
  render(<App />);
  await user.type(screen.getByLabelText("Request ID"), "missing");
  await user.click(screen.getByRole("button", { name: "Look up request" }));
  expect(await screen.findByRole("alert")).toHaveTextContent(
    "Request not found",
  );
});
it("registers an unavailable resource without miscounting available resources", async () => {
  const user = userEvent.setup();
  render(<App />);
  await user.click(screen.getByRole("button", { name: "Add resource" }));
  const dialog = screen.getByRole("dialog");
  await user.type(within(dialog).getByLabelText("Location"), "delhi");
  await user.click(within(dialog).getByLabelText("Available for matching"));
  await user.click(
    within(dialog).getByRole("button", { name: "Register resource" }),
  );
  expect(await screen.findByText("res-123")).toBeInTheDocument();
  expect(screen.getByLabelText("Available resources")).toHaveTextContent("0");
});
it("keeps an unmatched request pending and surfaces the server error", async () => {
  rows = [request];
  matchFails = true;
  const user = userEvent.setup();
  render(<App />);
  await user.click(
    await screen.findByRole("button", { name: "View request req-123" }),
  );
  await user.click(screen.getByRole("button", { name: "Find a match" }));
  expect(await screen.findByRole("alert")).toHaveTextContent(
    "no suitable resource available",
  );
  expect(
    within(screen.getByRole("dialog")).getByText("pending"),
  ).toBeInTheDocument();
  expect(
    screen.queryByRole("button", { name: "Mark completed" }),
  ).not.toBeInTheDocument();
});
it("cancels a pending request and removes lifecycle mutation buttons", async () => {
  rows = [request];
  const user = userEvent.setup();
  render(<App />);
  await user.click(
    await screen.findByRole("button", { name: "View request req-123" }),
  );
  await user.click(screen.getByRole("button", { name: "Cancel request" }));
  expect(
    await screen.findByText("Request marked cancelled."),
  ).toBeInTheDocument();
  expect(
    screen.queryByRole("button", { name: "Find a match" }),
  ).not.toBeInTheDocument();
  expect(
    screen.queryByRole("button", { name: "Cancel request" }),
  ).not.toBeInTheDocument();
});
it("prevents manual changes to a reserved resource", async () => {
  resources = [
    {
      id: "res-123",
      type: "water",
      location: "delhi",
      availability: false,
      status: "reserved",
    },
  ];
  const user = userEvent.setup();
  render(<App />);
  await user.click(screen.getByRole("tab", { name: /Resources/ }));
  expect(
    await screen.findByRole("button", { name: "Mark available res-123" }),
  ).toBeDisabled();
  expect(screen.getByLabelText("Available resources")).toHaveTextContent("0");
});
