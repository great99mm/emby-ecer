import { useState } from 'react';
import { useNavigate } from 'react-router-dom';
import useStore from '../store';
import { api } from '../api';
import toast from 'react-hot-toast';
import { LogIn, Radar, ArrowRight, Loader2 } from 'lucide-react';
import RadarArt from '../components/RadarArt';

export default function Login() {
  const [username, setUsername] = useState('');
  const [password, setPassword] = useState('');
  const [loading, setLoading] = useState(false);
  const setToken = useStore(s => s.setToken);
  const setSettings = useStore(s => s.setSettings);
  const navigate = useNavigate();

  const handleSubmit = async (e) => {
    e.preventDefault();
    if (!username.trim() || !password) {
      toast.error('请输入用户名和密码');
      return;
    }
    setLoading(true);
    try {
      const data = await api('/api/auth/login', {
        method: 'POST',
        body: JSON.stringify({ username: username.trim(), password }),
      });
      setToken(data.token);
      try {
        const settings = await api('/api/settings');
        setSettings(settings);
        const scan = await api('/api/scan/last');
        if (scan?.scannedAt) useStore.getState().setScan(scan);
      } catch {}
      toast.success('登录成功');
      navigate('/');
    } catch (err) {
      toast.error(err.message);
    } finally {
      setLoading(false);
    }
  };

  return (
    <div className="login-shell">
      <aside className="login-story">
        <div className="flex items-center gap-3"><span className="brand-symbol"><Radar size={23} /></span><span className="text-base font-semibold">Emby 补片助手</span></div>
        <div className="relative py-16">
          <div className="mb-10 hidden lg:block"><RadarArt /></div>
          <p className="eyebrow mb-4">LESS MISSING. MORE WATCHING.</p>
          <h1 className="text-4xl font-semibold leading-snug tracking-tight text-primary-900">让好故事，<br />每一集都完整。</h1>
          <p className="mt-6 max-w-sm text-sm leading-8 text-primary-700/80">发现媒体库里的缺集，联动 MoviePilot 补齐。把时间留给值得看的故事。</p>
        </div>
        <p className="flex flex-wrap items-center gap-3 text-xs text-primary-700/70"><span>扫描缺集</span><ArrowRight size={13} /><span>查找资源</span><ArrowRight size={13} /><span>补齐媒体库</span></p>
      </aside>
      <main className="login-form">
        <div className="w-full max-w-[340px]">
          <div className="mb-10 flex items-center gap-3 md:hidden"><span className="brand-symbol"><Radar size={23} /></span><span className="font-semibold">Emby 补片助手</span></div>
          <p className="eyebrow mb-3">YOUR LIBRARY, COMPLETE</p>
          <h2 className="text-[30px] font-semibold tracking-tight">欢迎回来</h2>
          <p className="mb-8 mt-3 text-sm text-gray-500">登录你的缺集工作台，继续补齐好故事。</p>
          <form onSubmit={handleSubmit} className="space-y-5">
            <div>
              <label htmlFor="username" className="mb-2 block text-sm font-medium">用户名</label>
              <input id="username" name="username" autoComplete="username" type="text" value={username} onChange={e => setUsername(e.target.value)} placeholder="请输入用户名" autoFocus required className="field" />
            </div>
            <div>
              <label htmlFor="password" className="mb-2 block text-sm font-medium">密码</label>
              <input id="password" name="password" autoComplete="current-password" type="password" value={password} onChange={e => setPassword(e.target.value)} placeholder="请输入密码" required className="field" />
            </div>
            <button type="submit" disabled={loading} className="btn-primary !mt-7 w-full">{loading ? <Loader2 size={16} className="animate-spin" /> : <LogIn size={16} />}{loading ? '登录中…' : '进入工作台'}</button>
          </form>
          <p className="mt-8 text-center text-xs text-gray-400">专注缺集扫描 · 联动 MoviePilot</p>
        </div>
      </main>
    </div>
  );
}
