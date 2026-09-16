# 客户端配置指南

把 `cf-speed-proxy` Worker 当成代理节点，配置到各种客户端。

## 关键信息

部署完成后你会拿到：
- **Worker URL**：`https://cf-speed-proxy.<子域>.workers.dev` 或 `https://proxy.yourdomain.com`
- **UUID**：配 Secret 时用的那串（如 `a1b2c3d4-e5f6-7890-abcd-ef1234567890`）
- **优选 IP**：从 `/sub` 端点或 VPS colo 桶拿到的 CF 边缘 IP

---

## 通用 VLESS 节点格式

不管哪个客户端，节点本质都是这个 VLESS URI：

```
vless://UUID@IP:443?encryption=none&security=tls&sni=HOST&fp=random&type=ws&host=HOST&path=/[?ip=...]#备注
```

字段说明：
- `UUID`：你设的 VLESS_UUID
- `IP`：CF 边缘 IP（优选 IP）
- `HOST`：Worker 域名（用于 TLS SNI 和 WebSocket Host）
- `path=/` 或 `/pyip%3D1.2.3.4:443`（如果设了 FALLBACK_IP）
- `#备注`：节点名字

---

## 客户端：v2rayN (Windows / macOS / Linux)

### 添加节点

1. 服务器 → 添加 [VLESS] 服务器
2. 填表：

   | 字段 | 值 |
   |---|---|
   | 地址 (Address) | **优选 IP**（如 `172.64.229.1`） |
   | 端口 (Port) | `443` |
   | 用户 ID (UUID) | 你的 UUID |
   | 流控 (Flow) | （留空）|
   | 加密 (Encryption) | `none` |
   | 传输协议 (Network) | `ws` |
   | WS Host | Worker 域名（如 `proxy.yourdomain.com`）|
   | WS Path | `/` |
   | TLS | 勾选 |
   | SNI | Worker 域名 |
   | allowInsecure | 不勾 |

3. 保存

### 测速

右键节点 → "测试服务器真连接延迟" → 看延迟。**挑延迟最低的几个 IP 当地址**。

### 全局代理

- 路由模式：选 "全局模式"
- 系统代理：开启
- 浏览器访问 https://ifconfig.me → 应该看到 CF 边缘 IP（不是你家的）

---

## 客户端：Shadowrocket (iOS)

### 添加节点

两种方式：

**方式 1：粘贴 VLESS URL**
1. 复制 `/sub` 返回的内容（每行一个 `vless://...`）
2. Shadowrocket 顶部 "+" → "类型" 选 "Subscribe"
3. 或者在首页粘贴 → 自动识别

**方式 2：手动**
1. 右上角 "+" → 类型 "VLESS"
2. 填表（同 v2rayN）
3. 保存

### 配置代理

- 设置 → 代理 → 默认：选刚加的节点
- 连接 → 开启

---

## 客户端：Karing (iOS / Android / 桌面)

Karing 支持订阅格式直接粘贴：

1. 设置 → 订阅 → 添加
2. 粘贴 `https://cf-speed-proxy.<子域>.workers.dev/sub?colos=NRT,ICN,KIX`
3. Karing 会定时拉这个 URL 拿最新节点

---

## 客户端：Clash / Mihomo

⚠️ **Clash / Mihomo 用 YAML 订阅，不能直接用 /sub 返回的 text/plain**。

需要 Worker 输出 YAML 格式。当前 Worker `/sub` 只输出 `vless://` 列表，**Clash 兼容性需要 Step B 扩展**。

临时方案：在 Clash 里手动加 VLESS 节点（GUI 大多支持）。

---

## 客户端：sing-box

支持 vless + ws + tls，直接粘贴 `vless://` 即可。

---

## 推荐：动态订阅（最优雅）

每个客户端配订阅 URL，而不是单个节点：

```
https://cf-speed-proxy.<子域>.workers.dev/sub?colos=NRT,ICN,KIX,HKG&top=3&min_speed=1.0
```

参数：
- `colos`：逗号分隔的 colo 列表（按优先级）
- `top`：每个 colo 取前 N 个 IP
- `min_speed`：最低速度（MB/s）

客户端每 N 小时拉一次这个 URL，自动拿到最新优选 IP 列表。

**前提**：VPS 上 cf-speed-pick 在跑（产生 colo 桶）+ Worker 配置了 `UPSTREAM_CACHE_BASE`。

---

## 订阅 URL 用法（按客户端）

### Shadowrocket

主页 → 右上角 "+" → 类型 "Subscribe" → 粘贴 URL

### Karing

设置 → 订阅 → 添加 → 粘贴 URL

### v2rayN

订阅 → 订阅设置 → 添加 → 粘贴 URL → 立即更新

### Clash Verge / Mihomo

Profiles → 从 URL 导入 → 粘贴 URL

⚠️ **Clash/Mihomo 需要 YAML 格式**，Worker 当前输出 text/plain。**等 Step B 扩展**。

---

## 测试代理是否工作

连上后：

```bash
# 应该返回 CF 边缘 IP，不是你家的 IP
curl https://ifconfig.me

# 应该能访问
curl https://www.google.com -I
```

---

## 常见问题

### Q: 节点显示延迟高 / 连不上
- 检查 UUID 对不对（v2rayN 里复制 UUID 和 Worker Secret 对比）
- 检查 SNI 是不是 Worker 域名
- 检查地址填的是不是 CF 边缘 IP（不是 Worker 域名！）
- 检查 Worker 在国外能访问（用 VPN 测 https://cf-speed-proxy.XXX.workers.dev/health）

### Q: /sub 返回的内容客户端不识别
- 大多数客户端需要 vless:// URL 列表（一行一个）
- Worker 当前输出就是这个格式
- Clash/Mihomo 需要 YAML（待扩展）

### Q: 怎么知道哪个 CF 边缘 IP 最快？
用 `cf-speed-pick` 跑一次（参考 DEPLOY-R7000.md），Top N 就是最快 IP。

### Q: Worker 域名被墙怎么办？
绑自定义域名（不在 GFW 黑名单里的）。参考 DEPLOY-WORKER.md 第五节。

### Q: UUID 泄漏了怎么办？
```bash
# 在 VPS 上生成新 UUID
NEW_UUID=$(uuidgen | tr 'A-Z' 'a-z')
# GitHub 仓库 Settings → Secrets → 改 VLESS_UUID 值
# 手动触发 Actions 重部署（Settings 改 secret 不会自动 push）
```

---

## 安全建议

1. **UUID 必须保密**：任何人拿到 UUID + 域名 + 任一 CF 边缘 IP 都能用你的代理
2. **定期换 UUID**：建议每月一次
3. **绑自定义域名**：避开 `*.workers.dev` 在国内被墙的问题
4. **不要在公开仓库提交 wrangler.toml**（如果以后填了 UPSTREAM_CACHE_BASE，会暴露 VPS 域名）
