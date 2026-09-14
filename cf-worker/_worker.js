// SPDX-License-Identifier: GPL-3.0-or-later
//
// cf-speed-pick Worker — VLESS over WebSocket proxy with optional fallback IP.
//
// Architecture:
//   [client] --wss/TLS--> [CF edge (优选 IP)] --> [this Worker]
//                                                       |
//                                            cloudflare:sockets (raw TCP)
//                                                       |
//                                                  [target site]
//                                              (or [FALLBACK_IP] if direct fails)
//
// Reference: gslege/CloudflareIP/CF-Worker/_worker.js (GPL-3.0)
// Rewritten with: env-injected UUID, empty default fallback, English comments,
// SPDX header, /health endpoint for diagnostics.
//
// Deployment:
//   1. npx wrangler login
//   2. UUID=$(uuidgen | tr 'A-Z' 'a-z'); echo "Generated UUID: $UUID"
//   3. echo "$UUID" | npx wrangler secret put VLESS_UUID
//   4. npx wrangler deploy
//   5. Visit https://<worker-name>.<account-subdomain>.workers.dev/health
//
// Endpoints:
//   /        — 404 (intentionally, hides existence from scanners)
//   /health  — JSON status (UUID prefix, fallback, version)
//   /sub     — text/plain subscription (one VLESS URI per line, all current best IPs)
//   any path with Upgrade: websocket — VLESS proxy

import { connect } from 'cloudflare:sockets';

const VERSION = '0.1.0';

// Secrets (set via `wrangler secret put`)
// - VLESS_UUID: client UUID (required)
// Vars (set via wrangler.toml or dashboard)
// - FALLBACK_IP: optional fallback when direct connect fails (e.g. "1.2.3.4:443")
// - UPSTREAM_CACHE_BASE: optional URL of cf-speed-pick colo CSV cache (for /sub)

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
      upstream_cache_base: env.UPSTREAM_CACHE_BASE || null,
    });
  }

  if (path === '/sub') {
    return handleSubscription(request, env);
  }

  return new Response('Not Found', { status: 404 });
}

// --- Subscription: dynamically build VLESS nodes from cf-speed-pick cache ---

async function handleSubscription(request, env) {
  const url = new URL(request.url);
  // Query params (with safe defaults):
  //   colos:       comma-separated IATA codes (default: NRT,ICN,KIX,TPE,HKG,SIN)
  //   top:         per-colo top N (default: 5)
  //   min_speed:   drop IPs slower than this MB/s (default: 0)
  const colos = (url.searchParams.get('colos') || 'NRT,ICN,KIX,TPE,HKG,SIN')
    .split(',').map(s => s.trim().toUpperCase()).filter(Boolean);
  const top = clampInt(url.searchParams.get('top'), 1, 50, 5);
  const minSpeed = parseFloat(url.searchParams.get('min_speed') || '0') || 0;

  const base = env.UPSTREAM_CACHE_BASE || '';
  const uuid = env.VLESS_UUID || '';
  const host = url.hostname;
  const fallback = env.FALLBACK_IP || '';

  if (!uuid) {
    return new Response('Worker not configured: VLESS_UUID missing', { status: 503 });
  }
  if (!base) {
    // Fall back to the static minimal list when no upstream cache is configured.
    return staticSubscription(uuid, host, fallback);
  }

  // Fetch each colo CSV in parallel.
  const fetches = colos.map(async (colo) => {
    try {
      const csvUrl = `${stripSlash(base)}/cache/colo/${colo}.csv`;
      const r = await fetch(csvUrl, { cf: { cacheTtl: 60 } });
      if (!r.ok) return [];
      const text = await r.text();
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
  if (ips.length === 0) {
    return new Response('No IPs available from upstream cache. Check VPS colo/*.csv exists.', { status: 502 });
  }

  const nodes = ips.map(({ ip, speed, colo }) =>
    buildVlessUri({
      uuid, host, ip, port: 443, fallback, colo, speed,
    })
  );

  // Text/plain body, one node per line. Most clients accept this.
  return new Response(nodes.join('\n') + '\n', {
    headers: {
      'Content-Type': 'text/plain; charset=utf-8',
      // Hint to clients that this is a subscription (some clients respect this).
      'Profile-Update-Interval': '6',
      'Subscription-Userinfo': `upload=0; download=0; total=0; expire=0`,
    },
  });
}

// Minimal static list (used when UPSTREAM_CACHE_BASE is not set).
function staticSubscription(uuid, host, fallback) {
  const defaults = [
    { ip: '172.64.229.0',  colo: 'NRT', note: 'jp' },
    { ip: '104.16.0.0',    colo: 'SJC', note: 'us' },
    { ip: '104.26.0.0',    colo: 'FRA', note: 'de' },
    { ip: '188.114.96.0',  colo: 'AMS', note: 'nl' },
  ];
  const nodes = defaults.map(d =>
    buildVlessUri({ uuid, host, ip: d.ip, port: 443, fallback, colo: d.colo, speed: 0, note: d.note })
  );
  return new Response(nodes.join('\n') + '\n', {
    headers: { 'Content-Type': 'text/plain; charset=utf-8' },
  });
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
  const tag = note || (speed > 0 ? `${colo} ${speed.toFixed(2)}MB/s` : colo);
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

function stripSlash(s) { return s.endsWith('/') ? s.slice(0, -1) : s; }

function jsonResponse(obj) {
  return new Response(JSON.stringify(obj, null, 2), {
    headers: { 'Content-Type': 'application/json; charset=utf-8' },
  });
}
