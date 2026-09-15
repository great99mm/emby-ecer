import test from 'node:test';
import assert from 'node:assert/strict';
import { createJobPoller } from './job-poller.js';
import { decodeScanResponse } from './scan-response.js';

const flush = () => new Promise(resolve => setImmediate(resolve));
const deferred = () => { let resolve; const promise = new Promise(r => { resolve = r; }); return { promise, resolve }; };
function state() {
  return { activeJobId: 'a', jobStatus: null, scan: null, setJobStatus(job) { this.jobStatus = job; }, setActiveJobId(id) { this.activeJobId = id; }, setScan(scan) { this.scan = scan; } };
}

test('compact payload preserves all series and episode identities for MP and ignores', () => {
  const result = decodeScanResponse({ result: { scan: { summary: { scanMode: 'single' }, missingGroups: [
    { series: { embySeriesId: 'a', tmdbId: 42, ownedEpisodes: 8 }, episodes: [{ season: 1, episode: 9, code: 'S01E09' }, { season: 2, episode: 1, code: 'S02E01' }] },
    { series: { embySeriesId: 'b', tmdbId: 99 }, episodes: [{ season: 1, episode: 1, code: 'S01E01' }] },
  ] } } });
  assert.equal(result.result.scan.missing.length, 3);
  assert.deepEqual(result.result.scan.missing[1], { embySeriesId: 'a', tmdbId: 42, ownedEpisodes: 8, season: 2, episode: 1, code: 'S02E01' });
  assert.equal(result.result.scan.missing[2].embySeriesId, 'b');
  assert.equal(result.result.scan.summary.scanMode, 'single');
  assert.equal(result.result.scan.missingGroups, undefined);
});

test('slow full results do not block progress polling or regress newer progress', async () => {
  const store = state(), full = deferred(); let progress = 10, fullCalls = 0;
  const poller = createJobPoller({ getState: () => store, request: path => path.includes('summary=1') ? Promise.resolve({ job: { id: 'a', status: 'running', progress } }) : (fullCalls++, full.promise) });
  await poller.poll(); progress = 65; await poller.poll();
  assert.equal(store.jobStatus.progress, 65);
  assert.equal(fullCalls, 1);
  full.resolve({ id: 'a', status: 'running', progress: 10, result: { scan: { missing: [] } } });
  await flush();
  assert.equal(store.jobStatus.progress, 65);
  assert.deepEqual(store.scan.missing, []);
});

test('single scans skip partial lists, fetch completion, and ignore stale downloads', async () => {
  const store = state(), full = deferred(); let status = 'running', fullCalls = 0;
  const poller = createJobPoller({ getState: () => store, request: path => path.includes('summary=1') ? Promise.resolve({ job: { id: 'a', status, targeted: true } }) : (fullCalls++, full.promise) });
  await poller.poll(); assert.equal(fullCalls, 0);
  status = 'done'; await poller.poll(); assert.equal(fullCalls, 1);
  store.activeJobId = 'b';
  full.resolve({ id: 'a', status: 'done', result: { scan: { missing: ['old'] } } });
  await flush();
  assert.equal(store.scan, null);
  assert.equal(store.activeJobId, 'b');
});

test('failed result download is retried and successful completion clears active job', async () => {
  const store = state(); let attempts = 0;
  const poller = createJobPoller({ getState: () => store, request: async path => {
    if (path.includes('summary=1')) return { job: { id: 'a', status: 'done' } };
    if (++attempts === 1) throw new Error('network');
    return { id: 'a', status: 'done', result: { scan: { missing: [] } } };
  } });
  await poller.poll(); await flush();
  assert.equal(store.activeJobId, 'a');
  await poller.poll(); await flush();
  assert.equal(attempts, 2);
  assert.equal(store.activeJobId, null);
  assert.deepEqual(store.scan.missing, []);
});

test('an obsolete slow download does not block a newer job result', async () => {
  const store = state(), old = deferred();
  const poller = createJobPoller({ getState: () => store, request: async path => {
    if (path.includes('summary=1')) return { job: { id: store.activeJobId, status: 'done' } };
    if (path === '/api/jobs/a') return old.promise;
    return { id: 'b', status: 'done', result: { scan: { missing: ['new'] } } };
  } });
  await poller.poll(); store.activeJobId = 'b'; await poller.poll(); await flush();
  assert.deepEqual(store.scan.missing, ['new']);
  old.resolve({ id: 'a', status: 'done', result: { scan: { missing: ['old'] } } });
  await flush();
  assert.deepEqual(store.scan.missing, ['new']);
});
