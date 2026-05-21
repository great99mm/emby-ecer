import { useState } from 'react';
import useStore from '../store';
import { api } from '../api';
import toast from 'react-hot-toast';
import { Save, Shield, Server, BadgeCheck, Search, CloudUpload, KeyRound, Download } from 'lucide-react';

const sectionIcons = { Emby: Server, TMDB: BadgeCheck, PanSou: Search, '115 转存': CloudUpload, MoviePilot: Download, '账号安全': Shield };

function AuthSection({ title, icon, children, ready, onTest, target }) {
  const Icon = icon;
  return (
    <div className="card">
      <div className="flex items-center justify-between mb-4">
        <div className="flex items-center gap-2">
          <Icon className="w-5 h-5 text-primary-600" />
          <div>
            <h2 className="font-bold text-gray-900">{title}</h2>
            <p className={`text-xs font-semibold ${ready ? 'text-emerald-600' : 'text-gray-400'}`}>
              {ready ? '已配置' : '待配置'}
            </p>
          </div>
        </div>
        {target && (
          <button type="button" onClick={() => onTest(target)} className="btn-outline text-xs px-3 py-1.5">
            测试
          </button>
        )}
      </div>
      <div className="space-y-3">{children}</div>
    </div>
  );
}

function Input({ name, label, value, onChange, type = 'text', placeholder = '' }) {
  return (
    <label className="block">
      <span className="block text-xs font-bold text-gray-600 mb-1">{label}</span>
      <input
        name={name}
        type={type}
        value={value}
        onChange={onChange}
        placeholder={placeholder}
        className="w-full rounded-xl border border-gray-300 bg-gray-50 px-4 py-2.5 text-sm font-medium placeholder:text-gray-400 focus:outline-none focus:ring-2 focus:ring-primary-500"
      />
    </label>
  );
}

function Textarea({ name, label, value, onChange, placeholder = '' }) {
  return (
    <label className="block">
      <span className="block text-xs font-bold text-gray-600 mb-1">{label}</span>
      <textarea
        name={name}
        rows={4}
        value={value}
        onChange={onChange}
        placeholder={placeholder}
        className="w-full rounded-xl border border-gray-300 bg-gray-50 px-4 py-2.5 text-sm font-medium placeholder:text-gray-400 focus:outline-none focus:ring-2 focus:ring-primary-500 resize-none"
      />
    </label>
  );
}

export default function Settings() {
  const settings = useStore(s => s.settings);
  const setSettings = useStore(s => s.setSettings);
  const [form, setForm] = useState({});
  const [saving, setSaving] = useState(false);

  const update = (name, value) => setForm(f => ({ ...f, [name]: value }));
  const get = (name) => form[name] !== undefined ? form[name] : (settings[name] || '');

  const testConnection = async (target) => {
    try {
      const data = await api('/api/settings/test', { method: 'POST', body: JSON.stringify({ target }) });
      const item = data[target];
      if (item?.ok) toast.success(`${target} 连接正常`);
      else toast.error(item?.error || `${target} 测试失败`);
    } catch (err) {
      toast.error(err.message);
    }
  };

  const handleSave = async (e) => {
    e.preventDefault();
    setSaving(true);
    try {
      const payload = {};
      for (const [k, v] of Object.entries(form)) {
        if (v !== undefined && v !== '') payload[k] = v;
      }
      const updated = await api('/api/settings', { method: 'POST', body: JSON.stringify(payload) });
      setSettings(updated);
      setForm({});
      toast.success('授权配置已保存');
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

  return (
    <form onSubmit={handleSave} className="space-y-4">
      <AuthSection title="Emby" icon={sectionIcons.Emby} ready={ready.emby} onTest={testConnection} target="emby">
        <Input name="embyUrl" label="Emby 地址" value={get('embyUrl')} onChange={e => update('embyUrl', e.target.value)} placeholder="http://你的Emby:8096" />
        <Input name="embyApiKey" label="Emby API Key" value={get('embyApiKey')} onChange={e => update('embyApiKey', e.target.value)} type="password" placeholder="留空表示不覆盖" />
        <Input name="embyUserId" label="Emby UserId (可选)" value={get('embyUserId')} onChange={e => update('embyUserId', e.target.value)} placeholder="不填则使用全局 /Items" />
      </AuthSection>

      <AuthSection title="TMDB" icon={sectionIcons.TMDB} ready={ready.tmdb} onTest={testConnection} target="tmdb">
        <Input name="tmdbApiKey" label="TMDB API Key" value={get('tmdbApiKey')} onChange={e => update('tmdbApiKey', e.target.value)} type="password" placeholder="留空表示不覆盖" />
      </AuthSection>

      <AuthSection title="PanSou" icon={sectionIcons.PanSou} ready={ready.pansou} onTest={testConnection} target="pansou">
        <Input name="pansouUrl" label="PanSou API 地址" value={get('pansouUrl')} onChange={e => update('pansouUrl', e.target.value)} placeholder="http://localhost:8888" />
        <Input name="pansouUsername" label="PanSou 用户名 (可选)" value={get('pansouUsername')} onChange={e => update('pansouUsername', e.target.value)} placeholder="未开启认证可不填" />
        <Input name="pansouPassword" label="PanSou 密码 (可选)" value={get('pansouPassword')} onChange={e => update('pansouPassword', e.target.value)} type="password" placeholder="留空表示不覆盖" />
        <Input name="pansouToken" label="PanSou Token (可选)" value={get('pansouToken')} onChange={e => update('pansouToken', e.target.value)} type="password" placeholder="有 token 可直接填" />
      </AuthSection>

      <AuthSection title="115 转存" icon={sectionIcons['115 转存']} ready={ready.p115} onTest={testConnection} target="p115">
        <Textarea name="p115Cookie" label="115 Cookie" value={get('p115Cookie')} onChange={e => update('p115Cookie', e.target.value)} placeholder="粘贴网页版 Cookie；留空表示不覆盖" />
        <Input name="p115TargetCid" label="目标目录 CID" value={get('p115TargetCid')} onChange={e => update('p115TargetCid', e.target.value)} placeholder="0" />
      </AuthSection>

      <AuthSection title="MoviePilot" icon={sectionIcons.MoviePilot} ready={ready.mp}>
        <Input name="mpUrl" label="MoviePilot 地址" value={get('mpUrl')} onChange={e => update('mpUrl', e.target.value)} placeholder="http://IP:3001" />
        <Input name="mpToken" label="API Token" value={get('mpToken')} onChange={e => update('mpToken', e.target.value)} type="password" placeholder="在 MP 设置 → API 获取" />
      </AuthSection>

      <AuthSection title="账号安全" icon={sectionIcons['账号安全']}>
        <Input name="oldPassword" label="当前密码" value={form.oldPassword || ''} onChange={e => update('oldPassword', e.target.value)} type="password" placeholder="输入当前密码" />
        <Input name="newPassword" label="新密码" value={form.newPassword || ''} onChange={e => update('newPassword', e.target.value)} type="password" placeholder="输入新密码" />
        <button type="button" onClick={changePassword} className="btn-outline w-full flex items-center justify-center gap-2">
          <KeyRound className="w-4 h-4" /> 修改密码
        </button>
      </AuthSection>

      <button type="submit" disabled={saving} className="btn-primary w-full flex items-center justify-center gap-2 sticky bottom-20 md:bottom-4">
        <Save className="w-4 h-4" />
        {saving ? '保存中...' : '保存授权配置'}
      </button>

      <p className="text-xs text-gray-400 text-center pb-4">密钥和 Cookie 保存在服务端。建议生产环境使用环境变量注入。</p>
    </form>
  );
}
