// 舔狗日记 · we.dog 的 Rust 重构
// 10,000 条日记编译期嵌入；发牌式分发：每副牌由种子确定性洗牌，
// 访客按游标顺序取牌，不重不漏发完 10,000 条后才进入下一副牌。
// 全部资产（HTML/CSS/JS/字体/图片）编译期嵌入 → 单文件二进制，部署即运行。
use axum::extract::Query;
use axum::http::{header, StatusCode, Uri};
use axum::response::{Html, IntoResponse, Response};
use axum::routing::get;
use axum::{Json, Router};
use serde::Deserialize;
use std::sync::LazyLock;

/// 编译期嵌入全部日记（UTF-8，按行分隔）
const DIARY_TEXT: &str = include_str!("../data/diary.txt");

/// 全量日记，&'static str 切片，首次访问时一次性构建，之后零分配只读
static DIARY: LazyLock<Vec<&'static str>> = LazyLock::new(|| {
    DIARY_TEXT
        .lines()
        .map(str::trim)
        .filter(|l| !l.is_empty())
        .collect()
});

const N: usize = 10_000;

// ---------- 内嵌资产 ----------

const INDEX_TPL: &str = include_str!("index.html");
const APP_JS: &str = include_str!("app.js");
const APP_CSS: &str = include_str!("app.css");
const FONT_BODY: &[u8] = include_bytes!("../assets/font/HYTiaoTiao.ttf");
const FONT_TITLE: &[u8] = include_bytes!("../assets/font/MFShangYa.ttf");
const LOGO_GIF: &[u8] = include_bytes!("../assets/images/logo.gif");
const REFRESH_GIF: &[u8] = include_bytes!("../assets/images/refresh.gif");
const FAV_0: &[u8] = include_bytes!("../assets/images/favicon/0.jpg");
const FAV_1: &[u8] = include_bytes!("../assets/images/favicon/1.jpg");
const FAV_2: &[u8] = include_bytes!("../assets/images/favicon/2.jpg");
const FAV_3: &[u8] = include_bytes!("../assets/images/favicon/3.jpg");
const FAV_4: &[u8] = include_bytes!("../assets/images/favicon/4.jpg");
const FAV_5: &[u8] = include_bytes!("../assets/images/favicon/5.jpg");

/// 启动期把 CSS/JS 注入 HTML 模板（只需一次）
static INDEX_HTML: LazyLock<String> = LazyLock::new(|| {
    INDEX_TPL
        .replace("__CSS__", APP_CSS)
        .replace("__JS__", APP_JS)
});

// ---------- 确定性洗牌 ----------

/// splitmix64：把任意种子均匀扩散到 64 位全空间
fn splitmix64(state: &mut u64) -> u64 {
    *state = state.wrapping_add(0x9E37_79B9_7F4A_7C15);
    let mut z = *state;
    z = (z ^ (z >> 30)).wrapping_mul(0xBF58_476D_1CE4_E5B9);
    z = (z ^ (z >> 27)).wrapping_mul(0x94D0_49BB_1331_11EB);
    z ^ (z >> 31)
}

/// 副牌：deck 种子 → 每日一洗；out[i] = 第 i 张发出的日记下标。
/// 键控 Fisher-Yates：随机源只依赖 deck，同一副牌任何请求算出的顺序一致。
fn deck_order(deck: u64, out: &mut [u16; N]) {
    for (i, slot) in out.iter_mut().enumerate() {
        *slot = i as u16;
    }
    let mut state = deck.wrapping_mul(0x9E37_79B9_7F4A_7C15) ^ 0xD1B5_4A32_D192_ED03;
    for i in (1..N).rev() {
        let r = (splitmix64(&mut state) % (i as u64 + 1)) as usize;
        out.swap(i, r);
    }
}

// ---------- API ----------

#[derive(Deserialize)]
struct DealQuery {
    deck: u64,
    pos: u32,
    n: u32,
}

const CHUNK: u32 = 32;

/// GET /api/deal?deck=&pos=&n= → {"deck":..,"pos":..,"total":10000,"items":[..]}
/// 无状态：服务端不存任何会话，进度完全由客户端持有；同一 (deck,pos) 响应恒定，可长缓存。
async fn deal(Query(q): Query<DealQuery>) -> Response {
    let n = q.n.clamp(1, CHUNK) as usize;
    let pos = q.pos as usize;
    if pos >= N {
        return cache_json(
            serde_json::json!({"deck": q.deck, "pos": pos, "total": N, "items": []}),
        );
    }
    let mut order = [0u16; N];
    deck_order(q.deck, &mut order);
    let end = (pos + n).min(N);
    let items: Vec<&str> = order[pos..end]
        .iter()
        .map(|&i| DIARY[i as usize])
        .collect();
    cache_json(serde_json::json!({
        "deck": q.deck, "pos": end, "total": N, "items": items
    }))
}

fn cache_json(body: serde_json::Value) -> Response {
    (
        [(header::CACHE_CONTROL, "public, max-age=86400")],
        Json(body),
    )
        .into_response()
}

async fn index() -> Html<&'static str> {
    // LazyLock<String> 泄漏为 'static 引用，进程生命周期内只构建一次
    Html(Box::leak(INDEX_HTML.clone().into_boxed_str()))
}

// ---------- 内嵌静态资产路由 ----------

fn static_asset(bytes: &'static [u8], mime: &'static str, days: u32) -> Response {
    (
        [
            (header::CONTENT_TYPE, mime),
            (
                header::CACHE_CONTROL,
                Box::leak(format!("public, max-age={}", days * 86400).into_boxed_str())
                    as &'static str,
            ),
        ],
        bytes,
    )
        .into_response()
}

async fn asset(uri: Uri) -> Response {
    match uri.path() {
        "/assets/font/HYTiaoTiao.ttf" => static_asset(FONT_BODY, "font/ttf", 30),
        "/assets/font/MFShangYa.ttf" => static_asset(FONT_TITLE, "font/ttf", 30),
        "/assets/images/logo.gif" => static_asset(LOGO_GIF, "image/gif", 30),
        "/assets/images/refresh.gif" => static_asset(REFRESH_GIF, "image/gif", 30),
        "/assets/images/favicon/0.jpg" => static_asset(FAV_0, "image/jpeg", 30),
        "/assets/images/favicon/1.jpg" => static_asset(FAV_1, "image/jpeg", 30),
        "/assets/images/favicon/2.jpg" => static_asset(FAV_2, "image/jpeg", 30),
        "/assets/images/favicon/3.jpg" => static_asset(FAV_3, "image/jpeg", 30),
        "/assets/images/favicon/4.jpg" => static_asset(FAV_4, "image/jpeg", 30),
        "/assets/images/favicon/5.jpg" => static_asset(FAV_5, "image/jpeg", 30),
        _ => StatusCode::NOT_FOUND.into_response(),
    }
}

#[tokio::main]
async fn main() {
    assert_eq!(DIARY.len(), N, "diary.txt 必须恰好 10000 条");
    let port: u16 = std::env::var("PORT")
        .ok()
        .and_then(|p| p.parse().ok())
        .unwrap_or(8080);
    let app = Router::new()
        .route("/", get(index))
        .route("/api/deal", get(deal))
        .fallback(get(asset))
        .layer(tower_http::compression::CompressionLayer::new());
    let listener = tokio::net::TcpListener::bind(("0.0.0.0", port))
        .await
        .expect("bind port");
    println!(
        "舔狗日记(rust) listening on http://127.0.0.1:{}  — {} 条日记已装填",
        port, N
    );
    axum::serve(listener, app).await.unwrap();
}
