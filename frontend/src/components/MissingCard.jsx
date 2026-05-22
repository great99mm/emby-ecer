import { useState } from 'react';
import useStore from '../store';
import { api } from '../api';
import toast from 'react-hot-toast';
import { Search, Download, X } from 'lucide-react';
import SearchResults from './SearchResults';

const TMDB_IMG = 'https://image.tmdb.org/t/p/w342';

export default function MissingCard({ group }) {
  const seriesKey = `series:${group.tmdbId || group.key}`;
  const search = useStore(s => s.seriesSearches[seriesKey]);
  const setSeriesSearch = useStore(s => s.setSeriesSearch);
  const [open, setOpen] = useState(false);
  const [mpPage, setMpPage] = useState(1);
  const pageSize = 20;

  const totalEps = group.totalEpisodes || 0;
  const ownedEps = group.ownedEpisodes || 0;
  const healthPct = totalEps > 0 ? Math.round((ownedEps / totalEps) * 100) : 0;
  const missingEps = (group.items || []).length;
  const codes = (group.codes || []).join('、');
  const codeList = group.codes || [];

  const doSearch = async () => {
    setSeriesSearch(seriesKey, prev => ({ ...prev, loading: true, codes }));
    try {
      const data = await api('/api/search', { method: 'POST', body: JSON.stringify({ keyword: group.title }) });
      setSeriesSearch(seriesKey, prev => ({ ...prev, loading: false, results: data.results || [], query: data.query, codes }));
    } catch (err) {
      setSeriesSearch(seriesKey, prev => ({ ...prev, loading: false, error: err.message, codes }));
    }
  };

  const doMPSearch = async () => {
    const keywords = [group.title];
    if (group.tmdbId) keywords.unshift(`tmdb:${group.tmdbId}`);
    setSeriesSearch(seriesKey, prev => ({ ...prev, mpLoading: true, mpKeywords: keywords }));
    try {
      const body = { keyword: group.title };
      if (group.tmdbId) body.tmdbId = String(group.tmdbId);
      const data = await api('/api/mp/search', { method: 'POST', body: JSON.stringify(body) });
      const arr = Array.isArray(data.results) ? data.results : [];
      const items = arr.map(r => ({
        title: r.title || r.description || '',
        description: r.description || '',
        url: r.enclosure || r.page_url || r.torrent_url || r.magnet || '',
        size: r.size ? (r.size >= 1e9 ? (r.size/1e9).toFixed(1)+'GB' : r.size >= 1e6 ? (r.size/1e6).toFixed(0)+'MB' : r.size+'B') : '',
        seeders: r.seeders || 0,
        source: r.site_name || r.site || '',
        pubdate: r.pubdate || '',
        raw: r,
      }));
      setSeriesSearch(seriesKey, prev => ({ ...prev, mpLoading: false, mpResults: items, query: group.title }));
    } catch (err) {
      setSeriesSearch(seriesKey, prev => ({ ...prev, mpLoading: false, mpError: err.message }));
    }
  };

  const doMPDownload = async (item) => {
    try {
      await api('/api/mp/download', { method: 'POST', body: JSON.stringify({ rawData: item.raw, tmdbId: String(group.tmdbId || '') }) }); toast.success('已提交下载'); }
    catch (err) { toast.error(err.message); }
  };

  const matchCode = (r) => {
    const t = (r.title || '').toUpperCase();
    const d = (r.description || '').toUpperCase();
    const combined = t + ' ' + d;
    // 精确匹配集号
    if (codeList.some(c => t.includes(c.toUpperCase()) || d.includes(c.toUpperCase()))) return 'match';
    // 描述中包含范围（如"第1-12集"、"全30集"），且缺的集在范围内
    const desc = r.description || r.title || '';
    const rangeMatch = desc.match(/第\s*(\d+)\s*[-~～]\s*(\d+)\s*集/) || 
                       desc.match(/全\s*(\d+)\s*集/);
    if (rangeMatch) {
      let start = 1, end = 0;
      if (rangeMatch[2]) { start = parseInt(rangeMatch[1]); end = parseInt(rangeMatch[2]); }
      else { end = parseInt(rangeMatch[1]); }
      if (end > 0 && codeList.some(c => { const m = c.match(/S\d+E(\d+)/i); return m && parseInt(m[1]) >= start && parseInt(m[1]) <= end; })) {
        return 'include';
      }
    }
    // S01 季匹配
    if (codeList.some(c => { const s = c.match(/S(\d+)/i); return s && combined.includes('S' + s[1]); })) return 'include';
    return false;
  };

  const matchType = (r) => matchCode(r);

  const allMP = search?.mpResults || [];
  const totalPages = Math.max(1, Math.ceil(allMP.length / pageSize));
  const pageMP = allMP.slice((mpPage - 1) * pageSize, mpPage * pageSize);
  const matchedMP = pageMP.filter(r => matchCode(r) !== false);
  const unmatchedMP = pageMP.filter(r => matchCode(r) === false);
  const matchedAll = allMP.filter(r => matchCode(r) !== false).length;
  const panResults = search?.results || [];
  const matchedPan = panResults.filter(r => matchCode(r) !== false);
  const unmatchedPan = panResults.filter(r => matchCode(r) === false);

  // Poster Card
  return (
    <>
      <div onClick={() => setOpen(true)} className="bg-white rounded-xl border border-gray-200 shadow-sm overflow-hidden cursor-pointer hover:shadow-md transition-shadow">
        <div className="relative aspect-[2/3] bg-gray-100">
          {group.posterPath ? (
            <img src={TMDB_IMG + group.posterPath} className="w-full h-full object-cover" loading="lazy" alt="" />
          ) : (
            <div className="w-full h-full flex items-center justify-center text-4xl text-gray-300">🎬</div>
          )}
          <div className="absolute inset-0 bg-gradient-to-t from-black/70 via-transparent to-transparent" />
          <span className={`absolute top-2 right-2 px-2 py-0.5 rounded-full text-[10px] font-bold border ${healthPct >= 100 ? 'bg-emerald-50 text-emerald-700 border-emerald-200' : 'bg-red-50 text-red-700 border-red-200'}`}>
            {healthPct >= 100 ? '完整' : '缺失'}
          </span>
          {totalEps > 0 && (
            <div className="absolute bottom-0 left-0 right-0 p-2">
              <div className="h-1 w-full rounded-full bg-white/30 mb-1">
                <div className={`h-full rounded-full ${healthPct >= 80 ? 'bg-emerald-400' : healthPct >= 50 ? 'bg-amber-400' : 'bg-red-400'}`} style={{ width: `${Math.max(healthPct, 5)}%` }} />
              </div>
              <div className="flex items-center justify-between text-white text-[10px] font-semibold">
                <span>{ownedEps}/{totalEps}集</span>
                <span>{healthPct}%</span>
              </div>
            </div>
          )}
        </div>
        <div className="px-2.5 py-2">
          <p className="text-xs font-bold text-gray-900 truncate">{group.title}</p>
          <p className="text-[10px] text-red-500 mt-0.5">缺{missingEps}集</p>
        </div>
      </div>

      {/* Modal */}
      {open && (
        <div className="fixed inset-0 z-50 flex items-start justify-center pt-8 px-4 pb-8 overflow-y-auto" onClick={() => setOpen(false)}>
          <div className="fixed inset-0 bg-black/40" />
          <div className="relative bg-white rounded-2xl shadow-2xl w-full max-w-lg my-auto" onClick={e => e.stopPropagation()}>
            <div className="flex items-start gap-3 p-4 border-b border-gray-100">
              {group.posterPath ? (
                <img src={TMDB_IMG + group.posterPath} className="w-14 h-[83px] rounded-lg object-cover shrink-0 bg-gray-100" alt="" />
              ) : (
                <div className="w-14 h-[83px] rounded-lg bg-gray-100 shrink-0 flex items-center justify-center text-xl">🎬</div>
              )}
              <div className="min-w-0 flex-1">
                <h2 className="text-base font-bold text-gray-900">{group.title}</h2>
                <p className="text-xs text-gray-400 mt-0.5">TMDB {group.tmdbId || ''}</p>
                <div className="flex items-center gap-2 mt-1">
                  <span className="text-xs text-red-500 font-semibold">缺{missingEps}集</span>
                  {totalEps > 0 && <span className="text-xs text-gray-400">{ownedEps}/{totalEps}集 · {healthPct}%</span>}
                </div>
                <p className="text-xs text-gray-500 mt-1">缺失集号：{codes}</p>
              </div>
              <button onClick={() => setOpen(false)} className="shrink-0 p-1 rounded-lg hover:bg-gray-100"><X className="w-5 h-5 text-gray-400" /></button>
            </div>
            <div className="px-4 py-3 grid grid-cols-2 gap-2">
              <button type="button" onClick={doMPSearch} disabled={!!search?.mpLoading} className="flex items-center justify-center gap-1.5 rounded-lg border-2 border-gray-300 px-3 py-2 text-sm font-semibold text-gray-600 hover:border-primary-400 hover:text-primary-600 disabled:opacity-50">
                <Download className="w-4 h-4" /> {search?.mpLoading ? 'MP中' : 'MP搜索'}
              </button>
              <button type="button" onClick={doSearch} disabled={!!search?.loading} className="flex items-center justify-center gap-1.5 rounded-lg bg-primary-600 hover:bg-primary-700 text-white px-3 py-2 text-sm font-semibold disabled:opacity-50">
                <Search className="w-4 h-4" /> {search?.loading ? '盘搜中' : '盘搜搜索'}
              </button>
            </div>
            <div className="px-4 pb-4 max-h-[60vh] overflow-y-auto">
              {search?.mpKeywords && (
                <div className="flex flex-wrap gap-1 mb-2">
                  {search.mpKeywords.map((kw, i) => <span key={i} className="inline-flex items-center px-2 py-0.5 rounded-full bg-primary-50 text-primary-700 text-[10px] font-semibold border border-primary-200">{kw}</span>)}
                </div>
              )}
              {search?.mpLoading && <p className="text-sm text-gray-400 py-2">MP搜索中...</p>}
              {search?.mpError && <div className="rounded-lg bg-red-50 border border-red-200 p-2.5 mb-2"><p className="text-sm font-semibold text-red-600">{search.mpError}</p></div>}

              {/* MP 结果统计 */}
              {!search?.mpLoading && search?.mpResults !== undefined && (
                <div className="flex items-center justify-between mb-2">
                  <p className="text-xs text-gray-400">MP结果 · {allMP.length} 条{totalPages > 1 ? ` · ${mpPage}/${totalPages}页` : ''}</p>
                  {totalPages > 1 && (
                    <div className="flex gap-1">
                      <button type="button" onClick={() => setMpPage(p => Math.max(1, p-1))} disabled={mpPage <= 1} className="text-[10px] font-semibold px-2 py-0.5 rounded border border-gray-200 disabled:opacity-30 hover:bg-gray-100">上一页</button>
                      <button type="button" onClick={() => setMpPage(p => Math.min(totalPages, p+1))} disabled={mpPage >= totalPages} className="text-[10px] font-semibold px-2 py-0.5 rounded border border-gray-200 disabled:opacity-30 hover:bg-gray-100">下一页</button>
                    </div>
                  )}
                </div>
              )}

              {matchedMP.length > 0 && (
                <div className="mb-3">
                  <p className="text-xs font-bold text-gray-400 mb-1.5">MP匹配 · {matchedAll}条</p>
                  {matchedMP.map((r, i) => {
                    const mt = matchType(r);
                    return (
                    <div key={'mmp'+i} className="rounded-lg p-2.5 border border-emerald-200 bg-emerald-50/30 mb-1.5">
                      <div className="flex items-start justify-between gap-2">
                        <div className="min-w-0 flex-1">
                          <p className="text-sm font-medium text-gray-800">
                            <span className={`inline-flex items-center rounded text-xs font-bold px-1.5 py-0.5 mr-1 ${mt === 'include' ? 'bg-amber-100 text-amber-700' : 'bg-emerald-100 text-emerald-700'}`}>{mt === 'include' ? '包含' : '✓'}</span>
                            {r.title}
                          </p>
                          {r.description && <p className="text-xs text-gray-400 mt-0.5 line-clamp-2">{r.description}</p>}
                          <p className="text-xs text-gray-400 mt-0.5">{r.source}{r.size ? ` · ${r.size}` : ''}{r.seeders ? ` · ${r.seeders}↑` : ''}</p>
                        </div>
                        <button type="button" onClick={() => doMPDownload(r)} className="shrink-0 text-xs font-semibold text-primary-600 hover:text-primary-700">下载</button>
                      </div>
                    </div>
                  )})}
                </div>
              )}

              {unmatchedMP.length > 0 && (
                (matchedMP.length === 0) ? (
                  <div className="mt-1 space-y-1.5">
                    {unmatchedMP.map((r, i) => (
                      <div key={'ump'+i} className="rounded-lg p-2 border border-gray-100 bg-gray-50">
                        <div className="flex items-start justify-between gap-2">
                          <div className="min-w-0 flex-1"><p className="text-sm font-medium text-gray-700">{r.title}</p>{r.description && <p className="text-xs text-gray-400 mt-0.5">{r.description}</p>}<p className="text-xs text-gray-400 mt-0.5">{r.source}{r.size ? ` · ${r.size}` : ''}</p></div>
                          <button type="button" onClick={() => doMPDownload(r)} className="shrink-0 text-xs text-primary-600">下载</button>
                        </div>
                      </div>
                    ))}
                  </div>
                ) : (
                  <details>
                    <summary className="cursor-pointer text-xs font-bold text-gray-400 hover:text-gray-600 py-1">MP未匹配 · {unmatchedMP.length}条</summary>
                    <div className="mt-1 space-y-1.5">
                      {unmatchedMP.map((r, i) => (
                        <div key={'ump'+i} className="rounded-lg p-2 border border-gray-100 bg-gray-50">
                          <div className="flex items-start justify-between gap-2">
                            <div className="min-w-0 flex-1"><p className="text-sm font-medium text-gray-700">{r.title}</p>{r.description && <p className="text-xs text-gray-400 mt-0.5">{r.description}</p>}<p className="text-xs text-gray-400 mt-0.5">{r.source}{r.size ? ` · ${r.size}` : ''}</p></div>
                            <button type="button" onClick={() => doMPDownload(r)} className="shrink-0 text-xs text-primary-600">下载</button>
                          </div>
                        </div>
                      ))}
                    </div>
                  </details>
                )
              )}

              {allMP.length === 0 && matchedMP.length === 0 && unmatchedMP.length === 0 && !search?.mpLoading && search?.mpResults !== undefined && (
                <p className="text-xs text-gray-400 py-2">MP无匹配结果</p>
              )}

              {panResults.length > 0 && (
                <div className="mt-3">
                  {matchedPan.length > 0 && (
                    <div className="mb-2">
                      <p className="text-xs font-bold text-gray-400 mb-1.5">盘搜匹配 · {matchedPan.length}条</p>
                      <SearchResults search={{ ...search, results: matchedPan }} />
                    </div>
                  )}
                  {unmatchedPan.length > 0 && (
                    matchedPan.length === 0 ? (
                      <div>
                        <p className="text-xs font-bold text-gray-400 mb-1.5">盘搜结果 · {unmatchedPan.length}条</p>
                        <SearchResults search={{ ...search, results: unmatchedPan }} />
                      </div>
                    ) : (
                      <details>
                        <summary className="cursor-pointer text-xs font-bold text-gray-400 hover:text-gray-600 py-1">盘搜未匹配 · {unmatchedPan.length}条</summary>
                        <div className="mt-1"><SearchResults search={{ ...search, results: unmatchedPan }} /></div>
                      </details>
                    )
                  )}
                </div>
              )}
            </div>
          </div>
        </div>
      )}
    </>
  );
}
