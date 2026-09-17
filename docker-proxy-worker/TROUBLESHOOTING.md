# 排错速查

从外部症状反查根因。先看症状是哪一类，再去对应表格。

**目录**：[DNS 层](#dns-层) · [TLS / 协议层](#tls--协议层) · [HTTP 状态码](#http-状态码) · [Worker 上传](#worker-上传) · [CI 部署](#ci-部署) · [客户端拉取](#客户端拉取)

---

## DNS 层

| 症状 | 可能根因 | 怎么确认 | 怎么修 |
|---|---|---|---|
| `dig +short` 返回空 / NXDOMAIN | DNS 记录没建 | `dig docker.cf.peeweecap.com` 看 ANSWER SECTION | Dashboard → DNS → Records 加 A 记录（详见 DEPLOY-WORKER.md 第四节） |
| `dig +short` 返回 `192.0.2.1` | Proxied 没勾（灰色云朵），CF 没接管 | 看 Dashboard 该记录 Proxy status | 改成 **Proxied**（橙色云朵） |
| `dig +short` 返回非 CF IP（不是 `104.x`/`172.x`） | 用了 `2001:db8::` 之类的 IPv6，或上游 hosts 配错 | `dig +short AAAA` 看 IPv6 | 删 hosts 覆盖，或在 daemon 配 IPv4 only |
| DNS 查询偶发 timeout | 本机网络 / `IFIX` 代理问题 | `dig +time=5 +tries=1` 测 latency | 配 `ALL_PROXY=socks5h://127.0.0.1:10808` 或换网络 |
| DNS 显示正确但 `curl` 报 `Could not resolve host` | 本机 resolver 问题（systemd-resolved / nsswitch） | `getent hosts docker.cf.peeweecap.com` | `sudo systemctl restart systemd-resolved` |

## TLS / 协议层

| 症状 | 可能根因 | 怎么确认 | 怎么修 |
|---|---|---|---|
| `curl: (35) TLS connect error: ... ssl/tls alert handshake failure` | curl 默认 IPv6 first-fail（DNS 返 AAAA `2606:4700:...`，本机无 IPv6 出口，握手中断） | `curl -v ... 2>&1 \| grep "Trying"` 看第一次连的是 IPv6 | **加 `--http1.1` 强 IPv4**，或 `curl -4 ...`，或 `curl --ipv4 ...` |
| `curl: (60) SSL certificate problem: unable to get local issuer certificate` | 本机 CA bundle 过期 | `ls -la /etc/ssl/certs/ca-certificates.crt` 看时间 | `sudo update-ca-certificates` |
| `curl: (28) Connection timed out` | 出站被 GFW 阻断，或目标 IP 不可达 | `curl -v --trace-time ... 2>&1 \| grep -E "Trying|connect"` | 换网络 / 开代理 |

## HTTP 状态码

| 症状 | 可能根因 | 怎么确认 | 怎么修 |
|---|---|---|---|
| **HTTP 401** `{"message":"UNAUTHORIZED"}` + `WWW-Authenticate: Bearer` | **正常！** Worker OAuth flow 第一步 | 看响应头里有 `WWW-Authenticate: Bearer realm=".../v2/auth"` | docker client 会自动 follow 拿 token。如果手 curl 想看 catalog，带 `-u user:pass` 或继续 follow auth URL |
| **HTTP 404** `{"routes": {...}}` | Host header 不在 Worker 内置 `routes` 表（少见：拼错域名或 `CUSTOM_DOMAIN` 没生效） | 看响应 body 里的 routes 字典 | 检查 wrangler.toml `[vars] CUSTOM_DOMAIN` 和 DNS 是否一致 |
| **HTTP 404**（CF 默认页面，不是 Worker 返的） | routes 表漏配或 pattern 写错 | `curl -v` 看 `Server: cloudflare` + body 是 CF 通用 404 | 补 routes（DEPLOY-WORKER.md 第五节 API 单条加） |
| **HTTP 502** | CF 边缘 → Worker 通讯失败 | Dashboard → Workers → Logs | 删 Worker 重投 |
| **HTTP 503** | Worker 抛异常 | Dashboard → Workers → Logs 看 stack trace | 修代码（一般不常见，因为我们不动 src/index.js） |
| **HTTP 521** | CF 边缘连不上 Worker（Worker 实际没起来） | Dashboard → cf-docker-proxy → Last deployment 看时间 | 重 deploy 或检查 token scope |
| **HTTP 522** | CF 边缘 → origin 超时（仅自定义 origin 模式，我们用 routes 不会有） | — | — |
| **HTTP 524** | CF 边缘等 origin 超过 100 秒 | — | — |

## Worker 上传

| 症状 | 可能根因 | 怎么确认 | 怎么修 |
|---|---|---|---|
| `Uploaded cf-docker-proxy` 后报 `code 10013` | wrangler 批量 routes 注册 bug | 看完整 wrangler 输出 | DEPLOY-WORKER.md 第五节 API 单条加 routes（幂等） |
| `wrangler deploy` 报 `Authentication error [code: 10000]` | token scope 缺 | 步骤一复盘 token | 重发 token，确保 Workers Scripts:Edit + Workers Routes:Edit 都有 |
| `wrangler deploy` 报 `Authentication error [code: 10000]` 在 DNS 阶段 | token 缺 Zone DNS:Edit | 试 `curl -X POST .../dns_records` 看是否 10000 | 加 DNS scope 或改用 Dashboard 手工 |
| `wrangler deploy` 报 `workers.dev subdomain is already in use` | 别的 Worker 已占用该 subdomain | Dashboard → Workers → Overview 找 `cf-docker-proxy` 之外用同名的 | 改 wrangler.toml `name` 字段 |
| `npx wrangler` 报 `command not found` | 没装 / 没 `cd docker-proxy-worker` | `ls node_modules/.bin/wrangler` | `npm install` 或确认 cd 正确 |
| `Module not found` 上传时报 | `npm install` 没跑 | `ls node_modules` | `cd docker-proxy-worker && npm install` |
| Bundle 体积超限（> 1 MiB 免费版 / > 10 MiB 付费版） | 意外加了 npm 依赖 | `wrangler deploy --dry-run` 看 size | 别加依赖，源码不引 npm |

## CI 部署

| 症状 | 可能根因 | 怎么确认 | 怎么修 |
|---|---|---|---|
| Actions 报 `CLOUDFLARE_API_TOKEN not set` | 仓库 secrets 没加 | https://github.com/jiamliang/cf-speed-pick/settings/secrets/actions | SETUP-GITHUB.md 步骤三 |
| Actions 报 `npm ci` 失败 | 缺 `package-lock.json` | 看 job log "Could not find package-lock.json" | 本机 `cd docker-proxy-worker && npm install` 提交 lockfile |
| Actions 报 `npm ci` 失败因 lockfileVersion 不一致 | 本机 Node 版本和 CI (node 20) 不一致 | 看 lockfileVersion 字段 | 升 Node 到 20+ 再 install |
| Actions 跑完了 cf-worker 而不是 docker-proxy | detect job grep bug（`^${d}/` 锚点） | 看 job log "worker_dir=cf-worker" | **改用 workflow_dispatch 手动触发** + dropdown 显式选 `docker-proxy-worker` |
| Actions 报 401 / code 10013 | token scope 或 routes bug 同 Worker 上传 | 看 job log | 同上 |

## 客户端拉取

| 症状 | 可能根因 | 怎么确认 | 怎么修 |
|---|---|---|---|
| `docker pull` 卡 "waiting for response" | Worker 收到请求但流式 chunk 失败 | Dashboard → Workers → Logs 看实时 | 大概率 429 限速；或 docker daemon version 太老（< 20） |
| `docker pull` 报 `429 Too Many Requests` | Docker Hub 限速 CF Worker IP 段 | 响应 header 有 `Retry-After` | 等几分钟重试，或换其他 7 个上游 |
| `docker pull` 报 `unauthorized: incorrect username or password` | registry-1.docker.io 要 auth | 看错误日志 | `docker login docker.cf.peeweecap.com` 用 dockerhub 账号 |
| `docker pull` 报 `x509: certificate signed by unknown authority` | daemon 配了非 443 端口的 mirror，或 TLS 校验 | `docker info` 看 registry-mirrors | 改 daemon.json 用 `https://` 且默认 443 |
| k8s pod `ImagePullBackOff` | imagePullSecrets 错或 mirror 没生效 | `kubectl describe pod` | 见 DEPLOY-WORKER.md 第九节 |
| `podman pull` 报 `connection refused` | registries.conf 配错 | `cat /etc/containers/registries.conf.d/cf-docker-proxy.conf` | 见 DEPLOY-WORKER.md 9.2 节 |
| `ctr images pull` (containerd CLI) | containerd 默认不走 mirror | `ctr -n k8s.io images ls` 看是否拉到 | 改 `/etc/containerd/config.toml` 重启 containerd |

---

## 通用排查步骤

1. **先看 CF 后台状态**：Dashboard → Workers → `cf-docker-proxy` → Last deployment 有时间戳吗？
2. **再查 DNS**：`dig +short docker.cf.peeweecap.com` 返 CF IP 吗？
3. **再查 routes**：`curl -H "Authorization: Bearer $CF_API_TOKEN" https://api.cloudflare.com/client/v4/zones/$CF_ZONE_ID/workers/routes | python3 -m json.tool` 有 8 条吗？
4. **再 curl /v2/**：必须 `--http1.1`，期望 401
5. **看 Worker 日志**：Dashboard → Workers → Logs（实时）
6. **看 wrangler tail**（如果本地部署过）：`npx wrangler tail`

---

## 参考

- [DEPLOY-WORKER.md](./DEPLOY-WORKER.md) — 完整部署
- [SETUP-GITHUB.md](./SETUP-GITHUB.md) — CI 配置
- [README.md](./README.md) — 项目总览