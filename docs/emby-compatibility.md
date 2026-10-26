# Emby 服务端兼容层

DoriyaMedia 提供面向第三方 Emby 客户端的基础服务端协议适配。实现参考 MeBox 的路由布局、认证入口和大小写兼容方式，独立编写适配代码，不引入 MeBox 的框架或数据库模型。

## 连接方式

1. 在网页完成首次初始化，创建账号并扫描媒体库。
2. 在播放器中选择添加 Emby 服务器。服务器地址填写 `http://服务器IP:5666`；账号密码使用现有 DoriyaMedia 账号。实际端口以部署配置为准。
3. 客户端要求路径时，可使用 `http://服务器IP:5666/emby`。兼容根路径、`/emby` 和 `/mediabrowser` 前缀，路由大小写不敏感。
4. 外网访问请配置 HTTPS。反向代理应保留请求头、查询参数和 Range，并允许较长的媒体探测超时。HLS 多流播放会引用 `/api/hls/`，不要只代理 `/emby/`。目前按站点根目录部署，不承诺任意反向代理子路径部署。

不需要单独启动 Emby 进程、监听 8096，或迁移媒体库。现有网页和原生 API 保留。可在 Infuse、SenPlayer 等支持手动添加 Emby 服务器的客户端上进行验收；当前自动化测试不等于这些客户端的真机认证，也不保证 Emby/Jellyfin 官方客户端全部功能可用。

## 已实现

- 服务端公开信息、Ping、稳定 ServerId、账号密码登录、用户信息和退出登录。公开用户列表为空，避免匿名枚举账号。
- 请求头 Token、Bearer、MediaBrowser/Emby Authorization，以及 `api_key`、`X-Emby-Token` 查询参数认证。凭证复用数据库会话，过期、撤销或禁用账号立即失效。
- 媒体库、电影、剧集、季、单集、详情、搜索、分页、基本排序、最近入库、继续观看、下一集、条目数量。详情导航的 `Ancestors` 返回由近及远的父条目及媒体库；`ThemeSongs`、`ThemeVideos`、`CriticReviews` 返回空集合，不代表支持主题媒体或影评数据。内部文件浏览缓存库不出现在媒体列表。
- 主海报、背景图、单集缩略图。官方图片 URL 不携带登录 Token，因此 DTO 图像 tag 使用绑定条目 ID、图像种类和缓存文件名的 HMAC；图片 GET/HEAD 可凭有效 tag 获取对应图片，或使用有效登录会话。无凭证/伪造 tag 不开放图片枚举，tag 不能授权资料或媒体流。tag 没有独立期限；更换缓存文件名或站点 Secret 后失效。支持查询串 tag、索引 0 和官方完整路径式图片 URL；尺寸/格式等变换目前仍返回原图。无原始海报的条目不会凭空生成真实海报。
- `/Users/{user}/Items/Latest` 默认按番剧分组（`GroupItems=true`）：同剧跨季单集只返回一个 Series，按最新匹配子集排序，`ChildCount` 为匹配集数；先分组再分页。显式 `GroupItems=false` 返回单集，不影响普通单集列表与继续观看。
- 单集的 `Primary` 优先使用已缓存的单集截图（`StillPath`），`Thumb` 仍可访问同一截图；签名和实际图片文件保持一致。没有单集截图时使用条目海报或父季/番剧海报兜底，不自动从视频截帧，也不会伪造每集图片。
- 多文件版本的 MediaSourceId 选择；播放前复用媒体引擎探测，提供时长、编码、音视频轨和可转换字幕轨。
- 本地 Range/HEAD、远程直链和代理播放复用原有驱动；字幕复用现有 WebVTT 转换。
- 收藏/取消收藏：`POST`/`DELETE /Users/{user}/FavoriteItems/{id}`，兼容 `POST .../Delete`；按用户和条目持久化，电影/番剧/季/单集可独立收藏，不更改播放进度，不同步 Bangumi 收藏。详情/列表 `UserData.IsFavorite` 及 `IsFavorite`/`Filters=IsFavorite` 查询读取同一状态。
- 播放开始、进度、停止事件；Ticks 与秒转换；跨用户进度隔离；已看/取消已看。完成观看使用现有 Bangumi 同步队列。

## 播放策略与限制

普通文件以原始容器直接播放/直流形式提供，不根据第三方 DeviceProfile 生成转码矩阵；客户端应具备源文件解码能力。服务器不宣称支持视频转码。音轨可在客户端原生切换，当前不提供按 Emby 音轨参数进行服务端转码。

B 站 DASH 等分离音视频源，在 PlaybackInfo 阶段复用现有转封装决策生成签名 HLS URL，不把视频单流误报为完整电影。此分支依赖可用的源账号、ffmpeg/ffprobe、源访问权限与现有封装配置；尚未完成真实远程源及客户端的端到端验收。HLS URL 使用既有文件签名有效期；它是临时播放授权，和原网页播放相同，撤销登录会话不会提前撤销已签发的签名 URL。

仅支持可转换为 WebVTT 的外部/内嵌文本字幕；不承诺 PGS 字幕外置、烧录或 Emby 字幕下载格式全兼容。

已实现 `DisplayPreferences/{id}` 的 GET/POST，按已认证用户、偏好 ID、client 在数据库持久化并隔离；官方客户端登录不仅等待 AuthenticateByName，还会等待显示偏好加载。补齐用户 Configuration 中客户端直接调用 `.includes()` 的数组。兼容层允许 `https://app.emby.media` 的非 Cookie 跨域请求与预检，不反射任意 Origin；浏览器仍可能限制 HTTPS 页面访问 HTTP 服务，请配置 HTTPS。

详情页会调用 `System/Endpoint` 后请求 `PlaybackInfo?IsPlayback=false` 读取音轨/字幕元数据；此分支只读缓存，不访问远程源、不启动 ffprobe、字幕扫描或转封装。缓存为空时音轨可能暂不可用，实际播放（true 或未指定）仍沿用现有探测/转封装。

未实现 UDP 自动发现、Emby Connect、QuickConnect、WebSocket 推送/遥控、多用户远程会话控制、音乐库、直播、插件和 Emby 管理后台。未知接口返回错误，不回退为网页 HTML。会话列表仅描述当前访问者会话，能力上报仅确认收到，不构成完整设备注册与遥控系统。

已看状态沿用 DoriyaMedia 的 90% 完成阈值。标记已看需要已有媒体时长（探测或历史进度）；未知时长返回 409，而不是伪造时长。文件夹/整季批量标记已看尚未实现。下一集在已经开始观看的剧集中选择最后完成集之后的首个未完成单集。

登录支持 JSON 或 `application/x-www-form-urlencoded` 请求体中的 `Username` 和明文 `Pw`（字段名不区分大小写），应通过 HTTPS 传输；不从 URL 查询参数读取账号密码。不支持仅发送旧版 SHA1 `Password` 的客户端：现有账号保存的是加盐 PBKDF2，不能从中还原 SHA1，也不会把 `Password` 字段误当明文密码。启用 Turnstile 时兼容登录明确拒绝请求，避免绕过网页验证码；需要登录的第三方客户端应在可信部署中关闭该验证码，不能将本接口当验证码绕过入口。邮箱验证、账号审核/禁用、登录限流与原站点保持一致。

URL 中的 api_key 和签名是凭证，不能公开分享；部署代理时应避免把查询串写入公开日志。

## 验证

自动化测试覆盖发现前缀/大小写、Token 形式、会话撤销/过期/禁用、限流、媒体层级、分页计数、搜索参数绑定、多用户隔离、版本归属、Range/HEAD、字幕、进度、已看/下一集、隐藏库过滤及原网页路由。

```sh
go test ./...
go test -race ./internal/api
go vet ./...
```

如果本机设置了不匹配的 GOROOT（例如 Go 1.26 命令搭配 Go 1.24 标准库），先修正本地工具链；可临时使用 `env -u GOROOT go test ./...`，无需修改项目 Go 版本。

## 官方实现核对（2026-10-26）

本次对照官方在线 Web 客户端 26.0.38 的实际调用，而不仅是接口 HTTP 200：

- https://dev.emby.media/reference/RestAPI/UserService/postUsersAuthenticatebyname.html — Username/Pw 与认证响应定义。
- https://app.emby.media/modules/emby-apiclient/connectionmanager.js — onAuthenticated → onLocalUserSignIn → usersettings.setUserInfo。
- https://app.emby.media/modules/common/usersettings/usersettingsbuilder.js — setUserInfo 等待 getDisplayPreferences；404 会使整个登录 Promise 拒绝。
- https://app.emby.media/modules/emby-apiclient/apiclient.js — 4.8 服务端走 DisplayPreferences/usersettings；首页直接读取 Configuration.LatestItemsExcludes.includes；播放参数位于查询串。
- https://app.emby.media/item/item.js 与 https://app.emby.media/modules/common/playback/playbackmanager.js — 详情在渲染前请求 Endpoint 和 IsPlayback=false 的 PlaybackInfo。

回归测试覆盖完整登录初始化所需请求、偏好数据库写入和用户/client 隔离、官方托管站点 CORS 预检、未就绪源的 metadata-only PlaybackInfo（不得访问驱动），以及显式播放仍走原探测流程。不等于真实客户端或远程播放全流程验收。

2026-10-26 后续详情/图像修复：官方 `getImageUrl` 不附带会话 Token，现改为上述图像 tag 范围授权，并用真实 PNG 字节校验 GET。详情响应日期校验后按 UTC 毫秒格式输出，未知/非法日期省略，不拼出无效 date-time；补齐可空集合和 UserData 默认字段属于防御性兼容，未证明是任何具体第三方客户端失败的唯一原因。补齐用户路径形式的 LocalTrailers、SpecialFeatures 和 Intros。

兼容请求失败或超过 1 秒时记录 Emby 路由模板、状态码、耗时；不记录查询串、请求头、密码、Token 或路径式图片 tag。第三方仍失败时，应提供客户端名称/版本和点击详情时日志；不能从未指定客户端的转圈症状断言协议失败位置。

参考项目：https://github.com/truewhile/MeBox 。重点参考其 Emby routes、path normalize、认证及播放信息接口；本项目实现范围以本文为准。
