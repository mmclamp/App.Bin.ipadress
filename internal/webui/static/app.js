/* IP 通知 - Web 配置页 */
(function () {
  'use strict';

  var $ = function (id) { return document.getElementById(id); };

  /* 安全存储（iframe 受限环境下 localStorage 可能被禁） */
  var store = {
    get: function (k) { try { return localStorage.getItem(k); } catch (e) { return null; } },
    set: function (k, v) { try { localStorage.setItem(k, v); } catch (e) { /* 忽略 */ } }
  };

  /* API 基址：统一网关场景下文档路径为 /app/ipaddress[/]，必须带上前缀；
     绝对路径 /api/... 会打到 fnOS 本体而非本应用。开发模式无前缀。 */
  var API_BASE = (function () {
    var marker = '/app/ipaddress';
    var p = window.location.pathname;
    if (p === marker || p.indexOf(marker + '/') === 0) return marker;
    return '';
  })();

  /* 本地诊断日志（接口异常时也会显示在页面上） */
  var localLogs = [];
  function diag(msg, isErr) {
    var line = { time: new Date().toISOString(), level: isErr ? 'error' : 'info', msg: msg };
    localLogs.push(line);
    if (localLogs.length > 100) localLogs.shift();
    try { console[isErr ? 'error' : 'log']('[ipaddress] ' + msg); } catch (e) { /* 忽略 */ }
  }

  function api(path, opts) {
    opts = opts || {};
    opts.headers = Object.assign({ 'Content-Type': 'application/json' }, opts.headers || {});
    var url = API_BASE + path;
    return fetch(url, opts).then(function (r) {
      return r.json().catch(function () { return {}; }).then(function (data) {
        if (!r.ok) {
          var msg = (data && data.error) ? data.error : ('HTTP ' + r.status + ' @ ' + url);
          diag('接口请求失败: ' + msg, true);
          showDiag('接口异常: ' + msg);
          throw new Error(msg);
        }
        return data;
      });
    }, function (err) {
      diag('网络请求异常: ' + url + ' (' + err + ')', true);
      showDiag('网络异常: ' + err + ' @ ' + url);
      throw err;
    });
  }

  function showDiag(msg) {
    var el = $('st-diag');
    if (el) {
      el.textContent = msg;
      el.style.color = 'var(--err)';
    }
  }

  function fmtTime(iso) {
    if (!iso) return '—';
    var d = new Date(iso);
    if (isNaN(d.getTime())) return '—';
    function p(n) { return n < 10 ? '0' + n : n; }
    return d.getFullYear() + '-' + p(d.getMonth() + 1) + '-' + p(d.getDate()) +
      ' ' + p(d.getHours()) + ':' + p(d.getMinutes()) + ':' + p(d.getSeconds());
  }

  function setHint(el, msg, cls) {
    el.textContent = msg || '';
    el.className = 'hint' + (cls ? ' ' + cls : '');
  }

  /* ---------- 主题 ---------- */
  function parentIsDark() {
    // 同源（统一网关）时读取 fnOS 桌面背景亮度；失败则返回 null
    try {
      var pd = window.parent && window.parent.document;
      var el = pd && (pd.body || pd.documentElement);
      if (!el) return null;
      var m = (getComputedStyle(el).backgroundColor.match(/-?\d+(\.\d+)?/g) || []).map(Number);
      var opaque = m.length >= 3 && (m.length < 4 || m[3] > 0);
      if (!opaque) return null;
      return (0.299 * m[0] + 0.587 * m[1] + 0.114 * m[2]) / 255 < 0.5;
    } catch (e) {
      return null;
    }
  }

  function applyTheme() {
    var t = store.get('theme') || 'auto';
    var dark;
    if (t === 'dark') dark = true;
    else if (t === 'light') dark = false;
    else {
      dark = parentIsDark();
      if (dark === null) {
        dark = window.matchMedia && window.matchMedia('(prefers-color-scheme: dark)').matches;
      }
    }
    document.documentElement.dataset.theme = dark ? 'dark' : 'light';
    var btn = $('btn-theme');
    if (btn) {
      btn.textContent = t === 'auto' ? '主题：自动'
        : t === 'light' ? '主题：亮色' : '主题：暗色';
    }
  }

  function toggleTheme() {
    var order = ['auto', 'light', 'dark'];
    var cur = store.get('theme') || 'auto';
    store.set('theme', order[(order.indexOf(cur) + 1) % order.length]);
    applyTheme();
  }

  /* ---------- 状态 ---------- */
  var currentIP = '';

  function setIPDisplay(ip) {
    currentIP = ip || '';
    var el = $('st-ip');
    el.textContent = currentIP || '未获取';
    el.title = currentIP ? '点击复制：' + currentIP : '';
  }

  function copyText(text) {
    // 优先 Clipboard API（需 https/localhost），失败回退 execCommand（fnOS 可能是 http）
    if (navigator.clipboard && navigator.clipboard.writeText) {
      return navigator.clipboard.writeText(text);
    }
    return new Promise(function (resolve, reject) {
      var ta = document.createElement('textarea');
      ta.value = text;
      ta.style.position = 'fixed';
      ta.style.opacity = '0';
      document.body.appendChild(ta);
      ta.select();
      var ok = false;
      try { ok = document.execCommand('copy'); } catch (e) { ok = false; }
      document.body.removeChild(ta);
      ok ? resolve() : reject(new Error('复制失败'));
    });
  }

  var toastTimer = null;
  function toast(msg) {
    var el = $('toast');
    el.textContent = msg;
    el.hidden = false;
    clearTimeout(toastTimer);
    toastTimer = setTimeout(function () { el.hidden = true; }, 1500);
  }

  function copyIP() {
    if (!currentIP) return;
    copyText(currentIP).then(function () {
      toast('已复制 ' + currentIP);
    }).catch(function () {
      toast('复制失败，请手动复制：' + currentIP);
    });
  }

  /* ---------- 立即探测（刷新按钮） ---------- */
  function probeNow(btn) {
    var old = btn.textContent;
    btn.disabled = true;
    btn.textContent = '探测中…';
    api('/api/probe', { method: 'POST' }).then(function (r) {
      if (r.ip) setIPDisplay(r.ip);
      setHint($('st-diag'), '探测成功', 'ok');
    }).catch(function (e) {
      setHint($('st-diag'), '探测失败：' + e.message, 'err');
    }).finally(function () {
      btn.disabled = false;
      btn.textContent = old;
      loadStatus().catch(function () { /* 诊断行已呈现 */ });
    });
  }

  function loadStatus() {
    return api('/api/status').then(function (s) {
      if (currentIP) {
        // 刚探测过就保留最新值，避免状态接口的旧值回退显示
        $('st-ip').textContent = currentIP;
        $('st-ip').title = '点击复制：' + currentIP;
      } else {
        setIPDisplay(s.last_ip);
      }
      $('st-last').textContent = fmtTime(s.last_send_time) +
        (s.last_send_ok === true ? '（成功）' : s.last_send_ok === false ? '（失败）' : '');
      $('st-next').textContent = s.next_run ? fmtTime(s.next_run) : '未调度';
      $('st-channels').textContent = s.enabled_channels + ' / ' + s.total_channels;
      if (s.version) $('ver').textContent = 'v' + s.version;
      var d = $('st-diag');
      if (d && d.textContent.indexOf('异常') === -1 && d.textContent !== '探测成功') {
        d.textContent = '接口正常（' + (API_BASE || '根路径') + '）';
        d.style.color = '';
      }
    });
  }

  /* ---------- 设置 ---------- */
  function loadConfig() {
    return api('/api/config').then(function (c) {
      $('cfg-cron').value = c.cron || '';
      $('cfg-template').value = c.template || '';
    });
  }

  function saveConfig() {
    var cfg = { cron: $('cfg-cron').value.trim(), template: $('cfg-template').value };
    setHint($('cfg-result'), '保存中…');
    api('/api/config', { method: 'PUT', body: JSON.stringify(cfg) }).then(function (r) {
      setHint($('cfg-result'), '已保存' + (r.next_run ? '，下次执行：' + fmtTime(r.next_run) : ''), 'ok');
      loadStatus();
    }).catch(function (e) {
      setHint($('cfg-result'), '保存失败：' + e.message, 'err');
    });
  }

  /* ---------- 渠道 ---------- */
  var channels = [];

  function renderChannels() {
    var tb = $('ch-table').querySelector('tbody');
    tb.innerHTML = '';
    if (!channels.length) {
      var tr = document.createElement('tr');
      tr.innerHTML = '<td colspan="6" class="empty">暂无渠道，点击"添加渠道"创建。</td>';
      tb.appendChild(tr);
      return;
    }
    channels.forEach(function (ch) {
      var tr = document.createElement('tr');
      var typeNames = { feishu: '飞书', dingtalk: '钉钉', 'feishu-app': '飞书应用' };
      var type = typeNames[ch.type] || ch.type;
      var on = ch.enabled ? '<span class="tag on">启用</span>' : '<span class="tag off">停用</span>';
      var target = ch.type === 'feishu-app' ? (ch.receive_id || '')
        : (ch.webhook || '');
      var short = target.length > 42 ? target.slice(0, 42) + '…' : target;
      tr.innerHTML =
        '<td>' + esc(ch.name) + '</td>' +
        '<td>' + esc(type) + '</td>' +
        '<td>' + on + '</td>' +
        '<td><code>' + esc(short) + '</code></td>' +
        '<td>' + (ch.template ? '<code>' + esc(ch.template) + '</code>' : '<span class="empty">全局</span>') + '</td>';
      var ops = document.createElement('td');
      ['测试', '编辑', '删除'].forEach(function (label) {
        var b = document.createElement('button');
        b.className = 'small' + (label === '删除' ? ' danger' : '');
        b.textContent = label;
        b.addEventListener('click', function () {
          if (label === '测试') testChannel(ch.id, b);
          else if (label === '编辑') openDialog(ch);
          else if (confirm('确定删除渠道「' + ch.name + '」？')) deleteChannel(ch.id);
        });
        ops.appendChild(b);
        ops.appendChild(document.createTextNode(' '));
      });
      tr.appendChild(ops);
      tb.appendChild(tr);
    });
  }

  function esc(s) {
    return String(s == null ? '' : s)
      .replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;').replace(/"/g, '&quot;');
  }

  function loadChannels() {
    return api('/api/config').then(function (c) {
      channels = c.channels || [];
      renderChannels();
    });
  }

  function deleteChannel(id) {
    api('/api/channel/delete', { method: 'POST', body: JSON.stringify({ id: id }) })
      .then(function () { return loadChannels(); })
      .then(loadStatus)
      .catch(function (e) { alert('删除失败：' + e.message); });
  }

  function testChannel(id, btn) {
    if (btn) { btn.disabled = true; btn.textContent = '发送中…'; }
    api('/api/channel/test', { method: 'POST', body: JSON.stringify({ id: id }) })
      .then(function () {
        setHint($('dlg-result'), '测试消息已发送', 'ok');
        loadLogs();
      })
      .catch(function (e) {
        setHint($('dlg-result'), '测试失败：' + e.message, 'err');
        if (!btn) alert('测试失败：' + e.message);
      })
      .finally(function () {
        if (btn) { btn.disabled = false; btn.textContent = '测试'; }
      });
  }

  /* ---------- 渠道编辑对话框 ---------- */
  var editingId = null;

  function syncTypeFields() {
    var isApp = $('f-type').value === 'feishu-app';
    document.querySelectorAll('.webhook-only').forEach(function (el) { el.hidden = isApp; });
    document.querySelectorAll('.app-only').forEach(function (el) { el.hidden = !isApp; });
  }

  function openDialog(ch) {
    editingId = ch ? ch.id : null;
    $('dlg-title').textContent = ch ? '编辑渠道' : '添加渠道';
      $('f-type').value = ch ? ch.type : 'feishu';
    $('f-name').value = ch ? ch.name : '';
    $('f-webhook').value = ch ? (ch.webhook || '') : '';
    $('f-secret').value = ch && ch.secret ? ch.secret : '';
    $('f-appid').value = ch ? (ch.app_id || '') : '';
    $('f-appsecret').value = ch ? (ch.app_secret || '') : '';
    $('f-receiveid').value = ch ? (ch.receive_id || '') : '';
    $('f-template').value = ch && ch.template ? ch.template : '';
    $('f-enabled').checked = ch ? !!ch.enabled : true;
    syncTypeFields();
    setHint($('dlg-result'), '');
    $('dlg').showModal();
  }

  function dialogChannel() {
    var t = $('f-type').value;
    return {
      id: editingId || genId(),
      type: t,
      name: $('f-name').value.trim(),
      webhook: t === 'feishu-app' ? '' : $('f-webhook').value.trim(),
      secret: t === 'feishu-app' ? '' : $('f-secret').value,
      app_id: t === 'feishu-app' ? $('f-appid').value.trim() : '',
      app_secret: t === 'feishu-app' ? $('f-appsecret').value : '',
      receive_id: t === 'feishu-app' ? $('f-receiveid').value.trim() : '',
      template: $('f-template').value.trim(),
      enabled: $('f-enabled').checked
    };
  }

  function saveDialog() {
    var ch = dialogChannel();
    setHint($('dlg-result'), '保存中…');
    api('/api/channel/save', { method: 'POST', body: JSON.stringify(ch) })
      .then(function () {
        $('dlg').close();
        return loadChannels().then(loadStatus);
      })
      .catch(function (e) { setHint($('dlg-result'), '保存失败：' + e.message, 'err'); });
  }

  function testDialog() {
    var ch = dialogChannel();
    setHint($('dlg-result'), '发送中…');
    api('/api/channel/test-new', { method: 'POST', body: JSON.stringify(ch) })
      .then(function () { setHint($('dlg-result'), '测试消息已发送', 'ok'); })
      .catch(function (e) { setHint($('dlg-result'), '测试失败：' + e.message, 'err'); });
  }

  function genId() {
    return 'c' + Date.now().toString(36) + Math.random().toString(36).slice(2, 8);
  }

  /* ---------- 立即推送 ---------- */
  function sendNow() {
    var btn = $('btn-send');
    btn.disabled = true;
    setHint($('send-result'), '正在获取公网 IP 并推送…');
    api('/api/send', { method: 'POST' })
      .then(function (r) {
        var lines = r.results.map(function (x) {
          return x.name + '：' + (x.ok ? '成功' : '失败（' + x.error + '）');
        });
        setHint($('send-result'), (r.ip ? '公网 IP：' + r.ip + '\n' : '') + lines.join('\n'),
          r.ok ? 'ok' : 'err');
      })
      .catch(function (e) { setHint($('send-result'), '推送失败：' + e.message, 'err'); })
      .finally(function () { btn.disabled = false; loadStatus(); loadLogs(); });
  }

  /* ---------- 日志（服务端日志 + 本地接口诊断合并） ---------- */
  function loadLogs() {
    return api('/api/logs').then(function (r) {
      var el = $('logs');
      el.innerHTML = '';
      var all = (r.logs || []).concat(localLogs);
      all.sort(function (a, b) { return a.time < b.time ? -1 : 1; });
      if (!all.length) {
        el.innerHTML = '<span class="empty">暂无日志</span>';
        return;
      }
      all.forEach(function (e) {
        var row = document.createElement('div');
        row.className = 'row' + (e.level === 'error' ? ' err' : '');
        var t = document.createElement('span');
        t.className = 't';
        t.textContent = fmtTime(e.time);
        row.appendChild(t);
        row.appendChild(document.createTextNode(e.msg));
        el.appendChild(row);
      });
      el.scrollTop = el.scrollHeight;
    });
  }

  /* ---------- 绑定（元素缺失时仅告警，不阻断其他功能） ---------- */
  function on(id, fn) {
    var el = $(id);
    if (!el) {
      diag('页面缺少元素 #' + id + '（HTML/JS 版本不一致？）', true);
      return;
    }
    el.addEventListener('click', fn);
  }
  on('btn-refresh', function () { probeNow($('btn-refresh')); });
  $('st-ip').addEventListener('click', copyIP);
  on('btn-send', sendNow);
  on('btn-save-cfg', saveConfig);
  on('btn-add', function () { openDialog(null); });
  on('dlg-ok', saveDialog);
  on('dlg-test', testDialog);
  on('dlg-cancel', function () { $('dlg').close(); });
  on('btn-theme', toggleTheme);
  $('f-type').addEventListener('change', syncTypeFields);

  function refresh() {
    loadStatus().catch(function (e) { /* 已在 showDiag 呈现 */ });
    loadConfig();
    loadChannels();
    loadLogs().catch(function () { /* 已在 showDiag 呈现 */ });
  }

  refresh();
  applyTheme();
  // 跟随系统偏好变化；自动模式下定时检测 fnOS 桌面主题切换
  if (window.matchMedia) {
    try {
      window.matchMedia('(prefers-color-scheme: dark)').addEventListener('change', applyTheme);
    } catch (e) { /* 旧浏览器忽略 */ }
  }
  setInterval(applyTheme, 2000);
  setInterval(loadStatus, 30000);
})();
