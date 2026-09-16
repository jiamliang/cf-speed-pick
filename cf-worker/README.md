# cf-speed-pick

国内 → Cloudflare 优选 IP 工具（Go）+ VLESS Worker 反代（JavaScript）

---

## 项目结构

```
cf-speed-pick/
├── main.go 等 Go 文件          # 优选 IP 测速核心（TCPing → HTTPing → 下载测速）
├── ranges.go                  # CF IP 段（内置 + 外部文件 loadRanges）
├── colo_priority.go           # colo 优先级分层采样
├── deploy/                    # 一键部署包：直接复制到目标机器
│   ├── cf-speed-pick-linux-amd64    # x86_64 二进制
│   ├── cf-speed-pick-linux-arm64    # aarch64 二进制（路由器）
│   ├── install.sh                   # 通用安装（不复制文件，detect 架构，注册 cron）
│   ├── speedtest-and-upload.sh      # 主脚本（测速 + 上传 KV）
│   ├── .env.example                 # env 模板（拷成 .env 后填）
│   └── cf-ranges.txt               # IP 段配置（强制存在）
│
├── scripts/                   # 源脚本（deploy/ 里的版本从这里 copy）
│   ├── install.sh
│   ├── speedtest-and-upload.sh
│   ├── .env.example
│   └── cf-ranges.txt
│
├── cf-worker/                 # Cloudflare Worker（科学上网 + 订阅）
│   ├── _worker.js             # VLESS over WebSocket 代理
│   ├── wrangler.toml          # Worker 部署配置
│   ├── package.json           # wrangler devDep
│   ├── test/                  # 测试
│   │   ├── test_sub_logic.js        # /sub 逻辑单元测试（8 个）
│   │   └── test_integration.js      # /sub 端到端测试（需境外网络）
│   ├── DEPLOY-WORKER.md       # 部署文档（vps 端 wrangler）
│   ├── SETUP-GITHUB.md        # GitHub Actions 自动部署指南
│   └── CLIENTS.md             # 客户端配置指南（v2rayN/Shadowrocket 等）
│
├── .github/workflows/
│   └── deploy-worker.yml      # GitHub Actions 自动部署
│
├── DEPLOY.md                  # 通用部署手册（替代旧的 DEPLOY-R7000.md）
├── README.md                  # 优选 IP 工具总览（本文件）
└── LICENSE                    # GPL-3.0
```

---

## 三个独立系统的协作

```
┌─────────────────────────────────────────────────────────────────┐
│ 1. VPS（青岛/福州）                                                │
│    - cf-speed-pick cron 跑完整三层                                  │
│    - 输出 out/colo/{NRT,SIN,...}.csv                              │
│    - nginx 暴露 /var/lib/cf-speed-pick/cache/colo/*.csv          │
└──────────────────┬──────────────────────────────────────────────┘
                   │
                   ▼ colo CSV
┌─────────────────────────────────────────────────────────────────┐
│ 2. Cloudflare Worker（cf-speed-proxy）                            │
│    - VLESS over WebSocket（科学上网代理）                           │
│    - /sub 端点拉 VPS colo 桶 → 生成订阅内容（vless:// 列表）        │
│    - 需要 UPSTREAM_CACHE_BASE 配置到 VPS                          │
└──────────────────┬──────────────────────────────────────────────┘
                   │ 优选 IP 列表
                   ▼
┌─────────────────────────────────────────────────────────────────┐
│ 3. 客户端（v2rayN / Shadowrocket / Karing）                       │
│    - 订阅地址 = https://cf-speed-proxy.XXX/sub?colos=...          │
│    - 自动拉取最新优选 IP 列表                                       │
│    - 用户连最快的那个                                              │
└─────────────────────────────────────────────────────────────────┘
```

---

## 当前进度

### Phase 1 + 2：Go 端优选 IP 工具 ✅ 完成

- 三阶段漏斗（TCPing → HTTPing → 下载测速）
- colo 优先级分层（Tier 1/2/3/Other）
- R7000 + 青岛 VPS 协作脚本
- 全部单元测试通过

### Phase 3 Step A：VLESS Worker ✅ 完成（部署成功，但国内访问受限）

- Worker 代码：VLESS over WS over CF sockets API
- GitHub Actions 自动部署（run #34836252683）
- `/health` `/sub` `/` 三个端点
- 客户端配置文档
- **限制**：`*.workers.dev` 子域名在国内被 GFW 屏蔽，需要绑自定义域名

### Phase 3 Step B：动态订阅（已写但未对接）

- Worker `/sub` 端点已经支持 `?colos=&top=&min_speed=` 参数
- 测试覆盖（8 个单元测试通过）
- **未对接**：没配 `UPSTREAM_CACHE_BASE`（需要 VPS 上 nginx 跑起来）

### Phase 3 Step C：端到端工作流（待开始）

- VPS 跑 cf-speed-pick → colo 桶更新 → Worker `/sub` 内容同步 → 客户端拉订阅

---

## 你需要做的事（按优先级）

### 1. 解决 Worker 国内访问问题（5 分钟）

`*.workers.dev` 在国内被 GFW 屏蔽。**绑自定义域名**：

1. 确认你 CF 上的域名（如 `example.com`）
2. 编辑 `cf-worker/wrangler.toml`：
   ```toml
   [[routes]]
   pattern = "proxy.example.com/*"
   custom_domain = true
   ```
3. `git push` 触发 Actions 重部署
4. CF 自动加 DNS + 签证书
5. 访问 `https://proxy.example.com/health` 验证

### 2. 配 VPS cache（10 分钟）

1. VPS 上跑 nginx 暴露 cf-speed-pick colo 桶（参考 DEPLOY-R7000.md 6.1 节）
2. 编辑 `cf-worker/wrangler.toml` 的 `UPSTREAM_CACHE_BASE`
3. `git push` 触发 Actions 重部署

### 3. 客户端配置

参考 `cf-worker/CLIENTS.md`：
- v2rayN / v2rayNG：手动加节点
- Shadowrocket / Karing：粘贴 vless:// URL 或订阅 URL

### 4. 测速验证

连上后：
```bash
curl https://ifconfig.me
# 应该返回 CF 边缘 IP，不是家里的
```

---

## 跑测试

### Go 端单元测试
```bash
./go-docker.sh test -short ./...
```

### Go 端 E2E 测试
```bash
./go-docker.sh test -tags=e2e -v ./...
```

### Worker 逻辑测试（Node）
```bash
cd cf-worker
node test/test_sub_logic.js
```

### Worker 集成测试（需境外网络）
```bash
cd cf-worker
WORKER_URL=https://cf-speed-proxy.你的子域.workers.dev node test/test_integration.js
```

---

## LICENSE

GPL-3.0-or-later（继承自 XIU2/CloudflareSpeedTest）
