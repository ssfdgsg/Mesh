import { useEffect, useMemo, useRef, useState } from "react";
import type { RouterStatus, SiderConfig } from "./api";
import { fetchConfig, fetchHealthz, fetchStatus, openConfigStream, reloadConfig } from "./api";

function fmtTime(s?: string): string {
  if (!s) return "-";
  const d = new Date(s);
  if (Number.isNaN(d.getTime())) return s;
  return d.toLocaleString();
}

export default function App() {
  const [healthy, setHealthy] = useState<boolean | null>(null);
  const [status, setStatus] = useState<RouterStatus | null>(null);
  const [config, setConfig] = useState<SiderConfig | null>(null);
  const [err, setErr] = useState<string | null>(null);
  const [streamOn, setStreamOn] = useState(true);
  const esRef = useRef<EventSource | null>(null);

  const listeners = useMemo(() => config?.listeners ?? [], [config]);

  useEffect(() => {
    const ac = new AbortController();
    (async () => {
      try {
        const [h, st, cfg] = await Promise.all([
          fetchHealthz(ac.signal),
          fetchStatus(ac.signal),
          fetchConfig(ac.signal),
        ]);
        setHealthy(h);
        setStatus(st);
        setConfig(cfg);
        setErr(null);
      } catch (e) {
        setErr(e instanceof Error ? e.message : String(e));
        setHealthy(false);
      }
    })();
    return () => ac.abort();
  }, []);

  useEffect(() => {
    if (!streamOn) {
      esRef.current?.close();
      esRef.current = null;
      return;
    }
    esRef.current?.close();
    esRef.current = openConfigStream((cfg) => setConfig(cfg));
    return () => {
      esRef.current?.close();
      esRef.current = null;
    };
  }, [streamOn]);

  async function onReload() {
    try {
      const cfg = await reloadConfig();
      setConfig(cfg);
      setErr(null);
      const st = await fetchStatus();
      setStatus(st);
    } catch (e) {
      setErr(e instanceof Error ? e.message : String(e));
    }
  }

  return (
    <div className="page">
      <header className="header">
        <div>
          <div className="title">Mesh Router</div>
          <div className="subtitle">Control plane UI</div>
        </div>
        <div className="right">
          <span className={healthy ? "badge ok" : healthy === false ? "badge bad" : "badge"}>
            {healthy ? "Healthy" : healthy === false ? "Unhealthy" : "Checking..."}
          </span>
        </div>
      </header>

      <main className="grid">
        <section className="card">
          <div className="cardTitle">Actions</div>
          <div className="row">
            <button className="btn" onClick={onReload}>
              Reload config
            </button>
            <label className="toggle">
              <input
                type="checkbox"
                checked={streamOn}
                onChange={(e) => setStreamOn(e.target.checked)}
              />
              <span>Live stream</span>
            </label>
          </div>
          <div className="kv">
            <div className="k">Config loaded</div>
            <div className="v">{status?.config_loaded ? "Yes" : "No"}</div>
            <div className="k">Updated at</div>
            <div className="v">{fmtTime(status?.config_updated_at)}</div>
            <div className="k">Poll</div>
            <div className="v">{status ? `${status.poll_interval_ms} ms` : "-"}</div>
          </div>
          {status?.last_error ? <div className="warn">Last error: {status.last_error}</div> : null}
          {err ? <div className="error">UI error: {err}</div> : null}
        </section>

        <section className="card">
          <div className="cardTitle">Listeners</div>
          {listeners.length === 0 ? (
            <div className="muted">No listeners</div>
          ) : (
            <div className="list">
              {listeners.map((l, idx) => (
                <div className="listItem" key={`${l.listen}-${idx}`}>
                  <div className="mono">{l.listen}</div>
                  <div className="muted">{l.upstreams.join(", ")}</div>
                </div>
              ))}
            </div>
          )}
        </section>

        <section className="card full">
          <div className="cardTitle">Config (JSON)</div>
          <pre className="code">
            {config ? JSON.stringify(config, null, 2) : "Loading..."}
          </pre>
        </section>
      </main>
    </div>
  );
}

