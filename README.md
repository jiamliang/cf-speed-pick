# cf-speed-pick

国内 → Cloudflare 优选 IP 工具。三阶段漏斗：TCPing → HTTPing → 下载测速。

## 背景

CF 在全球有数百个边缘节点，但**国内不同运营商访问不同节点的速度差异巨大**（电信/联通/移动各有最优解）。
gslege/CloudflareIP 把测速放在 GitHub Actions runner（美国）上跑，结果对国内用户毫无意义。
本工具在你**自己的国内机器**上跑完整三阶段，得到的是你**这条线路**的真实最优 IP。

---

## ⚠️ 测速前必须关闭科学上网 / VPN / 代理

测速机器本身开了代理，测出来的就是 **代理 → CF** 的延迟和速度，**不是** 你 → CF，结果完全没用。

检测方法：
```bash
curl https://ifconfig.me
# 应该返回你真实的国内 IP（如 121.x.x.x），不是香港/美国 IP
```

本工具启动时会检查 `HTTP_PROXY` / `HTTPS_PROXY` / `ALL_PROXY` 环境变量，以及 `~/.gitconfig` 的 proxy 配置，
检测到任意一个就直接拒绝运行。

---

## 工作原理（三阶段漏斗）

```
IP 池（CF 官方 + gslege + XIU2 三方合并去重）
   ~ 30,000 IPv4 IP
   │
   ▼
┌─────────────────────────────────────────┐
│ Layer 1: TCPing                         │
│ 对每个 IP TCP 443 握手 4 次             │
│ 并发 200，1s 超时                         │
│ 过滤：延迟 ≤ 200ms && 丢包率 ≤ 20%      │
│ 输出：~ 1,000 IP                        │
└─────────────────────────────────────────┘
   │
   ▼
┌─────────────────────────────────────────┐
│ Layer 2: HTTPing                        │
│ HTTP HEAD 请求到 CF-fronted 域名         │
│ 从 cf-ray 头提取机场三字码（IATA）        │
│ 并发 50，2s 超时                         │
│ 过滤：状态码 200/301/302 + 能拿 colo     │
│ 输出：~ 300 IP（带 colo 标签）            │
└─────────────────────────────────────────┘
   │
   ▼
┌─────────────────────────────────────────┐
│ Layer 3: 下载测速                         │
│ 真实下载 100MB+ 文件                      │
│ EWMA 平滑字节流速度                       │
│ 单 IP 跑 10s                             │
│ 取 Top N（默认 10）                       │
└─────────────────────────────────────────┘
   │
   ▼
out/03_top.csv
IP, download_speed_MBps, colo, delay_ms, loss_rate, timestamp
```

### Layer 3 colo 优先级分层

Layer 3 是单线程顺序测速，全跑一遍 500+ IP 太慢。按 colo（机场三字码）地理距离**分层采样**：

| Tier | colo（默认）| 采样比例 | 说明 |
|---|---|---|---|
| 1 | NRT, ICN, KIX, TPE, HKG, MFM, FUK | **100%** | 东北亚，最快 |
| 2 | SIN, BKK, SGN, MNL, KUL, CGK | **50%** | 东南亚 |
| 3 | LAX, SJC, SEA, PDX, DEN | **30%** | 美西（CN2 可能有惊喜）|
| Other | 其他 | 10% | 兜底 |

**自定优先级**：

```bash
# 自定 Tier 1（覆盖默认值）
./cf-speed-pick -colo-priority "HKG,SIN,KIX" -colo-strategy tiered

# 只测亚洲（跳过非优先级 colo）
./cf-speed-pick -colo-priority "NRT,ICN,KIX,TPE,HKG,SIN" -colo-strategy only

# 全平等（不推荐，慢）
./cf-speed-pick -colo-strategy all
```

**输出按 tier 排序**：最终 Top N 里 Tier 1 的 IP 优先排前，同 tier 内按下载速度降序。

---

## 编译

零外部依赖（`go.mod` 只声明 module name，没有 require）。

### 用 Docker 编译（推荐，避免本地装 Go）

```bash
# 自带 ./go-docker.sh 包装脚本
./go-docker.sh build -o ./cf-speed-pick .

# 交叉编译 linux/amd64
./go-docker.sh build -o ./cf-speed-pick-linux-amd64 .

# 交叉编译 linux/arm64（华硕 RT-AC86U 等 ARM64 路由器）
GOOS=linux GOARCH=arm64 ./go-docker.sh build -o ./cf-speed-pick-linux-arm64 .
```

### 本地 Go 工具链

```bash
make build              # 当前平台
make build-amd64        # linux/amd64
make build-arm64        # linux/arm64
make build-all          # 两个都编译
```

输出单文件 ~3MB（Go 1.21 默认带 DWARF 调试信息，`-ldflags="-s -w"` 可缩到 ~2MB）。

---

## 使用

### 最简调用（默认参数）
```bash
./cf-speed-pick
# 自动跑完整三层，输出到 ./out/03_top.csv
```

### 路由器低配（AC86U）
```bash
./cf-speed-pick -n 50 -t 2 -dn 5 -dt 5s
```

### 只跑到 Layer 2（更快，跳过下载测速）
```bash
./cf-speed-pick -skip-download
```

### 自建测速地址
```bash
# 必须是一个 CF-fronted 的域名下的可下载文件
./cf-speed-pick -url https://your-cf-domain.com/test-100mb.bin
```

### 只保留特定地区的 colo
```bash
./cf-speed-pick -colo HKG,NRT,SJC,FRA
# 只保留香港/东京/圣何塞/法兰克福节点
```

### IPv6
```bash
./cf-speed-pick -ipv6 -ipv6-sample 2000
```

---

## cron 部署

```bash
# 把二进制扔到 /usr/local/bin/
sudo cp ./cf-speed-pick-linux-amd64 /usr/local/bin/cf-speed-pick
sudo chmod +x /usr/local/bin/cf-speed-pick

# 用提供的 run.sh（自动检测 VPS / 路由器环境）
sudo cp ./run.sh /usr/local/bin/cf-speed-pick.sh
sudo chmod +x /usr/local/bin/cf-speed-pick.sh

# 创建输出目录
sudo mkdir -p /var/lib/cf-speed-pick/out

# 加 cron（每 2 小时一次）
crontab -e
# 加：
0 */2 * * * /usr/local/bin/cf-speed-pick.sh
```

### 路由器（华硕 RT-AC86U）

```bash
scp ./cf-speed-pick-linux-arm64 router:/jffs/cf-speed-pick
ssh router "chmod +x /jffs/cf-speed-pick"

# 路由器版 cron（在梅林固件里）
cru a cf-speed-pick "0 */2 * * * /jffs/cf-speed-pick -n 50 -t 2 -dn 5 -dt 5s -out /jffs/cf-speed-pick/out"
```

---

## CSV 输出格式

### `out/03_top.csv`（最终 Top N）
```csv
ip,download_speed_MBps,colo,delay_ms,loss_rate,timestamp
162.158.0.1,12.34,SJC,30,0.0000,2026-09-14T10:00:00+08:00
172.64.1.2,11.89,NRT,45,0.0000,2026-09-14T10:00:00+08:00
...
```

### `out/01_tcping.csv`（Layer 1 中间结果，-debug 时输出）
```csv
ip,sended,received,loss_rate,delay_ms,colo,timestamp,source
104.16.1.1,4,4,0.0000,30,,2026-09-14T10:00:00+08:00,layer1
...
```

### `out/02_httping.csv`（Layer 2 中间结果，-debug 时输出）
```csv
ip,sended,received,loss_rate,delay_ms,colo,timestamp,source
162.158.0.1,4,4,0.0000,30,SJC,2026-09-14T10:00:00+08:00,layer2
...
```

---

## 测试

```bash
# 单元测试（不需要网络）
./go-docker.sh test -short ./...

# 完整测试（含 1.1.1.1 网络探测，会被防火墙拦截则 skip）
./go-docker.sh test ./...
```

---

## LICENSE

GPL-3.0（继承自 XIU2/CloudflareSpeedTest）。

代码大量参考了：
- [XIU2/CloudflareSpeedTest](https://github.com/XIU2/CloudflareSpeedTest) — TCPing/HTTPing/下载测速核心算法
- [gslege/CloudflareIP](https://github.com/gslege/CloudflareIP) — CF IP 段清单参考

由于 XIU2 是 GPL-3.0 协议，复制其代码的本项目也必须是 GPL-3.0（强 copyleft）。

---

## 验证清单

- [x] `cat go.mod` 只有 module 声明，无 require
- [x] `make build-all` 输出 amd64 + arm64 两个静态二进制
- [x] `HTTP_PROXY=... ./cf-speed-pick` 立即报错退出
- [x] `./cf-speed-pick -h` 帮助输出完整
- [x] IPv4 池大小 27,000~30,000（cf 官方 + xiu2 段合并去重）
- [x] IPv6 默认禁用，需要 `-ipv6` 才启用
- [x] EWMA 自写实现（3 个测试用例）
- [x] cf-ray → IATA 提取正则（3 个测试用例）
- [x] CSV 读写 roundtrip 正确