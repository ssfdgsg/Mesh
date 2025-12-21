export type RouterStatus = {
  ok: boolean;
  poll_interval_ms: number;
  config_loaded: boolean;
  config_updated_at?: string;
  last_error?: string;
};

export type PluginConfig = { name: string; config: unknown };
export type ListenerConfig = {
  listen: string;
  upstreams: string[];
  plugins?: PluginConfig[];
};
export type SiderConfig = {
  listeners: ListenerConfig[];
  dial_timeout_ms: number;
};

const baseUrl = (import.meta.env.VITE_ROUTER_BASE_URL as string | undefined) ?? "";

export async function fetchHealthz(signal?: AbortSignal): Promise<boolean> {
  const resp = await fetch(`${baseUrl}/healthz`, { signal });
  return resp.ok;
}

export async function fetchStatus(signal?: AbortSignal): Promise<RouterStatus> {
  const resp = await fetch(`${baseUrl}/v1/ui/status`, { signal });
  if (!resp.ok) throw new Error(`status ${resp.status}`);
  return (await resp.json()) as RouterStatus;
}

export async function fetchConfig(signal?: AbortSignal): Promise<SiderConfig> {
  const resp = await fetch(`${baseUrl}/v1/ui/config`, { signal });
  if (!resp.ok) throw new Error(`status ${resp.status}`);
  return (await resp.json()) as SiderConfig;
}

export async function reloadConfig(): Promise<SiderConfig> {
  const resp = await fetch(`${baseUrl}/v1/ui/config/reload`, {
    method: "POST",
  });
  if (!resp.ok) throw new Error(`status ${resp.status}`);
  return (await resp.json()) as SiderConfig;
}

export function openConfigStream(onConfig: (cfg: SiderConfig) => void): EventSource {
  const es = new EventSource(`${baseUrl}/v1/ui/config/stream`);
  es.addEventListener("config", (evt) => {
    const msg = evt as MessageEvent<string>;
    try {
      onConfig(JSON.parse(msg.data) as SiderConfig);
    } catch {
      // ignore
    }
  });
  return es;
}

