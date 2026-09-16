# 部署手册（总览）

> 适用：服务器 (x86_64) 和路由器 (armv7l/aarch64)
> 部署包：`deploy/` 目录
>
> **👉 详细步骤、参数、排错见 [`deploy/DEPLOY.md`](deploy/DEPLOY.md)**
>
> 本文档只放高层说明 + 当前部署模型概述。装到具体机器上的步骤一律看 deploy/DEPLOY.md。

---

## 架构（一句话）

节点（服务器/路由器）跑 cf-speed-pick 测速 → 把 colo 桶 CSV 上传到 Worker KV → 客户端 `/sub` 读 KV 返回真实优选 IP 的 vless:// 列表。

```
┌─────────── 节点 ──────────┐         ┌─────────── 边缘 ───────────┐
│ 服务器/路由器              │         │ VLESS Worker                │
│ cf-speed-pick 跑完整测速   │  POST   │ /api/put 鉴权 → KV.put      │
│ → out/colo/*.csv          │ ──────► │ (key=operator/colo)         │
│ speedtest-and-upload.sh   │ bearer  │                             │
└───────────────────────────┘         │ /sub 读 KV → 返回 vless://    │
                                      └─────────────────────────────┘
                                                 │
                                          ┌──────▼──────┐
                                          │  CF KV      │
                                          │ unicom/NRT  │
                                          │ unicom/SIN  │
                                          │ telecom/NRT │
                                          └─────────────┘
```

---

## 部署包清单

`deploy/` 目录（git 跟踪 .sh / .txt，binary 在 .gitignore 里）：

| 文件 | 作用 |
|---|---|
| `DEPLOY.md` | 详细部署手册（服务器+路由器） |
| `build.sh` | 跨编译 amd64 + arm64 两个 binary |
| `install.sh` | 通用安装脚本：detect 架构 → 写 env → 注册 cron（不复制任何文件） |
| `speedtest-and-upload.sh` | 主脚本：跑测速 + 上传 KV（合并了原 upload-to-kv.sh） |
| `.env.example` | env 模板（拷成 `.env` 后填 WORKER_URL + PUT_TOKEN） |
| `cf-ranges.txt` | IP 段配置（强制要求存在，每行 CIDR，支持 # 注释） |
| `cf-speed-pick-linux-amd64` | 服务器二进制（gitignore） |
| `cf-speed-pick-linux-arm64` | 路由器二进制（gitignore） |

---

## 一键流程（概要）

### 服务器（x86_64）

```bash
# 1. 拷部署包
scp -r deploy/ root@<server>:/opt/cf-speed-pick/

# 2. 注册 + 填 env
ssh root@<server>
cd /opt/cf-speed-pick
./install.sh                          # 首次：写 .env，提示编辑
vi .env                               # 填 WORKER_URL + PUT_TOKEN
./install.sh                          # 二次：注册 cron

# 3. 手动验证
./speedtest-and-upload.sh
```

### 路由器（armv7l / aarch64，U 盘）

```bash
scp -r deploy/ admin@<router>:/tmp/mnt/usb/cf-speed-pick/
ssh admin@<router>
cd /tmp/mnt/usb/cf-speed-pick
./install.sh                          # 用梅林的 cru 而非 crontab
vi .env
./install.sh
cru l | grep cf-speed-pick            # 验证注册成功
```

---

## cf-ranges.txt

**强制要求存在**（不再有内置 fallback）。deploy/ 里自带完整 CF 官方段（15 IPv4 + 7 IPv6），可编辑后生效。

```bash
# 格式：每行一个 CIDR，支持 # 注释和空行
#   173.245.48.0/20      # Cloudflare 官方
#   # 104.16.0.0/12      # 注释掉这行（不测）
#   1.0.0.0/24           # 追加自己的段
```

CF 官方段更新公告：<https://www.cloudflare.com/ips/>

---

## install.sh 行为（要点）

- **不复制任何文件**：当前目录就是"安装"位置
- **detect 架构**：`uname -m` → 选对应 binary（仅验证存在）
- **写 env**：从 .env.example 复制（如不存在），chmod 600，自动注入 OPERATOR
- **强制 cf-ranges.txt**：缺失就 hard-error（不再从 .example 兜底，因为文件已随 deploy/ 提供）
- **注册 cron**：梅林用 `cru`，标准 Linux 用 `crontab -l | ... | crontab -`
- **绝对路径**：cron 命令用 `SCRIPT_DIR/speedtest-and-upload.sh`，不依赖 PATH

---

## 与 R7000 时代的差异（迁移须知）

| 旧（已删） | 新 |
|---|---|
| 7 个脚本（cron × 3, install × 2, ensure, push, upload）| 2 个脚本（install + speedtest-and-upload）|
| `cf-ranges.txt.example`（需要用户 cp） | `cf-ranges.txt`（强制存在） |
| env 用 export + PATH | env 文件 source，cron 用绝对路径 |
| ShellCrash 直接代理测速 | ShellCrash 配 CF 段直连（避免污染） |
| DEPLOY-R7000.md | DEPLOY.md（顶层）+ deploy/DEPLOY.md（详细） |

---

## 详细文档

| 文档 | 内容 |
|---|---|
| [deploy/DEPLOY.md](deploy/DEPLOY.md) | 服务器+路由器详细步骤、参数、排错 |
| [deploy/build.sh](deploy/build.sh) | 跨编译两个架构 binary |
| [cf-worker/README.md](cf-worker/README.md) | Worker 部署说明（KV binding、PUT_TOKEN、FALLBACK_IPS） |
| [cf-worker/CLIENTS.md](cf-worker/CLIENTS.md) | 客户端怎么用订阅 URL |

---

## 排错（高频）

| 问题 | 排查 |
|---|---|
| `missing cf-ranges.txt` | `cp cf-ranges.txt <目标目录>`，或从 deploy/ 包里复制 |
| `missing .env` | `cp .env.example .env` + 编辑填 WORKER_URL + PUT_TOKEN |
| `binary not found` | 确认 `cf-speed-pick-linux-amd64` 在当前目录，且 `chmod +x` |
| `unsupported arch` | 服务器用 amd64 binary，路由器用 arm64 binary |
| `上传失败 401` | env 里的 PUT_TOKEN 不对 — 重新从 CF Dashboard 抄 |
| `上传失败 400 Bad operator` | env 里 OPERATOR 必须是 `unicom` 或 `telecom` |
| `测速被代理污染` | 在 ShellCrash / clash 配 CF 段直连（`172.64.0.0/13` 等） |
| `路由器的 cf-ranges.txt 被清` | 用 U 盘路径，不要放 JFFS |

更详细的排错表见 deploy/DEPLOY.md 末尾。
