import { Routes, Route, Navigate } from 'react-router-dom';
import useStore from './store';
import { useEffect, useRef } from 'react';
import { api } from './api';
import Layout from './components/Layout';
import Login from './pages/Login';
import Home from './pages/Home';
import Missing from './pages/Missing';
import Settings from './pages/Settings';

function JobPoller() {
  const activeJobId = useStore(s => s.activeJobId);
  const setJobStatus = useStore(s => s.setJobStatus);
  const setScan = useStore(s => s.setScan);
  const applySearchResults = useStore(s => s.applySearchResults);
  const clearJob = useStore(s => s.clearJob);
  const intervalRef = useRef(null);

  useEffect(() => {
    if (!activeJobId) return;
    const poll = async () => {
      try {
        const j = await api(`/api/jobs/${activeJobId}`);
        setJobStatus(j);
        if (j.status === 'done' && j.result) {
          if (j.result.scan) setScan(j.result.scan);
          if (j.result.searched) applySearchResults(j.result.searched);
          clearJob();
        } else if (j.status === 'error') {
          clearJob();
        }
      } catch {}
    };
    poll();
    intervalRef.current = setInterval(poll, 2000);
    return () => clearInterval(intervalRef.current);
  }, [activeJobId]);

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
