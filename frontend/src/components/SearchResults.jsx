import { useState } from 'react';
import useStore from '../store';
import { api } from '../api';
import toast from 'react-hot-toast';
import { Download, Unlock } from 'lucide-react';

export default function SearchResults({ search, targetCid = '', subscriptionId = '', onUnlocked }) {
  const transfers = useStore(s => s.transfers);
  const setTransfer = useStore(s => s.setTransfer);
  const [unlockedResults, setUnlockedResults] = useState({});

  if (!search) return null;
  if (search.loading) return <p className="text-sm text-gray-400 py-3 text-center">搜索中...</p>;
  if (search.error) return <p className="text-sm font-semibold text-red-500 bg-red-50 rounded-md p-3 mt-2">{search.error}</p>;
  if (!search.results?.length) return search.results ? <p className="text-sm text-gray-400 py-3 text-center">没有搜到 115 资源</p> : null;

  const codes = (search.codes || '').split('、').filter(Boolean);

  const matchInfo = (result) => {
    // 后端解析出的集号命中优先
    const m = result.match;
    if (m) {
      if (m.matchedEpisodes?.length) return { matched: true, type: 'match', tags: m.tags };
      if (m.episodes?.length) return { matched: false };
      if (m.seasonPack || m.seasonMatched) return { matched: true, type: 'include', tags: m.tags };
    }
    const t = (result.title || '').toUpperCase();
    const d = (result.description || '');
    const desc = result.title || result.description || '';
    // 精确匹配
    if (codes.some(c => t.includes(c.toUpperCase()) || (d || '').toUpperCase().includes(c.toUpperCase()))) {
      return { matched: true, type: 'match' };
    }
    // 范围匹配
    const rangeMatch = desc.match(/第\s*(\d+)\s*[-~～]\s*(\d+)\s*集/) || desc.match(/全\s*(\d+)\s*集/);
    if (rangeMatch) {
      let start = 1, end = 0;
      if (rangeMatch[2]) { start = parseInt(rangeMatch[1]); end = parseInt(rangeMatch[2]); }
      else { end = parseInt(rangeMatch[1]); }
      if (end > 0 && codes.some(c => { const m = c.match(/S\d+E(\d+)/i); return m && parseInt(m[1]) >= start && parseInt(m[1]) <= end; })) {
        return { matched: true, type: 'include' };
      }
    }
    return { matched: false };
  };

  const doTransfer = async (result, index) => {
    const key = `${search.query || 's'}:${index}`;
    setTransfer(key, { loading: true });
    try {
      const data = await api('/api/115/transfer', {
        method: 'POST',
        body: JSON.stringify({ url: result.url, password: result.password, targetCid }),
      });
      setTransfer(key, { ok: true, count: data.count, targetCid: data.targetCid });
      toast.success('转存成功');
    } catch (err) {
      setTransfer(key, { error: err.message });
      toast.error(err.message);
    }
  };

  const doHDHiveUnlock = async (result, index) => {
    const key = `${search.query || 's'}:${index}`;
    setTransfer(key, { loading: true });
    try {
      const data = await api('/api/hdhive/unlock', {
        method: 'POST',
        body: JSON.stringify({ slug: result.hdhiveSlug, title: result.title, subscriptionId }),
      });
      const unlocked = { ...result, ...(data.result || {}), hdhiveLocked: false };
      setUnlockedResults(prev => ({ ...prev, [key]: unlocked }));
      setTransfer(key, {});
      onUnlocked?.(unlocked);
      toast.success('HDHive 解锁成功，可转存');
    } catch (err) {
      setTransfer(key, { error: err.message });
      toast.error(err.message);
    }
  };

  // 裁剪标题：去掉评分/类型/地区/语言/主演/简介，只留名称+大小
  const cleanTitle = (raw) => {
    if (!raw) return { title: '', size: '' };
    // 提取大小
    const sizeMatch = raw.match(/大小[：:]\s*([^\s]+(?:\s*[A-Za-z]+)?)/);
    const size = sizeMatch ? sizeMatch[1] : '';
    // 截断到评分/类型/简介之前
    let title = raw
      .replace(/评分[：:].*/s, '')
      .replace(/类型[：:].*/s, '')
      .replace(/地区[：:].*/s, '')
      .replace(/语言[：:].*/s, '')
      .replace(/主演[：:].*/s, '')
      .replace(/简介[：:].*/s, '')
      .trim();
    // 清理尾部多余的分隔符
    title = title.replace(/[\s,，;；]+$/, '');
    return { title: title || raw.split(/评分|类型|简介/)[0]?.trim() || raw, size };
  };

  return (
    <div className="mt-3 space-y-2">
      <p className="text-xs font-bold text-gray-400">搜索结果 · {search.results.length} 条</p>
      {search.results.map((result, index) => {
        const key = `${search.query || 's'}:${index}`;
        result = unlockedResults[key] || result;
        const { title, size } = cleanTitle(result.title);
        const info = matchInfo(result);
        const matched = info.matched;
        const t = transfers[key];
        const hasUrl = !!result.url;
        const canUnlock = result.source === 'HDHive' && result.hdhiveSlug && (result.hdhiveLocked || !hasUrl);

        return (
          <div key={index} className={`rounded-md bg-gray-50 p-3 border ${matched ? 'border-emerald-300 bg-emerald-50/30' : 'border-gray-100'}`}>
            <div className="flex items-start justify-between gap-2">
              <div className="min-w-0 flex-1">
                <p className="text-sm font-bold text-gray-900 leading-snug">
                  {matched && <span className={`inline-flex items-center rounded text-xs font-bold px-1.5 py-0.5 mr-1.5 ${info.type === 'include' ? 'bg-amber-100 text-amber-700' : 'bg-emerald-100 text-emerald-700'}`}>{info.type === 'include' ? '包含' : '✓'}</span>}
                  {title}
                  {size && <span className="text-gray-400 font-normal ml-1.5">{size}</span>}
                </p>
                <p className="mt-1 text-xs text-gray-400">{result.source || 'PanSou'}</p>
              </div>
              {t?.ok ? (
                <span className="shrink-0 text-xs font-bold text-emerald-600 bg-emerald-50 px-2 py-1 rounded">已转存</span>
              ) : t?.error ? (
                <span className="shrink-0 text-xs text-red-500">{t.error}</span>
              ) : canUnlock ? (
                <button
                  onClick={() => doHDHiveUnlock(result, index)}
                  disabled={t?.loading}
                  className="shrink-0 inline-flex items-center gap-1 rounded-md border border-amber-300 bg-amber-50 px-3 py-1.5 text-xs font-semibold text-amber-700 hover:border-amber-400 hover:bg-amber-100 transition-colors disabled:opacity-50"
                >
                  <Unlock className="w-3.5 h-3.5" />
                  {t?.loading ? '解锁中' : `解锁${result.unlockPoints ? ` ${result.unlockPoints}积分` : ''}`}
                </button>
              ) : !hasUrl ? (
                <span className="shrink-0 text-xs font-semibold text-amber-600 bg-amber-50 px-2 py-1 rounded">无链接</span>
              ) : (
                <button
                  onClick={() => doTransfer(result, index)}
                  disabled={t?.loading}
                  className="shrink-0 inline-flex items-center gap-1 rounded-md border border-gray-300 px-3 py-1.5 text-xs font-semibold text-gray-600 hover:border-primary-400 hover:text-primary-600 transition-colors disabled:opacity-50"
                >
                  <Download className="w-3.5 h-3.5" />
                  {t?.loading ? '转存中' : '转存'}
                </button>
              )}
            </div>
          </div>
        );
      })}
    </div>
  );
}
