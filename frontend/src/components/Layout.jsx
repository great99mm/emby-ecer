import { useNavigate, useLocation } from 'react-router-dom';
import useStore from '../store';
import { LogOut, Home, AlertTriangle, SlidersHorizontal, Clapperboard } from 'lucide-react';

const tabs = [
  { path: '/', label: '首页', icon: Home },
  { path: '/missing', label: '缺集', icon: AlertTriangle },
  { path: '/settings', label: '授权', icon: SlidersHorizontal },
];

export default function Layout({ children }) {
  const navigate = useNavigate();
  const location = useLocation();
  const logout = useStore(s => s.logout);

  return (
    <div className="min-h-screen bg-[#f5f7fb] flex text-gray-900">
      {/* Desktop Sidebar */}
      <aside className="hidden md:flex md:flex-col md:w-64 md:shrink-0 bg-white/90 backdrop-blur border-r border-gray-200 shadow-sm">
        <div className="px-5 py-5 border-b border-gray-100">
          <div className="flex items-center gap-3">
            <div className="flex h-11 w-11 items-center justify-center rounded-lg bg-primary-600 text-white shadow-sm shadow-primary-200">
              <Clapperboard className="w-5 h-5" />
            </div>
            <div>
              <p className="text-[11px] font-bold uppercase tracking-[0.2em] text-primary-600">Missing Radar</p>
              <span className="block text-lg font-extrabold leading-none text-gray-900">Emby Ecer</span>
            </div>
          </div>
        </div>
        <nav className="flex-1 px-4 py-5 space-y-1.5">
          {tabs.map(({ path, label, icon: Icon }) => {
            const active = location.pathname === path;
            return (
              <button
                key={path}
                onClick={() => navigate(path)}
                className={`w-full flex items-center gap-3 px-4 py-3 rounded-lg text-sm font-semibold transition-all ${
                  active
                    ? 'bg-primary-600 text-white shadow-sm shadow-primary-200'
                    : 'text-gray-500 hover:text-gray-800 hover:bg-gray-100'
                }`}
              >
                <Icon className={`w-5 h-5 ${active ? 'text-white' : 'text-gray-400'}`} />
                {label}
              </button>
            );
          })}
        </nav>
        <div className="px-4 py-5 border-t border-gray-100">
          <div className="mb-3 rounded-lg bg-gray-50 px-4 py-3 text-xs text-gray-500">
            <div className="font-bold text-gray-700">系统状态</div>
            <div className="mt-1">媒体库扫描、盘搜与 MP 搜索统一管理</div>
          </div>
          <button
            onClick={() => { logout(); navigate('/'); }}
            className="w-full flex items-center gap-3 px-4 py-3 rounded-lg text-sm font-semibold text-gray-500 hover:text-red-600 hover:bg-red-50 transition-colors"
          >
            <LogOut className="w-5 h-5" />
            退出登录
          </button>
        </div>
      </aside>

      {/* Main area */}
      <div className="flex-1 flex flex-col min-w-0">
        {/* Mobile header */}
        <header className="sticky top-0 z-30 bg-white/85 backdrop-blur border-b border-gray-200 md:hidden">
          <div className="h-14 flex items-center justify-between px-4">
            <div className="flex items-center gap-2">
              <Clapperboard className="w-5 h-5 text-primary-600" />
              <h1 className="text-base font-extrabold text-gray-900">Emby Ecer</h1>
            </div>
            <button
              onClick={() => { logout(); navigate('/'); }}
              className="p-2 rounded-xl hover:bg-gray-100 transition-colors"
            >
              <LogOut className="w-5 h-5 text-gray-500" />
            </button>
          </div>
        </header>

        {/* Content */}
        <main className="flex-1 w-full max-w-[1240px] mx-auto px-4 md:px-8 py-6 pb-24 md:pb-8">
          {children}
        </main>
      </div>

      {/* Mobile bottom nav */}
      <nav className="fixed bottom-0 left-0 right-0 z-30 bg-white/95 backdrop-blur border-t border-gray-200 md:hidden">
        <div className="flex justify-around px-2 py-2">
          {tabs.map(({ path, label, icon: Icon }) => {
            const active = location.pathname === path;
            return (
              <button
                key={path}
                onClick={() => navigate(path)}
                className={`flex min-w-0 flex-1 flex-col items-center gap-0.5 px-2 py-2 rounded-md transition-colors ${
                  active ? 'text-primary-600 bg-primary-50' : 'text-gray-500 hover:text-gray-700'
                }`}
              >
                <Icon className="w-5 h-5" />
                <span className="text-xs font-semibold">{label}</span>
              </button>
            );
          })}
        </div>
      </nav>
    </div>
  );
}
