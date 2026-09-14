# 在 R7000 / AC86U（梅林固件）上部署 cf-speed-pick

适用固件：梅林原版 / Koolshare 改版 / Merlin-clash / 其他基于 Asuswrt 的梅林衍生固件。

---

## 一、硬件架构

- **R7000** (Koolshare 梅林)：ARM Cortex-A9 双核 1.0GHz / 512MB RAM
- **AC86U** (梅林原版)：ARM Cortex-A53 四核 1.8GHz / 512MB RAM

两个路由器都是 **ARMv7 / ARMv8** 架构，需要交叉编译对应的 Go 二进制。

---

## 二、交叉编译

```bash
# 在开发机（普通 Linux VPS 或你自己的机器）
cd cf-speed-pick

# 编译 ARMv7（Koolshare R7000、AC66U、AC68U 等老路由器）
make build-armv7  # 注意：当前 Makefile 只有 amd64/arm64，参考下面手动编译

# 编译 ARM64（AC86U、RT-AX86U、GT-AX11000 等新路由器）
make build-arm64

# 手动编译（Makefile 没支持的目标）
docker run --rm \
    -v "$(pwd):/work" -w /work \
    -e CGO_ENABLED=0 -e GOOS=linux -e GOARCH=arm \
    golang:1.21-alpine \
    go build -ldflags="-s -w" -o cf-speed-pick-linux-arm .

docker run --rm \
    -v "$(pwd):/work" -w /work \
    -e CGO_ENABLED=0 -e GOOS=linux -e GOARCH=arm64 \
    golang:1.21-alpine \
    go build -ldflags="-s -w" -o cf-speed-pick-linux-arm64 .
```

输出文件名对应固件架构：

| 固件架构 | 路由器型号 | 二进制 |
|---|---|---|
| ARMv7 | R7000 / AC68U / AC66U | `cf-speed-pick-linux-arm` |
| ARM64 | AC86U / AX86U / AX11000 | `cf-speed-pick-linux-arm64` |

不确定？SSH 进路由器看：

```bash
uname -m
# armv7l  → 用 cf-speed-pick-linux-arm
# aarch64 → 用 cf-speed-pick-linux-arm64
```

---

## 三、推到路由器

```bash
# 假设路由器 Tailscale IP 是 100.100.100.10
ROUTER_IP=100.100.100.10

# 创建部署目录（梅林的 /jffs 是持久化目录，重启不丢）
ssh admin@$ROUTER_IP "mkdir -p /jffs/cf-speed-pick/{bin,out,log}"

# 推二进制
scp ./cf-speed-pick-linux-arm admin@$ROUTER_IP:/jffs/cf-speed-pick/bin/cf-speed-pick
# (AC86U 用 cf-speed-pick-linux-arm64)

# 加执行权限
ssh admin@$ROUTER_IP "chmod +x /jffs/cf-speed-pick/bin/cf-speed-pick"

# 验证
ssh admin@$ROUTER_IP "/jffs/cf-speed-pick/bin/cf-speed-pick -version"
```

---

## 四、Tailscale + SSH 免密登录

### 4.1 在路由器装 Tailscale（首次）

```bash
# SSH 进路由器
ssh admin@100.100.100.10

# 装 Tailscale（梅林用 entware）
opkg update
opkg install tailscale

# 启动并加入网络
tailscale up
# 复制显示的登录 URL，去浏览器授权
```

### 4.2 在 VPS 上配 SSH 免密登录路由器

```bash
# VPS 上生成专用 key（避免跟其他用途混）
ssh-keygen -t ed25519 -f ~/.ssh/cf-router -N "" -C "cf-speed-pick to R7000"

# 把公钥推到路由器 admin 用户
# 注意：梅林的 SSH key 默认在 /jffs/.ssh/authorized_keys
ssh admin@100.100.100.10 "mkdir -p /jffs/.ssh"
cat ~/.ssh/cf-router.pub | ssh admin@100.100.100.10 "cat >> /jffs/.ssh/authorized_keys"

# VPS 上配置 ~/.ssh/config
cat >> ~/.ssh/config <<'EOF'
Host cf-r7000
    HostName 100.100.100.10
    User admin
    IdentityFile ~/.ssh/cf-router
    IdentitiesOnly yes
    ServerAliveInterval 60
EOF

# 测试
ssh cf-r7000 "uname -m; uptime"
```

### 4.3 路由器部署 server 公钥（让路由器能 scp 到 VPS）

```bash
# VPS 上启 ssh server（应该已开）
# VPS 上确认 ~/.ssh/authorized_keys 有自己的公钥
cat ~/.ssh/cf-router.pub  # 这把 key

# 把 VPS 的公钥（通常是 VPS 默认 user 的 ~/.ssh/id_*.pub）加到路由器的 known_hosts
ssh admin@100.100.100.10 "mkdir -p /jffs/.ssh && cp /etc/ssh/ssh_host_ecdsa_key.pub /jffs/.ssh/known_hosts 2>/dev/null || true"
```

实际上更简单：让路由器用 sshpass 或者直接配置 VPS 的 SSH 服务允许 key 登录。

---

## 五、cron 配置

### 5.1 R7000 上的 cron

梅林的计划任务不在 `/etc/crontab`，而是用 `cru` 命令（写到 NVRAM）。

```bash
# SSH 到路由器
ssh cf-r7000

# 添加 cron（每 6 小时跑一次完整三层）
cru a cf-speed-pick-full "0 */6 * * * /jffs/cf-speed-pick/bin/cf-speed-pick -out /jffs/cf-speed-pick/out/full -debug -t 1 -max-delay 100 -n 50 -colo-strategy tiered -dt 5s -dn 10 -output-colos"

# 列出当前 cron
cru l

# 删除（如果要重设）
cru d cf-speed-pick-full
```

⚠️ **注意：梅林的 cron 在 NVRAM，Web UI 里改其他设置时会重置 NVRAM，可能丢 cron。** 建议用脚本定期检查 + 补回。

### 5.2 完整三层跑完后，路由器把结果推到 VPS

```bash
# 路由器上创建 push 脚本
cat > /jffs/cf-speed-pick/bin/push-to-vps.sh <<'EOF'
#!/bin/sh
# 把最新结果推到 VPS
VPS_USER="root"          # 或你的 VPS 用户名
VPS_HOST="100.100.100.1"  # VPS Tailscale IP
DATE=$(date +%Y%m%d-%H%M%S)

# 推送最近一次完整结果
if [ -f /jffs/cf-speed-pick/out/full/03_top.csv ]; then
    ssh $VPS_USER@$VPS_HOST "mkdir -p /var/lib/cf-speed-pick/results/R7000"
    scp /jffs/cf-speed-pick/out/full/03_top.csv $VPS_USER@$VPS_HOST:/var/lib/cf-speed-pick/results/R7000/${DATE}.csv
fi

# 推送 colo 分桶结果
if [ -d /jffs/cf-speed-pick/out/full/colo ]; then
    ssh $VPS_USER@$VPS_HOST "mkdir -p /var/lib/cf-speed-pick/cache"
    scp -r /jffs/cf-speed-pick/out/full/colo/* $VPS_USER@$VPS_HOST:/var/lib/cf-speed-pick/cache/ 2>/dev/null
fi

# 触发 VPS 重载 nginx（如果 VPS 用 nginx 提供 CSV）
ssh $VPS_USER@$VPS_HOST "nginx -s reload 2>/dev/null; true"
EOF

chmod +x /jffs/cf-speed-pick/bin/push-to-vps.sh
```

把 push 加到 cron：

```bash
cru a cf-speed-pick-push "30 */6 * * * /jffs/cf-speed-pick/bin/push-to-vps.sh >> /jffs/cf-speed-pick/log/push.log 2>&1"
```

---

## 六、VPS（中央）端配置

### 6.1 nginx 暴露 /var/lib/cf-speed-pick/cache

```nginx
# /etc/nginx/sites-available/cf-speed-pick
server {
    listen 80;
    server_name cf.example.com;  # 换成你的域名，或用 Tailscale IP

    # 启用 CORS，方便其他节点 GET
    add_header Access-Control-Allow-Origin *;

    location /cache/ {
        alias /var/lib/cf-speed-pick/cache/;
        autoindex on;
    }

    location /results/ {
        alias /var/lib/cf-speed-pick/results/;
        autoindex on;
    }
}
```

```bash
sudo ln -s /etc/nginx/sites-available/cf-speed-pick /etc/nginx/sites-enabled/
sudo nginx -t && sudo nginx -s reload
```

### 6.2 VPS 跑自己的 cron（青岛/福州 VPS）

VPS 自己也要跑 L3，并把结果也推到中央 VPS（如果有多个 VPS）。

简单方案：每个 VPS 都跑相同 cron，结果各自暴露 nginx，互相拉取。

---

## 七、其他节点怎么订阅

AC86U / OpenWrt 想用 R7000 的结果：

```bash
# 1. 拉 VPS 暴露的 colo 候选
wget https://cf.example.com/cache/NRT.csv -O /tmp/nrt.csv

# 2. 跑本地 L1 校准（30 秒）
/jffs/cf-speed-pick/bin/cf-speed-pick \
    -input /tmp/nrt.csv \
    -skip-httping \
    -out /jffs/cf-speed-pick/local \
    -n 50 -t 2 -max-delay 300 \
    -colo-strategy only \
    -colo-priority "NRT,ICN,KIX" \
    -dt 5s -dn 5

# 3. 写出的 03_top.csv 就是本地的 Top 5 IP
# 4. ShellCrash 读 /jffs/cf-speed-pick/local/03_top.csv 当 fallback
```

---

## 八、完整部署清单（一个最小 R7000 配置）

```bash
# === 路由器上 ===
mkdir -p /jffs/cf-speed-pick/{bin,out,log}

/jffs/cf-speed-pick/bin/cf-speed-pick -version
# 应该输出 cf-speed-pick v0.1.0

# 添加 cron（跑完整三层 + 推送到 VPS）
cru a cf-speed-pick-full "0 */6 * * * /jffs/cf-speed-pick/bin/cf-speed-pick -out /jffs/cf-speed-pick/out/full -debug -t 1 -max-delay 100 -n 50 -colo-strategy tiered -dt 5s -dn 10 -output-colos"
cru a cf-speed-pick-push "30 */6 * * * /jffs/cf-speed-pick/bin/push-to-vps.sh"

cru l

# === VPS 上 ===
sudo mkdir -p /var/lib/cf-speed-pick/{cache,results}
# 配置 nginx（如上）
sudo nginx -s reload

# === 验证 ===
# 1. 等 cron 跑一次
# 2. 看 VPS /var/lib/cf-speed-pick/cache/NRT.csv 有内容
# 3. curl http://cf.example.com/cache/NRT.csv 应该能下载
```

---

## 九、常见问题

### Q: 路由器跑 L3 太慢怎么办？

把 `dt` 调短：

```bash
# 路由器版：每 IP 3s，少测一些 IP
cru a cf-speed-pick-full "0 */6 * * * /jffs/cf-speed-pick/bin/cf-speed-pick -out /jffs/cf-speed-pick/out/full -debug -t 1 -max-delay 100 -n 50 -colo-strategy only -colo-priority 'NRT,ICN,KIX' -dt 3s -dn 5 -output-colos"
```

### Q: 路由器 CPU 太弱，n=50 都跑不动？

降到 `n=20`：

```bash
... -n 20 ...
```

### Q: VPS 推送失败？

检查：
1. 路由器能 ping 通 VPS（Tailscale 网络）
2. VPS 端 SSH 服务允许 key 登录
3. VPS 端 `~/.ssh/authorized_keys` 有 VPS 公钥
4. push 脚本里 `VPS_USER` / `VPS_HOST` 对了

### Q: nginx 没收到新结果？

cron 推完后，nginx 默认会读到磁盘文件，不需要 reload。但如果加了缓存层，需要手动 reload：

```bash
ssh vps "nginx -s reload"
```

### Q: 梅林固件 Web UI 修改后 cron 丢了？

```bash
# 重新加
cru a cf-speed-pick-full "..."
```

建议做一个 watchdog（路由器上定期检查并补回）：

```bash
cat > /jffs/cf-speed-pick/bin/ensure-cron.sh <<'EOF'
#!/bin/sh
# 每 30 分钟检查一次 cron 是否还在
if ! cru l | grep -q cf-speed-pick-full; then
    cru a cf-speed-pick-full "0 */6 * * * /jffs/cf-speed-pick/bin/cf-speed-pick -out /jffs/cf-speed-pick/out/full -debug -t 1 -max-delay 100 -n 50 -colo-strategy tiered -dt 5s -dn 10 -output-colos"
    echo "$(date) cron 丢失，已恢复" >> /jffs/cf-speed-pick/log/cron-watchdog.log
fi
EOF
chmod +x /jffs/cf-speed-pick/bin/ensure-cron.sh

# 加一个每 30 分钟检查的 cron
cru a cf-speed-pick-watchdog "*/30 * * * * /jffs/cf-speed-pick/bin/ensure-cron.sh"
```