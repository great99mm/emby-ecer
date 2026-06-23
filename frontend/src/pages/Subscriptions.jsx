import { useEffect, useState } from 'react';
import { api } from '../api';
import toast from 'react-hot-toast';
import { Play, Plus, Trash2, RefreshCw } from 'lucide-react';

const emptyForm = { title: '', mediaType: 'tv', tmdbId: '', season: '', enabled: true, autoTransfer: false, targetCid: '0' };

export default function Subscriptions() {
  const [items, setItems] = useState([]);
  const [running, setRunning] = useState(false);
  const [form, setForm] = useState(emptyForm);
  const [loading, setLoading] = useState(false);
  const [result, setResult] = useState(null);

  const load = async () => {
    try {
      const data = await api('/api/subscriptions');
      setItems(data.items || []);
      setRunning(!!data.running);
    } catch (err) {
      toast.error(err.message);
    }
  };

  useEffect(() => { load(); }, []);

  const update = (name, value) => setForm(prev => ({ ...prev, [name]: value }));

  const save = async (e) => {
    e.preventDefault();
    if (!form.title.trim()) return toast.error('请填写订阅标题');
    setLoading(true);
    try {
      await api('/api/subscriptions', {
        method: 'POST',
        body: JSON.stringify({
          ...form,
          tmdbId: Number(form.tmdbId || 0),
          season: Number(form.season || 0),
        }),
      });
      setForm(emptyForm);
      await load();
      toast.success('订阅已保存');
    } catch (err) {
      toast.error(err.message);
    } finally {
      setLoading(false);
    }
  };

  const remove = async (id) => {
    if (!window.confirm('确认删除这个订阅？')) return;
    try {
      const data = await api('/api/subscriptions/delete', { method: 'POST', body: JSON.stringify({ ids: [id] }) });
      setItems(data.items || []);
      toast.success('已删除');
    } catch (err) {
      toast.error(err.message);
    }
  };

  const run = async (id = '') => {
    setRunning(true);
    setResult(null);
    try {
      const data = await api('/api/subscriptions/run', { method: 'POST', body: JSON.stringify({ ids: id ? [id] : [] }) });
      setResult(data);
      await load();
      toast.success('订阅扫描完成');
    } catch (err) {
      toast.error(err.message);
    } finally {
      setRunning(false);
    }
  };

  return (
    <div className="space-y-5">
      <div className="card">
        <div className="flex items-start justify-between gap-4">
          <div>
            <h1 className="text-xl font-black text-gray-900">订阅与自动扫描</h1>
            <p className="mt-1 text-sm text-gray-500">按标题/TMDB 追踪 HDHive 115 资源，可结合大模型自动排序候选。</p>
          </div>
          <button onClick={() => run()} disabled={running || !items.length} className="btn-primary flex items-center gap-2 text-sm disabled:opacity-50">
            {running ? <RefreshCw className="h-4 w-4 animate-spin" /> : <Play className="h-4 w-4" />} 扫描全部
          </button>
        </div>
      </div>

      <form onSubmit={save} className="card space-y-3">
        <div className="flex items-center gap-2 text-sm font-bold text-gray-900"><Plus className="h-4 w-4 text-primary-600" /> 新增订阅</div>
        <input className="w-full rounded-md border border-gray-300 bg-gray-50 px-4 py-2.5 text-sm font-medium" placeholder="标题，例如 凡人修仙传" value={form.title} onChange={e => update('title', e.target.value)} />
        <div className="grid grid-cols-2 gap-3">
          <select className="rounded-md border border-gray-300 bg-gray-50 px-3 py-2.5 text-sm font-medium" value={form.mediaType} onChange={e => update('mediaType', e.target.value)}>
            <option value="tv">剧集</option>
            <option value="movie">电影</option>
          </select>
          <input className="rounded-md border border-gray-300 bg-gray-50 px-4 py-2.5 text-sm font-medium" placeholder="TMDB ID（可选）" value={form.tmdbId} onChange={e => update('tmdbId', e.target.value)} />
          <input className="rounded-md border border-gray-300 bg-gray-50 px-4 py-2.5 text-sm font-medium" placeholder="季号（可选）" value={form.season} onChange={e => update('season', e.target.value)} />
          <input className="rounded-md border border-gray-300 bg-gray-50 px-4 py-2.5 text-sm font-medium" placeholder="目标 CID" value={form.targetCid} onChange={e => update('targetCid', e.target.value)} />
        </div>
        <div className="grid grid-cols-2 gap-3 text-sm font-semibold text-gray-700">
          <label className="flex items-center gap-2 rounded-md bg-gray-50 border border-gray-200 px-3 py-2"><input type="checkbox" checked={form.enabled} onChange={e => update('enabled', e.target.checked)} /> 启用</label>
          <label className="flex items-center gap-2 rounded-md bg-gray-50 border border-gray-200 px-3 py-2"><input type="checkbox" checked={form.autoTransfer} onChange={e => update('autoTransfer', e.target.checked)} /> 自动转存</label>
        </div>
        <button disabled={loading} className="btn-primary w-full disabled:opacity-50">{loading ? '保存中...' : '保存订阅'}</button>
      </form>

      <div className="space-y-3">
        {items.length === 0 ? <div className="card text-center text-sm text-gray-500">暂无订阅</div> : items.map(item => (
          <div key={item.id} className="card">
            <div className="flex items-start justify-between gap-3">
              <div className="min-w-0">
                <div className="flex items-center gap-2">
                  <h2 className="truncate text-base font-bold text-gray-900">{item.title}</h2>
                  <span className="rounded-full bg-gray-100 px-2 py-0.5 text-[11px] font-bold text-gray-500">{item.mediaType === 'movie' ? '电影' : '剧集'}</span>
                  {!item.enabled && <span className="rounded-full bg-amber-50 px-2 py-0.5 text-[11px] font-bold text-amber-600">停用</span>}
                </div>
                <p className="mt-1 text-xs text-gray-400">TMDB {item.tmdbId || '未填'} · 上次：{item.lastRunAt ? new Date(item.lastRunAt).toLocaleString('zh-CN') : '未扫描'}</p>
                {item.lastMessage && <p className="mt-1 text-xs text-gray-500">{item.lastMessage}</p>}
              </div>
              <div className="shrink-0 flex items-center gap-1">
                <button onClick={() => run(item.id)} disabled={running} className="rounded-md border border-gray-300 px-2.5 py-1.5 text-xs font-semibold text-gray-600 hover:text-primary-600 disabled:opacity-40">扫描</button>
                <button onClick={() => remove(item.id)} className="rounded-md border border-red-200 px-2.5 py-1.5 text-xs font-semibold text-red-500 hover:bg-red-50"><Trash2 className="h-3.5 w-3.5" /></button>
              </div>
            </div>
            {!!item.lastResults?.length && <div className="mt-3 space-y-2 border-t border-gray-100 pt-3">
              {item.lastResults.slice(0, 3).map((r, i) => <div key={i} className="rounded-md bg-gray-50 px-3 py-2 text-xs text-gray-600"><b>{r.title}</b><div className="mt-0.5 text-gray-400">{r.source}</div></div>)}
            </div>}
          </div>
        ))}
      </div>

      {result && <div className="card text-sm text-gray-600">本次扫描：成功 {result.success || 0}，失败 {result.failed || 0}</div>}
    </div>
  );
}
