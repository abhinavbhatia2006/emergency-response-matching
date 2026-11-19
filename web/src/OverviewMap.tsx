import { useEffect, useMemo, useRef, useState } from "react";
import * as L from "leaflet";
import { ClipboardList, MapPin, Package, RefreshCw } from "lucide-react";
import { openStreetMapTiles } from "./mapTiles";
import { kilometers, title, validCoordinates, type RequestRecord, type Resource } from "./records";

type RecordKind = { kind: "request"; record: RequestRecord } | { kind: "resource"; record: Resource };
type MapRecord = RecordKind & { key: string };
type PinGroup = { key: string; point: L.LatLngTuple; entries: MapRecord[] };
type PinMarker = { marker: L.Marker; group: PinGroup; appearance: string };
type MapState = { map: L.Map; markers: Map<string, PinMarker>; framed: boolean };
type Props = {
  requests: RequestRecord[] | null;
  resources: Resource[] | null;
  selectedKey: string | null;
  onSelect: (key: string | null) => void;
  refreshing: boolean;
  onRefresh: () => void;
};

function groupRecords(requests: RequestRecord[] | null, resources: Resource[] | null) {
  const records: RecordKind[] = [
    ...(requests ?? []).map(record => ({ kind: "request" as const, record })),
    ...(resources ?? []).map(record => ({ kind: "resource" as const, record })),
  ];
  const groups = new Map<string, PinGroup>();
  for (const entry of records) {
    const coordinates = entry.record.coordinates;
    if (!validCoordinates(coordinates)) continue;
    // Both dateline values identify the same meridian.
    const longitude = coordinates.longitude === 180 ? -180 : coordinates.longitude;
    const point: L.LatLngTuple = [coordinates.latitude, longitude];
    const key = point.join(",");
    let group = groups.get(key);
    if (!group) {
      group = { key, point, entries: [] };
      groups.set(key, group);
    }
    group.entries.push({ ...entry, key: `${entry.kind}/${entry.record.id}` });
  }
  return [...groups.values()];
}

function shortID(id: string) {
  return id.length > 8 ? `${id.slice(0, 8)}…` : id;
}

function pinLabel({ kind, record }: MapRecord) {
  return `${title(kind)}: ${title(record.type)}, ${record.location || "Pinned location"}, ${shortID(record.id)}`;
}

function makeIcon(kind: string, count: number) {
  return L.divIcon({
    className: `overview-pin overview-pin--${kind}`,
    iconSize: [32, 32],
    iconAnchor: [16, 16],
    html: count > 1 ? `<span aria-hidden="true">${count}</span>` : '<span class="overview-pin__glyph" aria-hidden="true"></span>',
  });
}

function nearbyLongitude(longitude: number, reference: number) {
  const delta = longitude - reference;
  return Math.abs(delta) <= 180 ? longitude : longitude - 360 * Math.round(delta / 360);
}

function framePins(map: L.Map, points: L.LatLngTuple[], maxZoom = 14) {
  if (points.length) map.fitBounds(L.latLngBounds(points), { padding: [30, 30], maxZoom, animate: false });
}

export default function OverviewMap({ requests, resources, selectedKey, onSelect, refreshing, onRefresh }: Props) {
  const container = useRef<HTMLDivElement>(null);
  const heading = useRef<HTMLHeadingElement>(null);
  const mapState = useRef<MapState | null>(null);
  const [tileError, setTileError] = useState(false);
  const groups = useMemo(() => groupRecords(requests, resources), [requests, resources]);
  const selectedGroup = groups.find(group => group.entries.some(entry => entry.key === selectedKey));
  const selected = selectedGroup?.entries.find(entry => entry.key === selectedKey);
  const request = selected?.kind === "request" ? selected.record : undefined;
  const assignedID = request?.resource_id;
  const assignedResource = assignedID ? resources?.find(resource => resource.id === assignedID) : undefined;
  const linkedKey = request?.status === "matched" && assignedID ? `resource/${assignedID}` : null;
  const linkedGroup = groups.find(group => group.entries.some(entry => entry.key === linkedKey));
  const [selectedLatitude, selectedLongitude] = selectedGroup?.point ?? [];
  const [linkedLatitude, linkedLongitude] = linkedGroup?.point ?? [];
  const distance = request?.distance_meters;
  const savedDistance = typeof distance === "number" && Number.isFinite(distance) && distance >= 0 ? kilometers(distance) : null;
  const visibleRecords = groups.reduce((total, group) => total + group.entries.length, 0);
  const skippedRecords = (requests?.length ?? 0) + (resources?.length ?? 0) - visibleRecords;
  const unavailable = requests === null || resources === null;

  useEffect(() => {
    const map = L.map(container.current!, { scrollWheelZoom: false, renderer: new L.SVG() }).setView([20, 0], 2);
    openStreetMapTiles(setTileError).addTo(map);
    mapState.current = { map, markers: new Map(), framed: false };
    heading.current?.focus();
    return () => {
      map.remove();
      mapState.current = null;
    };
  }, []);

  useEffect(() => {
    const state = mapState.current;
    if (!state) return;
    const visible = new Set<string>();
    for (const group of groups) {
      visible.add(group.key);
      const count = group.entries.length;
      const first = group.entries[0];
      const kind = group.entries.some(entry => entry.kind !== first.kind) ? "mixed" : first.kind;
      const appearance = `${kind}/${count}`;
      const [latitude, longitude] = group.point;
      const point = L.latLng(latitude, selectedLongitude === undefined ? longitude : nearbyLongitude(longitude, selectedLongitude));
      let pin = state.markers.get(group.key);
      if (!pin) {
        const marker = L.marker(point, { icon: makeIcon(kind, count) }).addTo(state.map);
        const entry = { marker, group, appearance };
        const select = () => onSelect(entry.group.entries[0].key);
        marker.on("click", select);
        marker.on("keydown", ({ originalEvent }) => {
          if (originalEvent.key !== "Enter" && originalEvent.key !== " ") return;
          L.DomEvent.stop(originalEvent);
          select();
        });
        state.markers.set(group.key, entry);
        pin = entry;
      } else {
        if (!pin.marker.getLatLng().equals(point)) pin.marker.setLatLng(point);
        if (pin.appearance !== appearance) pin.marker.setIcon(makeIcon(kind, count));
        pin.group = group;
        pin.appearance = appearance;
      }
      const isSelected = group.entries.some(entry => entry.key === selectedKey);
      const isAssigned = group.entries.some(entry => entry.key === linkedKey);
      const label = count > 1 ? `${count} records at this pin` : pinLabel(first);
      const element = pin.marker.getElement();
      if (element) {
        element.setAttribute("aria-label", label);
        element.setAttribute("aria-pressed", String(isSelected));
        element.title = label;
        element.classList.toggle("overview-pin--selected", isSelected);
        element.classList.toggle("overview-pin--assigned", isAssigned);
      }
      pin.marker.setZIndexOffset(isSelected ? 1000 : isAssigned ? 500 : 0);
    }
    for (const [key, pin] of state.markers) {
      if (!visible.has(key)) {
        pin.marker.remove();
        state.markers.delete(key);
      }
    }
    if (!state.framed && groups.length) {
      if (selectedLongitude === undefined) framePins(state.map, groups.map(group => group.point));
      state.framed = true;
    }
  }, [groups, linkedKey, onSelect, selectedKey, selectedLongitude]);

  useEffect(() => {
    const map = mapState.current?.map;
    if (!map || selectedLatitude === undefined || selectedLongitude === undefined) return;
    const origin: L.LatLngTuple = [selectedLatitude, selectedLongitude];
    if (linkedLatitude === undefined || linkedLongitude === undefined) {
      map.setView(origin, Math.max(map.getZoom(), 13), { animate: false });
      return;
    }
    const destination: L.LatLngTuple = [linkedLatitude, nearbyLongitude(linkedLongitude, selectedLongitude)];
    framePins(map, [origin, destination], 15);
    if (origin[0] === destination[0] && origin[1] === destination[1]) return;
    const line = L.polyline([origin, destination], {
      className: "overview-match-line", color: "#343b38", weight: 3, dashArray: "7 6", interactive: false,
    }).addTo(map);
    if (savedDistance) line.bindTooltip(`Straight-line distance: ${savedDistance}`, { direction: "center", permanent: true });
    return () => { line.remove(); };
  }, [linkedLatitude, linkedLongitude, savedDistance, selectedKey, selectedLatitude, selectedLongitude]);

  function showAll() {
    onSelect(null);
    const map = mapState.current?.map;
    if (map) framePins(map, groups.map(group => group.point));
  }

  return (
    <section className="overview-map">
      <div className="overview-map__heading">
        <h1 ref={heading} tabIndex={-1}>Response map</h1>
        <button className="secondary" disabled={refreshing} onClick={onRefresh}>
          <RefreshCw size={15} />{refreshing ? "Refreshing…" : "Refresh data"}
        </button>
      </div>
      <div className="overview-map__toolbar">
        <ul className="overview-map__legend" aria-label="Map legend">
          <li><span className="map-legend-icon map-legend-icon--request"><ClipboardList size={14} aria-hidden="true" /></span>Requests</li>
          <li><span className="map-legend-icon map-legend-icon--resource"><Package size={14} aria-hidden="true" /></span>Resources</li>
        </ul>
        <span className="field-help">{visibleRecords} mapped</span>
        <button className="secondary" disabled={groups.length === 0} onClick={showAll}>Show all pins</button>
      </div>
      <div className="overview-map__layout">
        <div className="overview-map__canvas" ref={container} role="region" aria-label="Request and resource locations" />
        <div className="overview-map__sidebar">
          {selected && selectedGroup ? (
            <section aria-label="Selected map record">
              {selectedGroup.entries.length > 1 && (
                <ul className="overview-map__choices" aria-label="Records at this pin">
                  {selectedGroup.entries.map(entry => (
                    <li key={entry.key}>
                      <button className="secondary" aria-pressed={entry.key === selectedKey} onClick={() => onSelect(entry.key)}>
                        {entry.kind === "request" ? <ClipboardList size={16} aria-hidden="true" /> : <Package size={16} aria-hidden="true" />}
                        <span>{pinLabel(entry)}</span>
                      </button>
                    </li>
                  ))}
                </ul>
              )}
              <div className="overview-map__record">
                <span className={`overview-map__kind overview-map__kind--${selected.kind}`}>{title(selected.record.type)} {selected.kind}</span>
                <h2>{selected.record.location || "Pinned location"}</h2>
                <dl>
                  <div><dt>Status</dt><dd><span className={`pill ${selected.record.status}`}>{title(selected.record.status)}</span></dd></div>
                  <div><dt>Record ID</dt><dd title={selected.record.id}>{shortID(selected.record.id)}</dd></div>
                  <div><dt>Coordinates</dt><dd>{selectedGroup.point[0].toFixed(5)}, {selectedGroup.point[1].toFixed(5)}</dd></div>
                  {selected.record.directions && <div><dt>Directions</dt><dd>{selected.record.directions}</dd></div>}
                  {assignedID && <div>
                    <dt>{request?.status === "matched" ? "Assigned resource" : "Previous assignment"}</dt>
                    <dd>{assignedResource?.location || shortID(assignedID)}</dd>
                  </div>}
                  {savedDistance && <div><dt>Straight-line distance</dt><dd>{savedDistance}</dd></div>}
                </dl>
              </div>
            </section>
          ) : (
            <div className="overview-map__empty">
              <MapPin size={22} aria-hidden="true" />
              <p>{groups.length ? "Select a pin to see its details." : refreshing ? "Loading map records…" : unavailable ? "Map records unavailable." : "No records with saved coordinates."}</p>
            </div>
          )}
        </div>
      </div>
      {tileError && <p className="field-help overview-map__notice" role="status">Map tiles could not load. Saved pins are still available.</p>}
      {unavailable && !refreshing && groups.length > 0 && <p className="field-help overview-map__notice" role="status">Some map records could not be loaded.</p>}
      {skippedRecords > 0 && <p className="field-help overview-map__notice">{skippedRecords} {skippedRecords === 1 ? "record has" : "records have"} no usable coordinates. Find them in the lists.</p>}
    </section>
  );
}
