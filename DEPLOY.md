# 部署手册

> 适用：服务器 (x86_64) 和路由器 (armv7l/aarch64)
> 部署包：`deploy/` 目录

## 文件清单

```
deploy/
├── cf-speed-pick-linux-amd64    # 服务器二进制
├── cf-speed-pick-linux-arm64    # 路由器二进制
├── install.sh                   # 通用安装脚本（detect 架构）
├── speedtest-and-upload.sh      # 主脚本（测速 + 上传 KV）
├── env.example                  # env 模板
└── cf-ranges.txt.example        # IP 段示例（含注释）
```

## 服务器部署（x86_64）

```bash
# 1. 在你开发机上把整个 deploy/ 目录 scp 到服务器
scp -r deploy/ root@你的服务器IP:/opt/cf-speed-pick/

# 2. SSH 进去
ssh root@你的服务器IP

# 3. 进目录
cd /opt/cf-speed-pick

# 4. 注册 cron（不复制文件，只在当前目录操作）
./install.sh
#   会问：operator (telecom/unicom):  ← 输入 unicom 或 telecom
#   会写：env 文件（从 env.example 复制）
#
# 5. 编辑 env 填 WORKER_URL + PUT_TOKEN
vi env
#   WORKER_URL=https://vless.cf.peeweecap.com
#   PUT_TOKEN=<从 Worker CF Dashboard 抄来的 hex>
#   OPERATOR=unicom  ← install.sh 已经自动加

# 6. 再跑一次 install.sh 注册 cron
./install.sh
#   检测到 env 完整 → 注册 cron (crontab)
#   输出：registered via crontab: 0 */6 * * * /opt/cf-speed-pick/speedtest-and-upload.sh

# 7. 手动跑一次验证
./speedtest-and-upload.sh
#   会：detect x86_64 → 选 amd64 binary → 跑完整测速 → 上传 KV
#   跑完看 out/ 目录和速度测试日志
tail -50 speedtest.log

# 8. 验证 KV 里有数据
curl -s "https://vless.cf.peeweecap.com/sub?operator=unicom&colos=NRT&top=3"
#   应该看到 3 个 vless:// 用真实优选 IP
```

## 路由器部署（armv7l / aarch64）

```bash
# 1. 路由器挂载 U 盘（梅林固件通常自动挂 /tmp/mnt/usb/）

# 2. 把 deploy/ 目录复制到 U 盘
scp -r deploy/ admin@路由器IP:/tmp/mnt/usb/cf-speed-pick/
# 或者 USB 拷过去后插路由器

# 3. SSH 进去
ssh admin@路由器IP

# 4. 进目录
cd /tmp/mnt/usb/cf-speed-pick

# 5. 注册 cron（用梅林的 cru）
./install.sh
#   会问：operator (telecom/unicom):  ← 输入 unicom
#   会写：env 文件

# 6. 编辑 env
vi env

# 7. 再跑 install.sh 注册 cron
./install.sh
#   检测到 cru 命令 → 用 cru a 注册

# 8. 验证
cru l | grep cf-speed-pick
#   应该看到一行：xxx x x x x cf-speed-pick /tmp/mnt/usb/cf-speed-pick/speedtest-and-upload.sh
```

## 自定义 IP 段

`cf-ranges.txt` 是 IP 段配置文件（默认从内置段 fallback）：

```bash
# 编辑 cf-ranges.txt
vi cf-ranges.txt

# 格式：每行一个 CIDR，支持 # 注释和空行
# 例子：
#   173.245.48.0/20      # Cloudflare 官方
#   103.21.244.0/22
#   # 104.16.0.0/12     # 注释掉这行（不测）

# 重新跑测速时 speedtest-and-upload.sh 自动用这个文件
./speedtest-and-upload.sh
```

## cron 时间调整

```bash
# 默认每 6 小时跑一次
# 想改：编辑 install.sh 时的 --cron 参数，或手动改 crontab

# 改 VPS crontab
crontab -e
# 改：0 */6 * * * /opt/cf-speed-pick/speedtest-and-upload.sh
# 改成：0 */4 * * *   （每 4 小时）
# 或：   0 3,15 * * * （每天 3:00 和 15:00）

# 改路由器 cru
cru d cf-speed-pick
cru a cf-speed-pick "0 */4 * * * /tmp/mnt/usb/cf-speed-pick/speedtest-and-upload.sh"
```

## 排错

| 问题 | 排查 |
|---|---|
| `binary not found` | 确认 `cf-speed-pick-linux-amd64` 或 `cf-speed-pick-linux-arm64` 在当前目录 |
| `missing env` | `cp env.example env` + 编辑填 WORKER_URL + PUT_TOKEN |
| `unsupported arch` | 在错的机器上跑了 — 服务器用 amd64，路由器用 arm64 |
| `上传失败 401` | env 里的 PUT_TOKEN 不对 — 重新从 CF Dashboard 抄 |
| `测速被代理污染` | 加 CF 段到 ShellCrash 直连（见 SHELLCRASH-SETUP.md）|
| `路由器的 cf-ranges.txt 被清` | 用 U 盘，不要放 JFFS |
| 想看 cron 上次跑的结果 | `tail -50 speedtest.log` |
