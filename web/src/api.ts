import { useEffect, useState } from "react";
export async function get<T>(path: string, signal?: AbortSignal): Promise<T> {
  const r = await fetch(path, {
    signal,
    credentials: "same-origin",
    headers: { Accept: "application/json" },
  });
  const body = await r.json();
  if (body.api_version !== 1)
    throw new Error(
      "Unsupported API response. Refresh after rebuilding the dashboard.",
    );
  if (!r.ok || body.error)
    throw new Error(body.error?.message ?? "History query failed.");
  return body.data as T;
}
export function useRemote<T>(url: string | null) {
  const [state, setState] = useState<{
    url: string | null;
    data?: T;
    error?: string;
  }>({ url: null });
  useEffect(() => {
    if (!url) return;
    const controller = new AbortController();
    setState({ url });
    get<T>(url, controller.signal)
      .then((data) => setState({ url, data }))
      .catch((e) => {
        if (!controller.signal.aborted)
          setState({ url, error: String(e.message) });
      });
    return () => controller.abort();
  }, [url]);
  return state.url === url
    ? { ...state, loading: !!url && !state.data && !state.error }
    : { loading: !!url, data: undefined, error: undefined };
}
