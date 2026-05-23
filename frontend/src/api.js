const BASE = '';

export async function api(path, options = {}) {
  const token = localStorage.getItem('auth_token');
  const headers = { ...(options.headers || {}) };
  if (!headers['Content-Type'] && options.body) headers['Content-Type'] = 'application/json';
  if (token) headers.Authorization = `Bearer ${token}`;
  const res = await fetch(BASE + path, { ...options, headers });
  const data = await res.json().catch(() => ({}));
  if (!res.ok) {
    const err = new Error(data.error || data.message || `请求失败 ${res.status}`);
    err.status = res.status;
    throw err;
  }
  return data;
}
