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
    <div className="min-h-screen bg-gray-50 flex">
      {/* Desktop Sidebar */}
      <aside className="hidden md:flex md:flex-col md:w-56 md:shrink-0 bg-white border-r border-gray-200">
        <div className="h-14 flex items-center gap-2 px-4 border-b border-gray-200">
          <Clapperboard className="w-6 h-6 text-primary-600" />
          <span className="text-base font-extrabold text-gray-900">Emby 补片</span>
        </div>
        <nav className="flex-1 px-3 py-4 space-y-1">
          {tabs.map(({ path, label, icon: Icon }) => {
            const active = location.pathname === path;
            return (
              <button
                key={path}
                onClick={() => navigate(path)}
                className={`w-full flex items-center gap-3 px-3 py-2.5 rounded-xl text-sm font-semibold transition-colors ${
                  active
                    ? 'bg-primary-50 text-primary-700'
                    : 'text-gray-500 hover:text-gray-700 hover:bg-gray-100'
                }`}
              >
                <Icon className="w-5 h-5" />
                {label}
              </button>
            );
          })}
        </nav>
        <div className="px-3 py-4 border-t border-gray-200">
          <button
            onClick={() => { logout(); navigate('/'); }}
            className="w-full flex items-center gap-3 px-3 py-2.5 rounded-xl text-sm font-semibold text-gray-500 hover:text-red-600 hover:bg-red-50 transition-colors"
          >
            <LogOut className="w-5 h-5" />
            退出登录
          </button>
        </div>
      </aside>

      {/* Main area */}
      <div className="flex-1 flex flex-col min-w-0">
        {/* Mobile header */}
        <header className="sticky top-0 z-30 bg-white/80 backdrop-blur border-b border-gray-200 md:hidden">
          <div className="h-14 flex items-center justify-between px-4">
            <div className="flex items-center gap-2">
              <Clapperboard className="w-5 h-5 text-primary-600" />
              <h1 className="text-base font-extrabold text-gray-900">Emby 补片</h1>
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
        <main className="flex-1 max-w-4xl w-full mx-auto px-4 py-6 pb-24 md:pb-6">
          {children}
        </main>
      </div>

      {/* Mobile bottom nav */}
      <nav className="fixed bottom-0 left-0 right-0 z-30 bg-white border-t border-gray-200 md:hidden">
        <div className="flex justify-around py-2">
          {tabs.map(({ path, label, icon: Icon }) => {
            const active = location.pathname === path;
            return (
              <button
                key={path}
                onClick={() => navigate(path)}
                className={`flex flex-col items-center gap-0.5 px-4 py-1 rounded-xl transition-colors ${
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
