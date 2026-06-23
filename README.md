# Emby Ecer

Emby + TMDB 缺集扫描 → PanSou 搜索 → 115 转存 / MoviePilot 下载

Go 后端 + React 前端，Docker 一键部署。

## 功能

- **缺集扫描**：对接 Emby API，按 TMDB 官方季集信息比对，找出已播出但缺失的集数
- **PanSou 盘搜**：聚合搜索 115 网盘资源，一键转存
- **HDHive 订阅**：使用 Cookie 模式搜索 HDHive 115 资源，支持订阅自动扫描
- **AI 识别**：接入 OpenAI 兼容接口，对订阅候选资源排序辅助识别
- **MoviePilot 搜索**：对接 MP 站点搜索，一键发送下载
- **后台任务**：扫描和搜索后台执行，进度条实时更新
- **健康度统计**：海报卡片 + 健康度进度条 + 匹配标签

## Docker 部署

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

登录后在「授权」页配置：
- **Emby**：地址 + API Key
- **TMDB**：API Key
- **PanSou**：API 地址（默认 https://so.252035.xyz）
- **HDHive**：站点地址 + 网页 Cookie
- **115 网盘**：Cookie + 目标目录 CID
- **MoviePilot**：地址 + API Token
- **订阅扫描**：启用开关、自动扫描间隔、命中后自动转存
- **大模型识别**：OpenAI 兼容 Base URL + API Key + 模型名

也可以全部通过环境变量注入。

常用环境变量：
- `PANSOU_URL` / `PANSOU_USERNAME` / `PANSOU_PASSWORD`：PanSou 开启认证时建议配置账号密码，token 过期后会自动重登
- `HDHIVE_URL` / `HDHIVE_COOKIE`：HDHive Cookie 模式配置
- `SUBSCRIPTION_ENABLED` / `SUBSCRIPTION_INTERVAL_HOURS` / `SUBSCRIPTION_AUTO_TRANSFER`：订阅自动扫描配置
- `OPENAI_BASE_URL` / `OPENAI_API_KEY` / `OPENAI_MODEL`：OpenAI 兼容大模型识别配置
- `MP_DOWNLOAD_TIMEOUT_SECONDS`：MoviePilot 下载提交超时，默认 `90`

## 技术栈

- 后端：Go
- 前端：React 18 + Vite + Tailwind CSS + Zustand + Lucide React
- 数据库：无（配置和扫描结果存 JSON 文件）
