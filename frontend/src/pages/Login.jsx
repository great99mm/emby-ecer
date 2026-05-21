import { useState } from 'react';
import { useNavigate } from 'react-router-dom';
import useStore from '../store';
import { api } from '../api';
import toast from 'react-hot-toast';
import { LogIn, Clapperboard } from 'lucide-react';

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
        const saved = await api('/api/search-results');
        if (Array.isArray(saved)) useStore.getState().applySearchResults(saved);
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
    <div className="min-h-screen bg-gray-50 flex items-center justify-center p-4">
      <div className="w-full max-w-sm">
        <div className="text-center mb-8">
          <div className="inline-flex items-center justify-center w-16 h-16 rounded-2xl bg-primary-600 text-white mb-4">
            <Clapperboard className="w-8 h-8" />
          </div>
          <h1 className="text-2xl font-extrabold text-gray-900">Emby 补片助手</h1>
          <p className="mt-1 text-sm text-gray-500">115 缺集转存管理</p>
        </div>
        <form onSubmit={handleSubmit} className="card space-y-4">
          <div>
            <label className="block text-sm font-semibold text-gray-700 mb-1.5">用户名</label>
            <input
              type="text"
              value={username}
              onChange={e => setUsername(e.target.value)}
              placeholder="请输入用户名"
              autoFocus
              className="w-full rounded-xl border border-gray-300 bg-gray-50 px-4 py-2.5 text-sm font-medium placeholder:text-gray-400 focus:outline-none focus:ring-2 focus:ring-primary-500 focus:border-transparent"
            />
          </div>
          <div>
            <label className="block text-sm font-semibold text-gray-700 mb-1.5">密码</label>
            <input
              type="password"
              value={password}
              onChange={e => setPassword(e.target.value)}
              placeholder="请输入密码"
              className="w-full rounded-xl border border-gray-300 bg-gray-50 px-4 py-2.5 text-sm font-medium placeholder:text-gray-400 focus:outline-none focus:ring-2 focus:ring-primary-500 focus:border-transparent"
            />
          </div>
          <button
            type="submit"
            disabled={loading}
            className="btn-primary w-full flex items-center justify-center gap-2"
          >
            <LogIn className="w-4 h-4" />
            {loading ? '登录中...' : '登录'}
          </button>
        </form>
      </div>
    </div>
  );
}
