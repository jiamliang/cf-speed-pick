// SPDX-License-Identifier: GPL-3.0-or-later
//
// /sub 端点集成测试 — 启动本地 HTTP server 模拟 VPS cache，
// 然后用 Node fetch 调用 /sub 逻辑验证完整流程。
//
// 但 _worker.js 本身跑在 CF 沙箱，不能在 Node 直接跑。
// 所以这里做两件事：
//   1. 起一个本地 HTTP server 提供 /cache/colo/*.csv
//   2. 调用 worker 的实际 URL（CF 边缘）测 /sub 端到端
//
// 注意：Worker /sub 需要 UPSTREAM_CACHE_BASE 配置成这个 server 的 URL。
// 因为 Worker 默认 UPSTREAM_CACHE_BASE="" → 走静态模式，不会拉到我们的 mock。
//
// 这个测试在 CI 里是 manual step（需要 Worker 已部署 + UPSTREAM_CACHE_BASE 配好）。

const http = require('http');
const assert = require('assert');

const WORKER_URL = process.env.WORKER_URL || 'https://cf-speed-proxy.jiam-liang.workers.dev';

// 假 CSV 内容（模拟 cf-speed-pick 输出）
const FAKE_CACHE = {
  'NRT.csv': `ip,download_speed_MBps,colo,tier
172.64.229.1,12.5,NRT,1
172.64.229.2,11.8,NRT,1`,
  'SIN.csv': `ip,download_speed_MBps,colo,tier
162.158.1.1,8.5,SIN,2`,
  'LAX.csv': `ip,download_speed_MBps,colo,tier
104.16.1.1,6.8,LAX,3`,
};

function startMockCache(port) {
  return new Promise((resolve) => {
    const server = http.createServer((req, res) => {
      const path = req.url;
      console.log(`  [mock cache] GET ${path}`);
      if (path === '/cache/colo/NRT.csv') {
        res.writeHead(200, { 'Content-Type': 'text/csv' });
        res.end(FAKE_CACHE['NRT.csv']);
      } else if (path === '/cache/colo/SIN.csv') {
        res.writeHead(200, { 'Content-Type': 'text/csv' });
        res.end(FAKE_CACHE['SIN.csv']);
      } else if (path === '/cache/colo/LAX.csv') {
        res.writeHead(200, { 'Content-Type': 'text/csv' });
        res.end(FAKE_CACHE['LAX.csv']);
      } else {
        res.writeHead(404);
        res.end('not found');
      }
    });
    server.listen(port, '127.0.0.1', () => resolve(server));
  });
}

async function test() {
  console.log('=== /sub integration test ===');
  console.log(`Worker URL: ${WORKER_URL}`);
  console.log('');

  // 1. /health
  console.log('1. GET /health');
  const h = await fetch(`${WORKER_URL}/health`);
  assert.strictEqual(h.status, 200, 'health 200');
  const hj = await h.json();
  console.log('   ', JSON.stringify(hj));
  assert(hj.ok, 'health ok');
  assert(hj.uuid_prefix.length === 8, 'uuid prefix 8 chars');

  // 2. /sub (静态模式 — UPSTREAM_CACHE_BASE 没配)
  console.log('');
  console.log('2. GET /sub (static mode)');
  const s = await fetch(`${WORKER_URL}/sub`);
  assert.strictEqual(s.status, 200, 'sub 200');
  const stext = await s.text();
  console.log('   ', stext.split('\n')[0].slice(0, 80) + '...');
  const staticLines = stext.trim().split('\n');
  assert(staticLines.length >= 1, 'has at least 1 node');
  assert(staticLines.every(l => l.startsWith('vless://')), 'all vless://');
  assert(staticLines.every(l => l.includes(hj.uuid_prefix)), 'all use correct UUID');

  // 3. /sub with params (静态模式也支持参数，但用的是静态节点)
  console.log('');
  console.log('3. GET /sub?colos=NRT&top=2 (params, static mode)');
  const s2 = await fetch(`${WORKER_URL}/sub?colos=NRT&top=2`);
  const s2text = await s2.text();
  console.log('   ', s2text.trim().split('\n').length, 'nodes');

  // 4. /  should 404
  console.log('');
  console.log('4. GET / (should 404)');
  const r = await fetch(`${WORKER_URL}/`);
  assert.strictEqual(r.status, 404, 'root 404');

  console.log('');
  console.log('🎉 all integration tests passed');
}

test().catch((e) => {
  console.error('❌ test failed:', e.message);
  console.error(e.stack);
  process.exit(1);
});
