// 舔狗日记（wedog-go）—— we.dog 设计复刻 · Go 重构版
//
// 与 Rust 版同一套发牌协议：牌局号(UTC 天数)作种子，splitmix64 + 键控
// Fisher-Yates 确定性洗牌，访客按游标取牌，1 万条不重不漏发完才换牌。
// 数据、字体、图片、前端全部 go:embed，编译产物单文件、零依赖。
package main

import (
	"bytes"
	"embed"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// ---------- 内嵌资产 ----------

//go:embed web/diary.txt
var diaryRaw string

//go:embed web/index.html
var indexTemplate []byte

//go:embed web/app.css
var appCSS []byte

//go:embed web/app.js
var appJS []byte

//go:embed web/static
var staticFS embed.FS

// 启动后首次访问时把 CSS/JS 注入模板（只做一次，与 Rust 版 INDEX_HTML 等价）
var indexHTML = sync.OnceValue(func() []byte {
	return bytes.ReplaceAll(
		bytes.ReplaceAll(indexTemplate, []byte("__CSS__"), appCSS),
		[]byte("__JS__"), appJS)
})

const total = 10000

// diary: &str 切片等价物；字符串仍引用 diaryRaw 的底层数组，无重复分配
var diary []string

func init() {
	for line := range strings.SplitSeq(diaryRaw, "\n") {
		if s := strings.TrimSpace(line); s != "" {
			diary = append(diary, s)
		}
	}
	if len(diary) != total {
		panic(fmt.Sprintf("diary.txt 应为 %d 条，实际 %d 条", total, len(diary)))
	}
}

// ---------- 确定性洗牌（与 Rust 版逐位一致） ----------

func splitmix64(state *uint64) uint64 {
	*state += 0x9E3779B97F4A7C15
	z := *state
	z = (z ^ (z >> 30)) * 0xBF58476D1CE4E5B9
	z = (z ^ (z >> 27)) * 0x94D049BB133111EB
	return z ^ (z >> 31)
}

// deckOrder: out[i] = 第 i 张发出的日记下标。随机源只依赖 deck，可复现。
func deckOrder(deck uint64, out []uint16) {
	for i := range out {
		out[i] = uint16(i)
	}
	state := deck*0x9E3779B97F4A7C15 ^ 0xD1B54A32D192ED03
	for i := total - 1; i > 0; i-- {
		r := int(splitmix64(&state) % uint64(i+1))
		out[i], out[r] = out[r], out[i]
	}
}

// ---------- 当日牌局缓存 ----------
// 服务最热的路径是「今天这副牌」。全局缓存 (deck, 顺序副本)，同一天零重洗；
// 换天时单个请求重建，其余请求原子读到旧 deck 自动绕行。

type dailyDeck struct {
	deck  uint64
	order [total]uint16
}

var cached atomic.Pointer[dailyDeck]

func getDeck(deck uint64) *dailyDeck {
	if d := cached.Load(); d != nil && d.deck == deck {
		return d
	}
	d := &dailyDeck{deck: deck}
	deckOrder(deck, d.order[:])
	cached.Store(d)
	return d
}

// 今日牌局号：自纪元起的天数，与 Rust 版一致
func todayDeck() uint64 {
	return uint64(time.Now().UTC().Unix() / 86400)
}

// ---------- 发牌 API ----------
// GET /api/deal?deck=&pos=&n= → {"deck":..,"pos":..,"total":10000,"items":[..]}
// 手工拼 JSON（无反射、无中间分配），同一 (deck,pos) 响应恒定 → 长缓存。

const chunkMax = 32

func dealHandler(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	deck, _ := strconv.ParseUint(q.Get("deck"), 10, 64)
	pos, _ := strconv.Atoi(q.Get("pos"))
	n, _ := strconv.Atoi(q.Get("n"))
	if n < 1 {
		n = 1
	} else if n > chunkMax {
		n = chunkMax
	}
	if pos < 0 {
		pos = 0
	}
	end := pos + n
	if end > total {
		end = total
	}

	var b strings.Builder
	b.Grow(64 + n*140)
	b.WriteString(`{"deck":`)
	b.WriteString(strconv.FormatUint(deck, 10))
	b.WriteString(`,"pos":`)
	if pos >= total { // 越界：空页
		b.WriteString(strconv.Itoa(pos))
		b.WriteString(`,"total":`)
		b.WriteString(strconv.Itoa(total))
		b.WriteString(`,"items":[]}`)
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.Header().Set("Cache-Control", "public, max-age=86400")
		w.Write([]byte(b.String()))
		return
	}
	b.WriteString(strconv.Itoa(end))
	b.WriteString(`,"total":`)
	b.WriteString(strconv.Itoa(total))
	b.WriteString(`,"items":[`)
	d := getDeck(deck)
	first := true
	for _, idx := range d.order[pos:end] {
		if !first {
			b.WriteByte(',')
		}
		first = false
		b.WriteByte('"')
		s := diary[idx]
		for i := 0; i < len(s); i++ {
			c := s[i]
			switch {
			case c == '"':
				b.WriteString(`\"`)
			case c == '\\':
				b.WriteString(`\\`)
			case c == '\n':
				b.WriteString(`\n`)
			case c < 0x20:
				fmt.Fprintf(&b, `\u%04x`, c)
			default:
				b.WriteByte(c)
			}
		}
		b.WriteByte('"')
	}
	b.WriteString("]}")

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "public, max-age=86400")
	w.Write([]byte(b.String()))
}

// ---------- 首页 ----------

func indexHandler(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		staticHandler(w, r)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "public, max-age=300")
	w.Write(indexHTML())
}

// ---------- 静态资产（内嵌 FS） ----------

var mimeTypes = map[string]string{
	".css": "text/css; charset=utf-8",
	".js":  "text/javascript; charset=utf-8",
	".ttf": "font/ttf",
	".gif": "image/gif",
	".jpg": "image/jpeg",
	".png": "image/png",
	".svg": "image/svg+xml",
}

func staticHandler(w http.ResponseWriter, r *http.Request) {
	p := strings.TrimPrefix(r.URL.Path, "/")
	if !strings.HasPrefix(p, "static/assets/") {
		http.NotFound(w, r)
		return
	}
	data, err := staticFS.ReadFile("web/" + p)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	ext := p[strings.LastIndexByte(p, '.'):]
	if m, ok := mimeTypes[ext]; ok {
		w.Header().Set("Content-Type", m)
	}
	w.Header().Set("Cache-Control", "public, max-age=604800")
	w.Write(data)
}

// ---------- main ----------

func main() {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /", indexHandler)
	mux.HandleFunc("GET /api/deal", dealHandler)

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	srv := &http.Server{
		Addr:              "0.0.0.0:" + port,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second, // 防 slowloris
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      10 * time.Second,
		MaxHeaderBytes:    4 << 10,
	}
	fmt.Printf("舔狗日记(go) listening on http://127.0.0.1:%s — %d 条日记已装填\n", port, total)
	if err := srv.ListenAndServe(); err != nil {
		panic(err)
	}
}
