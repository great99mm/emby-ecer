import { Routes, Route, Navigate } from 'react-router-dom';
import useStore from './store';
import { useEffect } from 'react';
import { api } from './api';
import Layout from './components/Layout';
import Login from './pages/Login';
import Home from './pages/Home';
import Missing from './pages/Missing';
import Settings from './pages/Settings';

function JobPoller() {
  useEffect(() => {
    let stopped = false;
    let timer;
    let lastFull = { id: null, at: 0, terminal: false };
    const controller = new AbortController();
    const request = path => api(path, { signal: controller.signal });
    const poll = async () => {
      try {
        if (document.hidden) return;
        const requestedId = useStore.getState().activeJobId;
        const data = await request('/api/jobs/active?summary=1');
        let j = data.job;
        if (!j && requestedId) j = await request(`/api/jobs/${requestedId}?summary=1`);
        if (stopped || !j) return;
        const current = useStore.getState();
        if (current.activeJobId !== requestedId && current.activeJobId !== j.id) return;
        const terminal = ['done', 'error'].includes(j.status);
        current.setJobStatus(j);
        current.setActiveJobId(j.id);
        // Progress is tiny; refresh the episode list every 30 seconds and on
        // completion. Scheduling after each response prevents overlapping calls.
        if (lastFull.id !== j.id || (terminal && !lastFull.terminal) || (!terminal && Date.now() - lastFull.at >= 30000)) {
          const full = await request(`/api/jobs/${j.id}`);
          if (stopped || useStore.getState().activeJobId !== j.id) return;
          if (full.result?.scan && (full.result.scan.summary?.scanMode !== 'single' || full.status === 'done')) useStore.getState().setScan(full.result.scan);
          useStore.getState().setJobStatus({ ...full, result: undefined });
          lastFull = { id: j.id, at: Date.now(), terminal: ['done', 'error'].includes(full.status) };
          if (lastFull.terminal) useStore.getState().setActiveJobId(null);
        } else if (terminal) {
          current.setActiveJobId(null);
        }
      } catch (err) {
        // Keep the latest result through temporary network failures.
        if (!stopped && err?.status === 404) useStore.getState().setActiveJobId(null);
      } finally {
        if (!stopped) timer = setTimeout(poll, 2000);
      }
    };
    poll();
    return () => { stopped = true; clearTimeout(timer); controller.abort(); };
  }, []);

  return null;
}

export default function App() {
  const token = useStore(s => s.token);
  const init = useStore(s => s.init);

  useEffect(() => { init(); }, []);

  if (!token) return <Login />;

  return (
    <Layout>
      <JobPoller />
      <Routes>
        <Route path="/" element={<Home />} />
        <Route path="/missing" element={<Missing />} />
        <Route path="/settings" element={<Settings />} />
        <Route path="*" element={<Navigate to="/" replace />} />
      </Routes>
    </Layout>
  );
}
