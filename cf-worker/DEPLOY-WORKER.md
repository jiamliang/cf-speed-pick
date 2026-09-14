# 部署 cf-speed-pick Worker 到 Cloudflare

把 `cf-worker/` 里的代码部署到你的 CF 账号，得到一个 VLESS over WebSocket 代理 Worker。

**前置条件**：
- 一个 Cloudflare 账号
- Node.js ≥ 18（仅本机开发用，不在 VPS 上）
- 一个 UUID（脚本生成或你提供）

---

## 一、本机准备（一次性）

```bash
# 1. 进入目录
cd cf-worker

# 2. 安装 wrangler
npm install
# 或全局：npm install -g wrangler

# 3. 登录 CF（浏览器授权，最简单）
npx wrangler login
# 或者用 API Token：
# CLOUDFLARE_API_TOKEN=xxx npx wrangler deploy
```

`wrangler login` 会：
- 浏览器跳转到 CF 授权页
- 你点 "Allow"
- wrangler 把账号信息写入 `~/.config/.wrangler/config/default.toml`

**验证**：
```bash
npx wrangler whoami
# 应该输出你的账号邮箱和 Account ID
```

---

## 二、生成 UUID + 设置 Secret

**生成 UUID**：

```bash
# Linux / macOS / Git Bash
UUID=$(uuidgen | tr 'A-Z' 'a-z')
echo "$UUID"
# 类似：04c808e2-0b59-47b0-a54b-32fc7ef1c902
```

或者在线生成：https://www.uuidgenerator.net/

**写入 CF Secret**（**只存一次**，以后部署都用这个 UUID）：

```bash
echo "$UUID" | npx wrangler secret put VLESS_UUID
# 应该输出：✨ Successfully created secret VLESS_UUID
```

**校验**：去 CF Dashboard → Workers & Pages → cf-speed-proxy → Settings → Variables → Environment variables (secrets)，应该能看到 `VLESS_UUID` 一行（值是加密的，看不到原文）。

---

## 三、（可选）配 UPSTREAM_CACHE_BASE

只有**当 VPS 上有 cf-speed-pick cache** 时才需要配。

编辑 `wrangler.toml`：

```toml
[vars]
UPSTREAM_CACHE_BASE = "https://cf-cache.example.com"
```

其中 `cf-cache.example.com` 是 VPS 上 nginx 暴露的域名（参考 `DEPLOY-R7000.md` 的 nginx 配置）。

Worker 的 `/sub` 端点会拉 `${UPSTREAM_CACHE_BASE}/cache/colo/{COLO}.csv`。

**先留空也行**：`/sub` 会返回一个静态最小节点列表，能用但不包含优选 IP。

---

## 四、部署

```bash
npx wrangler deploy
# 输出：✨ Successfully published your Worker to ...
# 给出 URL：https://cf-speed-proxy.<你的子域>.workers.dev
```

记录这个 URL，下一步要用。

---

## 五、绑定自定义域名（推荐）

`*.workers.dev` 子域名在国内有时候被干扰，绑定你自己的域名更稳。

**方法 A：wrangler.toml 里 routes**（推荐）

```toml
[[routes]]
pattern = "proxy.example.com/*"
custom_domain = true
```

然后：

```bash
npx wrangler deploy
```

wrangler 会**自动**在 CF Dashboard 里给你的域名加 `proxy.example.com` 的 Worker Route。**注意 `example.com` 必须已经接入 CF（即 nameserver 指到 CF）**。

**方法 B：Dashboard 手动**：
1. CF Dashboard → Workers & Pages → cf-speed-proxy → Settings → Triggers
2. "Custom Domains" → "Add Custom Domain" → 输入 `proxy.example.com`
3. CF 自动加 DNS 记录 + 签证书

---

## 六、验证

### 6.1 /health 端点

```bash
curl https://cf-speed-proxy.<子域>.workers.dev/health
```

应该返回 JSON：

```json
{
  "ok": true,
  "version": "0.1.0",
  "uuid_prefix": "04c808e2",
  "fallback": null,
  "upstream_cache_base": null
}
```

`uuid_prefix` 应该跟你设置的 UUID 前 8 位一致。

### 6.2 /sub 端点（静态模式）

```bash
curl https://cf-speed-proxy.<子域>.workers.dev/sub
```

应该返回：

```
vless://04c808e2-...@172.64.229.0:443?encryption=none&security=tls&sni=cf-speed-proxy.<子域>.workers.dev&...#jp
vless://04c808e2-...@104.16.0.0:443?...#us
...
```

每个客户端订阅格式不同：
- **v2rayN / v2rayNG / Shadowrocket / Karing**：直接复制粘贴每个 URL 即可
- **Clash / Mihomo**：需要 YAML，不能直接用这个（下一步扩展）

### 6.3 实际代理测试

**用 curl 测 WebSocket（带 VLESS 头）比较复杂**，建议直接用客户端。

#### v2rayN (Windows / macOS / Linux)

1. 服务器 → 添加 [VLESS] 服务器
2. 地址：填 Worker 的 CF 边缘 IP（**优选 IP**！如 `172.64.229.1`）
3. 端口：443
4. UUID：填你设置的 UUID
5. 加密：none
6. 传输：ws
7. WS Host：填 `proxy.example.com`（你的 Worker 域名）
8. WS Path：`/`（如果没设 FALLBACK_IP）或 `/pyip%3D1.2.3.4:443`（如果设了）
9. TLS：开启，SNI = `proxy.example.com`，allowInsecure = false

10. 路由模式：全局代理
11. 浏览器访问 https://ifconfig.me → 应该显示 **CF 的 IP**（不是家里的）
12. 访问 Google → 应该能进

#### Shadowrocket / Karing (iOS / Android)

类似上面的配置。Shadowrocket 可以直接粘贴 VLESS URL 导入。

---

## 七、UUID 怎么告诉客户端

UUID 在两个地方要用：
1. **Worker Secret**（服务端，已经设过了）
2. **客户端节点配置**（要告诉每个客户端）

**最简单**：把 `/sub` 输出的内容发给客户端，让他们自己导入。

**安全**：UUID 是这个 Worker 的"密码"。**任何人拿到 UUID + Worker 域名 + 任一 CF 边缘 IP 都能用**。所以：
- 不要把 UUID 发到公开地方
- 不要用默认 UUID
- 定期换 UUID：`echo "$NEW_UUID" | npx wrangler secret put VLESS_UUID`

---

## 八、（可选）用优选 IP 当入口

Worker 本身在 CF 边缘上，客户端连的是 CF IP。但 CF 有几百个边缘 IP，国内访问不同 IP 速度差很多。

**自动**：用 `/sub?colos=NRT,ICN,KIX&top=5` 这种订阅 URL，**前提是 VPS 上有 cf-speed-pick cache 在跑**。Worker 会拉 VPS 的 colo 桶，把 Top IP 拼进订阅。

**手动**：用 `cf-speed-pick` 跑一次拿到优选 IP（如 `172.64.229.1`），手动加到客户端 v2rayN 节点配置的"地址"字段。

---

## 九、客户端 + 订阅工具搭配

### 选项 1：手贴订阅内容
```bash
# 在 VPS 上
curl https://cf-speed-proxy.<子域>.workers.dev/sub?colos=NRT,ICN,KIX > nodes.txt
# 把 nodes.txt 发给客户端，他们复制到 v2rayN
```

### 选项 2：客户端订阅 URL
在 v2rayN / Shadowrocket 里填：
```
https://cf-speed-proxy.<子域>.workers.dev/sub?colos=NRT,ICN,KIX&top=3
```
客户端会定时拉这个 URL，自动更新节点列表。

⚠️ 注意：这个 URL 里 **没有 UUID**（UUID 在 Worker 端），客户端拿到的是完整的 vless:// 链接（已经嵌入了 UUID）。

---

## 十、常见问题

### Q: 部署后 404？
等 30 秒，CF 边缘缓存生效。或 `npx wrangler tail` 看实时日志。

### Q: 客户端连不上？
检查：
1. UUID 一致（服务端 vs 客户端）
2. Worker 域名对（TLS SNI 必须填 Worker 域名）
3. CF 边缘 IP 能 TLS 握手（curl https://172.64.229.1 看证书）
4. Worker 的 `/health` 是否正常

### Q: Worker 部署成功但 YouTube 加载慢？
- **入口 IP 没优选**：客户端用 v2rayN 的"真连接延迟"测试，挑最快的 CF 边缘 IP 当地址
- **Worker 出口被目标站屏蔽**：配置 FALLBACK_IP，路径要带 `pyip%3D...`（Worker 代码已经处理）

### Q: /sub 返回 502？
检查 `UPSTREAM_CACHE_BASE` 是否能直接访问：
```bash
curl -I "${UPSTREAM_CACHE_BASE}/cache/colo/NRT.csv"
# 应该返回 200 + CSV
```

### Q: 怎么更新 UUID？
```bash
NEW_UUID=$(uuidgen | tr 'A-Z' 'a-z')
echo "$NEW_UUID" | npx wrangler secret put VLESS_UUID
# 老 UUID 立即失效，所有客户端必须更新
```

### Q: 怎么删除 Worker？
CF Dashboard → Workers & Pages → cf-speed-proxy → Settings → 最下方 Delete。

### Q: Workers 免费额度够吗？
CF Workers 免费计划：
- 100,000 请求/天
- 10ms CPU 时间/请求
- 这个 Worker 每个请求 ~2-5ms CPU（VLESS 转发），所以 ~20,000 次代理/天
- 个人用绝对够

如果流量大，升级到 Workers Paid（$5/月，无请求数限制）。

---

## 十一、安全建议

1. **UUID 必须自定义**（不要用默认）
2. **不要在公开仓库提交 wrangler.toml 里的真实 UUID**（Secret 不在 toml 里，但 vars 可能泄露你的域名/IP）
3. **定期换 UUID**：建议每月换一次
4. **CF 账号开启 2FA**
5. **如果怀疑被滥用**：`wrangler tail` 实时看请求，发现异常 IP 段就换 UUID
