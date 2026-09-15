import { useEffect, useState } from 'react';
import useStore from '../store';
import { api } from '../api';
import toast from 'react-hot-toast';
import { Save, Shield, Server, BadgeCheck, KeyRound, Download, Radar, Library, RefreshCw } from 'lucide-react';


function AuthSection({ title, icon, children, ready, onTest, target, testing, description }) {
  const Icon = icon;
  return (
    <div className="card">
      <div className="mb-4 flex items-start justify-between gap-3">
        <div className="flex items-start gap-3">
          <span className="connection-icon"><Icon size={19} /></span>
          <div>
            <h2 className="font-bold text-gray-900">{title}</h2>
            {description && <p className="mt-1 text-xs leading-5 text-gray-500">{description}</p>}
            {ready !== undefined && <span className={ready ? 'pill-green mt-2' : 'pill mt-2'}>{ready ? '已配置' : '待连接'}</span>}
          </div>
        </div>
        {target && (
          <button type="button" onClick={() => onTest(target)} disabled={testing} className="btn-outline shrink-0 !min-h-9 !px-3 !py-1.5 !text-xs">
            {testing ? '测试中…' : '测试连接'}
          </button>
        )}
      </div>
      <div className="space-y-3">{children}</div>
    </div>
  );
}

function Input({ name, label, value, onChange, type = 'text', placeholder = '', min, max }) {
  return (
    <label className="block">
      <span className="block text-xs font-bold text-gray-600 mb-1">{label}</span>
      <input
        name={name}
        min={min}
        max={max}
        autoComplete={name === 'oldPassword' ? 'current-password' : type === 'password' ? 'new-password' : 'off'}
        type={type}
        value={value}
        onChange={onChange}
        placeholder={placeholder}
        className="field"
      />
    </label>
  );
}

function Checkbox({ name, label, description, checked, onChange }) {
  return (
    <label className="flex items-center gap-2 rounded-md border border-gray-200 bg-gray-50 px-3 py-2 text-sm font-semibold text-gray-700">
      <input name={name} type="checkbox" checked={!!checked} onChange={onChange} className="h-4 w-4 rounded border-gray-300 accent-primary-600 text-primary-600 focus:ring-primary-500" />
      <span><span className="block">{label}</span>{description && <span className="mt-1 block text-xs font-normal leading-5 text-gray-500">{description}</span>}</span>
    </label>
  );
}

export default function Settings() {
  const [section, setSection] = useState('connections');
  const settings = useStore(s => s.settings);
  const setSettings = useStore(s => s.setSettings);
  const [form, setForm] = useState({});
  const [saving, setSaving] = useState(false);
  const [testing, setTesting] = useState({});
  const [libraries, setLibraries] = useState([]);
  const [loadingLibraries, setLoadingLibraries] = useState(false);

  const update = (name, value) => setForm(f => ({ ...f, [name]: value }));
  const get = (name) => form[name] !== undefined ? form[name] : ['embyApiKey','tmdbApiKey','mpToken'].includes(name) ? '' : (settings[name] ?? '');

  const excludedLibraries = form.excludedLibraries !== undefined ? form.excludedLibraries : (settings.excludedLibraries || []);
  const embyReady = !!settings.ready?.emby;

  const loadLibraries = async () => {
    setLoadingLibraries(true);
    try {
      const data = await api('/api/emby/libraries');
      setLibraries(data.libraries || []);
    } catch (err) {
      toast.error(err.message);
    } finally {
      setLoadingLibraries(false);
    }
  };

  useEffect(() => { if (embyReady) loadLibraries(); }, [embyReady]);

  const toggleLibrary = (id) => {
    const next = excludedLibraries.includes(id)
      ? excludedLibraries.filter(item => item !== id)
      : [...excludedLibraries, id];
    update('excludedLibraries', next);
  };

  const testConnection = async (target) => {
    if (testing[target]) return;
    const fields = { emby: ['embyUrl', 'embyApiKey', 'embyUserId'], tmdb: ['tmdbApiKey'], mp: ['mpUrl', 'mpToken'] }[target];
    const draft = Object.fromEntries(fields.map(name => [name, get(name)]));
    const label = { emby: 'Emby', tmdb: 'TMDB', mp: 'MoviePilot' }[target];
    setTesting(prev => ({ ...prev, [target]: true }));
    try {
      const data = await api('/api/settings/test', { method: 'POST', body: JSON.stringify({ target, settings: draft }) });
      const item = data[target];
      if (item?.ok) toast.success(`${label} 连接正常`);
      else toast.error(item?.error || `${label} 测试失败`);
    } catch (err) {
      toast.error(err.message);
    } finally {
      setTesting(prev => ({ ...prev, [target]: false }));
    }
  };

  const handleSave = async (e) => {
    e.preventDefault();
    setSaving(true);
    try {
      const payload = {};
      for (const [k, v] of Object.entries(form)) {
        if (['oldPassword','newPassword'].includes(k) || v === undefined) continue;
        if (['embyApiKey','tmdbApiKey','mpToken'].includes(k) && !String(v).trim()) continue;
        payload[k] = v;
      }
      const updated = await api('/api/settings', { method: 'POST', body: JSON.stringify(payload) });
      setSettings(updated);
      setForm({});
      toast.success('设置已保存');
    } catch (err) {
      toast.error(err.message);
    } finally {
      setSaving(false);
    }
  };

  const changePassword = async () => {
    const oldPwd = form.oldPassword || '';
    const newPwd = form.newPassword || '';
    if (!oldPwd || !newPwd) { toast.error('请填写新旧密码'); return; }
    try {
      await api('/api/auth/change-password', { method: 'POST', body: JSON.stringify({ oldPassword: oldPwd, newPassword: newPwd }) });
      toast.success('密码已修改');
      setForm(f => ({ ...f, oldPassword: '', newPassword: '' }));
    } catch (err) {
      toast.error(err.message);
    }
  };

  const ready = settings.ready || {};

  const dirty = Object.keys(form).some(key => !['oldPassword','newPassword'].includes(key));
  return (
    <div className="page-stack">
      <div><p className="eyebrow mb-2">PREFERENCES / CONNECTIONS</p><h1 className="page-title">连接与设置</h1><p className="page-description">连接媒体库，设置扫描方式，补片交给 MoviePilot。</p></div>
      <nav className="segmented" aria-label="设置分类">{[['connections','服务连接'],['scan','扫描偏好'],['security','账号安全']].map(([key,label]) => <button key={key} type="button" aria-pressed={section === key} onClick={() => setSection(key)}>{label}</button>)}</nav>
      <form onSubmit={handleSave} className="space-y-5" hidden={section === 'security'}>
        <div hidden={section !== 'connections'} className="space-y-5">
          <AuthSection title="Emby" icon={Server} ready={ready.emby} description="读取已有剧集，确定媒体库的实际库存。" onTest={testConnection} target="emby" testing={testing.emby}>
            <div className="grid gap-4 sm:grid-cols-2">
              <Input name="embyUrl" label="服务地址" value={get('embyUrl')} onChange={e => update('embyUrl', e.target.value)} placeholder="http://你的 Emby:8096" />
              <Input name="embyApiKey" label="API Key" value={get('embyApiKey')} onChange={e => update('embyApiKey', e.target.value)} type="password" placeholder={ready.emby ? '已配置，留空保留' : '在 Emby 控制台中获取'} />
            </div>
            <details className="pt-2"><summary className="text-xs font-medium text-gray-500">高级选项 · 用户与并发</summary><div className="mt-4 grid gap-4 sm:grid-cols-2"><Input name="embyUserId" label="用户 ID（可选）" value={get('embyUserId')} onChange={e => update('embyUserId', e.target.value)} placeholder="留空读取所有媒体库" /><Input name="scanConcurrency" label="扫描并发数" value={get('scanConcurrency')} onChange={e => update('scanConcurrency', e.target.value)} type="number" min={1} max={16} placeholder="默认 4，最大 16" /></div></details>
          </AuthSection>
          <div className="grid items-start gap-5 xl:grid-cols-2">
            <AuthSection title="TMDB" icon={BadgeCheck} ready={ready.tmdb} description="用官方季集信息，核对已播出的缺集。" onTest={testConnection} target="tmdb" testing={testing.tmdb}>
              <Input name="tmdbApiKey" label="API Key" value={get('tmdbApiKey')} onChange={e => update('tmdbApiKey', e.target.value)} type="password" placeholder={ready.tmdb ? '已配置，留空保留' : '填写 TMDB API Key'} />
            </AuthSection>
            <AuthSection title="MoviePilot" icon={Download} ready={ready.mp} description="搜索资源、提交下载与发送订阅。" onTest={testConnection} target="mp" testing={testing.mp}>
              <Input name="mpUrl" label="服务地址" value={get('mpUrl')} onChange={e => update('mpUrl', e.target.value)} placeholder="http://你的 MoviePilot:3001" />
              <Input name="mpToken" label="API Token" value={get('mpToken')} onChange={e => update('mpToken', e.target.value)} type="password" placeholder={ready.mp ? '已配置，留空保留' : '在 MoviePilot 设置中获取'} />
            </AuthSection>
          </div>
        </div>
        <div hidden={section !== 'scan'} className="space-y-5">
          <AuthSection title="扫描计划" icon={Radar} description="保持缺集列表更新，也可以随时手动扫描。">
            <Checkbox name="scanAutoEnabled" label="定时自动扫描" description="按设定间隔，在后台检查媒体库。" checked={get('scanAutoEnabled') === true || get('scanAutoEnabled') === 'true'} onChange={e => update('scanAutoEnabled', e.target.checked)} />
            <div className="grid gap-4 sm:grid-cols-2"><Input name="scanAutoInterval" label="扫描间隔（小时）" value={get('scanAutoInterval')} onChange={e => update('scanAutoInterval', e.target.value)} type="number" min={1} max={168} /></div>
            <Checkbox name="scanAutoRecentOnly" label="只扫描最近变更" description="定时扫描时复用未变化的剧集结果，减少等待。" checked={get('scanAutoRecentOnly') === true || get('scanAutoRecentOnly') === 'true'} onChange={e => update('scanAutoRecentOnly', e.target.checked)} />
          </AuthSection>
          <AuthSection title="排除媒体库" icon={Library} description="勾选后，扫描将跳过这些媒体库。">
            <button type="button" onClick={loadLibraries} disabled={loadingLibraries || !embyReady} className="btn-outline !min-h-9 !py-1.5"><RefreshCw size={14} className={loadingLibraries ? 'animate-spin' : ''} />{loadingLibraries ? '读取中…' : '刷新媒体库'}</button>
            {libraries.length ? <div className="grid gap-2 sm:grid-cols-2">{libraries.map(library => <label key={library.id} className="flex items-center gap-3 rounded-xl border border-gray-200 p-3 text-sm"><input type="checkbox" checked={excludedLibraries.includes(library.id) || excludedLibraries.includes(library.name)} onChange={() => toggleLibrary(library.id)} className="h-4 w-4 accent-primary-600" /><span className="min-w-0 flex-1 truncate">{library.name}</span><span className="text-xs text-gray-400">{library.seriesCount || 0} 部剧</span></label>)}</div> : <p className="rounded-xl bg-gray-50 px-4 py-5 text-sm text-gray-500">{embyReady ? '暂无媒体库，点击刷新重新读取。' : '连接并保存 Emby 后，就可以选择媒体库。'}</p>}
          </AuthSection>
        </div>
        <div className="settings-actions"><p className={`text-xs ${dirty ? 'text-amber-700' : 'text-gray-400'}`}>{dirty ? '有未保存的修改' : '修改后，点击保存即可生效'}</p><button type="submit" disabled={saving || !dirty} className="btn-primary shrink-0"><Save size={16} />{saving ? '保存中…' : '保存设置'}</button></div>
      </form>
      {section === 'security' && <form onSubmit={e => { e.preventDefault(); changePassword(); }} className="max-w-xl"><AuthSection title="账号安全" icon={Shield} description="使用独立的密码保护你的缺集工作台。"><Input name="oldPassword" label="当前密码" value={form.oldPassword || ''} onChange={e => update('oldPassword', e.target.value)} type="password" placeholder="输入当前密码" /><Input name="newPassword" label="新密码" value={form.newPassword || ''} onChange={e => update('newPassword', e.target.value)} type="password" placeholder="输入新的登录密码" /><button type="submit" className="btn-primary"><KeyRound size={16} />修改密码</button></AuthSection></form>}
    </div>
  );
}
