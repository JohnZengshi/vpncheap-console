import React, { useEffect, useMemo, useState } from "react";
import { createRoot } from "react-dom/client";
import "./index.css";

const button = "inline-flex items-center gap-1 rounded-md border border-[#2a2e3c] bg-[#1e2230] px-2.5 py-1.5 text-xs text-[#c8ccd4] transition hover:border-[#4b5563] disabled:cursor-not-allowed disabled:opacity-50";
const iconButton = `${button} px-2 py-1`;
const input = "rounded-md border border-[#2a2e3c] bg-[#1e2230] px-2 py-1.5 text-xs text-[#c8ccd4] placeholder:text-[#6b7280]";

async function api(path, options) {
  const response = await fetch(`/api${path}`, options);
  if (!response.ok) throw new Error(`${response.status} ${await response.text()}`);
  return response.status === 204 ? null : response.json();
}
const formatBytes = (n) => n < 1024 ? `${n} B` : n < 1048576 ? `${(n / 1024).toFixed(1)} KB` : `${(n / 1048576).toFixed(1)} MB`;
const latencyClass = (n) => !n ? "text-red-400" : n < 200 ? "text-green-400" : n < 800 ? "text-yellow-400" : "text-red-400";

function TopBar({ modes, mode, setMode, search, setSearch, onTestAll, onBest, onExit, tunnel, onTunnel }) {
  return <header className="flex flex-wrap items-center gap-3 border-b border-[#1e2230] bg-[#14161e] px-4 py-2.5">
    <h1 className="font-semibold text-[#e0e3e8]">VPNCheap</h1>
    <div className="flex overflow-hidden rounded-md border border-[#2a2e3c]">
      {modes.map((item) => <button key={item} className={`px-2.5 py-1 text-xs ${item === mode ? "bg-blue-600 text-white" : "bg-[#1e2230] text-[#7a8190]"}`} onClick={() => setMode(item)}>{item}</button>)}
    </div>
    <button className={button} onClick={onTestAll}>test all</button>
    <button className={button} onClick={onBest}>best</button>
    <button className={button} onClick={onExit}>exit ip</button>
    <input className={`${input} w-44`} value={search} onChange={(e) => setSearch(e.target.value)} placeholder="filter nodes" aria-label="Filter nodes" />
    <div className="ml-auto flex items-center gap-2">
      <span className={`h-2 w-2 rounded-full ${/Connected/i.test(tunnel.state) ? "bg-green-500" : "bg-red-500"}`} />
      <span className="text-xs text-[#7a8190]">{tunnel.state || "Unknown"}</span>
      <button className={button} onClick={() => onTunnel("connect")}>connect</button>
      <button className={`${button} border-red-900 text-red-400`} onClick={() => window.confirm("Disconnect the VPNCheap tunnel?") && onTunnel("disconnect")}>disconnect</button>
    </div>
  </header>;
}

function TrafficBar({ traffic, connections, exit }) {
  const width = (value, max) => `${Math.min(100, value / Math.max(max, 1) * 100)}%`;
  return <div className="flex flex-wrap items-center gap-5 border-b border-[#1e2230] px-4 py-2 text-xs">
    {["up", "down"].map((key) => <span key={key}>{key} <b className="text-[#e0e3e8]">{formatBytes(traffic[key])}/s</b><span className="ml-1.5 inline-block h-1.5 w-28 overflow-hidden rounded bg-[#1e2230] align-middle"><i className="block h-full bg-blue-600" style={{ width: width(traffic[key], traffic[`max${key[0].toUpperCase()}${key.slice(1)}`]) }} /></span></span>)}
    <span className="text-[#6b7280]">active conns <b className="text-[#c8ccd4]">{connections}</b></span>
    <span className="ml-auto text-[#7a8190]">{exit}</span>
  </div>;
}

function NodeTable({ nodes, labels, current, delays, busy, onSelect, onTest }) {
  if (!nodes.length) return <div className="p-8 text-center text-[#6b7280]">no nodes</div>;
  return <table className="w-full"><thead><tr className="sticky top-0 border-b border-[#1e2230] bg-[#14161e] text-[11px] uppercase text-[#6b7280]"><th className="w-8 px-3 py-1.5" /><th className="px-3 py-1.5">Node</th><th className="px-3 py-1.5">Type</th><th className="px-3 py-1.5">Delay</th><th className="px-3 py-1.5" /></tr></thead><tbody>{nodes.map((name) => {
    const delay = delays[name.name], selected = name.name === current, pending = busy[name.name];
    return <tr key={name.name} className="border-b border-[#1a1d27]"><td className="px-3 py-1.5 text-green-400">{selected ? "●" : pending ? "↻" : ""}</td><td className="px-3 py-1.5" title={name.name}><b>{labels[name.name] || name.name}</b><div className="max-w-56 overflow-hidden text-ellipsis whitespace-nowrap text-[10px] text-[#6b7280]">{name.name}</div></td><td className="px-3 py-1.5 text-[10px] text-[#6b7280]">{name.type || "-"}</td><td className={`px-3 py-1.5 ${pending ? "" : latencyClass(delay)}`}>{delay ? `${delay}ms` : pending ? "..." : "-"}</td><td className="px-3 py-1.5"><button className={iconButton} disabled={selected || pending} onClick={() => onSelect(name.name)}>use</button> <button className={iconButton} disabled={pending} onClick={() => onTest(name.name)}>test</button></td></tr>;
  })}</tbody></table>;
}

function Connections({ list, onClose }) {
  if (!list.length) return <div className="p-8 text-center text-[#6b7280]">no active connections</div>;
  return <table className="w-full"><thead><tr className="border-b border-[#1e2230] text-[11px] uppercase text-[#6b7280]"><th className="px-3 py-1.5">ID</th><th className="px-3 py-1.5">Network</th><th className="px-3 py-1.5">Source</th><th className="px-3 py-1.5">Destination</th><th className="px-3 py-1.5">Chain</th><th className="px-3 py-1.5">Up</th><th className="px-3 py-1.5">Down</th><th /></tr></thead><tbody>{list.map((conn) => { const m = conn.metadata || {}; return <tr key={conn.id} className="border-b border-[#1a1d27] text-[11px]"><td className="px-3 py-1.5 text-[#6b7280]">{conn.id?.slice(0, 8)}</td><td className="px-3 py-1.5">{m.network || "-"}</td><td className="px-3 py-1.5">{m.sourceIP || m.sourceHost || "-"}:{m.sourcePort || ""}</td><td className="px-3 py-1.5">{m.destinationIP || m.host || m.destinationPort || "-"}</td><td className="px-3 py-1.5">{(conn.chains || []).join(">")}</td><td className="px-3 py-1.5">{formatBytes(conn.upload || 0)}</td><td className="px-3 py-1.5">{formatBytes(conn.download || 0)}</td><td className="px-3 py-1.5"><button className={iconButton} onClick={() => onClose(conn.id)}>close</button></td></tr>; })}</tbody></table>;
}

function App() {
  const [mode, setModeState] = useState("Rule"), [modes, setModes] = useState(["Rule", "direct", "global"]);
  const [proxies, setProxies] = useState({}), [selector, setSelector] = useState(""), [current, setCurrent] = useState(""), [labels, setLabels] = useState({});
  const [delays, setDelays] = useState({}), [busy, setBusy] = useState({}), [search, setSearch] = useState(""), [tab, setTab] = useState("nodes");
  const [connections, setConnections] = useState([]), [tunnel, setTunnel] = useState({}), [health, setHealth] = useState(null), [exit, setExit] = useState("");
  const [traffic, setTraffic] = useState({ up: 0, down: 0, maxUp: 1, maxDown: 1 });
  const names = useMemo(() => Object.entries(proxies).flatMap(([key, value]) => (value.all || []).map((name) => ({ name, type: proxies[name]?.type || value.type }))).filter((item, index, list) => item.name !== "direct" && list.findIndex((x) => x.name === item.name) === index).filter((item) => `${item.name} ${labels[item.name] || ""}`.toLowerCase().includes(search.toLowerCase())), [proxies, labels, search]);
  const load = async () => { try { const [config, data] = await Promise.all([api("/configs"), api("/proxies")]); setModes(config["mode-list"] || modes); setModeState(config.mode || "Rule"); setProxies(data.proxies || {}); const selectorEntry = Object.entries(data.proxies || {}).find(([, value]) => value.type === "Selector" || value.type === "URLTest"); if (selectorEntry) { setSelector(selectorEntry[0]); setCurrent(selectorEntry[1].now || ""); } } catch {} };
  useEffect(() => { load(); const timer = setInterval(load, 5000); return () => clearInterval(timer); }, []);
  useEffect(() => { fetch("/labels").then((r) => r.json()).then((d) => setLabels(d.mapping || {})).catch(() => {}); }, []);
  useEffect(() => { let cancelled = false; const poll = () => api("/connections").then((d) => !cancelled && setConnections(d.connections || [])).catch(() => {}); poll(); const timer = setInterval(poll, 3000); return () => { cancelled = true; clearInterval(timer); }; }, []);
  useEffect(() => { let cancelled = false; const poll = () => fetch("/tunnel?action=status").then((r) => r.json()).then((d) => !cancelled && setTunnel(d)).catch(() => {}); poll(); const timer = setInterval(poll, 5000); return () => { cancelled = true; clearInterval(timer); }; }, []);
  useEffect(() => { const poll = () => fetch("/health").then((r) => r.json()).then(setHealth).catch(() => {}); poll(); const timer = setInterval(poll, 2000); return () => clearInterval(timer); }, []);
  useEffect(() => { let cancelled = false, reader; fetch("/api/traffic").then((r) => { reader = r.body?.getReader(); const decoder = new TextDecoder(); let buffer = ""; const read = () => reader?.read().then(({ done, value }) => { if (done || cancelled) return; buffer += decoder.decode(value); let newline; while ((newline = buffer.indexOf("\n")) >= 0) { const line = buffer.slice(0, newline); buffer = buffer.slice(newline + 1); try { const d = JSON.parse(line); setTraffic((old) => ({ up: d.up || 0, down: d.down || 0, maxUp: Math.max(old.maxUp, d.up || 0), maxDown: Math.max(old.maxDown, d.down || 0) })); } catch {} } read(); }); return read(); }).catch(() => {}); return () => { cancelled = true; reader?.cancel(); }; }, []);
  const setMode = async (value) => { setModeState(value); await api("/configs", { method: "PATCH", headers: { "Content-Type": "application/json" }, body: JSON.stringify({ mode: value }) }).catch(() => {}); };
  const testOne = async (name) => { setBusy((old) => ({ ...old, [name]: true })); try { const d = await api(`/proxies/${encodeURIComponent(name)}/delay?timeout=5000&url=http://www.gstatic.com/generate_204`); setDelays((old) => ({ ...old, [name]: d.delay || 0 })); } catch { setDelays((old) => ({ ...old, [name]: 0 })); } finally { setBusy((old) => ({ ...old, [name]: false })); } };
  const selectNode = async (name) => { setBusy((old) => ({ ...old, [name]: true })); try { await api(`/proxies/${encodeURIComponent(selector)}`, { method: "PUT", headers: { "Content-Type": "application/json" }, body: JSON.stringify({ name }) }); setCurrent(name); await api("/connections", { method: "DELETE" }); } catch {} finally { setBusy((old) => ({ ...old, [name]: false })); } };
  const testAll = () => Promise.all(names.map((item) => testOne(item.name)));
  const best = async () => { const d = await fetch("/best", { method: "POST" }).then((r) => r.json()).catch(() => ({})); (d.results || []).forEach((r) => setDelays((old) => ({ ...old, [r.name]: r.delay || 0 }))); if (d.best) setCurrent(d.best.name); };
  const checkExit = () => fetch("/exit").then((r) => r.json()).then((d) => setExit(d.error ? `exit: ${d.error}` : `exit: ${d.ip} ${d.city || ""} ${d.country ? `(${d.country})` : ""}`)).catch(() => setExit("exit: error"));
  return <main className="min-h-[100dvh]"><TopBar {...{ modes, mode, setMode, search, setSearch }} onTestAll={testAll} onBest={best} onExit={checkExit} tunnel={tunnel} onTunnel={(action) => fetch(`/tunnel?action=${action}`, { method: "POST" }).then(() => setTimeout(() => fetch("/tunnel?action=status").then((r) => r.json()).then(setTunnel), 1500))} />
    {health && health.phase !== "ready" && <div className="border-b border-[#1e2230] px-4 py-1.5 text-xs text-yellow-400">{health.phase}: {health.detail}</div>}
    <TrafficBar traffic={traffic} connections={connections.length} exit={exit} />
    <nav className="flex border-b border-[#1e2230]"><button className={`border-b-2 px-4 py-2 text-xs ${tab === "nodes" ? "border-blue-500 text-[#e0e3e8]" : "border-transparent text-[#7a8190]"}`} onClick={() => setTab("nodes")}>Nodes</button><button className={`border-b-2 px-4 py-2 text-xs ${tab === "conns" ? "border-blue-500 text-[#e0e3e8]" : "border-transparent text-[#7a8190]"}`} onClick={() => setTab("conns")}>Connections</button><button className="cursor-not-allowed border-b-2 border-transparent px-4 py-2 text-xs text-[#4b5563]" disabled title="Proxy disabled">Proxy</button></nav>
    {tab === "nodes" ? <NodeTable nodes={names} labels={labels} current={current} delays={delays} busy={busy} onSelect={selectNode} onTest={testOne} /> : <Connections list={connections} onClose={(id) => api(`/connections/${encodeURIComponent(id)}`, { method: "DELETE" }).then(() => setConnections((old) => old.filter((item) => item.id !== id)))} />}
  </main>;
}
createRoot(document.getElementById("root")).render(<App />);
