# Emby Ecer

Emby + TMDB 缺集扫描 → PanSou 搜索 → 115 转存 / MoviePilot 下载

Go 后端 + React 前端，Docker 一键部署。

## 功能

- **缺集扫描**：对接 Emby API，按 TMDB 官方季集信息比对，找出已播出但缺失的集数
- **PanSou 盘搜**：聚合搜索 115 网盘资源，一键转存
- **MoviePilot 搜索**：对接 MP 站点搜索，一键发送下载
- **后台任务**：扫描和搜索后台执行，进度条实时更新
- **健康度统计**：海报卡片 + 健康度进度条 + 匹配标签

## Docker 部署

```bash
docker run -d --name emby-ecer -p 3000:3000 \
  -v /path/to/data:/data \
  -e APP_USERS=admin:yourpassword \
  -e APP_JWT_SECRET=random-secret \
  ghcr.io/你的用户名/emby-ecer:latest
```

首次启动后访问 `http://IP:3000`，默认账号 `admin / admin123`（建议修改）。

## 配置

登录后在「授权」页配置：
- **Emby**：地址 + API Key
- **TMDB**：API Key
- **PanSou**：API 地址（默认 https://so.252035.xyz）
- **115 网盘**：Cookie + 目标目录 CID
- **MoviePilot**：地址 + API Token

也可以全部通过环境变量注入。

## 技术栈

- 后端：Go
- 前端：React 18 + Vite + Tailwind CSS + Zustand + Lucide React
- 数据库：无（配置和扫描结果存 JSON 文件）
