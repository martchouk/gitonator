const BASE_URL =
  import.meta.env.VITE_API_BASE_URL ??
  (import.meta.env.DEV ? '' : 'http://127.0.0.1:6666');

async function get<T>(path: string): Promise<T> {
  let res: Response;
  try {
    res = await fetch(`${BASE_URL}${path}`);
  } catch {
    throw new Error('offline');
  }
  if (!res.ok) {
    const ct = res.headers.get('content-type') ?? '';
    if (res.status >= 502 || ct.includes('text/html')) {
      throw new Error('offline');
    }
    const text = await res.text().catch(() => res.statusText);
    throw new Error(`${res.status}: ${text}`);
  }
  return res.json() as Promise<T>;
}

export { BASE_URL, get };
