import { act, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import App from "./App";
import { Marker } from "leaflet";
const request = {
  id: "req-123",
  type: "water",
  location: "delhi",
  details: "Drinking water for a family",
  status: "pending",
  created_at: "2026-11-15T12:00:00Z",
};
type RequestFixture = typeof request & { coordinates?: { latitude: number; longitude: number }; directions?: string; distance_meters?: number; resource_id?: string };
let rows: RequestFixture[];
let resources: object[];
let ready: boolean;
let matchFails: boolean;
let posted: Record<string, unknown>[];
let configResponse: unknown;
let configFails: boolean;
beforeEach(() => {
  rows = [];
  resources = [];
  ready = true;
  matchFails = false;
  posted = [];
  configResponse = {max_distance_meters: 50000, distance_method: "haversine"};
  configFails = false;
  vi.stubGlobal(
    "fetch",
    vi.fn(async (input: string, init?: RequestInit) => {
      if (input === "/config") return configFails ? new Response('{"error":"unavailable"}', {status: 503}) : Response.json(configResponse);
      if (input === "/readyz")
        return new Response("{}", { status: ready ? 200 : 503 });
      if (input === "/requests" && init?.method === "POST") {
        const body = JSON.parse(String(init.body));
        posted.push(body);
        if (body.type !== "water" || !body.coordinates || !body.details)
          return new Response('{"error":"bad input"}', { status: 400 });
        rows = [{ ...request, ...body, location: body.location?.toLowerCase() ?? "" }];
        return Response.json(rows[0], { status: 201 });
      }
      if (input === "/requests") return Response.json(rows);
      if (input === "/resources" && init?.method === "POST") {
        const body = JSON.parse(String(init.body));
        posted.push(body);
        resources = [
          {
            id: "res-123",
            ...body,
            location: body.location?.toLowerCase() ?? "",
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
it("offers a third Shard details activity tab", async () => {
  const user = userEvent.setup();
  render(<App />);
  await user.click(screen.getByRole("tab", { name: "Shard details" }));
  expect(screen.getByRole("tab", { name: "Shard details" })).toHaveAttribute("aria-selected", "true");
  expect(screen.getByRole("tabpanel", { name: "Shard details" })).toBeInTheDocument();
  expect(await screen.findByText(/development view/i)).toBeInTheDocument();
});
it("keeps keyboard focus inside the dialog after completing a request", async () => {
  rows = [{ ...request, status: "matched" }];
  const user = userEvent.setup();
  render(<App />);
  await user.click(await screen.findByRole("button", { name: "View request req-123" }));
  await user.click(screen.getByRole("button", { name: "Mark completed" }));
  await screen.findByText("Request marked completed.");
  await waitFor(() => expect(screen.getByRole("button", { name: "Close dialog" })).toHaveFocus());
  await user.keyboard("{Escape}");
  expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
});
it("shows the actual configured geographic matching limit", async () => {
  configResponse = {max_distance_meters: 12500, distance_method: "haversine"};
  const user = userEvent.setup();
  render(<App />);
  await waitFor(() => expect(screen.getByLabelText("Matching distance limit")).toHaveTextContent("12.5 km maximum"));
  await user.click(screen.getByRole("button", {name: "New request"}));
  expect(within(screen.getByRole("dialog")).getByText(/maximum matching distance: 12.5 km/i)).toBeInTheDocument();
});
it("shows unavailable instead of a guessed default when configuration cannot load", async () => {
  configFails = true;
  render(<App />);
  await screen.findByText("No requests yet");
  expect(screen.getByLabelText("Matching distance limit")).toHaveTextContent("Matching limit unavailable");
  expect(screen.queryByText(/50 km/)).not.toBeInTheDocument();
});
it.each([{max_distance_meters: -1, distance_method: "haversine"}, {max_distance_meters: "50000", distance_method: "haversine"}, {max_distance_meters: 50000, distance_method: "driving"}, null])("rejects unusable distance configuration %j", async (configuration) => {
  configResponse = configuration;
  render(<App />);
  await screen.findByText("No requests yet");
  expect(screen.getByLabelText("Matching distance limit")).toHaveTextContent("Matching limit unavailable");
});
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
  expect(within(dialog).queryByLabelText(/^location$/i)).not.toBeInTheDocument();
  await user.type(within(dialog).getByLabelText("Name"), "Gate 2");
  await user.type(within(dialog).getByLabelText("Latitude"), "0");
  await user.type(within(dialog).getByLabelText("Longitude"), "0");
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
  expect(within(screen.getByRole("dialog")).getByText("gate 2")).toBeInTheDocument();
  expect(posted[0]).toEqual({ type: "water", location: "Gate 2", details: "Drinking water for a family", coordinates: { latitude: 0, longitude: 0 } });
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
  await user.type(within(dialog).getByLabelText("Latitude"), "-90");
  await user.type(within(dialog).getByLabelText("Longitude"), "180");
  await user.click(within(dialog).getByLabelText("Available for matching"));
  await user.click(
    within(dialog).getByRole("button", { name: "Register resource" }),
  );
  expect(await screen.findByText("res-123")).toBeInTheDocument();
  expect(screen.getByLabelText("Available resources")).toHaveTextContent("0");
  expect(screen.getByText("Pinned location")).toBeInTheDocument();
  expect(posted[0]).toEqual({ type: "food", availability: false, coordinates: { latitude: -90, longitude: 180 } });
});

it("submits the device location with a new request only after the user asks", async () => {
  const position = vi.fn((success: PositionCallback) => success({
    coords: { latitude: 28.6139, longitude: 77.209 },
  } as GeolocationPosition));
  vi.stubGlobal("isSecureContext", true);
  vi.stubGlobal("navigator", Object.assign(Object.create(navigator), { geolocation: { getCurrentPosition: position } }));
  render(<App />);
  fireEvent.click(await screen.findByRole("button", { name: "New request" }));
  expect(position).not.toHaveBeenCalled();
  fireEvent.click(screen.getByRole("button", { name: "Use my location" }));
  fireEvent.change(screen.getByLabelText("Assistance type"), { target: { value: "water" } });
  fireEvent.change(screen.getByLabelText("Request details"), { target: { value: "Water needed" } });
  fireEvent.click(screen.getByRole("button", { name: "Create request" }));
  await waitFor(() => expect(posted[0]).toMatchObject({ coordinates: { latitude: 28.6139, longitude: 77.209 } }));
});

it("submits the resource pin even when an older device location arrives later", async () => {
  let receivePosition!: PositionCallback;
  vi.stubGlobal("isSecureContext", true);
  vi.stubGlobal("navigator", Object.assign(Object.create(navigator), { geolocation: {
    getCurrentPosition: (onPosition: PositionCallback) => { receivePosition = onPosition; },
  } }));
  render(<App />);
  fireEvent.click(await screen.findByRole("button", { name: "Add resource" }));
  fireEvent.click(screen.getByRole("button", { name: "Use my location" }));
  fireEvent.click(screen.getByRole("region", { name: "Location map" }), { clientX: 50, clientY: 80 });
  const latitude = (screen.getByLabelText("Latitude") as HTMLInputElement).valueAsNumber;
  const longitude = (screen.getByLabelText("Longitude") as HTMLInputElement).valueAsNumber;
  expect(Number.isFinite(latitude) && Number.isFinite(longitude)).toBe(true);
  act(() => receivePosition({ coords: { latitude: 1, longitude: 2 } } as GeolocationPosition));
  fireEvent.change(screen.getByLabelText("Name"), { target: { value: "Main gate" } });
  fireEvent.click(screen.getByRole("button", { name: "Register resource" }));
  await waitFor(() => expect(posted[0]).toMatchObject({ location: "Main gate", coordinates: { latitude, longitude } }));
});

it("keeps manual coordinates available when location permission is denied", async () => {
  vi.stubGlobal("isSecureContext", true);
  vi.stubGlobal("navigator", Object.assign(Object.create(navigator), { geolocation: {
    getCurrentPosition: (_: PositionCallback, onError: PositionErrorCallback) => onError({ code: 1 } as GeolocationPositionError),
  } }));
  render(<App />);
  fireEvent.click(await screen.findByRole("button", { name: "Add resource" }));
  fireEvent.click(screen.getByRole("button", { name: "Use my location" }));
  expect(screen.getByText(/permission was denied/i)).toBeInTheDocument();
  fireEvent.change(screen.getByLabelText("Latitude"), { target: { value: "0" } });
  expect(screen.getByLabelText("Latitude")).toHaveValue(0);
});

it("uses the final dragged position and allows manual entry if tiles fail", async () => {
  const added = vi.spyOn(Marker.prototype, "addTo");
  try {
    render(<App />);
    fireEvent.click(await screen.findByRole("button", { name: "Add resource" }));
    const map = screen.getByRole("region", { name: "Location map" });
    fireEvent.click(map, { clientX: 110, clientY: 72 });
    const marker = added.mock.instances[0];
    if (!(marker instanceof Marker)) throw new Error("Map click did not create a marker");
    expect(marker.dragging?.enabled()).toBe(true);
    act(() => { marker.setLatLng([12.5, 76.25]).fire("dragend"); });
    expect(screen.getByLabelText("Latitude")).toHaveValue(12.5);
    expect(screen.getByLabelText("Longitude")).toHaveValue(76.25);
    fireEvent.error(map.querySelector("img.leaflet-tile")!);
    expect(screen.getByText(/tiles could not load/i)).toBeInTheDocument();
    fireEvent.change(screen.getByLabelText("Longitude"), { target: { value: "77" } });
    fireEvent.blur(screen.getByLabelText("Longitude"));
    expect(screen.getByLabelText("Longitude")).toHaveValue(77);
  } finally {
    added.mockRestore();
  }
});
it.each([
  ["91", "0"], ["-91", "0"], ["0", "181"], ["0", "-181"], ["", "0"], ["0", ""],
])("rejects invalid coordinate pair %s / %s before sending a resource", async (latitude, longitude) => {
  const user = userEvent.setup();
  render(<App />);
  await user.click(screen.getByRole("button", { name: "Add resource" }));
  const dialog = screen.getByRole("dialog");
  if (latitude) await user.type(within(dialog).getByLabelText("Latitude"), latitude);
  if (longitude) await user.type(within(dialog).getByLabelText("Longitude"), longitude);
  // Exercise the submit boundary even when native constraint validation is bypassed.
  fireEvent.submit(within(dialog).getByRole("button", { name: "Register resource" }).closest("form")!);
  expect(await screen.findByRole("alert")).toHaveTextContent(/latitude.*-90.*90.*longitude.*-180.*180/i);
  expect(posted).toEqual([]);
  expect(screen.getByRole("dialog")).toBeInTheDocument();
});
it.each([[12345, "12.345 km"], [0, "0 km"], [50000, "50 km"]])("shows the assigned straight-line distance %s", async (meters, label) => {
  rows = [{ ...request, status: "matched", resource_id: "res-123", coordinates: {latitude: 28.6, longitude: 77.2}, distance_meters: Number(meters) }];
  const user = userEvent.setup();
  render(<App />);
  await user.click(await screen.findByRole("button", { name: "View request req-123" }));
  const dialog = within(screen.getByRole("dialog"));
  expect(dialog.getByText(String(label))).toBeInTheDocument();
  expect(dialog.getByText("28.6, 77.2")).toBeInTheDocument();
  expect(dialog.getByText(/straight-line geographic distance/i)).toBeInTheDocument();
  expect(dialog.queryByRole("link")).not.toBeInTheDocument();
});
it("labels legacy requests explicitly without inventing geographic distance", async () => {
  rows = [{ ...request, status: "matched", resource_id: "res-123" }];
  const user = userEvent.setup();
  render(<App />);
  await user.click(await screen.findByRole("button", { name: "View request req-123" }));
  const dialog = within(screen.getByRole("dialog"));
  expect(dialog.getByText("District matching · no coordinates")).toBeInTheDocument();
  expect(dialog.queryByText(/\d.*km/)).not.toBeInTheDocument();
});
it("shows resource coordinates and distinguishes coordinate-less resources in the ledger", async () => {
  resources = [
    { id: "res-geo", type: "water", location: "delhi", availability: true, status: "available", coordinates: {latitude: 0, longitude: 0} },
    { id: "res-old", type: "water", location: "delhi", availability: false, status: "unavailable" },
  ];
  const user = userEvent.setup();
  render(<App />);
  await user.click(screen.getByRole("tab", {name: /Resources/}));
  expect(await screen.findByText("0, 0")).toBeInTheDocument();
  expect(screen.getByText("District matching · no coordinates")).toBeInTheDocument();
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
