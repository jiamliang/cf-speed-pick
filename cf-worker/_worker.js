// SPDX-License-Identifier: GPL-3.0-or-later
//
// cf-speed-pick Worker — VLESS over WebSocket proxy + KV-backed subscription.
//
// Architecture:
//   [client] --wss/TLS--> [CF edge (优选 IP)] --> [this Worker]
//                                                       |
//                                            cloudflare:sockets (raw TCP)
//                                                       |
//                                                  [target site]
//                                              (or [FALLBACK_IP] if direct fails)
//
// Subscription data flow:
//   [R7000/VPS] --POST /api/put--> [Worker] --env.KV.put--> [CF KV]
//   [client]    --GET /sub-->      [Worker] --env.KV.get --> build vless:// list
//
// Reference: gslege/CloudflareIP/CF-Worker/_worker.js (GPL-3.0)
// Rewritten with: env-injected UUID, KV-backed /sub, /api/put for nodes,
//   /sub?operator=unicom|telecom, /sub?auto=1 reverse-lookup by client IP,
//   hardcoded FALLBACK_IPS for resilience when KV is empty.
//
// Deployment:
//   1. npx wrangler login
//   2. UUID=$(uuidgen | tr 'A-Z' 'a-z')
//   3. TOKEN=$(openssl rand -hex 32)
//   4. echo "$UUID"  | npx wrangler secret put VLESS_UUID
//   5. echo "$TOKEN" | npx wrangler secret put PUT_TOKEN
//   6. Create KV namespace "cf-speed-ips" in CF Dashboard, paste id into wrangler.toml
//   7. npx wrangler deploy
//
// Endpoints:
//   /        — 404 (intentionally, hides existence from scanners)
//   /health  — JSON status (UUID prefix, fallback, KV binding, FALLBACK_IPS count)
//   /sub     — text/plain subscription (one VLESS URI per line)
//              Query params:
//                operator=unicom|telecom|auto|test  (default: auto)
//                  unicom — 只读 unicom/all KV
//                  telecom — 只读 telecom/all KV
//                  auto — ?auto=1 自动反查 client IP 算出来的 operator
//                         若是 unicom/telecom → 单读一个
//                         若是其他（移动/教育网/未知/ip-api 失败）→ 合并 unicom + telecom
//                top=10                    (global top N)
//                min_speed=8               (drop slower IPs)
//                auto=1                    (auto-detect operator from client IP)
//   /api/put — node -> Worker KV upload endpoint
//              Method: POST
//              Headers: Authorization: Bearer <PUT_TOKEN>
//              Query:   ?operator=unicom|telecom
//              Body:    raw CSV (cf-speed-pick output, with header)
//   any path with Upgrade: websocket — VLESS proxy

import { connect } from 'cloudflare:sockets';

const VERSION = '0.2.0';

// Secrets (set via `wrangler secret put`)
// - VLESS_UUID: client UUID (required)
// - PUT_TOKEN:  shared secret for /api/put (32-byte hex)
// Vars (set via wrangler.toml or dashboard)
// - FALLBACK_IP: optional fallback when direct connect fails (e.g. "1.2.3.4:443")
// KV binding:
// - KV: namespace "cf-speed-ips" — key = "{operator}/all", value = CSV string

// =====================================================================
// FALLBACK_IPS — 兜底优选 IP（KV 没数据或失效时使用）
//
// 这些 IP 是 2026-09-15 在青岛电信机房（AS58541, CHINATELECOM SHANDONG QINGDAO IDC）
// 跑 cf-speed-pick 验证过的真实优选 IP。
// 每月跑一次 cf-speed-pick，挑 Top 10 更新此列表。
//
// 上次测速日期: 2026-09-15
// 数据源: /tmp/cf-test-out/02_httping.csv (1197 IPs from Layer 2)
// =====================================================================
const FALLBACK_IPS = [
  // === 日本 NRT (Top 5, 延迟 50ms) ===
  { ip: '172.64.229.112', colo: 'NRT', note: 'jp-tokyo-50ms' },
  { ip: '172.64.229.114', colo: 'NRT', note: 'jp-tokyo-50ms' },
  { ip: '172.64.229.120', colo: 'NRT', note: 'jp-tokyo-50ms' },
  { ip: '172.64.229.105', colo: 'NRT', note: 'jp-tokyo-51ms' },
  { ip: '172.64.229.129', colo: 'NRT', note: 'jp-tokyo-51ms' },

  // === 新加坡 SIN (Top 5, 延迟 71-74ms) ===
  { ip: '172.64.146.251', colo: 'SIN', note: 'sg-singapore-71ms' },
  { ip: '172.64.145.198', colo: 'SIN', note: 'sg-singapore-72ms' },
  { ip: '172.64.146.105', colo: 'SIN', note: 'sg-singapore-72ms' },
  { ip: '172.64.147.174', colo: 'SIN', note: 'sg-singapore-72ms' },
  { ip: '172.64.149.73',  colo: 'SIN', note: 'sg-singapore-72ms' },
];

// ASN -> operator mapping (for ?auto=1)
const TELECOM_ASN = ['as4134'];
const UNICOM_ASN = ['as4837', 'as9929'];
const MOBILE_ASN = ['as9808', 'as58453', 'as56041', 'as56040'];  // 中国移动
const EDU_NET_ORG = ['cernet', '教育网', 'china education'];
const GREAT_WALL_ORG = ['greatwall', '长城宽带', 'gwbn'];
const TELECOM_ORG = ['chinatelecom', '中国电信'];
const UNICOM_ORG = ['chinaunicom', 'unicom', '中国联通'];

// /sub?auto=1 解析后的 operator 值
// 'unicom' / 'telecom' — 确定的运营商
// 'auto'            — 未知（移动/教育网/长城/境外/失败）→ 合并 unicom + telecom
const OP_UNKNOWN = 'auto';

export default {
  async fetch(request, env, ctx) {
    const upgrade = request.headers.get('Upgrade');
    if (upgrade && upgrade.toLowerCase() === 'websocket') {
      return handleVless(request, env);
    }
    return handleHttp(request, env);
  },
};

// --- HTTP (non-WS) handlers ---

function handleHttp(request, env) {
  const url = new URL(request.url);
  const path = url.pathname;

  // Hide existence on root — return generic 404 so scanners don't fingerprint us.
  if (path === '/' || path === '') {
    return new Response('Not Found', { status: 404 });
  }

  if (path === '/health') {
    return jsonResponse({
      ok: true,
      version: VERSION,
      uuid_prefix: (env.VLESS_UUID || '').slice(0, 8),
      fallback: env.FALLBACK_IP || null,
      has_kv: !!env.KV,
      fallback_ips_count: FALLBACK_IPS.length,
    });
  }

  if (path === '/sub') {
    return handleSubscription(request, env);
  }

  if (path === '/api/put') {
    return handlePut(request, env);
  }

  return new Response('Not Found', { status: 404 });
}

// --- /api/put: nodes -> Worker -> KV ---

async function handlePut(request, env) {
  // Method check
  if (request.method !== 'POST') {
    return new Response('Method Not Allowed', {
      status: 405,
      headers: { Allow: 'POST' },
    });
  }

  // Auth: shared PUT_TOKEN
  const auth = request.headers.get('Authorization') || '';
  const expected = `Bearer ${env.PUT_TOKEN || ''}`;
  if (!env.PUT_TOKEN || !auth || auth !== expected) {
    return new Response('Unauthorized', { status: 401 });
  }

  // Parse query params
  const url = new URL(request.url);
  const operator = url.searchParams.get('operator') || '';

  // Validate
  if (!['unicom', 'telecom'].includes(operator)) {
    return new Response('Bad operator (use unicom or telecom)', { status: 400 });
  }

  // Read CSV body
  const csv = await request.text();
  if (!csv || csv.length > 100000) {
    return new Response(`Bad body: empty or >100KB (got ${csv ? csv.length : 0} bytes)`, {
      status: 413,
    });
  }

  // Basic sanity: must start with CSV header
  if (!csv.startsWith('ip,')) {
    return new Response('Body must be cf-speed-pick CSV (start with "ip,")', {
      status: 400,
    });
  }

  // KV write with 7-day TTL (cron runs every 6h, so KV always fresh)
  const key = `${operator}/all`;
  try {
    await env.KV.put(key, csv, { expirationTtl: 7 * 24 * 3600 });
  } catch (e) {
    return new Response(`KV write failed: ${e.message}`, { status: 500 });
  }

  return new Response(
    `OK: ${key} (${csv.length} bytes, ${csv.split('\n').length - 1} rows)\n`,
    { headers: { 'Content-Type': 'text/plain; charset=utf-8' } }
  );
}

// --- /sub: subscription ---

async function handleSubscription(request, env) {
  const url = new URL(request.url);

  // Query params
  //   默认 operator='auto'（不带参数 = 合并 unicom + telecom）
  //   ?operator=unicom|telecom|auto|test 显式指定
  //   ?auto=1 在 'auto' 基础上反查 client IP（识别为联通/电信则单读，否则合并）
  let operator = (url.searchParams.get('operator') || 'auto').toLowerCase();
  const top = clampInt(url.searchParams.get('top'), 1, 50, 10);
  const minSpeed = parseFloat(url.searchParams.get('min_speed') || '0') || 0;

  // ?auto=1 + operator=auto → 反查 client IP
  // 反查结果可能是:
  //   'unicom' / 'telecom' → 单读一个
  //   'auto'（未知/失败）  → 保持 'auto'（合并）
  if (url.searchParams.get('auto') === '1' && operator === 'auto') {
    operator = await detectOperator(request, env);
  }

  if (!['unicom', 'telecom', 'auto', 'test'].includes(operator)) {
    return new Response(`Bad operator: ${operator} (use unicom, telecom, auto, or test)`, {
      status: 400,
    });
  }

  const uuid = env.VLESS_UUID || '';
  const host = url.hostname;
  const fallback = env.FALLBACK_IP || '';

  if (!uuid) {
    return new Response('Worker not configured: VLESS_UUID missing', { status: 503 });
  }

  // 1) Try KV
  //    operator='auto' → 合并 unicom + telecom（移动/教育网/未知运营商走这条）
  let ips;
  if (operator === 'auto') {
    const [uni, tel] = await Promise.all([
      fetchTopFromKV(env, 'unicom', top, minSpeed),
      fetchTopFromKV(env, 'telecom', top, minSpeed),
    ]);
    // 简单合并：unicom 在前（先返回国内通用的）
    ips = [...uni, ...tel];
  } else {
    ips = await fetchTopFromKV(env, operator, top, minSpeed);
  }

  // 2) Fallback to hardcoded FALLBACK_IPS
  if (ips.length === 0) {
    ips = FALLBACK_IPS.slice(0, top).map(({ ip, colo, note }) => ({
      ip, speed: 0, colo, note,
    }));
  }

  // Final safety: never return empty
  if (ips.length === 0) {
    // FALLBACK_IPS is empty — return a placeholder so /sub doesn't 503
    return new Response(
      '# Worker FALLBACK_IPS is empty. Please update _worker.js FALLBACK_IPS array.\n',
      {
        status: 200,
        headers: { 'Content-Type': 'text/plain; charset=utf-8' },
      }
    );
  }

  const nodes = ips.map(({ ip, speed, colo, note }) =>
    buildVlessUri({
      uuid, host, ip, port: 443, fallback, colo, speed, note,
    })
  );

  return new Response(nodes.join('\n') + '\n', {
    headers: {
      'Content-Type': 'text/plain; charset=utf-8',
      'Profile-Update-Interval': '6',
      'Subscription-Userinfo': `upload=0; download=0; total=0; expire=0`,
    },
  });
}

// Fetch top N IPs from KV ({operator}/all), parse CSV, return sorted by speed (desc).
async function fetchTopFromKV(env, operator, top, minSpeed) {
  if (!env.KV) return [];
  try {
    const csv = await env.KV.get(`${operator}/all`);
    if (!csv) return [];
    const lines = csv.split('\n').slice(1); // skip header
    const ips = [];
    for (const line of lines) {
      const parts = line.split(',');
      if (parts.length < 4) continue;
      const ip = parts[0].trim();
      const speed = parseFloat(parts[1]) || 0;
      const colo = parts[2].trim();
      if (minSpeed > 0 && speed < minSpeed) continue;
      ips.push({ ip, speed, colo });
      if (ips.length >= top) break;
    }
    return ips;
  } catch (_) {
    return [];
  }
}

// Detect operator from client IP using ip-api.com (free 45 req/day).
// Cached in KV for 24h to stay under the limit.
//
// Returns:
//   'unicom'  — 中国联通
//   'telecom' — 中国电信
//   'auto'    — 未知（移动/教育网/长城/境外/ip-api 失败/无 IP 头）→ 合并 unicom + telecom
async function detectOperator(request, env) {
  const clientIp = request.headers.get('CF-Connecting-IP');
  if (!clientIp) return OP_UNKNOWN;

  // Try cache
  const cacheKey = `asn-lookup:${clientIp}`;
  if (env.KV) {
    try {
      const cached = await env.KV.get(cacheKey);
      if (cached) return cached;
    } catch (_) {}
  }

  // Lookup
  let result = OP_UNKNOWN;
  try {
    const r = await fetch(`http://ip-api.com/json/${clientIp}?fields=as,org,status`, {
      cf: { cacheTtl: 3600 },
    });
    if (r.ok) {
      const d = await r.json();
      if (d.status === 'success') {
        const asn = (d.as || '').toLowerCase();
        const org = (d.org || '').toLowerCase();

        if (TELECOM_ASN.some(a => asn.includes(a)) || TELECOM_ORG.some(o => org.includes(o))) {
          result = 'telecom';
        } else if (UNICOM_ASN.some(a => asn.includes(a)) || UNICOM_ORG.some(o => org.includes(o))) {
          result = 'unicom';
        }
        // 移动 / 教育网 / 长城 / 境外 — result 保持 OP_UNKNOWN
        // 其他无法识别 — result 保持 OP_UNKNOWN
      }
    }
  } catch (_) {}

  // Cache 24h (cache whatever we got, even 'auto')
  if (env.KV) {
    await env.KV.put(cacheKey, result, { expirationTtl: 24 * 3600 }).catch(() => {});
  }
  return result;
}

// Build a single VLESS URI.
// Format: vless://UUID@IP:443?encryption=none&security=tls&sni=HOST&fp=random&type=ws&host=HOST&path=pyip%3D[FALLBACK]#NOTE
// path uses pyip=<fallback> so the WebSocket can fall back if direct connect fails.
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
  const tag = note
    || (speed > 0 ? `${colo} ${speed.toFixed(2)}MB/s` : colo);
  return `vless://${uuid}@${ip}:${port}?${params.join('&')}#${encodeURIComponent(tag)}`;
}

// --- VLESS proxy over WebSocket ---

async function handleVless(request, env) {
  const uuid = env.VLESS_UUID;
  const fallback = env.FALLBACK_IP || '';

  if (!uuid) {
    return new Response('Worker not configured: VLESS_UUID missing', { status: 503 });
  }

  // Allow per-request override of fallback via ?ip=HOST:PORT in the WS path.
  const reqUrl = new URL(request.url);
  const overrideIp = reqUrl.searchParams.get('ip');
  const effectiveFallback = overrideIp || fallback;

  const [client, ws] = Object.values(new WebSocketPair());
  ws.accept();
  pipeVless(ws, uuid, effectiveFallback).catch((err) => {
    try { ws.close(1011, 'pipe error'); } catch (_) {}
  });
  return new Response(null, { status: 101, webSocket: client });
}

// VLESS protocol + bidirectional WS<->TCP pipe.
async function pipeVless(ws, expectedUuid, fallback) {
  let reader, writer, firstPacketDone = Promise.resolve();
  let headerInfo = null;
  let queue = Promise.resolve();

  ws.addEventListener('message', async (event) => {
    if (!headerInfo) {
      headerInfo = parseVlessHeader(new Uint8Array(event.data), expectedUuid);
      firstPacketDone = headerInfo.promise;
    } else {
      await headerInfo.promise;
      queue = queue.then(() => writer.write(new Uint8Array(event.data))).catch(() => {});
    }
  });

  try {
    const info = await firstPacketDone;
    writer = info.writer;
    reader = info.reader;

    // Send VLESS response header: version byte + 0
    ws.send(new Uint8Array([info.versionByte, 0]));

    // Pump TCP -> WS
    while (true) {
      await queue;
      const { done, value } = await reader.read();
      if (done) break;
      if (value && value.length > 0) {
        queue = queue.then(() => ws.send(value)).catch(() => {});
      }
    }
  } catch (_) {
    // Connection error or invalid UUID — drop silently.
  } finally {
    try { ws.close(); } catch (_) {}
  }
}

// Parse the VLESS first packet and open the upstream TCP socket.
// Throws on invalid UUID.
function parseVlessHeader(buf, expectedUuid) {
  const version = buf[0];
  const uuidBytes = sliceBuf(buf, 1, 17);
  if (formatUuid(uuidBytes) !== expectedUuid) {
    throw new Error('UUID mismatch');
  }

  // Options length (1 byte) + options + address
  const optLen = buf[17];
  let p = 18 + optLen;

  // Command: 1 = TCP, 2 = UDP (we only support TCP)
  const command = buf[p]; p += 1;
  if (command !== 1) throw new Error('Only TCP supported');

  // Port (2 bytes, big-endian)
  const port = (buf[p] << 8) | buf[p + 1]; p += 2;

  // Address type
  const addrType = buf[p]; p += 1;
  let address;
  if (addrType === 1) {
    // IPv4
    address = [buf[p], buf[p+1], buf[p+2], buf[p+3]].join('.');
    p += 4;
  } else if (addrType === 2) {
    // Domain
    const len = buf[p]; p += 1;
    address = new TextDecoder().decode(sliceBuf(buf, p, len));
    p += len;
  } else if (addrType === 3) {
    // IPv6
    const dv = new DataView(buf.buffer, buf.byteOffset + p, 16);
    const parts = [];
    for (let i = 0; i < 8; i++) parts.push(dv.getUint16(i * 2).toString(16));
    address = `[${parts.join(':')}]`;
    p += 16;
  } else {
    throw new Error('Unknown address type');
  }

  // Open TCP socket to target address.
  const socketPromise = openSocket(address, port).catch(async () => {
    if (!fallback) throw new Error('Direct connect failed and no FALLBACK_IP set');
    const [fh, fp = 443] = fallback.split(':');
    return await openSocket(fh, Number(fp));
  });

  const promise = socketPromise.then((sock) => {
    const writer = sock.writable.getWriter();
    // Write any initial payload that came with the header
    const initial = sliceBuf(buf, p);
    if (initial.length > 0) writer.write(initial).catch(() => {});
    const reader = sock.readable.getReader();
    return { writer, reader };
  });

  return { versionByte: version, promise };
}

async function openSocket(hostname, port) {
  // hostname may be IPv6-wrapped in brackets — strip them for connect().
  const clean = hostname.startsWith('[') && hostname.endsWith(']')
    ? hostname.slice(1, -1) : hostname;
  const sock = connect({ hostname: clean, port });
  await sock.opened;
  return sock;
}

// --- Utilities ---

function sliceBuf(buf, start, len) {
  if (len === undefined) return buf.slice(start);
  return buf.slice(start, start + len);
}

function formatUuid(bytes) {
  const hex = Array.from(bytes).map(b => b.toString(16).padStart(2, '0')).join('');
  return hex.replace(/(.{8})(.{4})(.{4})(.{4})(.{12})/, '$1-$2-$3-$4-$5');
}

function clampInt(s, min, max, dflt) {
  const n = parseInt(s, 10);
  if (Number.isNaN(n)) return dflt;
  return Math.min(max, Math.max(min, n));
}

function jsonResponse(obj) {
  return new Response(JSON.stringify(obj, null, 2), {
    headers: { 'Content-Type': 'application/json; charset=utf-8' },
  });
}
