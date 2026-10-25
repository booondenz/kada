# Kada

[English](README.md) | **简体中文**

短链接管理与分析平台。支持链接文件夹与标签、自定义域名、访问密码、过期时间、UTM 模板、点击分析看板，以及一套给自己脚本调用的 API。

后端用 Go 编写，前端是 Vite 单页应用（构建为静态文件）。

## 功能

- 没有登录墙：打开就能用，首个页面加载即开启一个游客会话（JWT）。手机号 + 密码是给游客账号“认领”身份用的，接口和测试都在，认领页面还没做
- 短链接的创建、编辑、删除与批量管理
- 自定义短码与自定义域名
- 访问密码与过期时间
- 文件夹与标签
- UTM 参数与 UTM 模板
- 链接预览、二维码与 CSV 导出
- 点击分析看板（总览、平台分布、每日趋势、访客明细）
- 开放 API 令牌
- 明暗两套主题

## 技术栈

| 层级 | 技术 |
|-------|------------|
| 后端 | Go 1.26、Gin、GORM |
| 存储 | PostgreSQL 16、Redis 7 |
| 前端 | Vite、React 19、React Router、TypeScript、SWR、Tailwind |
| 部署 | Docker Compose、Nginx、systemd、GitHub Actions |

## 架构

分层为 Handler -> Service -> Infra，是一个模块化单体。

每次点击由跳转路径自己的一个 goroutine 记录，跳转因此从不等一次数据库写入。它曾经发布到 Kafka、由第二个
二进制落库；那条管道从未部署过（主机上没有 broker，也没有 worker 单元），所以被移除，现在只有一条写入路径。

数据库表结构由 `internal/domain/entity` 下的 GORM 模型定义，由 `cmd/migrate`
（AutoMigrate）应用。仓库里没有 SQL 迁移文件：结构体是唯一事实来源，AutoMigrate 只补齐缺失的
表 / 列 / 索引，因此可以安全地重复执行。

## 快速开始

环境要求：Go 1.26+、Node 22+、Docker。

```bash
cp .env.example .env
docker compose up -d
```

本地开发：

```bash
go run ./cmd/migrate/         # 仅应用表结构，API 启动时会自动应用
go run ./cmd/server/main.go   # 8080
cd web && npm run dev         # 3000
```

## 实测基线

`cmd/loadgen` 对运行中的实例压测跳转路径，并从 `/metrics` 读回缓存计数：缓存命中对客户端是不可见的，
只有服务端自己的计数器能作证。它会自己写入测试链接，跑两个场景 —— 一个是有意义的工作集，一个是一串
不存在的短码。

```bash
go run ./cmd/migrate           # 仅应用表结构，API 启动时也会应用
go run ./cmd/server            # PostgreSQL 与 Redis

# 另开一个终端
go run ./cmd/loadgen -codes 200 -absent 2000 -requests 20000 -clients 4000 -concurrency 50
```

| 场景 | 请求数 | 吞吐 | p50 | p99 | 缓存命中率 |
|---|---|---|---|---|---|
| 工作集：200 个存在的短码 | 20,000 | 1,610 req/s | 20.9 ms | 124 ms | 100% |
| 不存在的短码：2,000 个 | 20,000 | 5,105 req/s | 6.5 ms | 56.7 ms | 90% |

在一台笔记本上测得（12 逻辑核，Windows），API、PostgreSQL 16 与 Redis 7 在同一台机器上，50 并发，
请求分摊到 4,000 个合成地址（用 `X-Real-IP` 发送，避免按地址的限流影响结果），工作集测量前先跑
2,000 次预热。数字来自单次运行：吞吐的可复现性在几个百分点内，尾延迟会在几十毫秒范围内浮动。

表里有两件事与缓存无关：

- **两行的差别是点击写入。** 302 会记录一次点击，不存在的短码不记录，因为没有链接可以挂。每次点击都是
  一个事务里的插入加一次计数更新，大约每秒 1,600–1,800 次，工作集那行就是被它限制的。
- **瓶颈既不是缓存，也不是工作集大小。** 把工作集换成 2,000 个短码，吞吐从 1,610 升到 1,776 req/s。
  第二行的 90% 来自首轮：2,000 个短码各访问一次 PostgreSQL；之后每个请求都命中。

## 环境变量

| 变量 | 说明 |
|----------|-------------|
| JWT_SECRET | 必填，强随机密钥 |
| POSTGRES_PASSWORD | 必填，生产环境必须修改 |
| DB_AUTO_MIGRATE | 是否让 API 服务在启动时应用表结构（默认 true；使用 cmd/migrate 时设为 false） |
| SMS_ACCESS_KEY_ID / SMS_ACCESS_KEY_SECRET | 阿里云短信。做账号认领（手机号 + 验证码）时必填；游客会话不需要 |
| SMS_SIGN_NAME / SMS_TEMPLATE_CODE | 阿里云 PNVS 控制台为你分配的短信签名与模板 |

生产环境下这些值来自 GitHub 仓库 secrets，由部署任务写入 `/opt/kada/backend/.env`；部署任务做了什么，
见 [docs/design.md](docs/design.md) 的部署一节。

## 目录结构

```text
cmd/       入口：API、表结构迁移、压测工具
config/    环境变量解析（唯一的配置来源）
internal/  应用主体：domain、handler、service、infra、middleware、metrics
web/       Vite 单页应用，构建为静态文件后由 nginx 提供
docs/      设计、贡献与代码风格文档
deploy/    部署脚本，以及 deploy/nginx/ 下的反向代理配置
scripts/   维护脚本与部署步骤测试
go.mod     模块根目录就是仓库根目录：不再需要 cd backend
docker-compose.yml
```
