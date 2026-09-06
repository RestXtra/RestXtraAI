# RestXtra AI Demo 宝塔部署完整教程

本文采用以下发布链路：

```text
本地源码 -> 本地构建纯静态 Demo -> ZIP 发布包 -> 宝塔上传 -> Nginx 静态托管
```

Demo **不提交、不推送到远程 Git，也不在服务器拉取源码**。服务器只保存构建后的静态文件，因此不需要 Go、Node.js、Docker、PostgreSQL、LLM Key、Skills 或 MCP 配置。

## 1. Demo 的功能与安全边界

Demo 使用 `NEXT_PUBLIC_MOCK=1` 构建，包含：

- 资产覆盖图；
- 漏洞摘要、证据、完整报告和链路图；
- Agent Event Log、Working Set、回合成本和性能基线；
- RBAC、审计、沙箱主机、容器、镜像和出口规则；
- PostgreSQL 与控制面容器的只读展示和操作隔离。

它不连接真实后端。浏览器中的新增、修改和删除只产生模拟结果，刷新后恢复内置数据。部署时遵守以下边界：

- 不上传项目源码、`.git`、数据库、Skills、MCP、日志和真实客户数据；
- 不上传 `.env`、Cookie、API Key、数据库 DSN 或私钥；
- Nginx 必须拒绝 `/api/`，防止 Demo 意外连接真实控制面；
- 云安全组不开放 `5432`、`2375`、`2376`、`8080` 或 `8787`；
- 只将 `web/out` 中的静态产物打包上传。

## 2. 准备域名和服务器

### 2.1 DNS

在域名服务商处添加记录：

```text
记录类型：A
主机记录：demo
记录值：云服务器公网 IP
```

例如最终域名为 `demo.example.com`。DNS 生效可在本地检查：

```powershell
Resolve-DnsName demo.example.com
```

结果应包含云服务器公网 IP。

### 2.2 云安全组

入方向只开放：

| 端口 | 用途 | 来源建议 |
| --- | --- | --- |
| `22/TCP` | SSH | 尽量限制为管理者 IP |
| `80/TCP` | HTTP 和证书签发 | `0.0.0.0/0`、`::/0` |
| `443/TCP` | HTTPS | `0.0.0.0/0`、`::/0` |

不要为静态 Demo 开放数据库、Docker API 或应用后端端口。

### 2.3 宝塔环境

在宝塔的“软件商店”安装 Nginx。纯静态 Demo 不需要 PHP、MySQL、PostgreSQL、Docker 或 Node.js。

## 3. 在本地 Windows 构建 Demo

建议使用项目当前兼容的 Node.js 版本，并确保本地已安装 npm。打开 PowerShell：

```powershell
cd F:\work-space\AI\AI-pentest\RestXtraAI\web
npm ci

$env:NEXT_PUBLIC_MOCK = "1"
try {
    npm run build:static
} finally {
    Remove-Item Env:NEXT_PUBLIC_MOCK -ErrorAction SilentlyContinue
}
```

`NEXT_PUBLIC_MOCK` 是构建期变量。缺少它时，页面可能请求真实 API，因此不能先普通构建再补变量。

验证关键静态页面：

```powershell
Test-Path .\out\index.html
Test-Path .\out\dashboard\index.html
Test-Path .\out\function\findings\index.html
Test-Path .\out\sandbox\containers\index.html
```

四项都应返回 `True`。然后生成带时间戳的发布包：

```powershell
$release = Get-Date -Format "yyyyMMdd-HHmmss"
$package = "..\restxtra-demo-$release.zip"
Compress-Archive -Path .\out\* -DestinationPath $package
Get-Item $package | Select-Object FullName, Length, LastWriteTime
```

发布包位于项目根目录。ZIP 内第一层应直接包含 `index.html`、`dashboard/` 和 `_next/`，不能再套一层 `out/`。

## 4. 在宝塔建立发布目录

打开“宝塔 -> 终端”，或使用 SSH 登录服务器，执行：

```bash
mkdir -p /www/wwwroot/restxtra-demo/packages
mkdir -p /www/wwwroot/restxtra-demo/releases
```

采用版本目录和 `current` 软链接：

```text
/www/wwwroot/restxtra-demo/
├── packages/
│   └── restxtra-demo-20260823-120000.zip
├── releases/
│   └── 20260823-120000/
│       ├── index.html
│       ├── dashboard/
│       └── _next/
└── current -> releases/20260823-120000
```

这种结构允许原子更新，也能在新版本异常时立即回滚。

## 5. 通过宝塔上传并解压

1. 打开“宝塔 -> 文件”。
2. 进入 `/www/wwwroot/restxtra-demo/packages`。
3. 上传本地的 `restxtra-demo-时间戳.zip`。
4. 不要直接在 `packages` 中解压，使用终端发布到独立版本目录。

假设包名为 `restxtra-demo-20260823-120000.zip`：

```bash
RELEASE=20260823-120000
BASE=/www/wwwroot/restxtra-demo

mkdir -p "$BASE/releases/$RELEASE"
unzip "$BASE/packages/restxtra-demo-$RELEASE.zip" \
  -d "$BASE/releases/$RELEASE"

test -f "$BASE/releases/$RELEASE/index.html"
test -f "$BASE/releases/$RELEASE/dashboard/index.html"
test -f "$BASE/releases/$RELEASE/function/findings/index.html"
```

三个 `test` 命令无输出且退出码为 `0` 表示目录正确。如果实际路径是 `releases/版本/out/index.html`，说明 ZIP 多套了一层目录，应重新打包。

首次发布时创建 `current`：

```bash
ln -s "$BASE/releases/$RELEASE" "$BASE/current"
chown -R www:www "$BASE"
find "$BASE" -type d -exec chmod 755 {} \;
find "$BASE" -type f -exec chmod 644 {} \;
```

检查链接：

```bash
readlink -f /www/wwwroot/restxtra-demo/current
```

输出应为本次版本目录。

## 6. 在宝塔创建纯静态网站

进入“宝塔 -> 网站 -> 添加站点”：

- 域名：`demo.example.com`，替换成你的真实域名；
- 根目录：`/www/wwwroot/restxtra-demo/current`；
- PHP 版本：纯静态；
- 数据库：不创建；
- FTP：不创建，除非你确实需要。

创建后进入“网站 -> 对应站点 -> 设置 -> 配置文件”。保留宝塔生成的 `listen`、`server_name`、SSL 证书和日志配置，确认 `server {}` 内有：

```nginx
root /www/wwwroot/restxtra-demo/current;
index index.html;

server_tokens off;

add_header X-Content-Type-Options "nosniff" always;
add_header X-Frame-Options "SAMEORIGIN" always;
add_header Referrer-Policy "strict-origin-when-cross-origin" always;
add_header Permissions-Policy "camera=(), microphone=(), geolocation=()" always;

location ^~ /api/ {
    return 404;
}

location ^~ /_next/static/ {
    try_files $uri =404;
    access_log off;
    expires 1y;
    add_header Cache-Control "public, max-age=31536000, immutable";
}

location / {
    try_files $uri $uri/ $uri/index.html /index.html;
    expires -1;
    add_header Cache-Control "no-store";
}
```

注意：

- 一个 `server {}` 中不要保留两个 `location /`；如宝塔已生成该块，就替换原块；
- 不要配置 `/api/` 反向代理；
- HTML 使用 `no-store`，只有带内容哈希的 `/_next/static/` 使用一年缓存；
- `try_files` 必须保留 `$uri/index.html`，否则直接打开子页面可能返回 404。

在宝塔终端检查配置：

```bash
nginx -t
```

看到 `syntax is ok` 和 `test is successful` 后，在宝塔中重载 Nginx。若服务器找不到 `nginx` 命令，也可以使用宝塔面板的配置检查与重载功能。

## 7. 申请 HTTPS 证书

进入“网站 -> 对应站点 -> SSL”：

1. 选择 Let's Encrypt；
2. 勾选 `demo.example.com`；
3. 申请证书；
4. 证书部署成功后开启“强制 HTTPS”。

申请失败时依次检查：DNS 是否已指向当前服务器、80/443 是否放行、域名是否被其他站点占用、Nginx 配置是否能正常重载。

## 8. 上线验收

在服务器或本地执行：

```bash
curl -I https://demo.example.com/
curl -I https://demo.example.com/dashboard/
curl -I https://demo.example.com/function/findings/
curl -I https://demo.example.com/sandbox/containers/
curl -i https://demo.example.com/api/platform/my
```

预期：

- 前四个请求返回 `200`；
- `/api/platform/my` 返回 `404`；
- HTTP 会跳转到 HTTPS；
- 响应中有 `X-Content-Type-Options: nosniff`。

浏览器继续检查：

- `/dashboard/`：Event Log、Working Set、Agent 成本和性能基线有数据；
- `/function/tasks/`：任务详情能展示资产覆盖图；
- `/function/findings/`：漏洞可点击，摘要、证据、报告和链路图完整；
- `/sandbox/containers/`：PostgreSQL 与控制面均显示“仅查看”；
- `/sandbox/hosts/`：进入后仍可正常切换其他页面；
- 桌面端和手机端均无横向溢出或内容遮挡。

## 9. 发布新版本

每次更新都在本地重新执行第 3 节，产生新的时间戳 ZIP，再通过宝塔上传到 `packages`。不要覆盖旧版本目录。

解压并验证新版本：

```bash
NEW_RELEASE=20260824-093000
BASE=/www/wwwroot/restxtra-demo

mkdir -p "$BASE/releases/$NEW_RELEASE"
unzip "$BASE/packages/restxtra-demo-$NEW_RELEASE.zip" \
  -d "$BASE/releases/$NEW_RELEASE"

test -f "$BASE/releases/$NEW_RELEASE/index.html"
test -f "$BASE/releases/$NEW_RELEASE/dashboard/index.html"
chown -R www:www "$BASE/releases/$NEW_RELEASE"
find "$BASE/releases/$NEW_RELEASE" -type d -exec chmod 755 {} \;
find "$BASE/releases/$NEW_RELEASE" -type f -exec chmod 644 {} \;
```

验证通过后原子切换：

```bash
ln -sfn "$BASE/releases/$NEW_RELEASE" "$BASE/current.next"
mv -Tf "$BASE/current.next" "$BASE/current"
nginx -t && systemctl reload nginx
```

宝塔安装的 Nginx 不一定由 `systemctl` 管理。如果最后一条提示服务不存在，请在 `nginx -t` 成功后使用宝塔面板重载 Nginx。静态文件切换本身不依赖重载。

上线后重复第 8 节的 `curl` 和浏览器验收。

## 10. 一键回滚到上一个版本

查看已有版本和当前指向：

```bash
ls -1 /www/wwwroot/restxtra-demo/releases
readlink -f /www/wwwroot/restxtra-demo/current
```

将 `OLD_RELEASE` 改为上一已验证版本：

```bash
OLD_RELEASE=20260823-120000
BASE=/www/wwwroot/restxtra-demo

test -f "$BASE/releases/$OLD_RELEASE/index.html"
ln -sfn "$BASE/releases/$OLD_RELEASE" "$BASE/current.next"
mv -Tf "$BASE/current.next" "$BASE/current"
nginx -t && systemctl reload nginx
```

随后重新执行上线验收。回滚只切换软链接，不改数据库，也不会触碰远程 Git。

## 11. 清理旧发布包

至少保留当前版本和最近两个已验证版本。确认某个旧版本不再用于回滚后，在宝塔文件管理器中删除对应的单个 ZIP 和版本目录。删除前先核对：

```bash
readlink -f /www/wwwroot/restxtra-demo/current
```

不要删除该命令当前指向的目录，也不要对 `/www/wwwroot` 或 `restxtra-demo` 根目录执行递归删除。

## 12. 常见问题

### 子页面刷新后 404

Nginx 缺少以下回退规则：

```nginx
try_files $uri $uri/ $uri/index.html /index.html;
```

### 页面请求真实 API

本地构建时没有设置 `NEXT_PUBLIC_MOCK=1`，或上传了错误构建产物。重新按第 3 节构建并发布。不要给 `/api/` 配置真实反向代理。

### 首页白屏或静态资源 404

常见原因是 ZIP 多套了一层 `out/`。正确路径必须是：

```text
/www/wwwroot/restxtra-demo/current/index.html
/www/wwwroot/restxtra-demo/current/_next/
```

### 更新后仍显示旧页面

确认 `current` 已指向新目录，并确保 HTML 的 `Cache-Control` 为 `no-store`。必要时在浏览器强制刷新；不要取消 `/_next/static/` 的长缓存，因为文件名带内容哈希。

### 宝塔显示 403

检查路径、软链接和权限：

```bash
readlink -f /www/wwwroot/restxtra-demo/current
namei -l /www/wwwroot/restxtra-demo/current/index.html
```

目录通常为 `755`，文件通常为 `644`，宝塔 Nginx 常用用户为 `www`。

### SSL 申请失败

先确认 DNS 已生效，80/443 已在云安全组和系统防火墙放行，并且站点可以通过 HTTP 访问。证书签发期间不能屏蔽 ACME 校验路径。

### `/api/` 返回 404

这是正确行为，证明 Demo 没有连接真实后端。

## 13. 最短部署清单

1. 本地设置 `NEXT_PUBLIC_MOCK=1` 并运行 `npm run build:static`。
2. 将 `web/out/*` 打包成带时间戳的 ZIP。
3. 用宝塔上传到 `/www/wwwroot/restxtra-demo/packages`。
4. 解压到 `/www/wwwroot/restxtra-demo/releases/时间戳`。
5. 将 `current` 软链接切换到新版本。
6. 宝塔创建纯静态站点，站点根目录指向 `current`。
7. 配置静态路由、拒绝 `/api/`，然后申请 SSL 并强制 HTTPS。
8. 验证关键页面为 `200`、API 为 `404`，再对外提供 Demo 地址。

此流程与远程 Git 完全解耦。远程仓库只保留正式项目代码；Demo 修改、mock 数据和发布产物均留在本地及 Demo 服务器。
