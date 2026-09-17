# 配置 GitHub Actions 自动部署 cf-docker-proxy

让 GitHub Actions 在你 `git push` 到 main 时自动把 `docker-proxy-worker/` 部署到 Cloudflare。

**前置条件**：

- 已经按 [DEPLOY-WORKER.md](./DEPLOY-WORKER.md) 至少成功部署过一次（routes、DNS 都齐）
- 你有 `jiamliang/cf-speed-pick` 仓库的 Admin 权限（能改 Secrets）

---

## 一、CF API Token

在 CF Dashboard 创建专用 token：

### 路径 A：用 "Edit zone DNS" 模板（推荐）

1. 打开 https://dash.cloudflare.com/profile/api-tokens
2. 点 **Create Token**
3. 模板列表选 **"Edit zone DNS"** → 右侧 **"Use template"**
4. **Zone Resources** 改成：
   ```
   Include → Specific zone → peeweecap.com → Edit
   ```
5. **TTL** 默认就行，下一步
6. **Continue to summary** → **Create Token**
7. 弹窗**只显示这一次**完整 token 字符串（`cfut_` 开头），点 **Copy to clipboard**

### 路径 B：Custom token

1. 模板列表选 **"Custom token"** → **Get started**
2. Permissions:
   - Account → **Workers Scripts: Edit**
   - Account → **Workers Routes: Edit**
   - Zone → **DNS: Edit**
3. Zone Resources: `Specific zone → peeweecap.com → Edit`
4. **Continue to summary** → **Create Token** → 复制 token

**为什么需要 Workers Scripts + Workers Routes + Zone DNS 三件套**：
- Workers Scripts:Edit — wrangler 上传 `cf-docker-proxy` bundle
- Workers Routes:Edit — wrangler 注册 8 条 `[[routes]]`（虽然 10013 时会失败，但脚本上传阶段会用到 verify）
- Zone DNS:Edit — 手工建 8 条 A 记录时需要（自动化部署可选；如果只用 CI 部署已有 Worker，可以不放 DNS scope）

---

## 二、CF Account ID

1. 打开 https://dash.cloudflare.com/
2. 首页右下角 "Account ID" → 点 **"Copy"**
3. 或者本机：
   ```bash
   npx wrangler whoami | grep "Account ID"
   ```

---

## 三、GitHub Secrets

1. 打开 https://github.com/jiamliang/cf-speed-pick/settings/secrets/actions
2. 点 **"New repository secret"**
3. 加两个 secret：

| Name | Value |
|---|---|
| `CLOUDFLARE_API_TOKEN` | 步骤一末尾复制的 token 字符串 |
| `CLOUDFLARE_ACCOUNT_ID` | 步骤二拿到的 Account ID |

**注意**：docker-proxy-worker **不需要** `VLESS_UUID` 或 `PUT_TOKEN`（那是 cf-worker vless 翻墙用的）。

---

## 四、触发 workflow

打开 https://github.com/jiamliang/cf-speed-pick/actions/workflows/deploy-worker.yml

### 手动触发（推荐）

1. 右侧 **"Run workflow"** 按钮
2. Branch: `main`
3. **"Worker 目录路径"** dropdown：**选 `docker-proxy-worker`**
4. 绿色 **"Run workflow"** 确认

30 秒后看日志：✅ Deploy docker-proxy-worker job 应为绿色。

### push 触发（🔴 暂不可靠）

理论上改 `docker-proxy-worker/**` 推 main 会自动跑。实际上**不会**：见 [DEPLOY-WORKER.md](./DEPLOY-WORKER.md#八ci-部署github-actions) 末尾的 detect grep bug —— push 触发只会去部署 cf-worker。

**正确用法**：改完 `docker-proxy-worker/**` 后 `git push`，**然后**去 Actions 页面手动触发。

---

## 五、常见问题

| 问题 | 排查 |
|---|---|
| Actions 报 `CLOUDFLARE_API_TOKEN not set` | 仓库 secrets 没加。回步骤三。 |
| Actions 报 `Authentication error [code: 10000]` | token scope 缺。CF Dashboard 编辑这个 token，确认 Zone Resources 选了 `Edit` 且 DNS capability 勾上 |
| Actions 报 `code 10013 subdomain operation failed` | wrangler 批量 routes 注册 bug。脚本其实已上传，routes 重复 POST 即可补齐 |
| Actions 卡在 `npm ci` 报错 | 缺 `package-lock.json`。本机 `cd docker-proxy-worker && npm install` 生成 lockfile 并提交 |
| push 改了 docker-proxy/** 但 Actions 部署了 cf-worker | detect job grep bug。改用手动触发 + dropdown 显式选 `docker-proxy-worker` |
| Actions 看到 8 条 routes 都 Published 但 CF 后台只有部分 | wrangler 10013 bug 截断了。手工 API POST 漏掉的 routes（幂等） |

---

## 参考

- [DEPLOY-WORKER.md](./DEPLOY-WORKER.md) — 完整部署文档
- [TROUBLESHOOTING.md](./TROUBLESHOOTING.md) — 症状 → 根因速查
- [README.md](./README.md) — 项目总览