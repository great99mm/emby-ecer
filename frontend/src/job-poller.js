export function createJobPoller({ request, getState, now = Date.now }) {
  let lastFull = { id: null, at: 0, terminal: false };
  const loading = new Set();
  let stopped = false;
  const terminal = job => ['done', 'error'].includes(job.status);

  const refreshResult = async job => {
    loading.add(job.id);
    try {
      const full = await request(`/api/jobs/${job.id}`);
      if (stopped || getState().scanStarting || getState().activeJobId !== job.id) return;
      if (full.result?.scan && (full.result.scan.summary?.scanMode !== 'single' || full.status === 'done')) {
        getState().setScan(full.result.scan);
      }
      lastFull = { id: job.id, at: now(), terminal: terminal(full) };
      // An older full response must not move a newer progress update backwards.
      if (terminal(full)) {
        getState().setJobStatus({ ...full, result: undefined });
        getState().setActiveJobId(null);
      }
    } catch {
      // Retry the result independently; progress keeps updating in the meantime.
    } finally { loading.delete(job.id); }
  };

  return {
    stop() { stopped = true; },
    async poll() {
      if (stopped || getState().scanStarting) return;
      const requestedId = getState().activeJobId;
      try {
        const data = await request('/api/jobs/active?summary=1');
        let job = data.job;
        if (!job && requestedId) job = await request(`/api/jobs/${requestedId}?summary=1`);
        if (stopped || !job || getState().scanStarting) return;
        const current = getState();
        if (current.activeJobId !== requestedId && current.activeJobId !== job.id) return;
        current.setJobStatus(job);
        current.setActiveJobId(job.id);
        const done = terminal(job);
        if (!loading.has(job.id) && (!job.targeted || done) && (
          lastFull.id !== job.id || (done && !lastFull.terminal) || (!done && now() - lastFull.at >= 30000)
        )) {
          void refreshResult(job);
        } else if (done && lastFull.id === job.id && lastFull.terminal) {
          current.setActiveJobId(null);
        }
      } catch (err) {
        if (!stopped && err?.status === 404 && getState().activeJobId === requestedId) getState().setActiveJobId(null);
      }
    },
  };
}
