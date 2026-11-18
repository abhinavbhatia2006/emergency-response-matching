import { useEffect, useState } from "react";

type Node = { id: string; ready: boolean; last_heartbeat: string | null };
type Shard = { id: number; nodes: Node[] };
function valid(value: unknown): value is { shards: Shard[] } {
  if (!value || typeof value !== "object" || !("shards" in value) || !Array.isArray(value.shards)) return false;
  return value.shards.every(s => s && Number.isInteger(s.id) && Array.isArray(s.nodes) && s.nodes.every((n: Node) => n && typeof n.id === "string" && typeof n.ready === "boolean" && (n.last_heartbeat === null || (typeof n.last_heartbeat === "string" && Number.isFinite(Date.parse(n.last_heartbeat))))));
}
export default function ShardDetails() {
  const [shards, setShards] = useState<Shard[] | null>(null);
  const [error, setError] = useState(false);
  const [revision, setRevision] = useState(0);
  const [loading, setLoading] = useState(true);
  useEffect(() => {
    const controller = new AbortController();
    const timeout = setTimeout(() => controller.abort(), 10000);
    let active = true;
    setLoading(true); setError(false); setShards(null);
    fetch("/development/shards", { signal: controller.signal }).then(async response => {
      if (!response.ok) throw new Error("unavailable");
      const result: unknown = await response.json();
      if (!valid(result)) throw new Error("invalid topology");
      if (active) setShards(result.shards);
    }).catch(() => { if (active) setError(true); }).finally(() => { if (active) setLoading(false); clearTimeout(timeout); });
    return () => { active = false; clearTimeout(timeout); controller.abort(); };
  }, [revision]);
  return <section className="shard-details">
    <p className="field-help">Development view · Coordinator-reported heartbeat readiness. Node names identify configured copies, not elected roles.</p>
    <button className="secondary" disabled={loading} onClick={() => setRevision(v => v + 1)}>Refresh shard details</button>
    {loading && <p role="status">Loading shard details…</p>}
    {error && <p role="alert">Shard details unavailable. Refresh to try again.</p>}
    {shards?.length === 0 && <p>No configured shards were reported.</p>}
    {shards?.map(shard => <section key={shard.id} className="shard-group"><h3>Shard {shard.id}</h3><ul>{shard.nodes.map(node => <li key={node.id}>
      <strong>{node.id}</strong><span className={`pill ${node.ready ? "available" : "unavailable"}`}>{node.ready ? "Ready" : "Not ready"}</span>
      <span>{node.last_heartbeat ? `Last heartbeat: ${new Date(node.last_heartbeat).toLocaleString()}` : "No heartbeat recorded"}</span>
    </li>)}</ul></section>)}
  </section>;
}
