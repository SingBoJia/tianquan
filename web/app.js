// 舔狗日记 前端：发牌协议客户端
// 牌局 deck 每天一换（UTC+8 日期散列）；进度存 localStorage，按序发牌不重不漏。
// 缓冲区剩 <PREFETCH 时后台取下一块（32 条/块），点击翻页零等待。
(function () {
  'use strict';
  var N = 10000, CHUNK = 32, PREFETCH = 8;
  var $ = function (id) { return document.getElementById(id); };

  // ---- 牌局号：每天一副新牌（同一天所有人同一副，进度可分享） ----
  var today = new Date();
  var deck = Math.floor((today.getTime() / 86400000) + 0) % 1000000000; // days since epoch
  $('decktag').textContent = '牌局 #' + deck;

  // ---- 进度：localStorage 持久化 ----
  var KEY = 'wedog.' + deck;
  var pos = parseInt(localStorage.getItem(KEY) || '0', 10) || 0;
  if (pos >= N) pos = 0; // 发完一万条 → 新的一天自然换牌；同日发完则重开
  var buf = [], fetching = false;          // 当前缓冲区：buf[0] 即下一条要展示的

  function save() { try { localStorage.setItem(KEY, String(pos - buf.length)); } catch (e) {} }

  // ---- 发牌 API ----
  function fetchChunk(p, cb) {
    var xhr = new XMLHttpRequest();
    xhr.open('GET', '/api/deal?deck=' + deck + '&pos=' + p + '&n=' + CHUNK);
    xhr.onload = function () {
      try { cb(JSON.parse(xhr.responseText)); } catch (e) {}
    };
    xhr.send();
  }
  function ensureBuffer() {
    if (buf.length >= PREFETCH) return;
    // 服务端游标 = 绝对进度 - 未消费缓冲
    var serverPos = pos - buf.length;
    if (serverPos >= N || fetching) return;
    fetching = true;
    fetchChunk(serverPos, function (r) {
      fetching = false;
      if (!r || !r.items || !r.items.length) return;
      buf = buf.concat(r.items);
      save();
      ensureBuffer();
    });
  }

  // ---- 渲染：**梗** → <mark>，复刻原站高亮 ----
  function render(t) {
    var el = $('text');
    el.style.opacity = 0;
    setTimeout(function () {
      el.innerHTML = t.replace(/\*\*(.*?)\*\*/g, '<mark>$1</mark>');
      el.scrollTop = 0;
      el.style.opacity = 1;
    }, 120);
  }

  function show() {
    if (!buf.length) { ensureBuffer(); setTimeout(showIfReady, 250); return; }
    var t = buf.shift();
    pos++;
    save();
    $('cur').textContent = pos;
    render(t);
    // 手绘边框随机换
    $('card').className = 'border-' + (1 + Math.floor(Math.random() * 6));
    // favicon 动画重启
    startFavicon();
    ensureBuffer();
  }
  function showIfReady() { if (buf.length) show(); else show(); }

  // ---- 动态 favicon：6 帧 80ms 轮播（复刻原站） ----
  var favTimer = null, favI = 0;
  function startFavicon() {
    if (favTimer) return;
    favTimer = setInterval(function () {
      var link = document.querySelector("link[rel='icon']");
      if (!link) {
        link = document.createElement('link');
        link.rel = 'icon'; link.type = 'image/jpeg';
        document.head.appendChild(link);
      }
      link.href = '/static/assets/images/favicon/' + favI + '.jpg';
      favI = (favI + 1) % 6;
    }, 80);
  }

  // ---- 日期 ----
  var d = new Date();
  $('date').textContent = d.getFullYear() + '-' + (d.getMonth() + 1) + '-' + d.getDate();

  // ---- 事件 ----
  $('next').addEventListener('click', show);
  document.addEventListener('keydown', function (e) {
    if (e.key === ' ' || e.key === 'Enter' || e.key === 'ArrowRight') { e.preventDefault(); show(); }
  });
  // 触屏左滑 = 下一篇
  var tx = null;
  document.addEventListener('touchstart', function (e) { tx = e.touches[0].clientX; }, { passive: true });
  document.addEventListener('touchend', function (e) {
    if (tx === null) return;
    if (tx - e.changedTouches[0].clientX > 60) show();
    tx = null;
  }, { passive: true });

  // ---- 启动：先取第一块，立即展示 ----
  ensureBuffer();
  var boot = setInterval(function () {
    if (buf.length) { clearInterval(boot); show(); }
  }, 120);
})();
