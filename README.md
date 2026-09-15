# Emby Ecer

专注 Emby 缺集扫描与 MoviePilot 联动：发现缺集、搜索匹配资源、提交下载。

Go 后端 + React 前端，Docker 一键部署。

## 功能

- **缺集扫描**：对接 Emby API，按 TMDB 官方季集信息比对，找出已播出但缺失的集数
  - 全库单集一次性拉取 + 整季完整时跳过 TMDB 季详情，大库扫描请求数大幅下降
  - 支持屏蔽指定媒体库、定时自动扫描、增量扫描（只重扫有变动的剧）
  - 快速校验：只核对当前缺集是否已补齐，秒级刷新列表
  - 免检名单支持整剧忽略和单集忽略；自动归档仅限 TMDB 明确完结、不再制作且已齐的剧，连载剧在增量扫描时继续核对更新
  - 同一 TMDB 剧集的兼容季集编号合并核对，避免 Emby 多条同剧记录造成重复报缺；单剧重扫会同步更新相关记录
  - 资源编号超出 TMDB 对应季范围时，单独检查原始文件编号，列出断号、检查范围和前后集标题
  - 正确展开合并集、去重重复编号，分段视频按资源编号计数；不会仅凭视频总数判定 TMDB 全集齐全
  - 资源编号检查只覆盖已观察到的范围；首尾及整季是否缺失，需要该版本完整目录。无法读取或来源编号冲突时不推断断号
- **MoviePilot 联动**：对接 MP 站点搜索，优先展示命中缺失季集的资源，一键提交下载
  - 缺集详情可发送当前目标季到 MP 订阅，并查询订阅状态；后续追踪由 MoviePilot 负责
  - 尚未对应 TMDB 的资源版本可按片名和版本搜索，不会将资源断号传作 TMDB 季集号或标记为精准命中
- **后台扫描**：实时进度、单剧重扫、任务恢复和错误提示
  - 进度每 2 秒更新，完整列表每 30 秒刷新；任务结束时立即刷新和保存
  - 进行中的结果每 30 秒保存检查点；重复提交会复用当前任务，避免并行重复扫描
  - 电影只统计 Emby 元数据，不进行与缺集无关的 TMDB 详情查询
- **健康度统计**：海报卡片 + 健康度进度条 + 匹配标签
  - 缺集列表每批显示 50 部剧，搜索仍覆盖所有结果

## 从当前源码部署

复制 `.env.example` 为 `.env`，设置登录账号和随机 `APP_JWT_SECRET` 后运行：

```sh
docker compose up -d --build
```

配置和扫描记录保存在 `app-data` 数据卷中，更新镜像不会丢失。

远程部署绑定 `127.0.0.1:33000` 时，Windows 可运行 `scripts/preview-tunnel.ps1`，将本机 `http://localhost:3000` 转发到 SSH 别名 `netcup`。脚本会自动重连；可通过当前用户的启动项在登录后自动运行。服务和扫描仍在 netcup 执行，本机只提供访问通道。

## 已发布镜像部署

```bash
docker run -d --name emby-ecer -p 3000:3000 \
  -v /path/to/data:/data \
  -e APP_USERS=admin:yourpassword \
  -e APP_JWT_SECRET=random-secret \
  dedehao/emby-ecer:latest
```

首次启动后访问 `http://IP:3000`，默认账号 `admin / admin123`（建议修改）。

## 镜像发布

创建并发布 GitHub Release 后，工作流会自动构建多架构镜像并推送到 Docker Hub：

- `dedehao/emby-ecer:latest`
- `dedehao/emby-ecer:<release-tag>`

工作流使用仓库 Secrets：`DOCKERHUB_USERNAME` 和 `DOCKERHUB_TOKEN`。

## 配置

登录后在「设置」页配置：
- **Emby**：地址 + API Key
- **缺集扫描**：定时扫描开关与间隔、增量模式、屏蔽不参与扫描的媒体库
- **TMDB**：API Key
- **MoviePilot**：地址 + API Token
- **账号安全**：修改登录密码

也可以全部通过环境变量注入。

常用环境变量：
- `SCAN_AUTO_ENABLED` / `SCAN_AUTO_INTERVAL_HOURS` / `SCAN_AUTO_RECENT_ONLY`：缺集定时扫描开关、间隔（默认 12 小时）与增量模式
- `SCAN_EXCLUDED_LIBRARIES`：不参与缺集扫描的媒体库，填库 ID 或库名，逗号分隔
- `MP_URL` / `MP_TOKEN`：MoviePilot 地址与 API Token
- `MP_DOWNLOAD_TIMEOUT_SECONDS`：MoviePilot 下载提交超时，默认 `90`

## 技术栈

- 后端：Go
- 前端：React 18 + Vite + Tailwind CSS + Zustand + Lucide React
- 存储：SQLite，兼容导入旧版 JSON 配置和扫描记录

## 验证

```sh
go test ./...
cd frontend
npm ci --legacy-peer-deps
npm run build
```
