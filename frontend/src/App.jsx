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

  const applyJob = (j) => {
    setJobStatus(j);
    if (j.result?.scan) {
      setScan(j.result.scan);
    }
    if (j.status === 'done') {
      if (j.result?.searched) applySearchResults(j.result.searched);
      clearJob();
    } else if (j.status === 'error') {
      clearJob();
    }
  };

  const recoverActiveJob = async () => {
    const data = await api('/api/jobs/active');
    if (data.job?.id) {
      setActiveJobId(data.job.id);
      applyJob(data.job);
      return true;
    }
    return false;
  };

  useEffect(() => {
    const poll = async () => {
      try {
        if (!activeJobId) {
          await recoverActiveJob();
          return;
        }
        const j = await api(`/api/jobs/${activeJobId}`);
        applyJob(j);
      } catch (err) {
        // 只有确认服务端没有活跃任务时才清理本地任务，避免刷新或跨设备轮询抖动导致任务条消失。
        try {
          const recovered = await recoverActiveJob();
          if (!recovered && err?.status === 404) {
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
