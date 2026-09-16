// SPDX-License-Identifier: GPL-3.0-or-later
//
// /sub 端点的核心逻辑提取 + 单元测试。
// 这份代码是 _worker.js 里 handleSubscription 的可测版本。
// Worker 实际跑在 CF 沙箱里，不能直接跑 Node 测试，所以抽出来单测。

// 模拟 _worker.js 里的 buildVlessUri
function buildVlessUri({ uuid, host, ip, port, fallback, colo, speed, note }) {
  const path = fallback ? `pyip%3D${encodeURIComponent(fallback)}` : '/';
  const params = [
    'encryption=none',
    'security=tls',
    `sni=${host}`,
    'fp=random',
    'alpn=http/1.1',
    'type=ws',
    `host=${host}`,
    `path=${path}`,
  ];
  const tag = note || (speed > 0 ? `${colo} ${speed.toFixed(2)}MB/s` : colo);
  return `vless://${uuid}@${ip}:${port}?${params.join('&')}#${encodeURIComponent(tag)}`;
}

// 模拟 _worker.js 里的 handleSubscription（核心 CSV 解析部分）
async function buildSubscriptionFromCsvs({ colos, top, minSpeed, uuid, host, fallback, fetchCsv }) {
  const fetches = colos.map(async (colo) => {
    try {
      const text = await fetchCsv(colo);
      const lines = text.split('\n').slice(1); // skip header
      const ips = [];
      for (const line of lines) {
        const parts = line.split(',');
        if (parts.length < 4) continue;
        const ip = parts[0].trim();
        const speed = parseFloat(parts[1]) || 0;
        const lineColo = parts[2].trim();
        if (minSpeed > 0 && speed < minSpeed) continue;
        ips.push({ ip, speed, colo: lineColo || colo });
        if (ips.length >= top) break;
      }
      return ips;
    } catch (_) {
      return [];
    }
  });
  const nested = await Promise.all(fetches);
  const ips = nested.flat();
  return ips.map(({ ip, speed, colo }) =>
    buildVlessUri({ uuid, host, ip, port: 443, fallback, colo, speed })
  );
}

// === 测试 ===

const assert = require('assert');

const UUID = 'a1b2c3d4-e5f6-7890-abcd-ef1234567890';
const HOST = 'cf-speed-proxy.example.com';

// 假 CSV 内容
const NRT_CSV = `ip,download_speed_MBps,colo,tier
172.64.229.1,12.5,NRT,1
172.64.229.2,11.8,NRT,1
172.64.229.3,10.3,NRT,1`;

const SIN_CSV = `ip,download_speed_MBps,colo,tier
162.158.1.1,8.5,SIN,2
162.158.1.2,7.2,SIN,2`;

const LAX_CSV = `ip,download_speed_MBps,colo,tier
104.16.1.1,6.8,LAX,3`;

async function test() {
  // Test 1: buildVlessUri 基本格式
  {
    const uri = buildVlessUri({
      uuid: UUID, host: HOST, ip: '1.2.3.4', port: 443,
      fallback: '', colo: 'NRT', speed: 10.5,
    });
    assert(uri.startsWith(`vless://${UUID}@1.2.3.4:443?`), 'URI prefix');
    assert(uri.includes('security=tls'), 'has TLS');
    assert(uri.includes(`sni=${HOST}`), 'has SNI');
    assert(uri.includes('type=ws'), 'has WS');
    assert(uri.includes(`host=${HOST}`), 'has WS host');
    assert(uri.includes('path=/'), 'has root path when no fallback');
    assert(uri.endsWith(encodeURIComponent('NRT 10.50MB/s')), 'has formatted tag');
    console.log('✅ test1: buildVlessUri basic');
  }

  // Test 2: buildVlessUri with fallback
  {
    const uri = buildVlessUri({
      uuid: UUID, host: HOST, ip: '1.2.3.4', port: 443,
      fallback: '5.6.7.8:443', colo: 'SIN', speed: 0,
    });
    assert(uri.includes('path=pyip%3D5.6.7.8%3A443'), 'path encoded fallback');
    assert(uri.endsWith(encodeURIComponent('SIN')), 'colo tag without speed');
    console.log('✅ test2: buildVlessUri with fallback');
  }

  // Test 3: 单 colo 拉取 + top 限制
  {
    const fetchCsv = async (colo) => {
      if (colo === 'NRT') return NRT_CSV;
      throw new Error('not found');
    };
    const nodes = await buildSubscriptionFromCsvs({
      colos: ['NRT'], top: 2, minSpeed: 0, uuid: UUID, host: HOST, fallback: '',
      fetchCsv,
    });
    assert.strictEqual(nodes.length, 2, 'top=2 gives 2 IPs');
    assert(nodes[0].includes('172.64.229.1'), 'first is fastest');
    assert(nodes[1].includes('172.64.229.2'), 'second is 2nd fastest');
    console.log('✅ test3: single colo with top limit');
  }

  // Test 4: 多 colo 合并（按优先级顺序）
  {
    const fetchCsv = async (colo) => {
      if (colo === 'NRT') return NRT_CSV;
      if (colo === 'SIN') return SIN_CSV;
      if (colo === 'LAX') return LAX_CSV;
      throw new Error('not found');
    };
    const nodes = await buildSubscriptionFromCsvs({
      colos: ['NRT', 'SIN', 'LAX'], top: 5, minSpeed: 0,
      uuid: UUID, host: HOST, fallback: '', fetchCsv,
    });
    assert.strictEqual(nodes.length, 6, 'all 6 IPs from 3 colos');
    // NRT should come first (priority), SIN second, LAX third
    assert(nodes[0].includes('172.64.229'), 'first NRT IP');
    assert(nodes[3].includes('162.158'), '4th is SIN');
    assert(nodes[5].includes('104.16'), '6th is LAX');
    console.log('✅ test4: multi-colo priority order');
  }

  // Test 5: min_speed 过滤
  {
    const fetchCsv = async (colo) => {
      if (colo === 'NRT') return NRT_CSV;
      throw new Error('not found');
    };
    const nodes = await buildSubscriptionFromCsvs({
      colos: ['NRT'], top: 5, minSpeed: 11.0,
      uuid: UUID, host: HOST, fallback: '', fetchCsv,
    });
    assert.strictEqual(nodes.length, 2, 'only IPs >= 11 MB/s');
    assert(nodes[0].includes('172.64.229.1'), '12.5 MB/s kept');
    assert(nodes[1].includes('172.64.229.2'), '11.8 MB/s kept');
    console.log('✅ test5: min_speed filter');
  }

  // Test 6: CSV 不存在 → 跳过，不报错
  {
    const fetchCsv = async (colo) => {
      if (colo === 'NRT') return NRT_CSV;
      throw new Error('404');
    };
    const nodes = await buildSubscriptionFromCsvs({
      colos: ['NRT', 'XXX'], top: 5, minSpeed: 0,
      uuid: UUID, host: HOST, fallback: '', fetchCsv,
    });
    assert.strictEqual(nodes.length, 3, 'missing colo gracefully skipped');
    console.log('✅ test6: missing colo graceful skip');
  }

  // Test 7: 空 CSV 处理
  {
    const fetchCsv = async (colo) => 'ip,download_speed_MBps,colo,tier\n';
    const nodes = await buildSubscriptionFromCsvs({
      colos: ['EMPTY'], top: 5, minSpeed: 0,
      uuid: UUID, host: HOST, fallback: '', fetchCsv,
    });
    assert.strictEqual(nodes.length, 0, 'empty CSV yields 0 nodes');
    console.log('✅ test7: empty CSV handling');
  }

  // Test 8: UUID 没传 → 返回空（不报错，由调用方处理）
  {
    const nodes = await buildSubscriptionFromCsvs({
      colos: ['NRT'], top: 5, minSpeed: 0,
      uuid: '', host: HOST, fallback: '',
      fetchCsv: async () => NRT_CSV,
    });
    assert.strictEqual(nodes.length, 3, 'empty UUID still produces nodes (Worker 503s separately)');
    console.log('✅ test8: empty UUID (callers should 503 before calling)');
  }

  console.log('\n🎉 all /sub logic tests passed');
}

test().catch((e) => {
  console.error('❌ test failed:', e.message);
  console.error(e.stack);
  process.exit(1);
});
