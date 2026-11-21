import { act, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import App from "./App";
import { Marker, Polyline } from "leaflet";
const request = {
  id: "req-123",
  type: "water",
  location: "delhi",
  details: "Drinking water for a family",
  status: "pending",
  created_at: "2026-11-15T12:00:00Z",
};
const depot = {
  id: "res-123",
  type: "water",
  location: "water depot",
  status: "available",
  availability: true,
  coordinates: { latitude: 28.62, longitude: 77.21 },
};
type RequestFixture = typeof request & { coordinates?: { latitude: number; longitude: number }; directions?: string; distance_meters?: number; resource_id?: string };
let rows: RequestFixture[];
let resources: object[];
let requestsFail: boolean;
let ready: boolean;
let matchFails: boolean;
let posted: Record<string, unknown>[];
let configResponse: unknown;
let configFails: boolean;
beforeEach(() => {
  vi.stubGlobal("scrollTo", vi.fn());
  rows = [];
  resources = [];
  requestsFail = false;
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
      if (input === "/requests")
        return requestsFail
          ? Response.json({ error: "unavailable" }, { status: 503 })
          : Response.json(rows);
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

it("shows requests and resources with saved coordinates in the overview map", async () => {
  rows = [
    { ...request, location: "city hall", coordinates: { latitude: 28.61, longitude: 77.21 } },
    { ...request, id: "legacy", location: "old district" },
  ];
  resources = [{
    id: "res-123", type: "medicine", location: "health centre",
    status: "available", availability: true, coordinates: { latitude: 28.62, longitude: 77.22 },
  }];
  const user = userEvent.setup();
  render(<App />);
  await user.click(screen.getByRole("button", { name: "Map" }));
  const map = await screen.findByRole("region", { name: "Request and resource locations" });
  expect(await within(map).findByRole("button", { name: /request.*city hall/i })).toHaveClass("overview-pin--request");
  expect(within(map).getByRole("button", { name: /resource.*health centre/i })).toHaveClass("overview-pin--resource");
  expect(within(map).queryByRole("button", { name: /old district/i })).not.toBeInTheDocument();
  await user.click(within(map).getByRole("button", { name: /resource.*health centre/i }));
  const details = screen.getByRole("region", { name: "Selected map record" });
  expect(within(details).getByRole("heading", { name: "health centre" })).toBeInTheDocument();
  expect(within(details).getByText("Available")).toBeInTheDocument();
  expect(within(details).getByText("res-123")).toBeInTheDocument();
  await user.click(screen.getByRole("button", { name: "Show all pins" }));
  expect(screen.queryByRole("region", { name: "Selected map record" })).not.toBeInTheDocument();
  within(map).getByRole("button", { name: /request.*city hall/i }).focus();
  await user.keyboard("{Enter}");
  expect(screen.getByRole("heading", { name: "city hall" })).toBeInTheDocument();
  within(map).getByRole("button", { name: /resource.*health centre/i }).focus();
  await user.keyboard(" ");
  expect(screen.getByRole("heading", { name: "health centre" })).toBeInTheDocument();
});

it("opens the selected request from the list and highlights its assigned resource", async () => {
  rows = [{
    ...request, location: "city hall", status: "matched", resource_id: "res-123",
    coordinates: { latitude: 28.61, longitude: 77.21 }, distance_meters: 1250,
  }];
  resources = [{
    id: "res-123", type: "water", location: "water depot", status: "reserved",
    availability: false, coordinates: { latitude: 28.62, longitude: 77.21 },
  }];
  const user = userEvent.setup();
  render(<App />);
  await user.click(await screen.findByRole("button", { name: "Show request req-123 on map" }));
  const map = await screen.findByRole("region", { name: "Request and resource locations" });
  expect(within(map).getByRole("button", { name: /request.*city hall/i })).toHaveClass("overview-pin--selected");
  expect(within(map).getByRole("button", { name: /resource.*water depot/i })).toHaveClass("overview-pin--assigned");
  expect(map.querySelector(".overview-match-line")).toBeInTheDocument();
  const details = screen.getByRole("region", { name: "Selected map record" });
  expect(within(details).getByText("water depot")).toBeInTheDocument();
  expect(within(details).getByText("1.25 km")).toBeInTheDocument();
});

it("lets users choose among records sharing one pin", async () => {
  const coordinates = { latitude: 0, longitude: 0 };
  rows = [{ ...request, location: "north gate", coordinates }];
  resources = [{
    id: "res-456", type: "water", location: "south gate",
    status: "available", availability: true, coordinates,
  }];
  const user = userEvent.setup();
  render(<App />);
  await user.click(screen.getByRole("button", { name: "Map" }));
  const map = await screen.findByRole("region", { name: "Request and resource locations" });
  await user.click(await within(map).findByRole("button", { name: "2 records at this pin" }));
  const shared = screen.getByRole("list", { name: "Records at this pin" });
  await user.click(within(shared).getByRole("button", { name: /resource.*south gate/i }));
  expect(screen.getByRole("heading", { name: "south gate" })).toBeInTheDocument();
  expect(within(shared).getByRole("button", { name: /resource.*south gate/i })).toHaveAttribute("aria-pressed", "true");
});

it("opens a resource from the ledger and keeps an invalid coordinate record accessible there", async () => {
  resources = [depot, {
    ...depot,
    id: "res-invalid",
    location: "old depot",
    coordinates: { latitude: 91, longitude: 77.21 },
  }];
  const user = userEvent.setup();
  render(<App />);
  await user.click(screen.getByRole("tab", { name: /Resources/ }));
  const invalid = await screen.findByRole("row", { name: /old depot/i });
  expect(within(invalid).getByText("No coordinates saved")).toBeInTheDocument();
  expect(within(invalid).queryByRole("button", { name: /show.*on map/i })).not.toBeInTheDocument();
  await user.click(screen.getByRole("button", { name: "Show resource res-123 on map" }));
  expect(screen.getByRole("heading", { name: "Response map" })).toHaveFocus();
  const map = within(screen.getByRole("region", { name: "Request and resource locations" }));
  expect(map.getByRole("button", { name: /resource.*water depot/i })).toHaveAttribute("aria-pressed", "true");
  expect(map.queryByRole("button", { name: /old depot/i })).not.toBeInTheDocument();
  expect(screen.getByText(/1 record has no usable coordinates/i)).toBeInTheDocument();
});

it("draws an assigned resource link along the short path across the dateline", async () => {
  rows = [{
    ...request, status: "matched", resource_id: depot.id, distance_meters: 33358,
    coordinates: { latitude: 0, longitude: 179.9 },
  }];
  resources = [{ ...depot, coordinates: { latitude: 0, longitude: -179.8 } }];
  const added = vi.spyOn(Polyline.prototype, "addTo");
  try {
    const user = userEvent.setup();
    render(<App />);
    await user.click(await screen.findByRole("button", { name: "Show request req-123 on map" }));
    expect(await screen.findByText("Straight-line distance: 33.358 km")).toBeInTheDocument();
    const link = added.mock.instances[0];
    if (!(link instanceof Polyline)) throw new Error("Selecting the matched request did not draw a line");
    expect(link.getLatLngs()).toEqual([{ lat: 0, lng: 179.9 }, { lat: 0, lng: 180.2 }]);
  } finally {
    added.mockRestore();
  }
});

it("groups opposite signed dateline coordinates and reports a zero-distance assignment", async () => {
  rows = [{
    ...request, status: "matched", resource_id: depot.id, distance_meters: 0,
    coordinates: { latitude: 0, longitude: 180 },
  }];
  resources = [{ ...depot, coordinates: { latitude: 0, longitude: -180 } }];
  const user = userEvent.setup();
  render(<App />);
  await user.click(await screen.findByRole("button", { name: "Show request req-123 on map" }));
  const map = screen.getByRole("region", { name: "Request and resource locations" });
  expect(within(map).getByRole("button", { name: "2 records at this pin" })).toHaveAttribute("aria-pressed", "true");
  const details = within(screen.getByRole("region", { name: "Selected map record" }));
  expect(details.getByText("0 km")).toBeInTheDocument();
  expect(map.querySelector(".overview-match-line")).not.toBeInTheDocument();
});

it("updates a selected map request after completion and removes the active assignment line", async () => {
  const matched = {
    ...request, status: "matched", resource_id: depot.id, distance_meters: 1250,
    coordinates: { latitude: 28.61, longitude: 77.21 },
  };
  rows = [matched];
  resources = [{ ...depot, status: "reserved", availability: false }];
  const user = userEvent.setup();
  render(<App />);
  await user.click(await screen.findByRole("button", { name: "Show request req-123 on map" }));
  const map = screen.getByRole("region", { name: "Request and resource locations" });
  expect(map.querySelector(".overview-match-line")).toBeInTheDocument();
  rows = [{ ...matched, status: "completed" }];
  resources = [{ ...depot, status: "unavailable", availability: false }];
  await user.click(screen.getByRole("button", { name: "Refresh data" }));
  const details = within(screen.getByRole("region", { name: "Selected map record" }));
  expect(await details.findByText("Completed")).toBeInTheDocument();
  expect(details.getByText("Previous assignment")).toBeInTheDocument();
  expect(details.getByText("1.25 km")).toBeInTheDocument();
  expect(within(map).getByRole("button", { name: /resource.*water depot/i })).not.toHaveClass("overview-pin--assigned");
  expect(map.querySelector(".overview-match-line")).not.toBeInTheDocument();
});

it("keeps saved map pins selectable when map tiles fail", async () => {
  resources = [depot];
  const user = userEvent.setup();
  render(<App />);
  await user.click(screen.getByRole("button", { name: "Map" }));
  const map = screen.getByRole("region", { name: "Request and resource locations" });
  const pin = await within(map).findByRole("button", { name: /resource.*water depot/i });
  const tile = map.querySelector("img.leaflet-tile");
  if (!tile) throw new Error("The map did not request a tile");
  fireEvent.error(tile);
  expect(screen.getByRole("status")).toHaveTextContent(/map tiles could not load/i);
  await user.click(pin);
  expect(screen.getByRole("heading", { name: "water depot" })).toBeInTheDocument();
  expect(screen.getByRole("status")).toHaveTextContent(/saved pins are still available/i);
});

it("recovers from a partial map data outage without leaving a stale dashboard error", async () => {
  requestsFail = true;
  resources = [depot];
  const user = userEvent.setup();
  render(<App />);
  expect(await screen.findByRole("alert")).toHaveTextContent(/live data could not be loaded/i);
  await user.click(screen.getByRole("button", { name: "Map" }));
  expect(screen.getByRole("status")).toHaveTextContent(/some map records could not be loaded/i);
  const map = within(screen.getByRole("region", { name: "Request and resource locations" }));
  expect(map.getByRole("button", { name: /resource.*water depot/i })).toBeInTheDocument();

  requestsFail = false;
  rows = [{ ...request, coordinates: { latitude: 28.61, longitude: 77.21 } }];
  await user.click(screen.getByRole("button", { name: "Refresh data" }));
  expect(await map.findByRole("button", { name: /request.*delhi/i })).toBeInTheDocument();
  expect(screen.queryByText(/some map records could not be loaded/i)).not.toBeInTheDocument();
  await user.click(screen.getByRole("button", { name: "Overview" }));
  expect(screen.getByLabelText("Pending requests")).toHaveTextContent("1");
  expect(screen.queryByRole("alert")).not.toBeInTheDocument();
});

it("clears a stale dashboard data warning when polling restores map records", async () => {
  requestsFail = true;
  resources = [depot];
  vi.useFakeTimers();
  try {
    await act(async () => { render(<App />); });
    expect(screen.getByRole("alert")).toHaveTextContent(/live data could not be loaded/i);
    fireEvent.click(screen.getByRole("button", { name: "Map" }));
    expect(screen.getByRole("status")).toHaveTextContent(/some map records could not be loaded/i);

    requestsFail = false;
    rows = [{ ...request, coordinates: { latitude: 28.61, longitude: 77.21 } }];
    await act(async () => { await vi.advanceTimersByTimeAsync(15_000); });
    const map = within(screen.getByRole("region", { name: "Request and resource locations" }));
    expect(map.getByRole("button", { name: /request.*delhi/i })).toBeInTheDocument();
    expect(screen.queryByRole("status")).not.toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "Overview" }));
    expect(screen.queryByRole("alert")).not.toBeInTheDocument();
  } finally {
    vi.useRealTimers();
  }
});

it("keeps a request lookup error visible after a successful background refresh", async () => {
  vi.useFakeTimers();
  try {
    await act(async () => { render(<App />); });
    fireEvent.change(screen.getByLabelText("Request ID"), { target: { value: "missing" } });
    await act(async () => {
      fireEvent.click(screen.getByRole("button", { name: "Look up request" }));
    });
    expect(screen.getByRole("alert")).toHaveTextContent("Request not found");

    rows = [request];
    await act(async () => { await vi.advanceTimersByTimeAsync(15_000); });
    expect(screen.getByLabelText("Pending requests")).toHaveTextContent("1");
    expect(screen.getByRole("alert")).toHaveTextContent("Request not found");
  } finally {
    vi.useRealTimers();
  }
});

it("shows only the relevant refresh button in each activity tab", async () => {
  const user = userEvent.setup();
  render(<App />);
  expect(screen.getByRole("button", { name: "Refresh data" })).toBeInTheDocument();
  await user.click(screen.getByRole("tab", { name: "Shard details" }));
  expect(screen.getByRole("tab", { name: "Shard details" })).toHaveAttribute("aria-selected", "true");
  expect(screen.getByRole("tabpanel", { name: "Shard details" })).toBeInTheDocument();
  expect(await screen.findByText(/development view/i)).toBeInTheDocument();
  expect(screen.getByRole("button", { name: "Refresh shard details" })).toBeInTheDocument();
  expect(screen.queryByRole("button", { name: "Refresh data" })).not.toBeInTheDocument();
  await user.click(screen.getByRole("tab", { name: /Resources/ }));
  expect(screen.getByRole("button", { name: "Refresh data" })).toBeInTheDocument();
  expect(screen.queryByRole("button", { name: "Refresh shard details" })).not.toBeInTheDocument();
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
  rows = [{ ...request, coordinates: { latitude: 28.6, longitude: 77.2 } }];
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
it("lets a legacy request match by district without showing a geographic distance", async () => {
  rows = [request];
  const user = userEvent.setup();
  render(<App />);
  await user.click(await screen.findByRole("button", { name: "View request req-123" }));
  const dialog = within(screen.getByRole("dialog"));
  expect(dialog.getByText("No coordinates saved")).toBeInTheDocument();
  await user.click(dialog.getByRole("button", { name: "Find a match" }));
  expect(await dialog.findByText("res-123")).toBeInTheDocument();
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
  expect(screen.getByText("No coordinates saved")).toBeInTheDocument();
});
it("keeps an unmatched request pending and surfaces the server error", async () => {
  rows = [{ ...request, coordinates: { latitude: 28.6, longitude: 77.2 } }];
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
