/* ===== API 2 Cursor – Admin Panel JS ===== */

(function () {
  'use strict';

  const API = '';
  let token = sessionStorage.getItem('admin_token') || '';

  // ── DOM refs ──
  const $loginPage  = document.getElementById('login-page');
  const $app        = document.getElementById('app');
  const $loginForm  = document.getElementById('login-form');
  const $loginKey   = document.getElementById('login-key');
  const $loginSubmit = document.getElementById('login-submit');
  const $loginReveal = document.getElementById('login-reveal');
  const $logoutBtn  = document.getElementById('btn-logout');
  const $curUser    = document.getElementById('current-user');
  const $modalOv    = document.getElementById('modal-overlay');
  const $modalTitle = document.getElementById('modal-title');
  const $modalForm  = document.getElementById('modal-form');
  const $sidebar    = document.getElementById('sidebar');
  const $hamburger  = document.getElementById('hamburger');
  const $backdrop   = document.getElementById('sidebar-backdrop');
  const $mobileTitle = document.getElementById('mobile-title');
  let channelOptions = [];

  var SECTION_TITLES = { stats: '统计概览', keys: '密钥管理', channels: '渠道管理', mappings: '模型映射', details: '请求详情' };

  var STAT_SVGS = {
    chart: '<svg width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M18 20V10"/><path d="M12 20V4"/><path d="M6 20v-6"/></svg>',
    check: '<svg width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M22 11.08V12a10 10 0 1 1-5.93-9.14"/><polyline points="22 4 12 14.01 9 11.01"/></svg>',
    'x-circle': '<svg width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><circle cx="12" cy="12" r="10"/><line x1="15" y1="9" x2="9" y2="15"/><line x1="9" y1="9" x2="15" y2="15"/></svg>',
    'arrow-down': '<svg width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><line x1="12" y1="5" x2="12" y2="19"/><polyline points="19 12 12 19 5 12"/></svg>',
    'arrow-up': '<svg width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><line x1="12" y1="19" x2="12" y2="5"/><polyline points="5 12 12 5 19 12"/></svg>',
    user: '<svg width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M20 21v-2a4 4 0 0 0-4-4H8a4 4 0 0 0-4 4v2"/><circle cx="12" cy="7" r="4"/></svg>',
    key: '<svg width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M21 2l-2 2m-7.61 7.61a5.5 5.5 0 1 1-7.778 7.778 5.5 5.5 0 0 1 7.777-7.777zm0 0L15.5 7.5m0 0l3 3L22 7l-3-3m-3.5 3.5L19 4"/></svg>',
    link: '<svg width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M10 13a5 5 0 0 0 7.54.54l3-3a5 5 0 1 0-7.07-7.07l-1.72 1.71"/><path d="M14 11a5 5 0 0 0-7.54-.54l-3 3a5 5 0 0 0 7.07 7.07l1.71-1.71"/></svg>',
  };

  function formatIPWithGeo(ip, location) {
    if (!ip) return '-';
    return 'IP ' + escHtml(ip) + (location ? '<span class="ip-geo"> ' + escHtml(location) + '</span>' : '');
  }

  // ── Utility ──

  function headers(extra) {
    const h = { 'Content-Type': 'application/json' };
    if (token) h['Authorization'] = 'Bearer ' + token;
    return Object.assign(h, extra);
  }

  async function api(method, path, body) {
    const opts = { method, headers: headers() };
    if (body !== undefined) opts.body = JSON.stringify(body);
    const resp = await fetch(API + path, opts);
    if (resp.status === 401) { logout(); throw new Error('认证失败'); }
    const data = await resp.json().catch(() => ({}));
    if (!resp.ok) throw new Error(data.error || data.message || '请求失败 ' + resp.status);
    return data;
  }

  // ── Toast ──

  function toast(msg, type) {
    type = type || 'info';
    const el = document.createElement('div');
    el.className = 'toast toast-' + type;
    el.textContent = msg;
    document.getElementById('toast-container').appendChild(el);
    setTimeout(function () {
      el.classList.add('fade-out');
      setTimeout(function () { el.remove(); }, 260);
    }, 3000);
  }

  // ── Auth ──

  function closeMobileNav() {
    if (!$sidebar || !$backdrop) return;
    $sidebar.classList.remove('open');
    $backdrop.classList.add('hidden');
  }

  function toggleMobileNav() {
    if (!$sidebar || !$backdrop) return;
    var open = $sidebar.classList.toggle('open');
    $backdrop.classList.toggle('hidden', !open);
  }

  function showApp() {
    $loginPage.classList.add('hidden');
    $app.classList.remove('hidden');
    $curUser.textContent = '已登录';
    closeMobileNav();
    switchTab('stats');
  }

  function logout() {
    token = '';
    sessionStorage.removeItem('admin_token');
    $app.classList.add('hidden');
    $loginPage.classList.remove('hidden');
    $loginKey.value = '';
  }

  if ($loginReveal && $loginKey) {
    var eye = $loginReveal.querySelector('.icon-eye');
    var eyeOff = $loginReveal.querySelector('.icon-eye-off');
    $loginReveal.addEventListener('click', function () {
      var toText = $loginKey.type === 'password';
      $loginKey.type = toText ? 'text' : 'password';
      $loginReveal.setAttribute('aria-pressed', toText ? 'true' : 'false');
      $loginReveal.setAttribute('aria-label', toText ? '隐藏密钥' : '显示密钥');
      if (eye && eyeOff) {
        eye.classList.toggle('hidden', toText);
        eyeOff.classList.toggle('hidden', !toText);
      }
    });
  }

  $loginForm.addEventListener('submit', async function (e) {
    e.preventDefault();
    const key = $loginKey.value.trim();
    if (!key) return;
    if ($loginSubmit) {
      $loginSubmit.disabled = true;
      $loginSubmit.classList.add('is-loading');
    }
    try {
      const res = await fetch(API + '/api/admin/login', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ key: key }),
      });
      const data = await res.json().catch(function () { return {}; });
      if (!res.ok) throw new Error(data.error || '登录失败');
      token = data.token || key;
      sessionStorage.setItem('admin_token', token);
      toast('登录成功', 'success');
      showApp();
    } catch (err) {
      toast(err.message, 'error');
    } finally {
      if ($loginSubmit) {
        $loginSubmit.disabled = false;
        $loginSubmit.classList.remove('is-loading');
      }
    }
  });

  $logoutBtn.addEventListener('click', function () {
    logout();
    toast('已退出登录', 'info');
  });

  if ($hamburger) {
    $hamburger.addEventListener('click', function () { toggleMobileNav(); });
  }
  if ($backdrop) {
    $backdrop.addEventListener('click', function () { closeMobileNav(); });
  }

  // ── Sidebar / Tabs ──

  const navItems = document.querySelectorAll('.nav-item');
  const tabPanels = document.querySelectorAll('.tab-panel');

  function switchTab(name) {
    navItems.forEach(function (b) { b.classList.toggle('active', b.dataset.tab === name); });
    tabPanels.forEach(function (p) { p.classList.toggle('active', p.id === 'tab-' + name); });
    if ($mobileTitle) {
      $mobileTitle.textContent = SECTION_TITLES[name] || 'API 2 Cursor';
    }
    closeMobileNav();
    var loaders = { stats: loadStats, keys: loadKeys, channels: loadChannels, mappings: loadMappings, details: loadDetails };
    if (loaders[name]) loaders[name]();
  }

  navItems.forEach(function (b) {
    b.addEventListener('click', function () { switchTab(b.dataset.tab); });
  });

  // ── Helpers ──

  function fmtDate(s) {
    if (!s) return '-';
    var d = new Date(s);
    return d.toLocaleDateString('zh-CN') + ' ' + d.toLocaleTimeString('zh-CN', { hour: '2-digit', minute: '2-digit' });
  }

  function statusBadge(v) {
    return v === 1 || v === '1'
      ? '<span class="badge badge-active">启用</span>'
      : '<span class="badge badge-disabled">禁用</span>';
  }

  function typeBadge(t) {
    return '<span class="badge badge-type">' + escHtml(t) + '</span>';
  }

  function escHtml(s) {
    if (s === null || s === undefined) return '';
    var div = document.createElement('div');
    div.textContent = String(s);
    return div.innerHTML;
  }

  function fmtNum(n) {
    if (n === undefined || n === null) return '0';
    return Number(n).toLocaleString('zh-CN');
  }

  function emptyRow(cols, msg) {
    return '<tr class="empty-row"><td colspan="' + cols + '">' + (msg || '暂无数据') + '</td></tr>';
  }

  function formatChannelNames(channelIds) {
    if (!channelIds) return '全部';
    var ids = String(channelIds).split(',').map(function (s) { return s.trim(); }).filter(Boolean);
    if (!ids.length) return '全部';
    if (!channelOptions.length) return ids.join(', ');
    return ids.map(function (id) {
      var ch = channelOptions.find(function (x) { return String(x.id) === String(id); });
      return ch ? (ch.name + ' (#' + ch.id + ')') : ('#' + id);
    }).join(', ');
  }

  function formatRouteName(route) {
    route = String(route || 'all').toLowerCase();
    if (route === 'chat') return 'Chat Completions';
    if (route === 'messages') return 'Messages';
    if (route === 'responses') return 'Responses';
    return '全部接口';
  }

  function fieldMultiCheckbox(name, label, values, options, help) {
    var selected = new Set((values || []).map(String));
    var html = '<div class="form-group"><label>' + label + '</label>';
    html += '<div class="checkbox-tools">'
      + '<button type="button" class="btn btn-sm btn-outline" onclick="toggleAllChannels(true)">全选</button>'
      + '<button type="button" class="btn btn-sm btn-outline" onclick="toggleAllChannels(false)">清空</button>'
      + '</div>';
    html += '<div class="checkbox-group">';
    if (!options.length) {
      html += '<div class="form-help">暂无渠道，请先创建渠道</div>';
    } else {
      options.forEach(function (o) {
        var checked = selected.has(String(o.value)) ? ' checked' : '';
        html += '<label class="checkbox-item">'
          + '<input type="checkbox" name="' + name + '" value="' + escHtml(o.value) + '"' + checked + '>'
          + '<span><strong>' + escHtml(o.label) + '</strong><em>' + escHtml(o.meta || '') + '</em></span>'
          + '</label>';
      });
    }
    html += '</div>';
    if (help) html += '<div class="form-help">' + help + '</div>';
    html += '</div>';
    return html;
  }

  async function ensureChannels() {
    if (channelOptions.length) return channelOptions;
    var list = await api('GET', '/api/admin/channels');
    var rows = Array.isArray(list) ? list : ((list && list.data) || []);
    channelOptions = rows.map(function (c) {
      return {
        id: c.id,
        name: c.name,
        value: c.id,
        label: c.name + ' (#' + c.id + ')',
        meta: (c.type || 'unknown') + ' · ' + (c.models || '全部模型')
      };
    });
    return channelOptions;
  }

  // ── Stats ──

  window.loadStats = async function () {
    try {
      var data = await api('GET', '/api/admin/stats');
      var cards = [
        { label: '总请求数', value: fmtNum(data.total_requests), color: 'blue', accent: 'accent-blue', svg: 'chart' },
        { label: '成功请求', value: fmtNum(data.success_requests), color: 'green', accent: 'accent-green', svg: 'check' },
        { label: '失败请求', value: fmtNum(data.error_requests), color: 'red', accent: 'accent-red', svg: 'x-circle' },
        { label: '输入 Token', value: fmtNum(data.total_input_tokens), color: 'orange', accent: 'accent-orange', svg: 'arrow-down' },
        { label: '输出 Token', value: fmtNum(data.total_output_tokens), color: 'purple', accent: 'accent-purple', svg: 'arrow-up' },
        { label: '连接 IP 数', value: fmtNum(data.active_users), color: 'cyan', accent: 'accent-cyan', svg: 'user' },
        { label: '活跃密钥', value: fmtNum(data.active_keys), color: 'green', accent: 'accent-green', svg: 'key' },
        { label: '活跃渠道', value: fmtNum(data.active_channels), color: 'blue', accent: 'accent-blue', svg: 'link' },
      ];
      var html = '';
      cards.forEach(function (c) {
        var icon = STAT_SVGS[c.svg] || '';
        html += '<div class="stat-card ' + c.accent + '">'
          + '<div class="stat-icon">' + icon + '</div>'
          + '<div class="stat-label">' + escHtml(c.label) + '</div>'
          + '<div class="stat-value">' + escHtml(c.value) + '</div>'
          + '</div>';
      });
      document.getElementById('stats-cards').innerHTML = html;
    } catch (err) {
      toast('加载统计失败: ' + err.message, 'error');
    }
  };

  // ── Keys ──

  async function loadKeys() {
    try {
      await ensureChannels();
      var list = await api('GET', '/api/admin/keys');
      var rows = Array.isArray(list) ? list : ((list && list.data) || []);
      var tbody = document.getElementById('keys-tbody');
      if (!rows.length) { tbody.innerHTML = emptyRow(9); return; }
      tbody.innerHTML = rows.map(function (k) {
        var stats = '请求 ' + fmtNum(k.request_count || 0)
          + ' / 成功 ' + fmtNum(k.success_count || 0)
          + ' / 失败 ' + fmtNum(k.error_count || 0)
          + ' / 输入 ' + fmtNum(k.input_tokens || 0)
          + ' / 输出 ' + fmtNum(k.output_tokens || 0)
          + ' / 最后使用 ' + (k.last_used_at ? fmtDate(k.last_used_at) : '未使用');
        return '<tr>'
          + '<td data-label="ID">' + k.id + '</td>'
          + '<td data-label="名称"><div>' + escHtml(k.name) + '</div><div class="cell-sub">' + escHtml(k.remark || '') + '</div><div class="cell-sub">' + escHtml(stats) + '</div></td>'
          + '<td class="cell-key" data-label="密钥" title="' + escHtml(k.key) + '">' + escHtml(k.key) + '</td>'
          + '<td data-label="绑定渠道">' + formatChannelNames(k.channel_ids) + '</td>'
          + '<td data-label="状态">' + statusBadge(k.status) + '</td>'
          + '<td data-label="QPM">' + (k.qpm || '无限') + '</td>'
          + '<td data-label="创建时间">' + fmtDate(k.created_at) + '</td>'
          + '<td class="actions">'
          +   '<button class="btn btn-sm btn-outline" onclick="copyKey(' + k.id + ')">复制</button>'
          +   '<button class="btn btn-sm btn-outline" onclick="toggleKeyStatus(' + k.id + ')">' + (k.status === 1 ? '禁用' : '启用') + '</button>'
          +   '<button class="btn btn-sm btn-outline" onclick="editKey(' + k.id + ')">编辑</button>'
          +   '<button class="btn btn-sm btn-danger btn-icon" onclick="deleteKey(' + k.id + ')">删除</button>'
          + '</td></tr>';
      }).join('');
    } catch (err) { toast('加载密钥失败: ' + err.message, 'error'); }
  }

  window.editKey = async function (id) {
    try {
      var list = await api('GET', '/api/admin/keys');
      var rows = Array.isArray(list) ? list : ((list && list.data) || []);
      var k = rows.find(function (x) { return x.id === id; });
      if (!k) { toast('未找到密钥', 'error'); return; }
      openModal('key', k);
    } catch (err) { toast(err.message, 'error'); }
  };

  window.copyKey = async function (id) {
    try {
      var list = await api('GET', '/api/admin/keys');
      var rows = Array.isArray(list) ? list : ((list && list.data) || []);
      var k = rows.find(function (x) { return x.id === id; });
      if (!k) { toast('未找到密钥', 'error'); return; }
      await navigator.clipboard.writeText(k.key);
      toast('密钥已复制', 'success');
    } catch (err) {
      toast('复制失败: ' + err.message, 'error');
    }
  };

  window.toggleKeyStatus = async function (id) {
    try {
      await api('POST', '/api/admin/keys/toggle/' + id);
      toast('状态已切换', 'success');
      loadKeys();
    } catch (err) {
      toast(err.message, 'error');
    }
  };

  window.deleteKey = async function (id) {
    if (!confirm('确定删除此密钥？')) return;
    try {
      await api('DELETE', '/api/admin/keys/' + id);
      toast('密钥已删除', 'success');
      loadKeys();
    } catch (err) { toast(err.message, 'error'); }
  };

  // ── Channels ──

  async function loadChannels() {
    try {
      var list = await api('GET', '/api/admin/channels');
      var rows = Array.isArray(list) ? list : ((list && list.data) || []);
    channelOptions = rows.map(function (c) {
      return {
        id: c.id,
        name: c.name,
        value: c.id,
        label: c.name + ' (#' + c.id + ')',
        meta: (c.type || 'unknown') + ' · ' + (c.models || '全部模型')
      };
    });
      var tbody = document.getElementById('channels-tbody');
      if (!rows.length) { tbody.innerHTML = emptyRow(13); return; }
      tbody.innerHTML = rows.map(function (c) {
        return '<tr>'
          + '<td data-label="ID">' + c.id + '</td>'
          + '<td data-label="名称">' + escHtml(c.name) + '</td>'
          + '<td data-label="类型">' + typeBadge(c.type) + '</td>'
          + '<td class="cell-models" data-label="模型" title="' + escHtml(c.models) + '">' + escHtml(c.models) + '</td>'
          + '<td data-label="状态">' + statusBadge(c.status) + '</td>'
          + '<td data-label="优先级">' + c.priority + '</td>'
          + '<td data-label="权重">' + c.weight + '</td>'
          + '<td data-label="QPM">' + (c.qpm || '无限') + '</td>'
          + '<td data-label="调用">' + fmtNum(c.used_count) + '</td>'
          + '<td data-label="失败">' + fmtNum(c.fail_count) + '</td>'
          + '<td data-label="输入Token">' + fmtNum(c.input_tokens) + '</td>'
          + '<td data-label="输出Token">' + fmtNum(c.output_tokens) + '</td>'
          + '<td class="actions">'
          +   '<button class="btn btn-sm btn-outline" onclick="editChannel(' + c.id + ')">编辑</button>'
          +   '<button class="btn btn-sm btn-danger btn-icon" onclick="deleteChannel(' + c.id + ')">删除</button>'
          + '</td></tr>';
      }).join('');
    } catch (err) { toast('加载渠道失败: ' + err.message, 'error'); }
  }

  window.editChannel = async function (id) {
    try {
      var list = await api('GET', '/api/admin/channels');
      var rows = Array.isArray(list) ? list : ((list && list.data) || []);
      var c = rows.find(function (x) { return x.id === id; });
      if (!c) { toast('未找到渠道', 'error'); return; }
      openModal('channel', c);
    } catch (err) { toast(err.message, 'error'); }
  };

  window.deleteChannel = async function (id) {
    if (!confirm('确定删除此渠道？')) return;
    try {
      await api('DELETE', '/api/admin/channels/' + id);
      toast('渠道已删除', 'success');
      loadChannels();
    } catch (err) { toast(err.message, 'error'); }
  };

  // ── Mappings ──

  async function loadMappings() {
    try {
      await ensureChannels();
      var list = await api('GET', '/api/admin/mappings');
      var rows = Array.isArray(list) ? list : ((list && list.data) || []);
      var tbody = document.getElementById('mappings-tbody');
      if (!rows.length) { tbody.innerHTML = emptyRow(10); return; }
      tbody.innerHTML = rows.map(function (m) {
        return '<tr>'
          + '<td data-label="ID">' + m.id + '</td>'
          + '<td data-label="规则名称"><div>' + escHtml(m.name || '') + '</div><div class="cell-sub">' + escHtml(m.description || '') + '</div></td>'
          + '<td data-label="客户端模型"><code>' + escHtml(m.client_model) + '</code></td>'
          + '<td data-label="适用接口">' + escHtml(formatRouteName(m.route)) + '</td>'
          + '<td data-label="适用渠道">' + formatChannelNames(m.channel_ids) + '</td>'
          + '<td data-label="上游模型"><code>' + escHtml(m.upstream_model) + '</code></td>'
          + '<td data-label="优先级">' + fmtNum(m.priority || 0) + '</td>'
          + '<td data-label="状态">' + statusBadge(m.status) + '</td>'
          + '<td data-label="创建时间">' + fmtDate(m.created_at) + '</td>'
          + '<td class="actions">'
          +   '<button class="btn btn-sm btn-outline" onclick="editMapping(' + m.id + ')">编辑</button>'
          +   '<button class="btn btn-sm btn-danger btn-icon" onclick="deleteMapping(' + m.id + ')">删除</button>'
          + '</td></tr>';
      }).join('');
    } catch (err) { toast('加载映射失败: ' + err.message, 'error'); }
  }

  window.editMapping = async function (id) {
    try {
      var list = await api('GET', '/api/admin/mappings');
      var rows = Array.isArray(list) ? list : ((list && list.data) || []);
      var m = rows.find(function (x) { return x.id === id; });
      if (!m) { toast('未找到映射', 'error'); return; }
      openModal('mapping', m);
    } catch (err) { toast(err.message, 'error'); }
  };

  window.deleteMapping = async function (id) {
    if (!confirm('确定删除此映射？')) return;
    try {
      await api('DELETE', '/api/admin/mappings/' + id);
      toast('映射已删除', 'success');
      loadMappings();
    } catch (err) { toast(err.message, 'error'); }
  };

  // ── Request Details ──

  var detailsPage = 0;
  var detailsLimit = 20;

  window.loadDetails = async function (page) {
    if (page !== undefined) detailsPage = page;
    var offset = detailsPage * detailsLimit;
    try {
      var res = await api('GET', '/api/admin/request-details?limit=' + detailsLimit + '&offset=' + offset);
      var rows = res.data || [];
      var total = res.total || 0;
      document.getElementById('details-summary').textContent = '共 ' + total + ' 条记录（最多保留 200 条）';

      var list = document.getElementById('details-list');
      if (!rows.length) {
        list.innerHTML = '<div class="card" style="padding:48px;text-align:center;color:var(--text-muted)">暂无请求详情记录</div>';
        document.getElementById('details-pager').innerHTML = '';
        return;
      }

      var html = '';
      rows.forEach(function (d) {
        var statusCls = d.status === 'success' ? 'badge-active' : 'badge-disabled';
        var streamLabel = d.stream ? '流式' : '非流式';
        html += '<div class="detail-card card" onclick="viewDetail(' + d.id + ')" style="cursor:pointer">'
          + '<div class="detail-card-header">'
          +   '<div class="detail-card-meta">'
          +     '<span class="badge ' + statusCls + '">' + escHtml(d.status) + '</span>'
          +     '<span class="badge badge-type">' + escHtml(d.route) + '</span>'
          +     '<span class="badge badge-user">' + escHtml(streamLabel) + '</span>'
          +     '<code>' + escHtml(d.client_model) + '</code>'
          +     (d.upstream_model && d.upstream_model !== d.client_model ? ' → <code>' + escHtml(d.upstream_model) + '</code>' : '')
          +   '</div>'
          +   '<div class="detail-card-time">' + fmtDate(d.created_at) + '</div>'
          + '</div>'
          + '<div class="detail-card-body">'
          +   '<div class="detail-field"><span class="detail-label">用户消息</span><span class="detail-value">' + escHtml(truncText(d.user_message, 200)) + '</span></div>'
          +   (d.prompt ? '<div class="detail-field"><span class="detail-label">提示词</span><span class="detail-value">' + escHtml(truncText(d.prompt, 120)) + '</span></div>' : '')
          +   '<div class="detail-card-stats">'
          +     (d.input_tokens ? '<span>输入 ' + fmtNum(d.input_tokens) + '</span>' : '')
          +     (d.output_tokens ? '<span>输出 ' + fmtNum(d.output_tokens) + '</span>' : '')
          +     (d.duration_ms ? '<span>' + d.duration_ms + 'ms</span>' : '')
          +     (d.client_ip ? '<span>' + formatIPWithGeo(d.client_ip, d.client_ip_location) + '</span>' : '')
          +   '</div>'
          + '</div>'
          + '</div>';
      });
      list.innerHTML = html;

      // 分页
      var totalPages = Math.ceil(total / detailsLimit);
      var pager = '';
      if (totalPages > 1) {
        pager += '<button class="btn btn-sm btn-outline" ' + (detailsPage <= 0 ? 'disabled' : '') + ' onclick="loadDetails(' + (detailsPage - 1) + ')">上一页</button>';
        pager += '<span style="line-height:32px;color:var(--text-secondary);font-size:13px">' + (detailsPage + 1) + ' / ' + totalPages + '</span>';
        pager += '<button class="btn btn-sm btn-outline" ' + (detailsPage >= totalPages - 1 ? 'disabled' : '') + ' onclick="loadDetails(' + (detailsPage + 1) + ')">下一页</button>';
      }
      document.getElementById('details-pager').innerHTML = pager;
    } catch (err) {
      toast('加载请求详情失败: ' + err.message, 'error');
    }
  };

  window.viewDetail = async function (id) {
    try {
      var d = await api('GET', '/api/admin/request-details/' + id);

      // 设置元数据供下载命名使用
      _detailMeta.backend = d.backend || '';
      _detailMeta.model = d.upstream_model || d.client_model || '';
      _detailMeta.time = d.created_at || '';

      var html = '<div class="detail-view">';

      // 基本信息
      html += '<div class="detail-section">'
        + '<h4 class="detail-section-title">基本信息</h4>'
        + '<div class="detail-grid">'
        +   detailItem('状态', d.status)
        +   detailItem('路由', d.route)
        +   detailItem('后端', d.backend)
        +   detailItem('流式', d.stream ? '是' : '否')
        +   detailItem('客户端模型', d.client_model)
        +   detailItem('上游模型', d.upstream_model)
        +   detailItem('输入Token', fmtNum(d.input_tokens))
        +   detailItem('输出Token', fmtNum(d.output_tokens))
        +   detailItem('耗时', d.duration_ms + 'ms')
        +   detailItemHtml('客户端IP', d.client_ip ? formatIPWithGeo(d.client_ip, d.client_ip_location).replace(/^IP\s/, '') : '-')
        +   detailItem('时间', fmtDate(d.created_at))
        + '</div></div>';

      if (d.error_msg) {
        html += '<div class="detail-section"><h4 class="detail-section-title" style="color:var(--red)">错误信息</h4>' + renderTruncatedPre(d.error_msg, '错误信息') + '</div>';
      }

      // 用户消息
      if (d.user_message) {
        html += '<div class="detail-section"><h4 class="detail-section-title">用户消息</h4>' + renderTruncatedPre(d.user_message, '用户消息') + '</div>';
      }

      // 提示词
      if (d.prompt) {
        html += '<div class="detail-section"><h4 class="detail-section-title">提示词 / 系统消息</h4>' + renderTruncatedPre(d.prompt, '提示词') + '</div>';
      }

      // AI 响应
      if (d.ai_response) {
        html += '<div class="detail-section"><h4 class="detail-section-title">AI 响应</h4>' + renderTruncatedPre(d.ai_response, 'AI响应') + '</div>';
      }

      // 工具调用
      if (d.tool_calls) {
        html += '<div class="detail-section"><h4 class="detail-section-title">工具调用</h4>' + renderTruncatedPre(tryFormatJSON(d.tool_calls), '工具调用') + '</div>';
      }

      // 请求头
      if (d.request_headers) {
        html += '<div class="detail-section"><h4 class="detail-section-title">请求头</h4>' + renderTruncatedPre(tryFormatJSON(d.request_headers), '请求头') + '</div>';
      }

      // 请求体
      if (d.request_body) {
        html += '<div class="detail-section"><h4 class="detail-section-title">请求体</h4>' + renderTruncatedPre(tryFormatJSON(d.request_body), '请求体', 'detail-code-long') + '</div>';
      }

      html += '</div>';
      document.getElementById('detail-modal-body').innerHTML = html;
      document.getElementById('detail-modal-title').textContent = '请求详情 #' + d.id;
      document.getElementById('detail-modal-overlay').classList.remove('hidden');
    } catch (err) {
      toast('加载详情失败: ' + err.message, 'error');
    }
  };

  window.closeDetailModal = function () {
    document.getElementById('detail-modal-overlay').classList.add('hidden');
    document.getElementById('detail-modal-body').innerHTML = '';
  };

  // 点击遮罩关闭详情弹窗
  (function () {
    var ov = document.getElementById('detail-modal-overlay');
    if (ov) {
      ov.addEventListener('mousedown', function (e) { ov.dataset.md = e.target === ov ? '1' : '0'; });
      ov.addEventListener('mouseup', function (e) { if (e.target === ov && ov.dataset.md === '1') closeDetailModal(); ov.dataset.md = '0'; });
    }
  })();

  window.clearDetails = async function () {
    if (!confirm('确定清空所有请求详情记录？此操作不可恢复。')) return;
    try {
      await api('DELETE', '/api/admin/request-details');
      toast('已清空所有请求详情', 'success');
      loadDetails(0);
    } catch (err) {
      toast(err.message, 'error');
    }
  };

  function detailItem(label, value) {
    return '<div class="detail-grid-item"><span class="detail-grid-label">' + escHtml(label) + '</span><span class="detail-grid-value">' + escHtml(value || '-') + '</span></div>';
  }

  function detailItemHtml(label, html) {
    return '<div class="detail-grid-item"><span class="detail-grid-label">' + escHtml(label) + '</span><span class="detail-grid-value">' + (html || '-') + '</span></div>';
  }

  function truncText(s, max) {
    if (!s) return '';
    if (s.length <= max) return s;
    return s.substring(0, max) + '...';
  }

  // 长文本截断阈值
  var MAX_LINES_TO_SHOW = 5;
  // 当前详情弹窗的元数据（用于下载命名）
  var _detailMeta = { backend: '', model: '', time: '' };

  function tryFormatJSON(s) {
    if (!s) return '';
    try {
      var parsed = JSON.parse(s);
      return JSON.stringify(parsed, null, 2);
    } catch (e) {
      // 可能是多行拼接或转义问题，尝试清理后再解析
      try {
        var cleaned = s.trim();
        var parsed2 = JSON.parse(cleaned);
        return JSON.stringify(parsed2, null, 2);
      } catch (e2) {
        return s;
      }
    }
  }

  /**
   * 将长文本截断为指定行数，返回 { above, remaining, full }
   */
  function truncateLines(text, maxLines) {
    if (!text) return { above: '', remaining: 0, full: '' };
    var lines = text.split('\n');
    var total = lines.length;
    if (total <= maxLines + 1) {
      return { above: text, remaining: 0, full: text };
    }
    var above = lines.slice(0, maxLines).join('\n');
    return { above: above, remaining: total - maxLines, full: text };
  }

  /**
   * 生成下载文件名：渠道_模型_时间戳_段名.txt
   */
  function makeDownloadName(sectionLabel) {
    var b = (_detailMeta.backend || 'unknown').replace(/[\\/:*?"<>|]/g, '_');
    var m = (_detailMeta.model || 'unknown').replace(/[\\/:*?"<>|]/g, '_');
    var t = (_detailMeta.time || new Date().toISOString()).replace(/[:\\s]/g, '-').replace(/\.\d+Z?$/, '');
    var s = (sectionLabel || 'data').replace(/[\\/:*?"<>|\s]/g, '_');
    return b + '_' + m + '_' + t + '_' + s + '.txt';
  }

  /**
   * 下载文本为文件
   */
  function downloadText(text, filename) {
    var blob = new Blob([text], { type: 'text/plain;charset=utf-8' });
    var a = document.createElement('a');
    a.href = URL.createObjectURL(blob);
    a.download = filename;
    document.body.appendChild(a);
    a.click();
    setTimeout(function () { URL.revokeObjectURL(a.href); a.remove(); }, 200);
  }

  /**
   * 渲染可截断的 <pre> 代码块
   * 折叠时显示前 N 行 + 折叠提示
   * 展开后显示完整内容（不折断，可横向滚动）
   * 每个区段带下载按钮
   */
  function renderTruncatedPre(text, sectionLabel, extraClass) {
    if (!text) return '';
    var cls = 'detail-code detail-code-nowrap' + (extraClass ? ' ' + extraClass : '');
    var uid = 'tc-' + Math.random().toString(36).slice(2, 10);
    var info = truncateLines(text, MAX_LINES_TO_SHOW);

    // 下载按钮（始终显示）
    var dlBtn = '<button class="btn btn-sm btn-outline detail-dl-btn" '
      + 'onclick="downloadSection(\'' + uid + '\', \'' + escHtml(sectionLabel || '') + '\')" title="下载此段内容">'
      + '<svg width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">'
      + '<path d="M21 15v4a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2v-4"/><polyline points="7 10 12 15 17 10"/>'
      + '<line x1="12" y1="15" x2="12" y2="3"/></svg>'
      + '<span>下载</span></button>';

    if (info.remaining <= 0) {
      // 内容不长，直接全部显示 + 下载按钮
      return '<div class="detail-truncated" id="' + uid + '">'
        + '<pre class="' + cls + '">' + escHtml(info.full) + '</pre>'
        + '<div class="detail-action-bar">' + dlBtn + '</div>'
        + '</div>';
    }

    // 折叠 + 展开
    return '<div class="detail-truncated" id="' + uid + '">'
      + '<pre class="' + cls + ' detail-code-collapsed" data-uid="' + uid + '">' + escHtml(info.above) + '</pre>'
      + '<div class="detail-expand-bar" onclick="expandTruncated(\'' + uid + '\')">'
      +   '<span class="detail-expand-hint">… 还有 ' + info.remaining + ' 行</span>'
      +   '<button class="btn btn-sm btn-outline detail-expand-btn">展开全部</button>'
      + '</div>'
      + '<pre class="' + cls + ' detail-code-full hidden">' + escHtml(info.full) + '</pre>'
      + '<div class="detail-action-bar">'
      +   '<button class="btn btn-sm btn-ghost detail-collapse-btn hidden" onclick="collapseTruncated(\'' + uid + '\')">'
      +     '<svg width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><polyline points="18 15 12 9 6 15"/></svg>'
      +     '<span>收起</span></button>'
      +   dlBtn
      + '</div>'
      + '</div>';
  }

  window.expandTruncated = function (uid) {
    var wrap = document.getElementById(uid);
    if (!wrap) return;
    var collapsed = wrap.querySelector('.detail-code-collapsed');
    var full = wrap.querySelector('.detail-code-full');
    var bar = wrap.querySelector('.detail-expand-bar');
    var collapseBtn = wrap.querySelector('.detail-collapse-btn');
    if (collapsed) collapsed.classList.add('hidden');
    if (bar) bar.classList.add('hidden');
    if (full) full.classList.remove('hidden');
    if (collapseBtn) collapseBtn.classList.remove('hidden');
  };

  window.collapseTruncated = function (uid) {
    var wrap = document.getElementById(uid);
    if (!wrap) return;
    var collapsed = wrap.querySelector('.detail-code-collapsed');
    var full = wrap.querySelector('.detail-code-full');
    var bar = wrap.querySelector('.detail-expand-bar');
    var collapseBtn = wrap.querySelector('.detail-collapse-btn');
    if (collapsed) collapsed.classList.remove('hidden');
    if (bar) bar.classList.remove('hidden');
    if (full) full.classList.add('hidden');
    if (collapseBtn) collapseBtn.classList.add('hidden');
  };

  window.downloadSection = function (uid, sectionLabel) {
    var wrap = document.getElementById(uid);
    if (!wrap) return;
    // 取完整内容（优先从 full 块取，兜底从 collapsed 或唯一 pre 取）
    var fullPre = wrap.querySelector('.detail-code-full');
    var pre = fullPre || wrap.querySelector('pre');
    if (!pre) return;
    var text = pre.textContent || '';
    downloadText(text, makeDownloadName(sectionLabel));
    toast('已下载 ' + (sectionLabel || '内容'), 'success');
  };

  // ── Modal ──

  var modalState = { type: '', editId: null };

  function field(name, label, type, value, opts) {
    opts = opts || {};
    var id = 'f-' + name;
    var html = '<div class="form-group">';
    html += '<label for="' + id + '">' + label + '</label>';
    if (type === 'select') {
      html += '<div class="select-wrap"><select id="' + id + '" name="' + name + '">';
      (opts.options || []).forEach(function (o) {
        var sel = String(value) === String(o.value) ? ' selected' : '';
        html += '<option value="' + o.value + '"' + sel + '>' + o.label + '</option>';
      });
      html += '</select></div>';
    } else if (type === 'textarea') {
      html += '<textarea id="' + id + '" name="' + name + '" rows="' + (opts.rows || 3) + '">' + escHtml(value) + '</textarea>';
    } else {
      html += '<input type="' + type + '" id="' + id + '" name="' + name + '" value="' + escHtml(value) + '"'
            + (opts.placeholder ? ' placeholder="' + opts.placeholder + '"' : '')
            + (opts.required ? ' required' : '')
            + '>';
    }
    html += '</div>';
    return html;
  }

  function fieldRow() {
    var args = Array.prototype.slice.call(arguments);
    return '<div class="form-row">' + args.join('') + '</div>';
  }

  window.openModal = async function (type, data) {
    data = data || {};
    modalState.type = type;
    modalState.editId = data.id || null;
    var isEdit = !!data.id;
    var html = '';

    if (type === 'key' || type === 'mapping') {
      await ensureChannels();
    }

    switch (type) {
      case 'key':
        $modalTitle.textContent = isEdit ? '编辑密钥' : '新增密钥';
        html += field('name', '名称', 'text', data.name || '', { required: true });
        html += field('remark', '备注', 'text', data.remark || '', { placeholder: '例如：客服专用 / 项目A / 内部测试' });
        html += field('key', '密钥', 'text', data.key || '', { placeholder: '留空自动生成', required: false });
        html += field('qpm', 'QPM (0=无限)', 'number', data.qpm || 0);
        html += fieldMultiCheckbox(
          'channel_ids',
          '绑定渠道',
          (data.channel_ids || '').split(',').map(function (s) { return s.trim(); }).filter(Boolean),
          channelOptions,
          '不勾选任何渠道表示此 Key 可使用全部渠道'
        );
        html += field('status', '状态', 'select', data.status !== undefined ? data.status : 1, {
          options: [{ value: 1, label: '启用' }, { value: 0, label: '禁用' }]
        });
        break;

      case 'channel':
        $modalTitle.textContent = isEdit ? '编辑渠道' : '新增渠道';
        html += fieldRow(
          field('name', '渠道名称', 'text', data.name || '', { required: true }),
          field('type', '类型', 'select', data.type || 'openai', {
            options: [
              { value: 'openai', label: 'OpenAI' },
              { value: 'anthropic', label: 'Anthropic' },
              { value: 'gemini', label: 'Gemini' },
              { value: 'responses', label: 'Responses' },
            ]
          })
        );
        html += field('base_url', 'Base URL', 'text', data.base_url || '', { placeholder: 'https://api.openai.com' });
        html += field('api_key', 'API Key', 'text', data.api_key || '');
        html += field('models', '模型 (逗号分隔)', 'text', data.models || '', { placeholder: 'gpt-4o,gpt-4o-mini' });
        html += fieldRow(
          field('status', '状态', 'select', data.status !== undefined ? data.status : 1, {
            options: [{ value: 1, label: '启用' }, { value: 0, label: '禁用' }]
          }),
          field('priority', '优先级', 'number', data.priority || 0)
        );
        html += fieldRow(
          field('weight', '权重', 'number', data.weight || 1),
          field('qpm', 'QPM (0=无限)', 'number', data.qpm || 0)
        );
        html += fieldRow(
          field('timeout', '超时(秒)', 'number', data.timeout || 300),
          field('max_retry', '最大重试', 'number', data.max_retry || 0)
        );
        html += field('custom_instructions', '自定义指令', 'textarea', data.custom_instructions || '');
        html += field('body_modifications', '请求体修改 (JSON)', 'textarea', data.body_modifications || '');
        html += field('header_modifications', '请求头修改 (JSON)', 'textarea', data.header_modifications || '');
        break;

      case 'mapping':
        $modalTitle.textContent = isEdit ? '编辑模型映射' : '新增模型映射';
        html += field('name', '规则名称', 'text', data.name || '', { placeholder: '例如：Chat 默认走 OpenAI' });
        html += field('description', '说明', 'text', data.description || '', { placeholder: '给自己看的备注说明，小白也能看懂' });
        html += field('client_model', '客户端模型', 'text', data.client_model || '', { required: true, placeholder: '例如：gpt-4o / claude-3-7-sonnet' });
        html += field('upstream_model', '上游模型', 'text', data.upstream_model || '', { required: true, placeholder: '例如：gpt-4o-2024-08-06' });
        html += fieldRow(
          field('route', '适用接口', 'select', data.route || 'all', {
            options: [
              { value: 'all', label: '全部接口（不确定就选这个）' },
              { value: 'chat', label: 'Chat Completions' },
              { value: 'messages', label: 'Messages' },
              { value: 'responses', label: 'Responses' }
            ]
          }),
          field('priority', '优先级', 'number', data.priority || 0)
        );
        html += fieldMultiCheckbox(
          'channel_ids',
          '适用渠道',
          (data.channel_ids || '').split(',').map(function (s) { return s.trim(); }).filter(Boolean),
          channelOptions,
          '不勾选任何渠道表示所有渠道都可生效；优先级越高，越优先命中'
        );
        html += field('status', '状态', 'select', data.status !== undefined ? data.status : 1, {
          options: [{ value: 1, label: '启用' }, { value: 0, label: '禁用' }]
        });
        break;
    }

    $modalForm.innerHTML = html;
    $modalOv.classList.remove('hidden');
  };

  window.toggleAllChannels = function (checked) {
    Array.prototype.forEach.call($modalForm.querySelectorAll('input[name="channel_ids"]'), function (el) {
      el.checked = !!checked;
    });
  };

  window.closeModal = function () {
    $modalOv.classList.add('hidden');
    $modalForm.innerHTML = '';
    modalState.type = '';
    modalState.editId = null;
  };

  function collectForm() {
    var data = {};
    var inputs = $modalForm.querySelectorAll('input, select, textarea');
    inputs.forEach(function (el) {
      if (!el.name || el.type === 'checkbox') return;
      var v = el.value;
      if (el.type === 'number') v = parseInt(v, 10) || 0;
      if (el.tagName === 'SELECT' && /^\d+$/.test(v)) v = parseInt(v, 10);
      data[el.name] = v;
    });
    var checkedChannelIds = Array.prototype.slice.call($modalForm.querySelectorAll('input[name="channel_ids"]:checked')).map(function (el) {
      return el.value;
    });
    if ($modalForm.querySelector('input[name="channel_ids"]')) {
      data.channel_ids = checkedChannelIds.join(',');
    }
    return data;
  }

  window.submitModal = async function () {
    var data = collectForm();
    var type = modalState.type;
    var id = modalState.editId;
    var pathMap = { key: '/api/admin/keys', channel: '/api/admin/channels', mapping: '/api/admin/mappings' };
    var base = pathMap[type];
    if (!base) return;

    try {
      if (id) {
        await api('PUT', base + '/' + id, data);
        toast('更新成功', 'success');
      } else {
        var createPath = (type === 'key' && !data.key) ? '/api/admin/keys/generate' : base;
        await api('POST', createPath, data);
        toast('创建成功', 'success');
      }
      closeModal();
      var reloadMap = { key: loadKeys, channel: loadChannels, mapping: loadMappings };
      if (reloadMap[type]) reloadMap[type]();
    } catch (err) {
      toast(err.message, 'error');
    }
  };

  $modalOv.addEventListener('mousedown', function (e) {
    if (e.target === $modalOv) {
      // 记录 mousedown 事件发生的目标，以便在 mouseup 时校验
      $modalOv.dataset.mouseDownTarget = 'true';
    } else {
      $modalOv.dataset.mouseDownTarget = 'false';
    }
  });

  $modalOv.addEventListener('mouseup', function (e) {
    // 只有当 mousedown 和 mouseup 都在 overlay 上时，才关闭弹窗
    if (e.target === $modalOv && $modalOv.dataset.mouseDownTarget === 'true') {
      closeModal();
    }
    $modalOv.dataset.mouseDownTarget = 'false';
  });

  // Close modal on Escape
  document.addEventListener('keydown', function (e) {
    if (e.key === 'Escape') {
      if (!document.getElementById('detail-modal-overlay').classList.contains('hidden')) {
        closeDetailModal();
      } else if (!$modalOv.classList.contains('hidden')) {
        closeModal();
      }
    }
  });

  // ── Login：文本解密动画 ──

  function runDecryptedText(el, options) {
    if (!el) return;
    var originalText = el.getAttribute('data-text') || el.textContent;
    el.setAttribute('data-text', originalText);
    var chars = options.chars || 'ABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789!@#$%^&*';
    var speed = options.speed || 30;
    var revealPerStep = options.revealPerStep || 0.3;
    var letters = originalText.split('');
    var progress = 0;
    
    var interval = setInterval(function() {
      var result = '';
      for (var i = 0; i < letters.length; i++) {
        if (letters[i] === ' ' || letters[i] === '·') {
          result += letters[i];
        } else if (i < Math.floor(progress)) {
          result += letters[i];
        } else {
          result += chars[Math.floor(Math.random() * chars.length)];
        }
      }
      el.textContent = result;
      
      if (progress >= letters.length) {
        clearInterval(interval);
        el.textContent = originalText;
      }
      progress += revealPerStep;
    }, speed);
  }

  function initDecryptedText() {
    var title = document.querySelector('.login-title');
    var subtitle = document.querySelector('.login-subtitle');
    var tagline = document.querySelector('.login-tagline');
    
    var chars = 'ABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789!@#$%^&*';
    
    if (title) runDecryptedText(title, { speed: 40, revealPerStep: 0.25, chars: chars });
    if (subtitle) setTimeout(function() { runDecryptedText(subtitle, { speed: 40, revealPerStep: 0.25, chars: chars }); }, 300);
    if (tagline) setTimeout(function() { runDecryptedText(tagline, { speed: 40, revealPerStep: 0.25, chars: chars }); }, 600);
  }

  // ── Init ──

  if (token) {
    showApp();
  } else {
    initDecryptedText();
  }

})();
