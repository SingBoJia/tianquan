# tianquan · 舔全

> 「我们是狗，舔狗。」—— 舔狗日记，一万条，发完为止。

[we.dog](http://we.dog)（舔狗日记）的复刻重构版：**Go 单二进制**，内嵌 **10,000 条**程序化生成、互不重复的舔狗日记。`wedog-rs/` 目录同时保留 **Rust 版**参考实现（axum），两版发牌协议逐位一致。

## 特性

- **单文件部署**：日记数据、手写字体（按真实字符集子集化）、图片、前端 CSS/JS 全部 `go:embed` 编进二进制，服务器上零依赖，拷过去就能跑
- **发牌式分发，不重不漏**：牌局号 = UTC 天数，每天全站自动换一副新牌；种子经 splitmix64 扩散后做键控 Fisher-Yates 洗牌。访客按游标顺序取牌，发满 10,000 条才会遇到重复（次日自动换牌）
- **服务最小化**：
  - 零第三方依赖（纯标准库 `net/http` + `embed`）
  - 手工拼 JSON，无反射、无中间分配
  - 当日牌局全局缓存（`atomic.Pointer`），同一天只洗一次牌
  - `ReadHeaderTimeout` 等防慢连接；静态资产与 API 均带长缓存头，可直接挂 CDN
- **设计复刻**：手绘风不对称圆角卡片（6 组随机切换）、荧光笔高亮（只盖文字下方 20%）、动态 favicon（6 帧轮播）、移动端 rem / 桌面端自适应

## 快速开始

```bash
# 构建（最小化）
go build -trimpath -ldflags "-s -w" -o tianquan .

# 运行
PORT=8080 ./tianquan
# → http://127.0.0.1:8080
```

交叉编译 Linux 服务器版：

```bash
GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -trimpath -ldflags "-s -w" -o tianquan-linux .
```

## API

```
GET /api/deal?deck=<牌局号>&pos=<游标>&n=<1..32>

→ {"deck":12345,"pos":32,"total":10000,"items":["...","..."]}
```

无状态：服务端不保存任何会话，进度由客户端持有；同一 `(deck, pos)` 响应恒定，`Cache-Control: max-age=86400`。

## 目录结构

```
tianquan/
├── main.go            # Go 服务端（发牌 + 内嵌资产 + 牌局缓存）
├── go.mod             # 零依赖
├── web/
│   ├── diary.txt      # 10,000 条日记（2.7MB，编译期嵌入）
│   ├── app.css / app.js / index.html   # 前端（构建时注入模板）
│   └── static/assets  # 字体 + 图片（嵌入）
└── rust-reference/    # Rust/axum 版参考实现（发牌协议逐位一致）
```

## 数据

1 万条日记由模板家族 × 素材池程序化组合生成（34 个故事套路：谩骂谐音解读、工资全转、礼物挂闲鱼再借钱买回、"你喜欢老实人"……），规范化去重后逐条唯一，遵循原站核心公式：**恶意回应 → "这其中一定有什么含义" → 荒谬自洽的解读 → 更爱了**。

## systemd 部署示例

```ini
# /etc/systemd/system/tianquan.service
[Unit]
Description=tianquan - 舔狗日记
After=network.target

[Service]
ExecStart=/opt/tianquan/tianquan-linux
Environment=PORT=80
Restart=always
User=www-data

[Install]
WantedBy=multi-user.target
```

## License

日记语料与原站设计灵感来自 [we.dog](http://we.dog)，仅作技术学习交流。
