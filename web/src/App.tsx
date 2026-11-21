import ShardDetails from "./ShardDetails";
import LocationPicker from "./LocationPicker";
import OverviewMap from "./OverviewMap";
import { kilometers, title, validCoordinates, type Coordinates, type Kind, type RequestRecord, type Resource } from "./records";
import {
  useCallback,
  useEffect,
  useRef,
  useState,
  type FormEvent,
  type ReactNode,
} from "react";
import {
  ArrowRight,
  ArrowUpRight,
  Box,
  CheckCircle2,
  CircleHelp,
  ClipboardList,
  Clock3,
  Droplets,
  HeartHandshake,
  Home,
  LayoutDashboard,
  MapPin,
  Package,
  Plus,
  RefreshCw,
  ShieldCheck,
  Truck,
  Utensils,
  X,
} from "lucide-react";

const kinds: Kind[] = [
  "food",
  "water",
  "medicine",
  "transportation",
  "shelter",
];
const icons = {
  food: Utensils,
  water: Droplets,
  medicine: HeartHandshake,
  transportation: Truck,
  shelter: Home,
};
const coordinateLabel = ({ latitude, longitude }: Coordinates) => `${latitude}, ${longitude}`;
type DashboardError = { source: "refresh" | "action"; message: string };
async function api<T>(
  path: string,
  method = "GET",
  body?: unknown,
): Promise<T> {
  const response = await fetch(path, {
    method,
    headers: body ? { "Content-Type": "application/json" } : undefined,
    body: body ? JSON.stringify(body) : undefined,
    signal: AbortSignal.timeout(12000),
  });
  if (!response.ok) {
    let message = `Unable to complete the request (${response.status}).`;
    try {
      const data = await response.json();
      if (typeof data.error === "string") message = data.error;
    } catch {
      /* Non-JSON gateway errors retain the status. */
    }
    throw new Error(message);
  }
  return response.json() as Promise<T>;
}
function Badge({ status }: { status: string }) {
  return (
    <span className={`pill ${status}`}>
      <span
        className="dot"
        style={{ background: "currentColor", width: 4, height: 4 }}
      />
      {status}
    </span>
  );
}
function Dialog({
  title: heading,
  onClose,
  children,
}: {
  title: string;
  onClose: () => void;
  children: ReactNode;
}) {
  const ref = useRef<HTMLDivElement>(null);
  useEffect(() => {
    // A status change can remove the focused action button while the dialog stays open.
    if (document.activeElement === document.body) {
      ref.current?.querySelector<HTMLElement>("button:not(:disabled),input,select,textarea")?.focus();
    }
  });
  useEffect(() => {
    const previous = document.activeElement as HTMLElement | null;
    const original = document.body.style.overflow;
    document.body.style.overflow = "hidden";
    ref.current?.querySelector<HTMLElement>("input,select,button")?.focus();
    return () => {
      document.body.style.overflow = original;
      previous?.focus();
    };
  }, []);
  return (
    <div
      className="overlay"
      onClick={(event) => {
        if (event.target === event.currentTarget) onClose();
      }}
    >
      <div
        ref={ref}
        role="dialog"
        aria-modal="true"
        aria-labelledby="dialog-title"
        className="dialog"
        onKeyDown={(event) => {
          if (event.key === "Escape") onClose();
          if (event.key === "Tab") {
            const items = ref.current?.querySelectorAll<HTMLElement>(
              'button:not(:disabled),input,select,textarea,a[href],[tabindex="0"]',
            );
            if (!items?.length) return;
            const first = items[0],
              last = items[items.length - 1];
            if (event.shiftKey && document.activeElement === first) {
              event.preventDefault();
              last.focus();
            } else if (!event.shiftKey && document.activeElement === last) {
              event.preventDefault();
              first.focus();
            }
          }
        }}
      >
        <header>
          <h2 id="dialog-title">{heading}</h2>
          <button
            className="icon-button"
            aria-label="Close dialog"
            onClick={onClose}
          >
            <X size={20} />
          </button>
        </header>
        {children}
      </div>
    </div>
  );
}
export default function App() {
  const [requests, setRequests] = useState<RequestRecord[] | null>(null),
    [resources, setResources] = useState<Resource[] | null>(null);
  const [health, setHealth] = useState<"checking" | "ready" | "offline">(
    "checking",
  );
  const [tab, setTab] = useState<"requests" | "resources" | "shard details">("requests"),
    [nav, setNav] = useState("Overview");
  const [mapSelection, setMapSelection] = useState<string | null>(null);
  const [form, setForm] = useState<"request" | "resource" | null>(null),
    [selected, setSelected] = useState<RequestRecord | null>(null);
  const [busy, setBusy] = useState(false),
    [refreshing, setRefreshing] = useState(false),
    [error, setError] = useState<DashboardError | null>(null),
    [notice, setNotice] = useState(""),
    [lookup, setLookup] = useState(""),
    [updated, setUpdated] = useState<Date | null>(null);
  const [maxDistance, setMaxDistance] = useState<number | null>(null);
  const limitDescription = maxDistance === null ? "Matching limit unavailable." : `Maximum matching distance: ${kilometers(maxDistance)}.`;
  const refresh = useCallback(async () => {
    setRefreshing(true);
    const results = await Promise.allSettled([
      api<RequestRecord[] | null>("/requests"),
      api<Resource[] | null>("/resources"),
      fetch("/readyz", { signal: AbortSignal.timeout(12000) }).then(
        (r) => r.ok,
      ),
      api<unknown>("/config"),
    ]);
    const [requestResult, resourceResult, readyResult, configResult] = results;
    const config = configResult.status === "fulfilled" && typeof configResult.value === "object" && configResult.value !== null ? configResult.value as Record<string, unknown> : null;
    setMaxDistance(config?.distance_method === "haversine" && typeof config.max_distance_meters === "number" && Number.isFinite(config.max_distance_meters) && config.max_distance_meters >= 0 ? config.max_distance_meters : null);
    if (requestResult.status === "fulfilled")
      setRequests(requestResult.value ?? []);
    else setRequests(null);
    if (resourceResult.status === "fulfilled")
      setResources(resourceResult.value ?? []);
    else setResources(null);
    setHealth(
      readyResult.status === "fulfilled" && readyResult.value
        ? "ready"
        : "offline",
    );
    if (
      requestResult.status === "fulfilled" &&
      resourceResult.status === "fulfilled"
    ) {
      setError((current) => current?.source === "refresh" ? null : current);
      setUpdated(new Date());
    } else {
      setError({
        source: "refresh",
        message: "Live data could not be loaded. Check the service connection and refresh.",
      });
    }
    setRefreshing(false);
  }, []);
  useEffect(() => {
    void refresh();
    const timer = setInterval(() => void refresh(), 15000);
    return () => clearInterval(timer);
  }, [refresh]);
  function refreshData() {
    setError(null);
    void refresh();
  }
  async function action(work: () => Promise<void>) {
    setBusy(true);
    setError(null);
    setNotice("");
    try {
      await work();
    } catch (err) {
      setError({
        source: "action",
        message: err instanceof Error
          ? err.message
          : "The service could not be reached. Please try again.",
      });
    } finally {
      setBusy(false);
    }
  }
  const close = () => {
    if (!busy) {
      setForm(null);
      setSelected(null);
    }
  };
  const openForm = (kind: "request" | "resource") => {
    setError(null);
    setNotice("");
    setForm(kind);
  };
  const showOnMap = (kind: "request" | "resource", id: string) => {
    setMapSelection(`${kind}/${id}`);
    setNav("Map");
  };
  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    const data = new FormData(event.currentTarget);
    await action(async () => {
      const latitudeText = String(data.get("latitude") ?? "").trim();
      const longitudeText = String(data.get("longitude") ?? "").trim();
      const latitude = Number(latitudeText);
      const longitude = Number(longitudeText);
      if (!latitudeText || !longitudeText || !Number.isFinite(latitude) || !Number.isFinite(longitude) || latitude < -90 || latitude > 90 || longitude < -180 || longitude > 180) {
        throw new Error("Enter both coordinates: latitude must be -90 to 90 and longitude must be -180 to 180.");
      }
      const body = {
        type: String(data.get("type")),
        location: String(data.get("location") ?? "").trim() || undefined,
        coordinates: { latitude, longitude },
      };
      if (form === "request") {
        const saved = await api<RequestRecord>("/requests", "POST", {
          ...body,
          details: String(data.get("details")).trim(),
        });
        setForm(null);
        setSelected(saved);
        setTab("requests");
        setNotice("Request created. Matching will run automatically.");
      } else {
        await api<Resource>("/resources", "POST", {
          ...body,
          availability: data.get("availability") === "on",
        });
        setForm(null);
        setTab("resources");
        setNotice("Resource registered.");
      }
      await refresh();
    });
  }
  async function updateRequest(status: "completed" | "cancelled") {
    if (!selected) return;
    await action(async () => {
      setSelected(
        await api<RequestRecord>(
          `/requests/${encodeURIComponent(selected.id)}/status`,
          "PUT",
          { status },
        ),
      );
      setNotice(`Request marked ${status}.`);
      await refresh();
    });
  }
  const pending = requests?.filter((r) => r.status === "pending").length;
  const available = resources?.filter(
    (r) => r.status === "available" && r.availability,
  ).length;
  const completed = requests?.filter((r) => r.status === "completed").length;
  const count = (number: number | undefined) =>
    number === undefined ? "—" : number;
  const rows = tab === "requests" ? requests : resources;
  const Feedback = () => (
    <>
      {error && (
        <div role="alert" className="feedback error">
          <CircleHelp size={16} />
          <span>{error.message}</span>
          <button aria-label="Dismiss error" onClick={() => setError(null)}>
            <X size={15} />
          </button>
        </div>
      )}
      {notice && (
        <div role="status" className="feedback success">
          <CheckCircle2 size={16} />
          <span>{notice}</span>
          <button
            aria-label="Dismiss notification"
            onClick={() => setNotice("")}
          >
            <X size={15} />
          </button>
        </div>
      )}
    </>
  );
  return (
    <>
      <header className="site-header">
        <div className="brand">
          <img className="brand-icon" src="/sahara.svg" width="40" height="40" alt="" />
          <strong>Sahara</strong>
          <span className="service-name">Emergency response coordination</span>
        </div>
        <div className="connection"><span className={`health ${health === "offline" ? "offline" : ""}`}>
              <span className="dot" />
              {health === "ready"
                ? "Service connected"
                : health === "checking"
                  ? "Checking service…"
                  : "Service unavailable"}
            </span></div>
        <nav aria-label="Main navigation">
          {[
            { name: "Overview", icon: LayoutDashboard },
            { name: "Requests", icon: ClipboardList },
            { name: "Resources", icon: Package },
            { name: "Map", icon: MapPin },
          ].map((item) => (
            <button
              key={item.name}
              className={`nav-item ${nav === item.name ? "active" : ""}`}
              aria-current={nav === item.name ? "page" : undefined}
              onClick={() => {
                setNav(item.name);
                if (item.name === "Requests") setTab("requests");
                if (item.name === "Resources") setTab("resources");
                if (item.name === "Map" && nav !== "Map") setMapSelection(null);
              }}
            >
              <item.icon size={17} />
              {item.name}
              {item.name === "Requests" && (
                <span className="count">{count(requests?.length)}</span>
              )}
            </button>
          ))}
        </nav>
      </header>
      <div className="shell">

        <main>
          {nav === "Map" ? (
            <OverviewMap
              requests={requests}
              resources={resources}
              selectedKey={mapSelection}
              onSelect={setMapSelection}
              refreshing={refreshing}
              onRefresh={refreshData}
            />
          ) : <>
          <div className="page-heading">
            <div>
              <h1>{nav === "Overview" ? "Community request directory" : nav}</h1>
              <p className="directory-intro">Find local assistance requests, check their status and register available resources.</p>
            </div>
            <div className="actions">
              <button
                className="primary"
                onClick={() => openForm("request")}
              >
                <Plus size={15} />
                New request
              </button>
              <button
                className="secondary"
                onClick={() => openForm("resource")}
              >
                <Plus size={15} />
                Add resource
              </button>
            </div>
          </div>
          {!form && !selected && <Feedback />}
          <section className="metrics" aria-label="Response overview">
            {[
              {
                label: "Total requests",
                value: requests?.length,
                icon: ClipboardList,
              },
              {
                label: "Pending requests",
                value: pending,
                icon: Clock3,
              },
              {
                label: "Available resources",
                value: available,
                icon: Package,
              },
              {
                label: "Completed requests",
                value: completed,
                icon: CheckCircle2,
              },
            ].map((metric) => (
              <div
                className="metric"
                key={metric.label}
                aria-label={metric.label}
              >
                <div className="metric-top">
                  <span>{metric.label}</span>
                  <span className="metric-icon">
                    <metric.icon size={16} />
                  </span>
                </div>
                <strong>{count(metric.value)}</strong>
              </div>
            ))}
          </section>
          <div className="content-grid">
            <section className="panel">

              <div className="panel-header">
                <div>
                  <h2>Local activity</h2>
                </div>
                <button
                  className="refresh"
                  disabled={refreshing}
                  onClick={refreshData}
                  aria-label="Refresh data"
                >
                  <RefreshCw size={13} />
                  {refreshing ? "Updating" : "Refresh"}
                </button>
              </div>
              <div className="tabs" role="tablist" aria-label="Activity type">
                <button
                  role="tab"
                  aria-selected={tab === "requests"}
                  className={`tab ${tab === "requests" ? "active" : ""}`}
                  onClick={() => setTab("requests")}
                >
                  Requests <span>{count(requests?.length)}</span>
                </button>
                <button
                  role="tab"
                  aria-selected={tab === "resources"}
                  className={`tab ${tab === "resources" ? "active" : ""}`}
                  onClick={() => setTab("resources")}
                >
                  Resources <span>{count(resources?.length)}</span>
                </button>
                <button role="tab" aria-selected={tab === "shard details"} className={`tab ${tab === "shard details" ? "active" : ""}`} onClick={() => setTab("shard details")}>Shard details</button>
              </div>
              <div role="tabpanel" aria-label={title(tab)}>
                {tab === "shard details" ? <ShardDetails /> : <>
                {rows === null ? (
                  <div className="loading">
                    {refreshing
                      ? "Loading live activity…"
                      : "Activity unavailable. Refresh to try again."}
                  </div>
                ) : rows.length === 0 ? (
                  <div className="empty">
                    <Box size={35} strokeWidth={1.2} />
                    <h3>No {tab} yet</h3>
                    <p>
                      {tab === "requests"
                        ? "New requests will appear here once submitted."
                        : "Registered resources will appear here."}
                    </p>
                    <button
                      className="quiet"
                      onClick={() =>
                        openForm(tab === "requests" ? "request" : "resource")
                      }
                    >
                      {tab === "requests"
                        ? "Create your first request"
                        : "Register your first resource"}
                      <ArrowRight size={13} />
                    </button>
                  </div>
                ) : (
                  <div className="table-wrap">
                    <table>
                      <thead>
                        <tr>
                          <th>{tab === "requests" ? "Request" : "Resource"}</th>
                          <th>Location</th>
                          <th>Status</th>
                          <th>
                            <span aria-hidden="true">Action</span>
                          </th>
                        </tr>
                      </thead>
                      <tbody>
                        {rows.map((record) => {
                          const Icon = icons[record.type] ?? Package;
                          return (
                            <tr key={record.id}>
                              <td>
                                <div className="type-label">
                                  <span className="category-icon">
                                    <Icon size={15} />
                                  </span>
                                  <div>
                                    {title(record.type)}
                                    <span className="id">{record.id}</span>
                                  </div>
                                </div>
                              </td>
                              <td>
                                <span className="location">
                                  {record.location || "Pinned location"}
                                </span>
                                <span className="coordinate-meta">{validCoordinates(record.coordinates) ? coordinateLabel(record.coordinates) : "No coordinates saved"}</span>
                                {record.directions && <span className="directions-note">Directions: {record.directions}</span>}
                              </td>
                              <td>
                                <Badge status={record.status} />
                              </td>
                              <td>
                                <div className="row-actions">
                                {tab === "requests" ? (
                                  <button
                                    className="row-action"
                                    aria-label={`View request ${record.id}`}
                                    disabled={busy}
                                    onClick={() =>
                                      void action(async () =>
                                        setSelected(
                                          await api<RequestRecord>(
                                            `/requests/${encodeURIComponent(record.id)}`,
                                          ),
                                        ),
                                      )
                                    }
                                  >
                                    <ArrowUpRight size={17} />
                                  </button>
                                ) : (
                                  <button
                                    className="row-action"
                                    disabled={
                                      busy || record.status === "reserved"
                                    }
                                    aria-label={`${record.status === "available" ? "Mark unavailable" : "Mark available"} ${record.id}`}
                                    title={
                                      record.status === "reserved"
                                        ? "Assigned to a request"
                                        : record.status === "available"
                                          ? "Mark unavailable"
                                          : "Mark available"
                                    }
                                    onClick={() =>
                                      void action(async () => {
                                        await api<Resource>(
                                          `/resources/${encodeURIComponent(record.id)}/status`,
                                          "PUT",
                                          {
                                            status:
                                              record.status === "available"
                                                ? "unavailable"
                                                : "available",
                                          },
                                        );
                                        setNotice(
                                          "Resource availability updated.",
                                        );
                                        await refresh();
                                      })
                                    }
                                  >
                                    {record.status === "reserved" ? (
                                      <ShieldCheck size={15} />
                                    ) : (
                                      <RefreshCw size={15} />
                                    )}
                                  </button>
                                )}
                                {validCoordinates(record.coordinates) && (
                                  <button
                                    className="row-action"
                                    aria-label={`Show ${tab === "requests" ? "request" : "resource"} ${record.id} on map`}
                                    title="Show on map"
                                    onClick={() => showOnMap(tab === "requests" ? "request" : "resource", record.id)}
                                  >
                                    <MapPin size={16} />
                                  </button>
                                )}
                                </div>
                              </td>
                            </tr>
                          );
                        })}
                      </tbody>
                    </table>
                  </div>
                )}
                </>}
              </div>
              <div className="table-footer">
                <span>
                  {tab === "shard details" ? "Development diagnostics" : rows
                    ? `${rows.length} ${tab} in your workspace`
                    : "Live data unavailable"}
                </span>
                <span>{tab === "shard details" ? "Refresh to update topology" : "Updates every 15 seconds"}</span>
              </div>
            </section>
            <aside className="right-column" aria-label="Request services">
              <section className="lookup-section">
                <h2>Track a request</h2>
              <form
                  className="lookup"
                  onSubmit={(event) => {
                    event.preventDefault();
                    void action(async () =>
                      setSelected(
                        await api<RequestRecord>(
                          `/requests/${encodeURIComponent(lookup.trim())}`,
                        ),
                      ),
                    );
                  }}
                >
                  <label>
                    Request ID
                    <input
                      required
                      value={lookup}
                      onChange={(e) => setLookup(e.target.value)}
                      placeholder="Enter a request ID"
                    />
                  </label>
                  <button
                    className="secondary"
                    disabled={busy || !lookup.trim()}
                  >
                    Look up request
                    <ArrowRight size={13} />
                  </button>
                </form>
              </section>
              <section className="panel side-content">
                <h3>Matching area</h3>
                <p>Requests match with the nearest available resource of the same type by straight-line distance.</p>
                <strong className="matching-limit" aria-label="Matching distance limit">
                  {maxDistance === null ? "Matching limit unavailable" : `${kilometers(maxDistance)} maximum`}
                </strong>
              </section>
              <section className="panel side-content workflow">
                <h3>How matching works</h3>
                {[
                  {
                    title: "Submit a request",
                    text: "Choose the assistance type and pin the location.",
                  },
                  {
                    title: "Match a resource",
                    text: `The nearest same-type resource by straight-line distance. ${limitDescription}`,
                  },
                  {
                    title: "Confirm delivery",
                    text: "Mark completed when assistance is delivered.",
                  },
                ].map((step, index) => (
                  <div className="step" key={step.title}>
                    <span className="step-number">0{index + 1}</span>
                    <div>
                      <strong>{step.title}</strong>
                      <p>{step.text}</p>
                    </div>
                  </div>
                ))}
              </section>
            </aside>
          </div>
          </>}
          <footer>
            <span>Sahara</span>
            <span>
              {updated
                ? `Last updated ${updated.toLocaleTimeString([], { hour: "2-digit", minute: "2-digit" })}`
                : "Waiting for live data"}{" "}
              · All times local
            </span>
          </footer>
        </main>
      </div>
      {form && (
        <Dialog
          title={
            form === "request" ? "Create a request" : "Register a resource"
          }
          onClose={close}
        >
          <form className="dialog-body" onSubmit={submit}>
            <Feedback />
            <p className="field-help">
              {form === "request"
                ? "Tell us what is needed. Matching starts automatically once the request is saved."
                : "Share the assistance you can provide in a specific location."}
            </p>
            <label>
              Assistance type
              <select name="type" defaultValue="food">
                {kinds.map((kind) => (
                  <option key={kind} value={kind}>
                    {title(kind)}
                  </option>
                ))}
              </select>
            </label>
            <label>
              Name
              <input name="location" maxLength={200} placeholder="e.g. IIITB main gate" aria-describedby="location-name-help" />
            </label>
            <p id="location-name-help" className="field-help" style={{ marginTop: -10 }}>Optional label for the pin.</p>
            <LocationPicker />
            <p id="coordinate-help" className="field-help" style={{ marginTop: -10 }}>Both coordinates are required. Use decimal degrees from -90 to 90 for latitude and -180 to 180 for longitude. Matching uses straight-line geographic distance. {limitDescription}</p>
            {form === "request" ? (
              <label>
                Request details
                <textarea
                  name="details"
                  required
                  maxLength={4000}
                  placeholder="Describe the assistance needed…"
                />
              </label>
            ) : (
              <label className="checkbox">
                <input name="availability" type="checkbox" defaultChecked />
                Available for matching
              </label>
            )}
            <div className="actions">
              <button
                type="button"
                className="secondary"
                disabled={busy}
                onClick={close}
              >
                Cancel
              </button>
              <button className="primary" disabled={busy}>
                {busy
                  ? "Saving…"
                  : form === "request"
                    ? "Create request"
                    : "Register resource"}
                <ArrowRight size={14} />
              </button>
            </div>
          </form>
        </Dialog>
      )}
      {selected && (
        <Dialog title="Request details" onClose={close}>
          <div className="dialog-body">
            <Feedback />
            <div className="detail-line">
              <strong>{title(selected.type)} assistance</strong>
              <Badge status={selected.status} />
            </div>
            <div className="detail-line">
              <span>Request ID</span>
              <span className="detail-id">{selected.id}</span>
            </div>
            <div className="detail-line">
              <span>Location</span>
              <span
                style={{
                  textTransform: "capitalize",
                  display: "flex",
                  alignItems: "center",
                  gap: 5,
                }}
              >
                <MapPin size={12} />
                {selected.location || "Pinned location"}
              </span>
            </div>
            <div className="detail-line">
              <span>Created</span>
              <span>{new Date(selected.created_at).toLocaleString()}</span>
            </div>
            <p className="detail-copy">{selected.details}</p>
            {selected.directions && (
              <div className="detail-line">
                <span>Landmark or directions</span>
                <span className="directions-note">{selected.directions}</span>
              </div>
            )}
            {validCoordinates(selected.coordinates) ? <>
              <div className="detail-line"><span>Coordinates (lat, lon)</span><span className="detail-id">{coordinateLabel(selected.coordinates)}</span></div>
              <section className="distance-summary" aria-label="Geographic matching">
                <p className="distance-kicker">STRAIGHT-LINE GEOGRAPHIC DISTANCE</p>
                {typeof selected.distance_meters === "number" && Number.isFinite(selected.distance_meters) && selected.distance_meters >= 0 ?
                  <><strong className="assigned-distance">{kilometers(selected.distance_meters)}</strong><p className="field-help">Distance recorded at matching time. Geographic proximity does not measure travel distance or arrival time.</p></> :
                  <p className="field-help">No assigned distance yet. {limitDescription}</p>}
              </section>
            </> : <p className="legacy-note">No coordinates saved</p>}
            {selected.resource_id && (
              <div className="detail-line">
                <span>Assigned resource</span>
                <span className="detail-id">{selected.resource_id}</span>
              </div>
            )}
            <div className="actions">
              {validCoordinates(selected.coordinates) && (
                <button className="secondary" disabled={busy} onClick={() => {
                  showOnMap("request", selected.id);
                  setSelected(null);
                }}>
                  <MapPin size={15} /> Show on map
                </button>
              )}
              {selected.status === "pending" && (
                <button
                  className="primary"
                  disabled={busy}
                  onClick={() =>
                    void action(async () => {
                      setSelected(
                        await api<RequestRecord>(
                          `/match/${encodeURIComponent(selected.id)}`,
                          "POST",
                        ),
                      );
                      setNotice("An available resource has been matched.");
                      await refresh();
                    })
                  }
                >
                  <HeartHandshake size={15} />
                  Find a match
                </button>
              )}
              {selected.status === "matched" && (
                <button
                  className="primary"
                  disabled={busy}
                  onClick={() => void updateRequest("completed")}
                >
                  <CheckCircle2 size={15} />
                  Mark completed
                </button>
              )}
              {(selected.status === "pending" ||
                selected.status === "matched") && (
                <button
                  className="danger"
                  disabled={busy}
                  onClick={() => void updateRequest("cancelled")}
                >
                  Cancel request
                </button>
              )}
              <button
                className="secondary"
                disabled={busy}
                onClick={() =>
                  void action(async () =>
                    setSelected(
                      await api<RequestRecord>(
                        `/requests/${encodeURIComponent(selected.id)}`,
                      ),
                    ),
                  )
                }
                aria-label="Refresh request status"
              >
                <RefreshCw size={14} />
              </button>
            </div>
            <p className="field-help">
              {selected.status === "pending"
                ? validCoordinates(selected.coordinates)
                  ? `Matching selects the nearest available same-type resource using geographic coordinates. ${limitDescription}`
                  : "This request has no coordinates and matches by district."
                : selected.status === "matched"
                  ? "Complete this request after assistance is delivered. Cancelling releases the assigned resource."
                  : "This request is closed and its status cannot be changed."}
            </p>
          </div>
        </Dialog>
      )}
    </>
  );
}
