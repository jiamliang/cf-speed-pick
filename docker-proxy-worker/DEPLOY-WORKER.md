# 部署 cf-docker-proxy Worker 到 Cloudflare

把 `docker-proxy-worker/` 里的代码部署到你的 CF 账号，得到一个把 8 个 Docker registry 上游反代到 `*.cf.peeweecap.com` 子域名的 Worker。

**前置条件**：

- 一个 Cloudflare 账号，且 `peeweecap.com` 在该账号下
- Node.js ≥ 18（仅本机开发用）
- wrangler ^3.114.17（`npm install` 时装）

---

## 一、本机准备（一次性）

```bash
# 1. 进入目录
cd docker-proxy-worker

# 2. 安装 wrangler
npm install
# 或全局：npm install -g wrangler

# 3. 登录 CF（浏览器授权，最简单）
npx wrangler login
# 或者用 API Token：
# CLOUDFLARE_API_TOKEN=xxx npx wrangler deploy
```

`wrangler login` 会：浏览器跳 CF 授权页 → 点 Allow → wrangler 把账号信息写入 `~/.config/.wrangler/config/default.toml`。

**验证**：

```bash
npx wrangler whoami
# 应该输出你的账号邮箱和 Account ID（记下 Account ID，后面要用）
```

---

## 二、`[vars]` 配置

`wrangler.toml` 顶层的 `[vars]` 段定义 Worker 运行时能直接引用的全局变量。**三个都不是 secret**，明文写在 toml 里：

```toml
[vars]
CUSTOM_DOMAIN = "cf.peeweecap.com"
MODE = "production"
TARGET_UPSTREAM = ""
```

源码 `src/index.js` 第 8-21 行直接引用（**不是** `process.env.X`，wrangler 编译时把 vars 内联成 bundle 顶层的 free variable）：

```javascript
const routes = {
  ["docker." + CUSTOM_DOMAIN]: dockerHub,
  ["quay." + CUSTOM_DOMAIN]: "https://quay.io",
  ...
};
```

各变量作用：

| 变量 | 作用 | 当前值 |
|---|---|---|
| `CUSTOM_DOMAIN` | Worker 用来拼接路由 key 的域名前缀（`docker.` + `cf.peeweecap.com` = `docker.cf.peeweecap.com`） | `cf.peeweecap.com` |
| `MODE` | `production` 走 `routes` 表；`debug` 模式 fallthrough 到 `TARGET_UPSTREAM`（自定义调试用） | `production` |
| `TARGET_UPSTREAM` | 仅 `MODE=debug` 时生效；平时空字符串 | `""` |

要换域（比如部署到 `cf.example.com`）就改 `CUSTOM_DOMAIN`，**源码、wrangler.toml 的 `[[routes]]` 都要同步改**。

---

## 三、`[[routes]]` 配置

`wrangler.toml` 里有 8 条 `[[routes]]`，把 8 个子域名的请求都交给 `cf-docker-proxy` Worker：

```toml
[[routes]]
pattern = "docker.cf.peeweecap.com/*"
zone_name = "peeweecap.com"

[[routes]]
pattern = "quay.cf.peeweecap.com/*"
zone_name = "peeweecap.com"
# ... 共 8 条
```

**关键坑：用 `zone_name = "peeweecap.com"`，**不要用 `custom_domain = true`。**

`cf-worker/wrangler.toml` 注释明确写了：`custom_domain = true` 有 bug —— WebSocket Upgrade 请求 CF 边缘会自己处理 101，**Worker 根本收不到**。HTTP 也按 `zone_name` 走更稳。我们的 8 条都是 HTTP（无 WS），但保持 precedent 一致。

**再次提醒**：`[[routes]]` 只在 CF 边缘**注册 Worker 路由**，**不会自动创建 DNS 记录**。下一步必须手工建。

---

## 四、8 条 DNS A 记录

DNS 是单独的一层，必须手工建（CF Dashboard 或 API 都行）。

### 路径 A：CF Dashboard 手工加（5-10 分钟，最稳）

打开 https://dash.cloudflare.com/ → 选 `peeweecap.com` 域 → 顶部 **DNS** → **Records** → **Add record**，循环 8 次：

| Type | Name | IPv4 address | Proxy status |
|---|---|---|---|
| A | `docker` | `192.0.2.1` | **Proxied**（橙色云朵） |
| A | `quay` | `192.0.2.1` | **Proxied** |
| A | `gcr` | `192.0.2.1` | **Proxied** |
| A | `k8s-gcr` | `192.0.2.1` | **Proxied** |
| A | `k8s` | `192.0.2.1` | **Proxied** |
| A | `ghcr` | `192.0.2.1` | **Proxied** |
| A | `cloudsmith` | `192.0.2.1` | **Proxied** |
| A | `ecr` | `192.0.2.1` | **Proxied** |

### 为什么是 `192.0.2.1`？

RFC 5737 规定的 **TEST-NET-1** 文档保留 IP（`192.0.2.0/24`），IANA 永不分配，**全球 DNS 不可能解析到真实服务**。Proxied 后 CF 边缘**完全忽略 content 字段**，用 CF anycast IP 接管请求。`192.0.2.1` 只是个"占位让记录合法"的值，CF 文档、Cloudflare Tunnel、Pages 全这么用。

### 路径 B：CF API 批量加（1-2 分钟）

```bash
export CF_API_TOKEN='<your-token-with-zone-dns-edit-scope>'
export CF_ZONE_ID='<your-zone-id>'

for sub in docker quay gcr k8s-gcr k8s ghcr cloudsmith ecr; do
  curl -sS -X POST "https://api.cloudflare.com/client/v4/zones/$CF_ZONE_ID/dns_records" \
    -H "Authorization: Bearer $CF_API_TOKEN" \
    -H "Content-Type: application/json" \
    -d "{\"type\":\"A\",\"name\":\"${sub}.cf\",\"content\":\"192.0.2.1\",\"proxied\":true,\"ttl\":1}" \
    | python3 -c "import json,sys; d=json.load(sys.stdin); print(f'{d[\"result\"][\"name\"]}: {d[\"success\"]}')"
done
```

**Token 必须有 `Zone → DNS → Edit` scope**。CF Dashboard 创建 token 时选 "Edit zone DNS" 模板，或 Custom token 的 Zone Resources 选 `Edit` 后**展开勾选 DNS sub-capability**。

---

## 五、部署

```bash
cd docker-proxy-worker
npx wrangler deploy
```

**期望输出**：

```
Total Upload: 5.34 KiB / gzip: 1.60 KiB
Your worker has access to the following bindings:
- Vars:
  - CUSTOM_DOMAIN: "cf.peeweecap.com"
  - MODE: "production"
  - TARGET_UPSTREAM: ""
Uploaded cf-docker-proxy (0.88 sec)
```

### 已知 bug：`code 10013 subdomain operation failed`

脚本上传成功，但**批量注册 routes 那步**偶发报 `code 10013`：

```
Uploaded cf-docker-proxy (0.88 sec)
✘ [ERROR] A request to the Cloudflare API (/accounts/.../workers/scripts/cf-docker-proxy/subdomain) failed.
  An unknown error has occurred. [code: 10013]
```

**症状**：wrangler 退出非零，但脚本实际已上传（dashboard 能看到 cf-docker-proxy）；8 条 routes 可能没注册齐。

**绕开办法**：手工用 CF API 单条加 routes（已经测过，全部 200 OK）：

```bash
export CF_API_TOKEN='<your-token>'
export CF_ZONE_ID='<your-zone-id>'

for sub in docker quay gcr k8s-gcr k8s ghcr cloudsmith ecr; do
  curl -sS -X POST "https://api.cloudflare.com/client/v4/zones/$CF_ZONE_ID/workers/routes" \
    -H "Authorization: Bearer $CF_API_TOKEN" \
    -H "Content-Type: application/json" \
    -d "{\"pattern\": \"${sub}.cf.peeweecap.com/*\", \"script\": \"cf-docker-proxy\"}" \
    | python3 -c "import json,sys; d=json.load(sys.stdin); print(f'{sub}: {d[\"success\"]}')"
done
```

**幂等**：重复 POST 同一条会覆盖，不会创建重复。

---

## 六、验证

跑完部署和 DNS 后，按顺序验证：

### 6.1 DNS 解析

```bash
for sub in docker quay gcr k8s-gcr k8s ghcr cloudsmith ecr; do
  printf "%-12s " "${sub}.cf"
  dig +short "${sub}.cf.peeweecap.com" A | head -1
done
```

**期望**：每行返回 `104.x.x.x` 或 `172.x.x.x`（CF anycast IP）。

**如果返回 `192.0.2.1`** → Proxied 没勾，去 Dashboard 改。

### 6.2 `/v2/` 探活

```bash
for sub in docker quay gcr k8s-gcr k8s ghcr cloudsmith ecr; do
  out=$(curl --http1.1 -sS -o /dev/null -w "%{http_code} %{time_total}s" --max-time 20 \
    "https://${sub}.cf.peeweecap.com/v2/" 2>&1)
  printf "  %-30s %s\n" "${sub}.cf.peeweecap.com/v2/" "$out"
done
```

**期望**：8 个全 **HTTP 401**。

**关键：`--http1.1` 不可省** —— 本机如果没 IPv6 出口，curl 默认先试 IPv6（`2606:4700:...`）失败，会报假的 `TLS handshake failure`。强 HTTP/1.1 让 curl 只走 IPv4。

### 6.3 真 catalog 请求

```bash
# quay (匿名 catalog 公开)
curl --http1.1 -sS "https://quay.cf.peeweecap.com/v2/coreos/etcd/tags/list" | head -c 300; echo

# k8s (公开)
curl --http1.1 -sS "https://k8s.cf.peeweecap.com/v2/pause/tags/list" | head -c 300; echo

# k8s-gcr (公开)
curl --http1.1 -sS "https://k8s-gcr.cf.peeweecap.com/v2/pause/tags/list" | head -c 300; echo

# docker (匿名 catalog 不开放，401 正常)
curl --http1.1 -sS -o /dev/null -w "%{http_code}\n" \
  "https://docker.cf.peeweecap.com/v2/library/hello-world/tags/list"

# ghcr / ecr 同理 401
```

**期望**：
- quay / k8s / k8s-gcr → HTTP 200 + JSON tag 列表
- docker / ghcr / ecr → HTTP 401（上游策略，不开放匿名 catalog，docker client 会自动 follow `WWW-Authenticate` 头去拿 token 再请求）

---

## 七、（可选）真 docker pull

```bash
sudo mkdir -p /etc/docker
echo '{"registry-mirrors":["https://docker.cf.peeweecap.com"]}' \
  | sudo tee /etc/docker/daemon.json
sudo systemctl restart docker

docker pull docker.cf.peeweecap.com/library/hello-world
docker run --rm docker.cf.peeweecap.com/library/hello-world
```

**如果 pull 卡 "waiting for response"**：Worker 响应了但流式反代失败，去 CF Dashboard → Workers → Logs 看实时日志。

**如果 429 Too Many Requests**：Docker Hub 限速 CF Worker IP（README 警告），重试或换其他 7 个上游。

---

## 八、CI 部署（GitHub Actions）

仓库根 `.github/workflows/deploy-worker.yml` 已经支持 `docker-proxy-worker`。详细配置见 [SETUP-GITHUB.md](./SETUP-GITHUB.md)。

**触发方式**（任选其一）：

| 方式 | 操作 | 何时生效 |
|---|---|---|
| **网页手动** | Actions → Deploy Worker → "Run workflow" → worker_dir dropdown 选 `docker-proxy-worker` | 立即 |
| push 触发 | 改 `docker-proxy-worker/**` 推 main | ⚠️ 见下方 bug |

### 🔴 必读坑：push 自动部署实际会去部署 cf-worker

detect job 的 grep 锚点有 bug：

```bash
for d in cf-worker docker-proxy-worker; do
  if echo "${{ github.event.head_commit.modified }} ${{ github.event.head_commit.added }}" \
     | grep -q "^${d}/"; then
    echo "worker_dir=$d" >> "$GITHUB_OUTPUT"
    exit 0
  fi
done
```

`head_commit.modified` 是**空格分隔的多文件字符串**，`^${d}/` 锚定整串开头，**只有第一个文件刚好是要部署的目录时才会匹配**。否则 fallback 到默认 `cf-worker`。

**结论**：
- 改 `docker-proxy-worker/**` 后 `git push origin main` → Actions **会去部署 cf-worker**，不是 docker-proxy-worker
- 同时改两个目录 → 只识别一个（不一定是 docker-proxy）

**绕行**：永远用 **workflow_dispatch 手动触发** + 显式选 `worker_dir=docker-proxy-worker`。push 自动触发别用。

---

## 九、客户端 + 镜像配置

### 9.1 Docker daemon（Linux）

```bash
sudo mkdir -p /etc/docker
cat <<EOF | sudo tee /etc/docker/daemon.json
{
  "registry-mirrors": ["https://docker.cf.peeweecap.com"]
}
EOF
sudo systemctl restart docker
```

### 9.2 Podman

```bash
sudo mkdir -p /etc/containers/registries.conf.d
cat <<EOF | sudo tee /etc/containers/registries.conf.d/cf-docker-proxy.conf
[[registry]]
location = "docker.cf.peeweecap.com"
insecure = false
EOF
```

### 9.3 Kubernetes imagePullSecrets

不需要。Pod spec 里直接用：

```yaml
image: docker.cf.peeweecap.com/library/nginx:latest
```

或者全局改 containerd 配置 `/etc/containerd/config.toml`：

```toml
[plugins."io.containerd.grpc.v1.cri".registry.mirrors]
  [docker.io] = ["https://docker.cf.peeweecap.com"]
```

---

## 十、常见问题

| 问题 | 排查 |
|---|---|
| `Uploaded cf-docker-proxy` 后报 `code 10013` | wrangler 批量 routes 注册偶发 bug。用第五节的 API 单条加 routes 绕开 |
| `dig +short` 返回 `192.0.2.1` 而不是 CF IP | Dashboard 该条 A 记录的 Proxy 没勾。改成 Proxied（橙色云朵） |
| `curl /v2/` 报 `TLS handshake failure` | 大概率是 curl 默认 IPv6 first 失败。强加 `--http1.1` 走 IPv4 |
| `curl /v2/` 返回 404 | routes 表漏配或 pattern 写错。对照 wrangler.toml 第三节的 8 条 pattern 查 |
| `curl /v2/` 返回 521 / 522 / 524 | CF 边缘 → Worker 通讯异常，去 Dashboard → Workers → Logs 看 |
| `docker pull` 卡 "waiting for response" | Worker 收到请求但反代流式 chunk 失败。看 Worker 实时日志 |
| `docker pull` 429 Too Many Requests | Docker Hub 限速 CF Worker IP 段。重试即可，其他 7 个上游不受影响 |
| CI `npm ci` 失败 | 缺 `package-lock.json`，先本机 `npm install` 生成并提交 |
| push 改 docker-proxy/** 后 Actions 部署的是 cf-worker | detect job grep bug。改用 workflow_dispatch 手动触发 |
| Actions 报 `CLOUDFLARE_API_TOKEN not set` | 仓库 secrets 没加。详见 [SETUP-GITHUB.md](./SETUP-GITHUB.md) |

---

## 十一、安全建议

1. **token rotate**：这个 token 在会话历史里明文存过，建议部署完后去 CF Dashboard → My Profile → API Tokens → rotate，并把新值同步到 GitHub Secrets
2. **DNS scope 限定 Specific zone**：CF Dashboard 创建 token 时，Zone Resources 选 `Specific zone → peeweecap.com`，不要选 `All zones`，降低爆炸半径
3. **`[vars]` 不放敏感信息**：当前 `CUSTOM_DOMAIN` / `MODE` / `TARGET_UPSTREAM` 都是公开配置。**任何 secret 都用 `wrangler secret put`**，不上 toml
4. **监控 Worker 日志**：CF Dashboard → Workers → Logs 开启 observability（已在 wrangler.toml 里 `enabled = true`），关注异常 status code 和异常 origin
5. **公开镜像代理，无鉴权**：任何人能直接 pull 你的 `*.cf.peeweecap.com`，**会有滥用风险**（被刷、被打 Docker Hub 限速）。如果出问题，考虑加 rate limit（修改源码）或仅自己用