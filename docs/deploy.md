# 部署、升级与命令行

## 1. 最快跑起来(本地试用)

**已经有 Go 1.26+**:前端产物已随仓库提交,直接编译即可,不需要 Node。

```
CGO_ENABLED=0 go build -ldflags "-s -w" -o doriyamedia ./cmd/doriyamedia
./doriyamedia                  # 默认监听 0.0.0.0:5666,数据目录 ./data
```

**在 Windows 上一键构建**(顺带构建前端,需要 pnpm):

```
scripts\build.ps1              # 构建前端 + 嵌入,产出 doriyamedia.exe
.\doriyamedia.exe
```

运行环境只需要 `ffmpeg` / `ffprobe`(转封装、字幕转换都靠它),按 PATH 查找,
也可以在 **设置 → 系统 → 工具路径与性能** 里改成绝对路径。

浏览器打开 `http://127.0.0.1:5666`,首次进入会要求创建管理员账号。

> 项目从 `Doriya` 改名为 `DoriyaMedia`(模块 `doriyamedia`,二进制 `doriyamedia.exe`)。
> 数据库也改名为 `data/doriyamedia.db`:**首次启动会自动把旧的 `data/doriya.db`(含 WAL 文件)
> 改名过来**,媒体库、扫描结果、账号都原样保留,不需要重扫;`config.json` 文件名不变。

也可以直接下预编译包(见下节),不必装 Go / Node。

## 2. Linux 一键安装(Debian / Ubuntu)

不想在服务器上装 Go 和 Node?直接下预编译包 —— 纯静态、无 CGO 依赖,Release 由
GitHub Actions 自动构建:

| 平台 | 架构 |
| --- | --- |
| linux | amd64 / arm64 / armv7 |
| windows | amd64 |

一键安装(Debian 12/13、Ubuntu 22.04/24.04 及衍生版):

```
curl -fsSL https://raw.githubusercontent.com/qiuxiuya/DoriyaMedia/main/scripts/install.sh | sudo bash
```

国内网络 `raw.githubusercontent.com` 常常打不开,那就用镜像把脚本取下来(脚本内部还会自动把
下载源回退到镜像,所以只要脚本本身能拉下来就行):

```
curl -fsSL https://ghfast.top/https://raw.githubusercontent.com/qiuxiuya/DoriyaMedia/main/scripts/install.sh | sudo bash
```

脚本会依次:安装 ffmpeg → 创建系统用户和数据目录 `/opt/doriyamedia` → 生成 `config.json`
→ 注册 systemd 服务并启动。下载 Release 时官方不通会**自动按
`官方 → ghfast.top → ghproxy.net → gh-proxy.com` 的顺序回退**,不用手动指定。
升级就是再跑一次同样的命令(自动覆盖二进制并重启);或者装好后直接用下面的 `dm` 命令。

安装位置:

| 内容 | 路径 |
| --- | --- |
| 程序 | `/usr/local/bin/doriyamedia` |
| 数据(配置 / 数据库 / 图片 / 缓存) | `/opt/doriyamedia` |
| 配置文件 | `/opt/doriyamedia/config.json` |
| 管理命令 | `/usr/local/bin/dm` |
| systemd 单元 | `/etc/systemd/system/doriyamedia.service` |

> **从旧版升级**:旧版本的数据目录是 `/var/lib/doriyamedia`。重跑安装脚本(或用 `dm update`)
> 时会**先把服务停掉,再把整个目录搬到 `/opt/doriyamedia`**,并同步改写 `config.json` 里的
> `data_dir` —— 媒体库、账号、播放进度、站点外观都原样保留。
> 如果你显式传了 `--data-dir`,脚本会尊重你的选择、不做迁移;两个目录都存在且新目录非空时也会跳过迁移并告警,
> 不会覆盖任何一边。

### 常用安装选项

```
# 指定版本
curl -fsSL <上面任一脚本地址> | sudo bash -s -- --version v0.1.0
# 换端口 / 数据目录 / 运行用户
sudo bash install.sh --port 8080 --data-dir /srv/doriyamedia --user media
# 固定用某个镜像源(不走自动回退)
sudo bash install.sh --base-url https://ghfast.top
# 只装不启动(不设开机自启)
sudo bash install.sh --no-start
# 卸载(--purge 连数据目录一起删)
sudo bash install.sh --uninstall
sudo bash install.sh --purge
```

完整选项:`-v/--version TAG`、`-p/--port N`、`--data-dir PATH`、`--user NAME`、
`--base-url URL`、`--no-start`、`--uninstall`、`--purge`、`-h/--help`。

## 3. 日常运维:`dm` 命令

装完的日常操作,直接用安装附带的 **`dm` 管理命令**(类似宝塔的 `bt`,输入 `dm` 回车进菜单):

```
dm                  # 管理菜单:检查更新 / 更新 / 启停 / 日志 / 卸载
dm update           # 更新到最新版(dm update v0.2.1 可指定版本,也能用来降级)
dm check            # 只看看有没有新版本
dm status           # 版本 / 端口 / 运行状态
dm start|stop|restart
dm log              # 跟随日志(Ctrl+C 退出)
dm version          # 已安装版本
dm uninstall        # 卸载(保留数据);dm purge 连数据一起删
dm help             # 显示帮助
```

不用 `dm` 的话,等价的 systemd 操作:

```
systemctl status doriyamedia
systemctl restart doriyamedia
journalctl -u doriyamedia -f        # 看日志
doriyamedia -version                # 看当前版本
```

## 4. 自己编译

需要 Go 1.26+(`go.mod` 里写的就是 1.26.0)。前端产物 `internal/web/dist` 已随仓库提交,
所以**不需要 Node**:

```
CGO_ENABLED=0 go build -ldflags "-s -w" -o doriyamedia ./cmd/doriyamedia
./doriyamedia                       # 前台运行,数据落在 ./data
```

可以交叉编译:

```
GOOS=linux GOARCH=arm64 CGO_ENABLED=0 go build -ldflags "-s -w" -o doriyamedia-arm64 ./cmd/doriyamedia
```

## 5. Linux 上的几个注意点

- `data/` 和 `config.json` 都相对**当前工作目录**,systemd 单元里已经设好了 `WorkingDirectory`;
  手动跑的话记得在自己想要的目录里启动,否则数据会落在奇怪的地方;
- ffmpeg / ffprobe 按 PATH 查找,也可以在配置里改成绝对路径(**设置 → 系统**);
- **代理默认值是 Windows 上的本地代理**:Bangumi 的 `use_proxy` 默认 `true` 且指向
  `socks5://127.0.0.1:7890`,在服务器上必然超时 —— 安装脚本会预置成直连。TMDB 默认直连。
  国内网络两者都需要在 设置 → 刮削与播放 里勾选代理并填自己的地址;
- 端口 5666 是非特权端口,服务以专用用户 `doriyamedia` 运行,不需要 root。

## 6. 环境变量

| 变量 | 作用 |
| --- | --- |
| `DORIYAMEDIA_LOCAL=1` | 只监听 `127.0.0.1`,不对外暴露 |
| `DORIYAMEDIA_CONFIG=PATH` | 指定配置文件路径(默认 `./config.json`) |
| `DORIYAMEDIA_PORT=N` | 覆盖监听端口 |
| `DORIYAMEDIA_GITHUB_BASE` / `DORIYAMEDIA_API_BASE` | 安装脚本与 `dm` 的下载源前缀 |

## 7. 数据目录里有什么

用安装脚本装的话,数据目录是 **`/opt/doriyamedia`**(systemd 的 `WorkingDirectory` 与
`data_dir` 都指向它);手动运行时默认是当前工作目录下的 `./data`。

| 路径 | 内容 |
| --- | --- |
| `config.json` | 端口、密钥、刮削、播放、SMTP、Emby 等全部配置 |
| `doriyamedia.db` | 账号、媒体库、条目、文件、播放进度、同步队列 |
| `images/` | 海报、背景图、剧照缓存 |
| `cache/hls/` | 封装分片缓存(按 `hls_cache_mb` 淘汰) |
| `site.json` | 站点外观(登录页文案、页眉页脚),见 [界面与站点外观](ui-and-site.md) |

**备份就备份整个数据目录**。`images/` 丢了不要紧,重扫会把海报补回来。

> 目录权限是 `doriyamedia:doriyamedia`、`0750`。想直接管理里面的文件,
> 用 `sudo -u doriyamedia` 或加进同名用户组,不要改成 777。

## 8. 命令行(CLI)

二进制自带一套子命令,和网页设置页操作的是同一个 `config.json` 和数据库,所以没有浏览器
(或者服务正停着)时也能在 SSH 里把活干完。不带子命令时行为不变,仍然是启动服务。

```
doriyamedia                       # 启动服务(等价于 doriyamedia serve)
doriyamedia help                  # 全部命令
doriyamedia status                # 版本/端口/数据目录/媒体库与账号数量
doriyamedia doctor                # 自检:ffmpeg、数据目录、数据库、管理员、代理
```

全局选项可以写在命令前,也可以写在命令后:`-config PATH`(或环境变量
`DORIYAMEDIA_CONFIG`)、`-json`(机器可读输出)、`-quiet`、`-port`(仅 serve)。

**存储源与媒体库**

```
doriyamedia source add --name 云盘 --type openlist --base-url http://host:5244 --token xxx
doriyamedia source add --name 本地 --type local --root /srv/media
doriyamedia source list
doriyamedia source test --id 1              # 列一次根目录,确认凭据可用
doriyamedia source update --id 1 --token 新token
doriyamedia source rm --id 1 --yes

doriyamedia library add --name 电影 --type movie --source-id 1 --path /Movies
doriyamedia library list
doriyamedia library update --id 1 --providers nfo,bgm,tmdb
```

`source add` 默认先做一次连接测试,连不上就不写库;确认无误可以加 `--no-test`。
删除被媒体库引用的源、或删除媒体库本身,都需要显式 `--yes`。

**扫描(会等它跑完)**

```
doriyamedia scan --id 1                     # 全库扫描,实时打印进度
doriyamedia scan --name 电影
doriyamedia scan --id 1 --source-id 1 --path /Movies/新片   # 只扫一个子目录
doriyamedia scan --id 1 --json              # 结束时输出最终状态 JSON
```

CLI 的扫描和设置页的扫描走同一套代码,同样会刮削;区别是它在前台运行、跑完才退出,
失败时退出码非 0。Ctrl+C 会请求取消而不是留下半个事务。注意这条命令不能和正在运行的
服务同时写同一个库,大规模扫描建议停掉服务再跑。

**账号**

```
doriyamedia user list
doriyamedia user add --name admin --admin          # 省略 --password 则交互输入(不回显)
doriyamedia user passwd --name admin               # 重置密码,并让已登录会话失效
doriyamedia user role --name admin --role admin
doriyamedia user status --name someone --status disabled
doriyamedia user rm --name someone --yes
```

最后一个可用管理员不能被降级、禁用或删除——这一点和网页端一致。

**配置与条目**

```
doriyamedia config path                     # 当前使用的配置文件
doriyamedia config show                     # 打印配置(secret/密码/token 打码)
doriyamedia config get playback.probe_size_mb
doriyamedia config set port 8080            # 按原值类型写入,写错类型会拒绝
doriyamedia item list --library-id 1 --limit 20
doriyamedia item show 12                    # 条目详情与它的文件
```

`config set` 只改已存在的键,拼错的路径会报错而不是新增一个没人读的字段;
改完需要重启服务(或在网页设置里保存一次)才生效。

## 9. 发布新版本(维护者)

打 tag 即自动构建并发布 Release:

```
git tag v0.1.0 && git push origin v0.1.0
```

`.github/workflows/release.yml` 会:构建前端 → 交叉编译 4 个平台 → 打包 `tar.gz`/`zip`
→ 生成 `SHA256SUMS` → 创建 Release。版本号通过 `-ldflags "-X main.version=<tag>"` 注入。
`ci.yml` 在推送到 main 和 PR 时跑 `go vet` / `go test` / 编译校验。

## 10. 从源码开发

```
webui\        Vue3 + Vite 前端(pnpm dev 热更新,代理 /api → 5666)
  src/components/  顶栏、影轨、海报卡、Hero、搜索面板、Toast、图标…
  src/views/       首页 / 媒体库 / 详情 / 季集 / 文件 / 播放 / 设置 / 登录
cmd\doriyamedia\   服务端入口
cmd\mockopenlist\  OpenList v3 API 的本地 mock(测试 302 链路用)
scripts\e2e-test.ps1   端到端冒烟测试(先启动 doriyamedia + mockopenlist + testdata)
scripts\browse-live-test.ps1   对真实 OpenList 跑浏览→决策→302/转封装链路
scripts\bilibili-live-test.ps1 B 站番剧整链路验证(见 docs/bilibili.md)
testdata\     ffmpeg 生成的测试片源(h264/hvc1/hev1/mkv-eac3/剧集+字幕)
```

调试前端:`pnpm dev`(端口 5166)+ 后端 `.\doriyamedia.exe`,浏览器开 http://127.0.0.1:5166。
也可以给后端配置 `dev_web_dir`(例如 `internal/web/dist`),这样改完前端只需 `pnpm build`、不用重启后端。

改完前端记得 `pnpm build`:页面是**嵌进二进制**的(`internal/web/dist`),不 build 的话 `go build` 出来的
还是旧界面。编辑工具栏的回归脚本在 `.uitest/verify.mjs`(需要先起后端,并用
`chrome --headless=new --remote-debugging-port=9333` 开一个 Chrome),它会把工具栏点一遍并截图。

## 11. 常见问题

| 现象 | 先检查什么 |
| --- | --- |
| 番剧列表是空的 / 变空 | 代理与地区,见 [B 站番剧源](bilibili.md) 第 5 节;也可能是风控限流,等几分钟 |
| 番剧只有 480P,会员集只能试看 | Cookie 失效,改用扫码登录,见 [B 站番剧源](bilibili.md) 第 3 节 |
| 手机用流量播放 403 | 网盘直链绑定出口 IP,在播放页点「强制转封装」,见 [存储源](storage-sources.md) 的「直连(302)的注意点」 |
| 没有海报 / 简介 | 媒体库没勾刮削源,或 TMDB Key/代理没配好,用设置页的「测试连接」 |
| 扫描报 `object not found` | 媒体库里填的路径在存储源上不存在,去「文件」页确认实际路径 |
| 登录页一直转圈 / 提交无反应 | Cloudflare Turnstile 打不开,见 [账号](accounts.md) 第 6.4 节 |
| 播放某文件提示「无法播放」 | 该编码本机解不了且不转码,见 [播放](playback.md) 第 5 节 |
| 字幕全都没有 | 该集确实没有;或字幕目录被当作分类目录跳过,检查目录结构 |
