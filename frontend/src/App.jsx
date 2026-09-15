import { Routes, Route, Navigate } from 'react-router-dom';
import useStore from './store';
import { useEffect } from 'react';
import { api } from './api';
import { createJobPoller } from './job-poller';
import Layout from './components/Layout';
import Login from './pages/Login';
import Home from './pages/Home';
import Missing from './pages/Missing';
import Settings from './pages/Settings';

function JobPoller() {
  useEffect(() => {
    let stopped = false;
    let timer;
    const controller = new AbortController();
    const request = path => api(path, { signal: controller.signal });
    const poller = createJobPoller({ request, getState: useStore.getState });
    const poll = async () => {
      try {
        if (document.hidden) return;
        await poller.poll();
      } finally {
        if (!stopped) timer = setTimeout(poll, 2000);
      }
    };
    poll();
    return () => { stopped = true; poller.stop(); clearTimeout(timer); controller.abort(); };
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
