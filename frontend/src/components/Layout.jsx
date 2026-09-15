import { useEffect, useLayoutEffect } from 'react';
import { NavLink, useNavigate, useLocation } from 'react-router-dom';
import useStore from '../store';
import ConnectionBadge from './ConnectionBadge';
import { LogOut, LayoutDashboard, Library, SlidersHorizontal, Radar, ArrowUpRight } from 'lucide-react';

const tabs = [
  { path: '/', label: '工作台', icon: LayoutDashboard },
  { path: '/missing', label: '缺集列表', icon: Library },
  { path: '/settings', label: '连接设置', icon: SlidersHorizontal },
];

export default function Layout({ children }) {
  const navigate = useNavigate();
  const { pathname } = useLocation();
  useLayoutEffect(() => { window.scrollTo(0, 0); }, [pathname]);
  const logout = useStore(s => s.logout);
  const settings = useStore(s => s.settings);
  const connectionStatus = useStore(s => s.connectionStatus);
  const checkConnections = useStore(s => s.checkConnections);
  useEffect(() => {
    if (!settings.ready) return;
    const refresh = () => { if (!document.hidden) checkConnections(); };
    refresh();
    const interval = setInterval(refresh, 60000);
    document.addEventListener('visibilitychange', refresh);
    window.addEventListener('focus', refresh);
    return () => {
      clearInterval(interval);
      document.removeEventListener('visibilitychange', refresh);
      window.removeEventListener('focus', refresh);
    };
  }, [settings, pathname, checkConnections]);
  const count = useStore(s => s.missing.length);
  const exit = () => { logout(); navigate('/'); };
  return (
    <div className="app-shell">
      <aside className="app-sidebar">
        <NavLink to="/" className="app-brand" aria-label="Emby Ecer 工作台">
          <span className="brand-symbol"><Radar size={22} strokeWidth={1.6} /></span>
          <span><span className="block text-lg font-semibold tracking-tight">Emby Ecer</span><span className="mt-0.5 block text-[11px] text-gray-400">专注缺集，轻松补齐</span></span>
        </NavLink>
        <nav className="app-nav" aria-label="主导航">
          {tabs.map(({ path, label, icon: Icon }) => <NavLink key={path} to={path} end={path === '/'}><Icon size={19} strokeWidth={1.65} />{label}{path === '/missing' && count > 0 && <span className="nav-count">{count > 99 ? '99+' : count}</span>}</NavLink>)}
        </nav>
        <div className="mt-auto px-2 pt-10">
          <div className="rounded-xl border border-gray-200/80 bg-white p-4">
            <div className="mb-3 flex items-center justify-between"><p className="text-xs font-medium text-gray-500">服务连接</p><NavLink to="/settings" aria-label="管理连接" className="text-gray-400 hover:text-primary-600"><ArrowUpRight size={15} /></NavLink></div>
            {[['emby','Emby'],['tmdb','TMDB'],['mp','MoviePilot']].map(([key,name]) => <div key={key} className="mt-2 flex items-center justify-between gap-2 text-xs"><span className="text-gray-600">{name}</span><ConnectionBadge connection={connectionStatus[key]} compact /></div>)}
          </div>
          <button onClick={exit} className="btn-ghost mt-4 w-full justify-start"><LogOut size={16} />退出登录</button>
        </div>
      </aside>
      <header className="mobile-header">
        <NavLink to="/" className="flex items-center gap-2.5"><span className="brand-symbol !h-9 !w-9"><Radar size={21} /></span><span className="text-base font-semibold tracking-tight">Emby Ecer</span></NavLink>
        <button onClick={exit} className="icon-button" aria-label="退出登录"><LogOut size={18} /></button>
      </header>
      <main className="app-main"><div className="app-content">{children}</div></main>
      <nav className="mobile-nav" aria-label="底部导航">
        {tabs.map(({ path, label, icon: Icon }) => <NavLink key={path} to={path} end={path === '/'}><Icon size={20} strokeWidth={1.65} /><span>{label}</span></NavLink>)}
      </nav>
    </div>
  );
}
