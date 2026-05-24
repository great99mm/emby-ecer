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
  const setActiveJobId = useStore(s => s.setActiveJobId);
  const setJobStatus = useStore(s => s.setJobStatus);
  const setScan = useStore(s => s.setScan);
  const applySearchResults = useStore(s => s.applySearchResults);
  const clearJob = useStore(s => s.clearJob);
  const intervalRef = useRef(null);
  const activeJobIdRef = useRef(activeJobId);

  useEffect(() => {
    activeJobIdRef.current = activeJobId;
  }, [activeJobId]);

  const applyJob = (j) => {
    if (!j) return;
    setJobStatus(j);
    if (j.id && activeJobIdRef.current !== j.id) {
      setActiveJobId(j.id);
    }
    if (j.result?.scan) {
      setScan(j.result.scan);
    }
    if (j.status === 'done') {
      if (j.result?.searched) applySearchResults(j.result.searched);
      // 不自动清除，让用户手动关闭
    } else if (j.status === 'error') {
      // 不自动清除，让用户手动关闭
    }
  };

  const recoverActiveJob = async () => {
    const data = await api('/api/jobs/active');
    if (data.job?.id) {
      applyJob(data.job);
      return true;
    }
    return false;
  };

  useEffect(() => {
    const poll = async () => {
      try {
        const recovered = await recoverActiveJob();
        if (recovered) {
          return;
        }
        const currentJobId = activeJobIdRef.current;
        if (!currentJobId) {
          return;
        }
        const j = await api(`/api/jobs/${currentJobId}`);
        applyJob(j);
      } catch (err) {
        // 以服务端活跃任务为准；瞬时失败时保留当前展示，避免任务条误消失。
        try {
          const recovered = await recoverActiveJob();
          if (!recovered && err?.status === 404 && activeJobIdRef.current) {
            clearJob();
          }
        } catch {
          // 网络/认证瞬时失败时保留当前任务状态，下次轮询继续恢复。
        }
      }
    };
    poll();
    intervalRef.current = setInterval(poll, 2000);
    return () => clearInterval(intervalRef.current);
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
