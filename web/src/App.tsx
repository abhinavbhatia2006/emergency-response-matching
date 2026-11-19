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
  Activity,
  ArrowRight,
  ArrowUpRight,
  Box,
  CheckCircle2,
  ChevronRight,
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
  Search,
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
    [error, setError] = useState(""),
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
      requestResult.status === "rejected" ||
      resourceResult.status === "rejected"
    )
      setError(
        "Live data could not be loaded. Check the service connection and refresh.",
      );
    if (
      requestResult.status === "fulfilled" &&
      resourceResult.status === "fulfilled"
    )
      setUpdated(new Date());
    setRefreshing(false);
  }, []);
  useEffect(() => {
    void refresh();
    const timer = setInterval(() => void refresh(), 15000);
    return () => clearInterval(timer);
  }, [refresh]);
  async function action(work: () => Promise<void>) {
    setBusy(true);
    setError("");
    setNotice("");
    try {
      await work();
    } catch (err) {
      setError(
        err instanceof Error
          ? err.message
          : "The service could not be reached. Please try again.",
      );
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
    setError("");
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
          <span>{error}</span>
          <button aria-label="Dismiss error" onClick={() => setError("")}>
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
      <aside>
        <div className="brand">
          <span className="brand-icon">
            <Activity size={24} strokeWidth={2.5} />
          </span>
          RELAY<span className="brand-stop">®</span>
        </div>
        <p className="workspace-label">Emergency response<br />Coordination desk</p>
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
        <div className="sidebar-bottom">
          <span className="desk-code">OPERATIONS / 01</span>
          <strong>People. Resources. Response.</strong>
        </div>
      </aside>
      <div className="shell">
        <header className="topbar">
          <div className="breadcrumb">
            <span>Operations desk</span>
            <ChevronRight size={12} />
            <strong>{nav}</strong>
          </div>
          <div className="topbar-right">
            <span className={`health ${health === "offline" ? "offline" : ""}`}>
              <span className="dot" />
              {health === "ready"
                ? "Service connected"
                : health === "checking"
                  ? "Checking service…"
                  : "Service unavailable"}
            </span>
            <span className="avatar" title="Response coordination">
              RESPONSE NETWORK
            </span>
          </div>
        </header>
        <main>
          {nav === "Map" ? (
            <OverviewMap
              requests={requests}
              resources={resources}
              selectedKey={mapSelection}
              onSelect={setMapSelection}
              refreshing={refreshing}
              onRefresh={() => void refresh()}
            />
          ) : <>
          <div className="page-heading">
            <div>
              <p className="eyebrow"><span className="section-number">01 /</span> Emergency response coordination</p>
              <h1>
                {nav === "Overview"
                  ? <>NEED MEETS<br /><span className="heading-accent">ACTION.</span></>
                  : nav === "Requests"
                    ? <>EVERY REQUEST.<br /><span className="heading-accent">A NEXT STEP.</span></>
                    : <>LOCAL SUPPORT.<br /><span className="heading-accent">READY TO GO.</span></>}
              </h1>
              <p className="subtitle">
                A shared desk for urgent needs and available help.<br />
                Log it. Match it. Follow it through.
              </p>
            </div>
            <div className="actions">
              <span className="action-caption">MAKE THE NEXT CONNECTION</span>
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
          <section className="intro-banner">
            <div>
              <h2><span className="signal-mark" aria-hidden="true">↗</span> Response starts here.</h2>
              <p>
                New requests connect to the nearest available, same-type resource by straight-line geographic distance.
                <span className="matching-limit" aria-label="Matching distance limit">{maxDistance === null ? "Matching limit unavailable" : `${kilometers(maxDistance)} maximum`}</span>
              </p>
            </div>
            <div className="banner-icon">
              <HeartHandshake size={31} strokeWidth={1.5} />
            </div>
          </section>
          <section className="metrics" aria-label="Response overview">
            {[
              {
                label: "Total requests",
                value: requests?.length,
                icon: ClipboardList,
                note: "RECORDED IN THE LEDGER",
              },
              {
                label: "Pending requests",
                value: pending,
                icon: Clock3,
                note: "AWAITING A RESOURCE",
              },
              {
                label: "Available resources",
                value: available,
                icon: Package,
                note: "READY FOR DISPATCH",
              },
              {
                label: "Completed requests",
                value: completed,
                icon: CheckCircle2,
                note: "ASSISTANCE DELIVERED",
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
                <small>{metric.note}</small>
              </div>
            ))}
          </section>
          <div className="content-grid">
            <section className="panel">
              <div className="panel-header">
                <div>
                  <h2><span className="section-number">02 /</span> The response ledger</h2>
                  <p>Requests in. Resources out. Keep every connection accounted for.</p>
                </div>
                <button
                  className="refresh"
                  disabled={refreshing}
                  onClick={() => {
                    setError("");
                    void refresh();
                  }}
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
                        ? "When someone needs a hand, this is where their request begins."
                        : "Make local support visible. Register your first resource to get started."}
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
            <div className="right-column">
              <section className="panel side-content">
                <Search
                  size={19}
                  color="currentColor"
                  style={{ marginBottom: 13 }}
                />
                <h3>Trace a request.</h3>
                <p>
                  Enter an ID to inspect its status, assigned resource, and next action.
                </p>
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
              <section className="panel guide">
                <p className="protocol-label">FIELD NOTES / THE WORKFLOW</p>
                <h3>Nothing falls<br />through the gaps.</h3>
                {[
                  {
                    title: "Capture the need",
                    text: "Choose the assistance type and pin the location.",
                  },
                  {
                    title: "Connect the right help",
                    text: `The nearest same-type resource by straight-line distance. ${limitDescription}`,
                  },
                  {
                    title: "Close the loop",
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
            </div>
          </div>
          </>}
          <footer>
            <span>RELAY / COMMUNITY RESPONSE INFRASTRUCTURE</span>
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
