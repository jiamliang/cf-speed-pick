# GitHub Actions 自动部署 — 一次性配置

## 总览

```
[push 代码到 GitHub]
        ↓
[GitHub Actions 触发]
        ↓
[Ubuntu runner 跑 wrangler deploy]
        ↓
[Worker 部署到 CF 边缘]
```

整个流程**不需要你本机/VPS 装 wrangler**。

---

## 一、CF 端：创建 API Token

1. 浏览器打开 https://dash.cloudflare.com/profile/api-tokens
2. 点 **"Create Token"**
3. 选模板 **"Edit Cloudflare Workers"**（图里左边第三个）
   - 如果模板列表没有，点 **"Get more templates"** 找，或者用 **"Create Custom Token"** 自己配权限
4. **Token name**：随便起，比如 `cf-speed-pick-deploy`
5. **Permissions**：
   - Account → Workers Scripts → Edit
   - Account → Workers KV Storage → Edit（如果以后要用 KV）
   - Account → Workers Routes → Edit（如果绑自定义域名）
   - Zone → Workers Routes → Edit（如果绑自定义域名且想用 Zone 级别路由）
6. **Account Resources**：选你的账号
7. **Zone Resources**（如果绑自定义域名）：Include → Specific zone → 选你的域名
8. 点 **"Continue to summary"** → **"Create Token"**
9. **复制 Token 值**（一长串，只显示一次！存到密码管理器里）

---

## 二、CF 端：拿到 Account ID

1. 打开 https://dash.cloudflare.com/
2. 登录后右下角有个 **"Account ID"** 字段
3. 点拷贝图标，记下来

---

## 三、生成 UUID（一次性）

**在本机或 VPS**：

```bash
UUID=$(uuidgen | tr 'A-Z' 'a-z')
echo "$UUID"
```

类似：`a1b2c3d4-e5f6-7890-abcd-ef1234567890`

**保存下来**，以后更新 Worker 配置要用，丢了得换新 UUID。

---

## 四、GitHub 端：建仓库

1. 打开 https://github.com/new
2. Repository name：`cf-speed-pick`（或别的）
3. **Private**（私有，避免泄露 wrangler.toml 配置）
4. **不要**勾选 "Initialize with README"（我们本地已有代码）
5. 点 **"Create repository"**

---

## 五、推代码

**在 VPS 上**：

```bash
cd /data/service/cloudflare/cf-speed-pick

# 初始化 git（如果还没）
git init
git add .
git commit -m "initial: cf-speed-pick + cf-worker"

# 添加 GitHub remote
git remote add origin git@github.com:你的用户名/cf-speed-pick.git
# 或用 HTTPS：git remote add origin https://github.com/你的用户名/cf-speed-pick.git

# 改分支名为 main
git branch -M main

# 推送
git push -u origin main
```

如果 VPS 之前没有配过 SSH key 给 GitHub，先配：

```bash
# 生成 key
ssh-keygen -t ed25519 -f ~/.ssh/github -N "" -C "cf-speed-pick vps"

# 把 ~/.ssh/github.pub 复制到 GitHub Settings → SSH and GPG keys → New SSH key
cat ~/.ssh/github.pub

# 加到 ~/.ssh/config
cat >> ~/.ssh/config <<'EOF'
Host github.com
    IdentityFile ~/.ssh/github
    IdentitiesOnly yes
EOF
```

---

## 六、GitHub 端：配 Secrets

1. 打开 GitHub 仓库页面
2. **Settings** → **Secrets and variables** → **Actions**
3. 点 **"New repository secret"**，依次添加：

| Name | Value |
|---|---|
| `CLOUDFLARE_API_TOKEN` | 第一步的 Token 值 |
| `CLOUDFLARE_ACCOUNT_ID` | 第二步的 Account ID |
| `VLESS_UUID` | 第三步的 UUID |

⚠️ **VLESS_UUID 是 secret**，跟 Token 一样只在 Actions 里可见。

---

## 七、触发部署

**首次部署**（两种方式）：

**方式 A：自动触发**
- 你刚才 `git push -u origin main` 已经把代码推上去了
- push 触发 workflow 自动跑
- 看：仓库页面 → Actions tab → 选 "Deploy Worker" 看日志

**方式 B：手动触发**
- 仓库页面 → Actions → 左侧 "Deploy Worker" → 右侧 "Run workflow" → 选 main → 点绿色按钮

---

## 八、看部署结果

跑完后：

1. **GitHub Actions 日志**：
   - 应该看到 `✨ Successfully published your Worker to ...`
   - 会给 URL，类似 `https://cf-speed-proxy.<子域>.workers.dev`

2. **CF Dashboard 验证**：
   - https://dash.cloudflare.com/ → Workers & Pages
   - 应该看到 `cf-speed-proxy` Worker

3. **健康检查**：
   ```bash
   curl https://cf-speed-proxy.<子域>.workers.dev/health
   ```
   返回 JSON 里的 `uuid_prefix` 应该跟你的 UUID 前 8 位一致。

---

## 九、以后更新 Worker

```bash
# 在 VPS 上改代码
vim cf-worker/_worker.js
# 或 vim wrangler.toml

git add cf-worker/
git commit -m "feat: 改了点啥"
git push origin main

# GitHub Actions 自动部署，30 秒后 CF 边缘生效
```

**修改 UUID**（比如换 UUID 提高安全性）：

1. 本机生成新 UUID：`NEW_UUID=$(uuidgen | tr 'A-Z' 'a-z')`
2. GitHub 仓库 Settings → Secrets → 改 `VLESS_UUID` 为新值
3. 手动触发一次 workflow（Settings 改 secret 不会自动触发 push）

---

## 十、绑自定义域名（可选）

`*.workers.dev` 在国内有时候被干扰。绑你的域名更稳。

编辑 `cf-worker/wrangler.toml`：

```toml
[[routes]]
pattern = "proxy.example.com/*"
custom_domain = true
```

提交 + push：

```bash
git add cf-worker/wrangler.toml
git commit -m "feat: 绑定自定义域名"
git push
```

CF 自动加 DNS + 签证书（前提：`example.com` 已接入 CF）。

---

## 常见问题

### Q: Actions 跑失败，看日志？
仓库 → Actions → 失败的 run → 点进去看具体哪步失败。

最常见错误：
- `Authentication error [code: 10000]` → Token 错了或权限不够
- `wrangler secret put VLESS_UUID` 失败 → VLESS_UUID secret 没设

### Q: 怎么知道 Worker 实际跑的是哪个版本？
```bash
curl https://cf-speed-proxy.<子域>.workers.dev/health
# 返回里有 "version": "0.1.0"
```

改 `_worker.js` 顶部的 `VERSION` 常量，部署后能看到版本变化。

### Q: VPS 上没有 git 仓库？
整个 `/data/service/cloudflare/cf-speed-pick/` 目录推上去即可：
- `cf-speed-pick/` Go 二进制代码（核心）
- `cf-worker/` Worker 代码
- `scripts/` shell 脚本
- `.github/workflows/` 部署 workflow

不需要推二进制文件（`cf-speed-pick-linux-*`），加进 `.gitignore`。

### Q: 仓库要不要公开？
**建议 Private**：
- `wrangler.toml` 里有 `UPSTREAM_CACHE_BASE`，泄露了别人知道你 VPS 域名
- 但**没**UUID（UUID 在 GitHub Secrets 里），所以即使仓库公开，UUID 不会泄露

### Q: 怎么回滚？
- CF Dashboard → Workers & Pages → cf-speed-proxy → Deployments
- 选上一个版本 → "Rollback"
