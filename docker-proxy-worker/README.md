# cf-docker-proxy

Cloudflare Worker，把 8 个 Docker / OCI registry 上游反代到 `*.cf.peeweecap.com` 子域名。

**最后更新**：2026-09-17 · 改动请同步更新本文档

---

## 它做什么

| 子域名 | 上游 registry | 用途 |
|---|---|---|
| `docker.cf.peeweecap.com` | `registry-1.docker.io` | Docker Hub |
| `quay.cf.peeweecap.com` | `quay.io` | Quay |
| `gcr.cf.peeweecap.com` | `gcr.io` | Google Container Registry |
| `k8s-gcr.cf.peeweecap.com` | `k8s.gcr.io` | Kubernetes 旧 GCR |
| `k8s.cf.peeweecap.com` | `registry.k8s.io` | Kubernetes 新 registry |
| `ghcr.cf.peeweecap.com` | `ghcr.io` | GitHub Container Registry |
| `cloudsmith.cf.peeweecap.com` | `docker.cloudsmith.io` | Cloudsmith |
| `ecr.cf.peeweecap.com` | `public.ecr.aws` | AWS Public ECR |

客户端把 docker daemon 的 `registry-mirrors` 指向 `https://docker.cf.peeweecap.com`，pull 镜像时流量就走本 Worker → 上游。

## 架构图

```
┌─────────────┐     DNS A 记录 (proxied)       ┌──────────────┐
│  docker CLI │  ─── docker.cf.peeweecap.com ──→│  CF 边缘 IP  │
│  / k8s     │                                 │  104.x / 172.x│
└─────────────┘                                 └──────┬───────┘
                                                       │ routes 表匹配
                                                       │ (8 条 [[routes]] → cf-docker-proxy)
                                                       ▼
                                                ┌──────────────┐
                                                │ Worker       │
                                                │ cf-docker-proxy │
                                                │ src/index.js │
                                                └──┬───────────┘
                                                   │ fetch()
                                                   ▼
                                            ┌──────────────┐
                                            │ upstream     │
                                            │ registry-1   │
                                            │ .docker.io   │
                                            └──────────────┘
```

**三层全都要齐**，少一层就坏：

1. **DNS 层**：8 条 proxied A 记录（CF 边缘要能解析域名）
2. **CF 路由层**：8 条 `[[routes]]`（CF 边缘要会分发给 Worker）
3. **源码层**：内置的 `routes` 对象（Worker 要知道哪个 Host 转哪个上游）

## 三个文件各干什么

| 文件 | 作用 | 改不改 |
|---|---|---|
| `src/index.js` | Worker 源码。**逐字来自上游 `ciiiii/cloudflare-docker-proxy`，零修改** | ❌ 不动 |
| `wrangler.toml` | 部署配置：`[vars]` + 8 条 `[[routes]]` | ❌ 已验证 |
| `package.json` | npm 脚本（`deploy` / `delete` / `dev`）+ wrangler ^3.114.17 依赖 | ❌ 已验证 |
| `LICENSE` | MIT，credit ciiiii + jiamliang | ❌ 不动 |

## 上游 & License

- **上游**：[`ciiiii/cloudflare-docker-proxy`](https://github.com/ciiiii/cloudflare-docker-proxy)，MIT License
- **本仓库子项目是上游的静态 fork**：`src/index.js` 一字未改（`diff -q` 与本机 `jiamliang/cloudflare-docker-proxy` clone 完全相同）
- **License 不一致**：仓库根 `LICENSE` 是 GPL-3.0（继承 XIU2/CloudflareSpeedTest），**但 `docker-proxy-worker/LICENSE` 是 MIT**（fork 自 ciiiii）。子项目独立，源码头注释来自上游。

## 链接

- 📘 [DEPLOY-WORKER.md](./DEPLOY-WORKER.md) — 从 0 部署一份新 cf-docker-proxy
- ⚙️ [SETUP-GITHUB.md](./SETUP-GITHUB.md) — GitHub Actions 一次性配置
- 🔧 [TROUBLESHOOTING.md](./TROUBLESHOOTING.md) — 症状 → 根因 → 修法速查

## 验证清单

- [x] 8 个子域名 `dig +short` 返回 CF 代理 IP（`104.x` / `172.x` 段）
- [x] 8 个 `/v2/` curl 全部 401（bearer auth 标准响应）
- [x] quay / k8s / k8s-gcr 真 catalog 请求返 200 + 真实 tag 列表
- [x] gcr / ghcr / ecr / cloudsmith 真 catalog 返 401 或 404（上游策略）
- [x] `git push origin main` 同步 `6c0284d`