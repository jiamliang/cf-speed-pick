# deploy/ 部署手册

> 适用：服务器 (x86_64) 和路由器 (armv7l/aarch64)
> 全部 6 个文件放在一起使用

## 文件清单

```
deploy/
├── DEPLOY.md                      # 本文件
├── build.sh                       # 编译两个架构 binary（可选，git clone 后用）
├── install.sh                     # 通用安装脚本（detect 架构 + 注册 cron）
├── speedtest-and-upload.sh        # 主脚本：测速 + 上传 KV
├── env.example                    # env 模板（拷成 env 后填）
├── cf-ranges.txt                  # IP 段配置（可编辑，决定测哪些 CF 段）
├── cf-speed-pick-linux-amd64      # 服务器二进制（x86_64）
└── cf-speed-pick-linux-arm64      # 路由器二进制（aarch64 / armv7l）
```

---

## 服务器部署（x86_64）

### 一次性操作

```bash
# 1) 在你开发机上把整个 deploy/ 目录 scp 到服务器
scp -r deploy/ root@你的服务器IP:/opt/cf-speed-pick/

# 2) SSH 进去
ssh root@你的服务器IP
cd /opt/cf-speed-pick

# 3) 注册 cron（首次会写 env 模板）
./install.sh
# 提示：operator (telecom/unicom):   ← 输入 unicom 或 telecom

# 4) 编辑 env 填 WORKER_URL + PUT_TOKEN
vi env
#   WORKER_URL=https://vless.cf.peeweecap.com
#   PUT_TOKEN=<从 CF Dashboard 抄来的 32 字节 hex>
#   OPERATOR=unicom   ← install.sh 已经自动加

# 5) 再跑 install.sh 完成注册
./install.sh
#   registered via crontab: 0 */6 * * * /opt/cf-speed-pick/speedtest-and-upload.sh
```

### 手动跑一次验证

```bash
./speedtest-and-upload.sh
#   detect x86_64 → 选 amd64 binary → 跑完整测速 → 上传 KV
#   跑完约 5-15 分钟

# 看日志
tail -50 speedtest.log

# 验证 KV 里有数据
curl -s "https://vless.cf.peeweecap.com/sub?operator=unicom&colos=NRT&top=3"
# 应该返回 3 个 vless:// 用真实优选 IP
```

---

## 路由器部署（armv7l / aarch64）

> 重要：JFFS 重启会清空，必须放 U 盘！

```bash
# 1) 路由器挂载 U 盘（梅林固件通常自动挂 /tmp/mnt/usb/）

# 2) 把整个 deploy/ 目录复制到 U 盘
scp -r deploy/ admin@路由器IP:/tmp/mnt/usb/cf-speed-pick/
# 或者 USB 拷过去后插路由器

# 3) SSH 进去
ssh admin@路由器IP
cd /tmp/mnt/usb/cf-speed-pick

# 4) 注册 cron（用梅林的 cru）
./install.sh
# 提示：operator (telecom/unicom):   ← 输入 unicom

# 5) 编辑 env
vi env

# 6) 再跑 install.sh
./install.sh
#   registered via cru: cf-speed-pick -> /tmp/mnt/usb/cf-speed-pick/speedtest-and-upload.sh

# 7) 验证
cru l | grep cf-speed-pick
```

---

## 编辑 cf-ranges.txt（自定义测速段）

`cf-ranges.txt` 是 IP 段配置文件，**speedtest-and-upload.sh 强制要求它存在**。

```bash
vi cf-ranges.txt

# 格式：每行一个 CIDR，支持 # 注释和空行
# 例子：
#   173.245.48.0/20     # Cloudflare 官方
#   103.21.244.0/22
#   # 104.16.0.0/12     # 注释掉这行（不测）
#   1.0.0.0/24          # 追加自己的段

# 改完直接生效，下次跑测速自动用新段
./speedtest-and-upload.sh
```

**CF 官方段更新**：到 https://www.cloudflare.com/ips/ 看最新段，对照 `cf-ranges.txt` 加新段 / 删废弃段。

---

## 调整 cron 时间

默认每 6 小时跑一次。想改：

```bash
# VPS：
crontab -e
# 改：0 */6 * * * /opt/cf-speed-pick/speedtest-and-upload.sh
# 改成：0 */4 * * *   （每 4 小时）
# 或：0 3,15 * * *   （每天 3:00 和 15:00）

# 路由器：
cru d cf-speed-pick
cru a cf-speed-pick "0 */4 * * * /tmp/mnt/usb/cf-speed-pick/speedtest-and-upload.sh"
```

或者重跑 install.sh 时带参数：

```bash
./install.sh --operator unicom --cron "0 */4 * * *"
```

---

## 重新编译 binary（如果改了 Go 源码）

```bash
# 方式 1：本地有 go
./deploy/build.sh

# 方式 2：没装 go，用 Docker
docker run --rm -v "$(pwd):/src" -w /src golang:1.21-alpine sh /src/deploy/build.sh

# 编译完 deploy/ 里就有新的 binary
ls -lh deploy/cf-speed-pick-linux-*
```

---

## 排错

| 问题 | 排查 |
|---|---|
| `missing cf-ranges.txt` | `cp cf-ranges.txt /opt/cf-speed-pick/`，或从 deploy/ 包里复制 |
| `missing env` | `cp env.example env` + 编辑填 WORKER_URL + PUT_TOKEN |
| `binary not found` | 确认 `cf-speed-pick-linux-amd64` 在 deploy/ 目录，且 `chmod +x` |
| `unsupported arch` | 服务器用 amd64 binary，路由器用 arm64 binary |
| `上传失败 401` | env 里的 PUT_TOKEN 不对 — 重新从 CF Dashboard 抄 |
| `上传失败 400 Bad operator` | env 里 OPERATOR 写成 `unicom` / `telecom`（不是 china-unicom）|
| `测速被代理污染` | 在 ShellCrash / clash 配 CF 段直连（`172.64.0.0/13` 等）|
| `路由器的 cf-ranges.txt 被清` | 改用 U 盘路径，不要放 JFFS |
| `想看 cron 上次跑的结果` | `tail -50 speedtest.log` |
| `想要手动触发上传但不跑测速` | 用 `cf-speed-pick` 跑一次，再 `OPERATOR=unicom WORKER_URL=... PUT_TOKEN=... ./speedtest-and-upload.sh`（脚本会跳过 cf-speed-pick）— **不支持，目前脚本总是跑测速** |
