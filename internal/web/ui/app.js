const pages = {
  overview: '\u603b\u89c8',
  groups: 'Group \u7ba1\u7406',
  clients: 'Group / Client \u6d4f\u89c8',
  requests: '\u8bf7\u6c42\u8bb0\u5f55',
  devices: '\u8bbe\u5907\u76d1\u63a7',
  users: '\u8d26\u6237\u7ba1\u7406',
  invoke: '\u624b\u52a8\u8c03\u7528'
};

const AUTO_REFRESH_MS = {
  overview: 10000,
  groups: 5000,
  clients: 5000,
  devices: 15000
};

const state = {
  token: localStorage.getItem('r0rpc_token') || '',
  user: JSON.parse(localStorage.getItem('r0rpc_user') || 'null'),
  page: 'overview',
  selectedGroup: '',
  selectedClient: '',
  groupsPage: 1,
  requestsPage: 1,
  requestsPageSize: 20,
  requestsTotal: 0,
  requestsTotalPages: 0,
  health: null,
  trends: [],
  groups: [],
  groupClients: [],
  clientRecentRequests: [],
  requests: [],
  requestFilterOptions: { groups: [], actions: [], clientIds: [] },
  trendFilterOptions: { groups: [], actions: [], clientIds: [] },
  weekly: [],
  daily: [],
  users: [],
  requestDetailCopy: { request: '-', response: '-' }
};

let refreshTimer = null;
let autoRefreshing = false;

const el = {
  navMenu: document.getElementById('navMenu'),
  loginPanel: document.getElementById('loginPanel'),
  sessionCard: document.getElementById('sessionCard'),
  loginForm: document.getElementById('loginForm'),
  loginError: document.getElementById('loginError'),
  loginSubmitBtn: document.getElementById('loginSubmitBtn'),
  sessionText: document.getElementById('sessionText'),
  sessionTip: document.getElementById('sessionTip'),
  pageTitle: document.getElementById('pageTitle'),
  serverMeta: document.getElementById('serverMeta'),
  topbar: document.getElementById('topbar'),
  overviewCards: document.getElementById('overviewCards'),
  trendFilterForm: document.getElementById('trendFilterForm'),
  groupFilterForm: document.getElementById('groupFilterForm'),
  groupSummaryCards: document.getElementById('groupSummaryCards'),
  groupsBody: document.getElementById('groupsBody'),
  groupsPrevBtn: document.getElementById('groupsPrevBtn'),
  groupsNextBtn: document.getElementById('groupsNextBtn'),
  groupsPageInfo: document.getElementById('groupsPageInfo'),
  reloadGroupsBtn: document.getElementById('reloadGroupsBtn'),
  clientExplorerGroupForm: document.getElementById('clientExplorerGroupForm'),
  clientExplorerTree: document.getElementById('clientExplorerTree'),
  clientExplorerSummary: document.getElementById('clientExplorerSummary'),
  clientExplorerTitle: document.getElementById('clientExplorerTitle'),
  clientExplorerSubTitle: document.getElementById('clientExplorerSubTitle'),
  reloadClientExplorerBtn: document.getElementById('reloadClientExplorerBtn'),
  clientInfoGrid: document.getElementById('clientInfoGrid'),
  clientLatestResult: document.getElementById('clientLatestResult'),
  clientRecentRequestsBody: document.getElementById('clientRecentRequestsBody'),
  clientDetailModal: document.getElementById('clientDetailModal'),
  clientDetailBackdrop: document.getElementById('clientDetailBackdrop'),
  requestDetailModal: document.getElementById('requestDetailModal'),
  requestDetailBackdrop: document.getElementById('requestDetailBackdrop'),
  requestDetailTitle: document.getElementById('requestDetailTitle'),
  requestDetailMeta: document.getElementById('requestDetailMeta'),
  requestDetailSummary: document.getElementById('requestDetailSummary'),
  requestDetailRequestPayload: document.getElementById('requestDetailRequestPayload'),
  requestDetailRequestCopyBtn: document.getElementById('requestDetailRequestCopyBtn'),
  requestDetailResponseTitle: document.getElementById('requestDetailResponseTitle'),
  requestDetailResponsePayload: document.getElementById('requestDetailResponsePayload'),
  requestDetailResponseCopyBtn: document.getElementById('requestDetailResponseCopyBtn'),
  requestFilterForm: document.getElementById('requestFilterForm'),
  requestsBody: document.getElementById('requestsBody'),
  requestsPrevBtn: document.getElementById('requestsPrevBtn'),
  requestsNextBtn: document.getElementById('requestsNextBtn'),
  requestsPageInfo: document.getElementById('requestsPageInfo'),
  reloadRequestsBtn: document.getElementById('reloadRequestsBtn'),
  metricsFilterForm: document.getElementById('metricsFilterForm'),
  deviceSummaryCards: document.getElementById('deviceSummaryCards'),
  weeklyMetricsBody: document.getElementById('weeklyMetricsBody'),
  reloadDevicesBtn: document.getElementById('reloadDevicesBtn'),
  createUserForm: document.getElementById('createUserForm'),
  usersBody: document.getElementById('usersBody'),
  reloadUsersBtn: document.getElementById('reloadUsersBtn'),
  invokeForm: document.getElementById('invokeForm'),
  invokeResult: document.getElementById('invokeResult'),
  toast: document.getElementById('toast'),
  requestsChart: document.getElementById('requestsChart'),
  successRateChart: document.getElementById('successRateChart'),
  latencyChart: document.getElementById('latencyChart'),
  deviceTrendModal: document.getElementById('deviceTrendModal'),
  deviceTrendBackdrop: document.getElementById('deviceTrendBackdrop'),
  deviceTrendTitle: document.getElementById('deviceTrendTitle'),
  deviceTrendMeta: document.getElementById('deviceTrendMeta'),
  deviceTrendRequestsChart: document.getElementById('deviceTrendRequestsChart'),
  deviceTrendSuccessChart: document.getElementById('deviceTrendSuccessChart'),
  deviceTrendBody: document.getElementById('deviceTrendBody')
};
boot();

function boot() {
  bindEvents();
  renderShell();
  if (state.token) {
    refreshCurrentPage().catch(handleError);
  }
}

function bindEvents() {
  if (el.loginForm) {
    el.loginForm.addEventListener('submit', onLogin);
    el.loginForm.addEventListener('input', clearLoginFeedback);
  }

  const logoutBtn = document.getElementById('logoutBtn');
  if (logoutBtn) {
    logoutBtn.addEventListener('click', logout);
  }

  document.querySelectorAll('.nav-btn').forEach((btn) => btn.addEventListener('click', () => switchPage(btn.dataset.page)));
  bindRefreshButton(el.reloadGroupsBtn, loadGroupManagement, { idle: '\u5237\u65b0 Group', loading: '\u5237\u65b0\u4e2d...', success: 'Group \u5df2\u5237\u65b0' });
  bindRefreshButton(el.reloadClientExplorerBtn, loadClientExplorer, { idle: '\u5237\u65b0\u6d4f\u89c8\u9875', loading: '\u5237\u65b0\u4e2d...', success: '\u6d4f\u89c8\u9875\u6570\u636e\u5df2\u5237\u65b0' });
  if (el.clientDetailBackdrop) { el.clientDetailBackdrop.addEventListener('click', closeClientDetailModal); }
  if (el.requestDetailBackdrop) { el.requestDetailBackdrop.addEventListener('click', closeRequestDetailModal); }
  if (el.deviceTrendBackdrop) { el.deviceTrendBackdrop.addEventListener('click', closeDeviceTrendModal); }
  if (el.requestDetailModal) {
    el.requestDetailModal.addEventListener('click', (event) => {
      if (!event.target.closest('.request-detail-panel')) {
        closeRequestDetailModal();
      }
    });
  }
  if (el.requestDetailRequestCopyBtn) {
    el.requestDetailRequestCopyBtn.addEventListener('click', () => copyRequestDetailField('request'));
  }
  if (el.requestDetailResponseCopyBtn) {
    el.requestDetailResponseCopyBtn.addEventListener('click', () => copyRequestDetailField('response'));
  }
  bindRefreshButton(el.reloadRequestsBtn, loadRequests, { idle: '\u5237\u65b0\u8bf7\u6c42', loading: '\u5237\u65b0\u4e2d...', success: '\u8bf7\u6c42\u5df2\u5237\u65b0' });
  bindRefreshButton(el.reloadUsersBtn, loadUsers, { idle: '\u5237\u65b0\u8d26\u6237', loading: '\u5237\u65b0\u4e2d...', success: '\u8d26\u6237\u5df2\u5237\u65b0' });
  bindRefreshButton(el.reloadDevicesBtn, loadDeviceMetrics, { idle: '\u5237\u65b0\u8bbe\u5907', loading: '\u5237\u65b0\u4e2d...', success: '\u8bbe\u5907\u6307\u6807\u5df2\u5237\u65b0' });

  if (el.trendFilterForm) {
    el.trendFilterForm.addEventListener('change', () => {
      loadOverview().catch(handleError);
    });
  }

  if (el.groupFilterForm) {
    el.groupFilterForm.addEventListener('submit', (event) => {
      event.preventDefault();
      state.groupsPage = 1;
      renderGroupManagement();
    });
    el.groupFilterForm.addEventListener('change', () => {
      state.groupsPage = 1;
      renderGroupManagement();
    });
  }
  if (el.groupsPrevBtn) {
    el.groupsPrevBtn.addEventListener('click', () => {
      state.groupsPage = Math.max(1, state.groupsPage - 1);
      renderGroupManagement();
    });
  }
  if (el.groupsNextBtn) {
    el.groupsNextBtn.addEventListener('click', () => {
      state.groupsPage += 1;
      renderGroupManagement();
    });
  }

  if (el.clientExplorerGroupForm) {
    const handleClientExplorerFilterChange = (event) => {
      const target = event && event.target ? String(event.target.tagName || '').toLowerCase() : '';
      if (event && event.type === 'change' && target && target !== 'select') {
        return;
      }
      syncClientExplorerSelection().catch(handleError);
    };
    el.clientExplorerGroupForm.addEventListener('input', handleClientExplorerFilterChange);
    el.clientExplorerGroupForm.addEventListener('change', handleClientExplorerFilterChange);
  }

  if (el.requestFilterForm) {
    el.requestFilterForm.addEventListener('submit', (event) => {
      event.preventDefault();
      state.requestsPage = 1;
      loadRequests().catch(handleError);
    });
    el.requestFilterForm.addEventListener('change', (event) => {
      const target = event.target;
      if (!target) {
        return;
      }
      state.requestsPage = 1;
      if (['group', 'action', 'client'].includes(target.name)) {
        loadRequestFilterOptions().catch(handleError);
      }
    });
  }
  if (el.requestsPrevBtn) {
    el.requestsPrevBtn.addEventListener('click', () => {
      if (state.requestsPage <= 1) {
        return;
      }
      state.requestsPage -= 1;
      loadRequests().catch(handleError);
    });
  }
  if (el.requestsNextBtn) {
    el.requestsNextBtn.addEventListener('click', () => {
      if (state.requestsPage >= state.requestsTotalPages) {
        return;
      }
      state.requestsPage += 1;
      loadRequests().catch(handleError);
    });
  }

  if (el.metricsFilterForm) {
    el.metricsFilterForm.addEventListener('submit', (event) => {
      event.preventDefault();
      loadDeviceMetrics().catch(handleError);
    });
  }
  if (el.createUserForm) {
    el.createUserForm.addEventListener('submit', onCreateUser);
  }
  if (el.invokeForm) {
    el.invokeForm.addEventListener('submit', onInvoke);
  }

  document.addEventListener('visibilitychange', () => {
    if (!document.hidden && state.token) {
      refreshCurrentPage().catch(handleError);
    }
  });
}
function bindRefreshButton(button, loader, labels = {}) {
  if (!button) {
    return;
  }
  const idleText = labels.idle || button.textContent.trim() || '\u5237\u65b0';
  button.textContent = idleText;
  button.addEventListener('click', async () => {
    button.disabled = true;
    button.textContent = labels.loading || '\u5237\u65b0\u4e2d...';
    try {
      await loadHealth();
      await loader();
      toast(labels.success || '\u5237\u65b0\u5b8c\u6210');
    } catch (error) {
      handleError(error);
    } finally {
      button.disabled = false;
      button.textContent = idleText;
    }
  });
}
async function onLogin(event) {
  event.preventDefault();
  clearLoginFeedback();
  setLoginSubmitting(true);

  try {
    const form = new FormData(el.loginForm);
    const data = await request('/api/auth/login', {
      method: 'POST',
      body: JSON.stringify({
        username: String(form.get('username') || '').trim(),
        password: String(form.get('password') || '')
      })
    }, false);

    state.token = data.token;
    state.user = data.user;
    localStorage.setItem('r0rpc_token', state.token);
    localStorage.setItem('r0rpc_user', JSON.stringify(state.user));
    renderShell();
    toast('Login successful');
    await refreshCurrentPage();
  } catch (error) {
    const rawMessage = String(error?.payload?.error || error?.message || 'Login failed').trim();
    const loginMessage = rawMessage === 'invalid credentials'
      ? 'Invalid username or password'
      : rawMessage;
    showLoginError(loginMessage);
    toast(loginMessage);
  } finally {
    setLoginSubmitting(false);
  }
}
function logout() {
  state.token = '';
  state.user = null;
  localStorage.removeItem('r0rpc_token');
  localStorage.removeItem('r0rpc_user');
  stopAutoRefresh();
  renderShell();
  toast('Logged out');
}

function setLoginSubmitting(submitting) {
  if (!el.loginSubmitBtn) {
    return;
  }
  el.loginSubmitBtn.disabled = submitting;
  el.loginSubmitBtn.textContent = submitting ? '\u767b\u5f55\u4e2d...' : '\u767b\u5f55';
}
function clearLoginFeedback() {
  el.loginPanel.classList.remove('is-error');
  if (el.loginError) {
    el.loginError.textContent = '';
    el.loginError.classList.add('hidden');
  }
}

function showLoginError(message) {
  el.loginPanel.classList.add('is-error');
  if (el.loginError) {
    el.loginError.textContent = message;
    el.loginError.classList.remove('hidden');
  }
  const passwordInput = el.loginForm?.elements?.namedItem('password');
  if (passwordInput && typeof passwordInput.focus === 'function') {
    passwordInput.focus();
    if (typeof passwordInput.select === 'function') {
      passwordInput.select();
    }
  }
}

function renderShell() {
  const loggedIn = Boolean(state.token && state.user);
  el.loginPanel.classList.toggle('hidden', loggedIn);
  el.sessionCard.classList.toggle('hidden', !loggedIn);
  el.navMenu.classList.toggle('hidden', !loggedIn);
  el.topbar.classList.toggle('hidden', !loggedIn);

  if (loggedIn) {
    clearLoginFeedback();
    el.sessionText.textContent = `${state.user.username} / ${state.user.role}`;
    el.sessionTip.textContent = state.user.canRpc
      ? '\u5f53\u524d\u8d26\u53f7\u5141\u8bb8\u53d1\u8d77 RPC \u8c03\u7528\u3002'
      : '\u5f53\u524d\u8d26\u53f7\u4e0d\u80fd\u53d1\u8d77 RPC \u8c03\u7528\u3002';
  }

  switchPage(state.page, false);
}

function switchPage(page, load = true) {
  if (page !== 'clients') {
    closeClientDetailModal();
  }
  if (page !== 'requests') {
    closeRequestDetailModal();
  }
  state.page = page;
  document.querySelectorAll('.page').forEach((node) => node.classList.add('hidden'));
  if (state.token) {
    const pageNode = document.getElementById('page-' + page);
    if (pageNode) {
      pageNode.classList.remove('hidden');
    }
  }
  document.querySelectorAll('.nav-btn').forEach((btn) => btn.classList.toggle('active', btn.dataset.page === page));
  el.pageTitle.textContent = pages[page] || 'R0RPC';
  configureAutoRefresh();
  if (load && state.token) {
    refreshCurrentPage().catch(handleError);
  }
}

async function refreshCurrentPage() {
  await loadHealth();
  switch (state.page) {
    case 'overview':
      await loadOverview();
      break;
    case 'groups':
      await loadGroupManagement();
      break;
    case 'clients':
      await loadClientExplorer();
      break;
    case 'requests':
      await loadRequests();
      break;
    case 'devices':
      await loadDeviceMetrics();
      break;
    case 'users':
      await loadUsers();
      break;
    case 'invoke':
      break;
    default:
      break;
  }
}

function configureAutoRefresh() {
  stopAutoRefresh();
  const interval = AUTO_REFRESH_MS[state.page];
  if (!interval || !state.token) {
    return;
  }
  refreshTimer = window.setInterval(async () => {
    if (document.hidden || autoRefreshing) {
      return;
    }
    autoRefreshing = true;
    try {
      await loadHealth();
      if (state.page === 'groups') {
        await loadGroupManagement();
      } else if (state.page === 'clients') {
        await loadClientExplorer();
      } else if (state.page === 'overview') {
        await loadOverview();
      } else if (state.page === 'devices') {
        await loadDeviceMetrics();
      }
    } catch {
      // silent auto refresh
    } finally {
      autoRefreshing = false;
    }
  }, interval);
}
function stopAutoRefresh() {
  if (refreshTimer) {
    clearInterval(refreshTimer);
    refreshTimer = null;
  }
}

async function loadHealth() {
  state.health = await request('/healthz', { method: 'GET' }, false);
  el.serverMeta.textContent = `\u670d\u52a1\u5668 ID: ${state.health.serverId} / ${state.health.name}`;
}

async function loadTrendFilterOptions() {
  const groupSelect = el.trendFilterForm.elements.namedItem('group');
  const actionSelect = el.trendFilterForm.elements.namedItem('action');
  const clientSelect = el.trendFilterForm.elements.namedItem('client');
  const filters = {
    group: String(groupSelect ? groupSelect.value : '').trim(),
    action: String(actionSelect ? actionSelect.value : '').trim(),
    client: String(clientSelect ? clientSelect.value : '').trim()
  };
  const data = await fetchRequestFilterOptions(filters);
  state.trendFilterOptions = {
    groups: data.groups || [],
    actions: data.actions || [],
    clientIds: data.clientIds || []
  };
  fillSelectElement(groupSelect, state.trendFilterOptions.groups, '\u5168\u90e8 group');
  fillSelectElement(actionSelect, state.trendFilterOptions.actions, '\u5168\u90e8 action');
  fillSelectElement(clientSelect, state.trendFilterOptions.clientIds, '\u5168\u90e8 client');
  if (groupSelect) { groupSelect.value = filters.group; }
  if (actionSelect) { actionSelect.value = filters.action; }
  if (clientSelect) { clientSelect.value = filters.client; }
}

async function loadOverview() {
  const groupSelect = el.trendFilterForm.elements.namedItem('group');
  const actionSelect = el.trendFilterForm.elements.namedItem('action');
  const clientSelect = el.trendFilterForm.elements.namedItem('client');
  const currentFilters = {
    group: String(groupSelect?.value || '').trim(),
    action: String(actionSelect?.value || '').trim(),
    client: String(clientSelect?.value || '').trim()
  };

  const query = new URLSearchParams(objectFromForm(el.trendFilterForm));
  const [trendsResp, groupsResp, filterResp] = await Promise.all([
    request(`/api/metrics/trends?${query.toString()}`, { method: 'GET' }),
    request('/api/groups', { method: 'GET' }),
    fetchRequestFilterOptions(currentFilters)
  ]);

  state.trends = trendsResp.items || [];
  state.groups = groupsResp.items || [];
  state.trendFilterOptions = {
    groups: filterResp.groups || [],
    actions: filterResp.actions || [],
    clientIds: filterResp.clientIds || []
  };

  fillSelectElement(groupSelect, state.trendFilterOptions.groups, '\u5168\u90e8 group');
  fillSelectElement(actionSelect, state.trendFilterOptions.actions, '\u5168\u90e8 action');
  fillSelectElement(clientSelect, state.trendFilterOptions.clientIds, '\u5168\u90e8 client');
  if (groupSelect) { groupSelect.value = currentFilters.group; }
  if (actionSelect) { actionSelect.value = currentFilters.action; }
  if (clientSelect) { clientSelect.value = currentFilters.client; }

  renderOverview();
}

function renderOverview() {
  const totalRequests = state.trends.reduce((sum, item) => sum + Number(item.totalRequests || 0), 0);
  const totalSuccess = state.trends.reduce((sum, item) => sum + Number(item.successRequests || 0), 0);
  const totalLatency = state.trends.reduce((sum, item) => sum + Number(item.avgLatencyMs || 0) * Number(item.totalRequests || 0), 0);
  const avgLatency = totalRequests ? Math.round(totalLatency / totalRequests) : 0;
  const successRate = totalRequests ? ((totalSuccess * 100) / totalRequests).toFixed(1) : '0.0';
  const totalGroups = state.groups.length;
  const onlineGroups = state.groups.filter((item) => item.status === 'online').length;
  const staleGroups = state.groups.filter((item) => item.status === 'stale').length;
  const noDeviceGroups = state.groups.filter((item) => item.status === 'no_device').length;
  const days = Math.max(3, Math.min(30, Number(el.trendFilterForm?.elements?.namedItem('days')?.value || 7) || 7));
  const overviewHeading = document.querySelector('#page-overview .page-intro h3');
  if (overviewHeading) {
    overviewHeading.textContent = `${days} \u5929\u6982\u89c8`;
  }

  const cards = [
    { label: '\u670d\u52a1\u5668', value: state.health?.serverId || '--', foot: state.health?.name || 'R0RPC' },
    { label: `${days} \u5929\u8bf7\u6c42\u91cf`, value: String(totalRequests), foot: `${totalSuccess} \u6b21\u6210\u529f` },
    { label: `${days} \u5929\u6210\u529f\u7387`, value: `${successRate}%`, foot: '\u6210\u529f\u8bf7\u6c42 / \u603b\u8bf7\u6c42' },
    { label: '\u5e73\u5747\u5ef6\u8fdf', value: `${avgLatency} ms`, foot: '\u6309\u8bf7\u6c42\u91cf\u52a0\u6743' },
    { label: 'Group \u5065\u5eb7\u5ea6', value: `${onlineGroups}/${totalGroups}`, foot: `${staleGroups} \u4e2a\u957f\u671f\u4e0d\u6d3b\u8dc3\uff0c${noDeviceGroups} \u4e2a\u65e0\u8bbe\u5907` }
  ];

  el.overviewCards.innerHTML = cards.map((card) => cardTemplate(card)).join('');

  const labels = state.trends.map((item) => compactDateLabel(item.statDate));
  drawBarChart(el.requestsChart, labels, state.trends.map((item) => Number(item.totalRequests || 0)), '#ffd166', '\u8bf7\u6c42\u91cf');
  drawLineChart(el.successRateChart, labels, state.trends.map((item) => Number(item.successRate || 0)), '#7bdff2', '%');
  drawLineChart(el.latencyChart, labels, state.trends.map((item) => Number(item.avgLatencyMs || 0)), '#78d97a', 'ms');
}
async function loadGroupManagement() {
  const data = await request('/api/groups', { method: 'GET' });
  state.groups = data.items || [];
  if (!state.groups.some((item) => item.group === state.selectedGroup)) {
    state.selectedGroup = state.groups.length > 0 ? state.groups[0].group : '';
  }
  renderGroupManagement();
}

function renderGroupManagement() {
  const filters = objectFromForm(el.groupFilterForm);
  const keyword = String(filters.keyword || '').toLowerCase();
  const status = String(filters.status || '');
  const sort = String(filters.sort || 'recent_request_desc');
  const pageSize = Number(filters.pageSize || 10) || 10;
  const filtered = sortGroups(filteredGroups(keyword, status), sort);
  const totalPages = Math.max(1, Math.ceil(filtered.length / pageSize) || 1);
  state.groupsPage = Math.min(state.groupsPage, totalPages);
  state.groupsPage = Math.max(state.groupsPage, 1);

  const start = (state.groupsPage - 1) * pageSize;
  const pageItems = filtered.slice(start, start + pageSize);

  const summaryCards = [
    { label: '\u5168\u90e8 Group', value: state.groups.length, foot: '\u5386\u53f2\u51fa\u73b0\u8fc7\u7684 group' },
    { label: '\u5728\u7ebf Group', value: state.groups.filter((item) => item.status === 'online').length, foot: '\u5f53\u524d\u81f3\u5c11\u6709\u4e00\u53f0\u5728\u7ebf\u8bbe\u5907' },
    { label: '\u79bb\u7ebf Group', value: state.groups.filter((item) => item.status === 'offline').length, foot: '\u6709\u8bbe\u5907\u4f46\u5f53\u524d\u90fd\u4e0d\u5728\u7ebf' },
    { label: '\u957f\u671f\u4e0d\u6d3b\u8dc3', value: state.groups.filter((item) => item.status === 'stale').length, foot: '7 \u5929\u4ee5\u4e0a\u6ca1\u6709\u8bbe\u5907\u5728\u7ebf' },
    { label: '\u957f\u671f\u65e0\u8bbe\u5907', value: state.groups.filter((item) => item.status === 'no_device').length, foot: '\u53ea\u6709\u5386\u53f2\u8bf7\u6c42\uff0c\u6ca1\u6709\u73b0\u5b58\u8bbe\u5907' }
  ];
  el.groupSummaryCards.innerHTML = summaryCards.map((card) => cardTemplate(card)).join('');

  el.groupsBody.innerHTML = renderRows(pageItems, 9, (item) => `
    <tr>
      <td>${escapeHTML(item.group)}</td>
      <td>${statusBadge(item.status)}</td>
      <td>${Number(item.totalDevices || 0)}</td>
      <td>${Number(item.onlineDevices || 0)}</td>
      <td>${Number(item.requests7d || 0)}</td>
      <td>${Number(item.successRate || 0).toFixed(1)}%</td>
      <td>${formatDate(item.lastSeenAt)}</td>
      <td>${formatDate(item.lastRequestAt)}</td>
      <td><button class="ghost mini-btn" type="button" onclick="openGroupClients('${escapeJS(item.group)}')">\u67e5\u770b Client</button></td>
    </tr>
  `, '\u6682\u65e0\u5339\u914d\u7684 group');

  el.groupsPageInfo.textContent = `\u7b2c ${state.groupsPage} / ${totalPages} \u9875\uff0c\u5171 ${filtered.length} \u4e2a group`;
  el.groupsPrevBtn.disabled = state.groupsPage <= 1;
  el.groupsNextBtn.disabled = state.groupsPage >= totalPages;
}
function filteredGroups(keyword, status) {
  return state.groups.filter((item) => {
    if (keyword && !String(item.group || '').toLowerCase().includes(keyword)) {
      return false;
    }
    if (status && item.status !== status) {
      return false;
    }
    return true;
  });
}

function sortGroups(items, sort) {
  const result = [...items];
  const dateValue = (value) => {
    if (!value) return 0;
    const time = new Date(value).getTime();
    return Number.isNaN(time) ? 0 : time;
  };
  result.sort((a, b) => {
    switch (sort) {
      case 'name_asc':
        return String(a.group || '').localeCompare(String(b.group || ''), 'zh-CN');
      case 'requests_desc':
        return Number(b.requests7d || 0) - Number(a.requests7d || 0) || String(a.group || '').localeCompare(String(b.group || ''), 'zh-CN');
      case 'online_desc':
        return Number(b.onlineDevices || 0) - Number(a.onlineDevices || 0) || Number(b.totalDevices || 0) - Number(a.totalDevices || 0);
      case 'devices_desc':
        return Number(b.totalDevices || 0) - Number(a.totalDevices || 0) || Number(b.onlineDevices || 0) - Number(a.onlineDevices || 0);
      case 'success_desc':
        return Number(b.successRate || 0) - Number(a.successRate || 0) || Number(b.requests7d || 0) - Number(a.requests7d || 0);
      case 'recent_request_desc':
      default:
        return dateValue(b.lastRequestAt) - dateValue(a.lastRequestAt) || dateValue(b.lastSeenAt) - dateValue(a.lastSeenAt) || String(a.group || '').localeCompare(String(b.group || ''), 'zh-CN');
    }
  });
  return result;
}
async function loadClientExplorer() {
  const data = await request('/api/devices?limit=200', { method: 'GET' });
  state.groupClients = (data.items || []).sort((a, b) => {
    const groupCompare = String(a.group || '').localeCompare(String(b.group || ''), 'zh-CN');
    if (groupCompare !== 0) {
      return groupCompare;
    }
    if ((a.status === 'online') !== (b.status === 'online')) {
      return a.status === 'online' ? -1 : 1;
    }
    return String(a.clientId || '').localeCompare(String(b.clientId || ''), 'zh-CN');
  });
  await syncClientExplorerSelection();
}

async function loadGroupClients() {
  await loadClientExplorer();
}

async function syncClientExplorerSelection() {
  const filters = objectFromForm(el.clientExplorerGroupForm);
  const groupKeyword = String(filters.groupKeyword || '').trim().toLowerCase();
  const clientKeyword = String(filters.clientKeyword || '').trim().toLowerCase();
  const clientStatus = String(filters.clientStatus || '').trim();
  const visibleClients = filterGroupClients(state.groupClients, groupKeyword, clientKeyword, clientStatus);
  const nextClient = visibleClients.some((item) => item.clientId === state.selectedClient)
    ? state.selectedClient
    : (visibleClients[0] ? visibleClients[0].clientId : '');

  if (nextClient !== state.selectedClient) {
    state.selectedClient = nextClient;
    if (state.selectedClient) {
      await loadSelectedClientActivity();
    } else {
      state.clientRecentRequests = [];
    }
  } else if (!state.selectedClient) {
    state.clientRecentRequests = [];
  }

  const selectedClient = state.groupClients.find((item) => item.clientId === state.selectedClient);
  state.selectedGroup = selectedClient ? String(selectedClient.group || '') : '';

  renderClientExplorerTree();
  renderClientExplorerDetail();
}

function renderClientExplorerTree() {
  const filters = objectFromForm(el.clientExplorerGroupForm);
  const groupKeyword = String(filters.groupKeyword || '').trim().toLowerCase();
  const clientKeyword = String(filters.clientKeyword || '').trim().toLowerCase();
  const clientStatus = String(filters.clientStatus || '').trim();
  const visibleClients = filterGroupClients(state.groupClients, groupKeyword, clientKeyword, clientStatus);

  el.clientExplorerTree.innerHTML = `
    <div class="table-wrap tall client-explorer-table-wrap">
      <table>
        <thead>
          <tr>
            <th>ClientId</th>
            <th>Group</th>
            <th>平台</th>
            <th>状态</th>
            <th>最后在线</th>
            <th>IP</th>
            <th>创建时间</th>
            <th>操作</th>
          </tr>
        </thead>
        <tbody>
          ${renderRows(visibleClients, 8, (client) => {
            const isActive = client.clientId === state.selectedClient;
            return `
              <tr class="clickable ${isActive ? 'table-row-active' : ''}" onclick="selectExplorerClient('${escapeJS(client.clientId)}')">
                <td>${escapeHTML(client.clientId)}</td>
                <td>${escapeHTML(client.group || '-')}</td>
                <td>${escapeHTML(client.platform || '-')}</td>
                <td>${statusBadge(client.status)}</td>
                <td>${formatDate(client.lastSeenAt)}</td>
                <td>${escapeHTML(client.lastIp || '-')}</td>
                <td>${formatDate(client.createdAt)}</td>
                <td><button class="ghost mini-btn" type="button" onclick="event.stopPropagation(); selectExplorerClient('${escapeJS(client.clientId)}')">详情</button></td>
              </tr>
            `;
          }, '暂无匹配的 client')}
        </tbody>
      </table>
    </div>
  `;
}

function filterGroupClients(items, groupKeyword, keyword, status) {
  return items.filter((item) => {
    if (groupKeyword && !String(item.group || '').toLowerCase().includes(groupKeyword)) {
      return false;
    }
    if (keyword) {
      const haystack = `${item.clientId || ''} ${item.platform || ''} ${item.lastIp || ''}`.toLowerCase();
      if (!haystack.includes(keyword)) {
        return false;
      }
    }
    if (status && item.status !== status) {
      return false;
    }
    return true;
  });
}

async function selectExplorerGroup(group) {
  closeClientDetailModal();
  const groupInput = el.clientExplorerGroupForm && el.clientExplorerGroupForm.elements
    ? el.clientExplorerGroupForm.elements.namedItem('groupKeyword')
    : null;
  if (groupInput) {
    groupInput.value = group || '';
  }
  await syncClientExplorerSelection();
}

async function selectExplorerClient(clientId) {
  state.selectedClient = clientId;
  const client = state.groupClients.find((item) => item.clientId === clientId) || null;
  state.selectedGroup = client ? String(client.group || '') : '';
  renderClientExplorerTree();
  await loadSelectedClientActivity();
  renderClientExplorerDetail();
  openClientDetailModal();
}

async function loadSelectedClientActivity() {
  const client = state.groupClients.find((item) => item.clientId === state.selectedClient) || null;
  if (!client || !state.selectedClient) {
    state.clientRecentRequests = [];
    return;
  }
  const query = new URLSearchParams();
  if (client.group) {
    query.set('group', client.group);
  }
  query.set('client', state.selectedClient);
  query.set('page', '1');
  query.set('pageSize', '12');
  const data = await request(`/api/monitor/requests?${query.toString()}`, { method: 'GET' });
  state.clientRecentRequests = data.items || [];
}
function syncModalBodyState() {
  const clientOpen = el.clientDetailModal && !el.clientDetailModal.classList.contains('hidden');
  const requestOpen = el.requestDetailModal && !el.requestDetailModal.classList.contains('hidden');
  const deviceTrendOpen = el.deviceTrendModal && !el.deviceTrendModal.classList.contains('hidden');
  document.body.classList.toggle('modal-open', Boolean(clientOpen || requestOpen || deviceTrendOpen));
}

function openClientDetailModal() {
  if (!el.clientDetailModal) {
    return;
  }
  el.clientDetailModal.classList.remove('hidden');
  syncModalBodyState();
}

function closeClientDetailModal() {
  if (!el.clientDetailModal) {
    return;
  }
  el.clientDetailModal.classList.add('hidden');
  syncModalBodyState();
}

function parseJSONValue(raw) {
  const text = String(raw || '').trim();
  if (!text) {
    return null;
  }
  try {
    return JSON.parse(text);
  } catch {
    return text;
  }
}

function isPlainObject(value) {
  return Object.prototype.toString.call(value) === '[object Object]';
}

function isWrappedResponseObject(value) {
  return isPlainObject(value) && (
    Object.prototype.hasOwnProperty.call(value, 'requestId') ||
    Object.prototype.hasOwnProperty.call(value, 'group') ||
    Object.prototype.hasOwnProperty.call(value, 'action') ||
    Object.prototype.hasOwnProperty.call(value, 'status') ||
    Object.prototype.hasOwnProperty.call(value, 'httpCode') ||
    Object.prototype.hasOwnProperty.call(value, 'is_ok')
  );
}

function normalizeStoredRequestBody(item) {
  const parsed = parseJSONValue(item?.requestPayload);
  if (isPlainObject(parsed) && (
    Object.prototype.hasOwnProperty.call(parsed, 'payload') ||
    Object.prototype.hasOwnProperty.call(parsed, 'clientId') ||
    Object.prototype.hasOwnProperty.call(parsed, 'timeoutSeconds')
  )) {
    return parsed;
  }
  return {
    clientId: item?.clientId || '',
    payload: parsed === null ? {} : parsed
  };
}

function extractRequestPayloadBody(item) {
  const requestBody = normalizeStoredRequestBody(item);
  if (isPlainObject(requestBody) && Object.prototype.hasOwnProperty.call(requestBody, 'payload')) {
    return requestBody.payload;
  }
  return parseJSONValue(item?.requestPayload) ?? {};
}

function normalizeStoredResponseBody(item) {
  const parsed = parseJSONValue(item?.responsePayload);
  if (isWrappedResponseObject(parsed)) {
    return parsed;
  }

  const response = {
    action: item?.action || '',
    clientId: item?.clientId || '',
    error: item?.errorMessage || '',
    group: item?.group || '',
    httpCode: Number(item?.httpCode || 0),
    is_ok: String(item?.status || '').toLowerCase() === 'success' && !String(item?.errorMessage || '').trim(),
    requestId: item?.requestId || '',
    requestPayload: extractRequestPayloadBody(item),
    status: item?.status || ''
  };

  if (Number(item?.latencyMs || 0) > 0) {
    response.latencyMs = Number(item.latencyMs || 0);
  }
  if (parsed !== null) {
    response.data = parsed;
  }
  return response;
}

function buildRequestDetailResponse(item) {
  return {
    title: '\u539f\u59cb\u8fd4\u56de JSON',
    text: JSON.stringify(normalizeStoredResponseBody(item), null, 2)
  };
}

function openRequestDetailItem(item) {
  if (!item || !el.requestDetailModal) {
    return;
  }

  const responseDetail = buildRequestDetailResponse(item);
  const summaryItems = [
    ['\u8bf7\u6c42 ID', item.requestId || '-'],
    ['\u5f00\u59cb\u65f6\u95f4', formatDate(item.createdAt)],
    ['\u5b8c\u6210\u65f6\u95f4', formatDate(item.finishedAt)],
    ['Group', item.group || '-'],
    ['Action', item.action || '-'],
    ['Client', item.clientId || '-'],
    ['\u72b6\u6001', statusText(item.status)],
    ['HTTP', String(Number(item.httpCode || 0))],
    ['\u5ef6\u8fdf', `${Number(item.latencyMs || 0)} ms`]
  ];

  el.requestDetailTitle.textContent = `\u8bf7\u6c42\u8be6\u60c5 / ${item.action || '-'}`;
  el.requestDetailMeta.textContent = `${item.group || '-'} / ${item.clientId || '-'} / ${formatDate(item.createdAt)}`;
  el.requestDetailSummary.innerHTML = summaryItems.map(([key, value]) => `
    <div class="info-item">
      <div class="info-key">${escapeHTML(key)}</div>
      <div class="info-value">${escapeHTML(String(value || '-'))}</div>
    </div>
  `).join('');
  const requestText = JSON.stringify(normalizeStoredRequestBody(item), null, 2);
  state.requestDetailCopy.request = requestText;
  state.requestDetailCopy.response = responseDetail.text;
  el.requestDetailRequestPayload.textContent = requestText;
  el.requestDetailResponseTitle.textContent = responseDetail.title;
  el.requestDetailResponsePayload.textContent = responseDetail.text;
  el.requestDetailModal.classList.remove('hidden');
  syncModalBodyState();
}

function openRequestDetail(index) {
  openRequestDetailItem(state.requests[index]);
}

function openClientRequestDetail(index) {
  openRequestDetailItem(state.clientRecentRequests[index]);
}

function closeRequestDetailModal() {
  if (!el.requestDetailModal) {
    return;
  }
  el.requestDetailModal.classList.add('hidden');
  syncModalBodyState();
}

async function copyText(text) {
  const value = String(text || '-');
  if (navigator.clipboard && typeof navigator.clipboard.writeText === 'function') {
    await navigator.clipboard.writeText(value);
    return;
  }

  const textarea = document.createElement('textarea');
  textarea.value = value;
  textarea.setAttribute('readonly', 'readonly');
  textarea.style.position = 'fixed';
  textarea.style.opacity = '0';
  document.body.appendChild(textarea);
  textarea.select();
  document.execCommand('copy');
  document.body.removeChild(textarea);
}

async function copyRequestDetailField(type) {
  const label = type === 'response' ? '\u8fd4\u56de JSON' : '\u8bf7\u6c42 JSON';
  const text = type === 'response' ? state.requestDetailCopy.response : state.requestDetailCopy.request;
  try {
    await copyText(text);
    toast(`\u5df2\u590d\u5236${label}`);
  } catch (error) {
    handleError(error);
  }
}

function openDeviceTrendModal() {
  if (!el.deviceTrendModal) {
    return;
  }
  el.deviceTrendModal.classList.remove('hidden');
  syncModalBodyState();
}

function closeDeviceTrendModal() {
  if (!el.deviceTrendModal) {
    return;
  }
  el.deviceTrendModal.classList.add('hidden');
  syncModalBodyState();
}

function renderDeviceTrendCharts() {
  if (!el.deviceTrendRequestsChart || !el.deviceTrendSuccessChart) {
    return;
  }
  const labels = state.daily.map((item) => String(item.statDate || '').slice(5));
  const requestValues = state.daily.map((item) => Number(item.totalRequests || 0));
  const successValues = state.daily.map((item) => {
    const total = Number(item.totalRequests || 0);
    return total ? (Number(item.successRequests || 0) * 100) / total : 0;
  });
  drawLineChart(el.deviceTrendRequestsChart, labels, requestValues, '#ffd166', '');
  drawLineChart(el.deviceTrendSuccessChart, labels, successValues, '#7bdff2', '%');
}
function renderClientExplorerDetail() {
  const client = state.groupClients.find((item) => item.clientId === state.selectedClient) || null;

  if (!state.selectedClient || !client) {
    el.clientExplorerTitle.textContent = '客户端详情';
    el.clientExplorerSubTitle.textContent = '在表格里按 group / client 筛选，然后点击详情查看。';
    el.clientExplorerSummary.innerHTML = '';
    if (el.clientInfoGrid) {
      el.clientInfoGrid.innerHTML = '<div class="empty">请选择一个 client。</div>';
    }
    if (el.clientLatestResult) {
      el.clientLatestResult.textContent = '选择 client 后查看最新结果。';
    }
    if (el.clientRecentRequestsBody) {
      el.clientRecentRequestsBody.innerHTML = renderEmptyRow(5, '选择 client 后查看最近请求。');
    }
    closeClientDetailModal();
    return;
  }

  el.clientExplorerTitle.textContent = `客户端详情 / ${client.clientId}`;
  el.clientExplorerSubTitle.textContent = `${client.group || '-'} / ${statusText(client.status)}`;
  el.clientExplorerSummary.innerHTML = '';

  if (!el.clientInfoGrid || !el.clientLatestResult || !el.clientRecentRequestsBody) {
    return;
  }

  const infoItems = [
    ['ClientId', client.clientId],
    ['Group', client.group || '-'],
    ['平台', client.platform || '-'],
    ['状态', statusText(client.status)],
    ['最后在线', formatDate(client.lastSeenAt)],
    ['IP', client.lastIp || '-'],
    ['创建时间', formatDate(client.createdAt)],
    ['更新时间', formatDate(client.updatedAt)]
  ];
  el.clientInfoGrid.innerHTML = `
    <div class="table-wrap">
      <table>
        <thead>
          <tr>
            <th>字段</th>
            <th>值</th>
          </tr>
        </thead>
        <tbody>
          ${infoItems.map(([key, value]) => `
            <tr>
              <td>${escapeHTML(key)}</td>
              <td>${escapeHTML(String(value || '-'))}</td>
            </tr>
          `).join('')}
        </tbody>
      </table>
    </div>
  `;

  const latest = state.clientRecentRequests[0];
  el.clientLatestResult.textContent = latest ? buildLatestResult(latest) : '当前 client 暂无最近请求结果。';

  el.clientRecentRequestsBody.innerHTML = renderRows(state.clientRecentRequests, 5, (item, index) => `
    <tr>
      <td>${formatDate(item.createdAt)}</td>
      <td>${escapeHTML(item.action)}</td>
      <td>${statusBadge(item.status)}</td>
      <td>${Number(item.latencyMs || 0)} ms</td>
      <td><button class="ghost mini-btn" type="button" onclick="openClientRequestDetail(${index})">查看</button></td>
    </tr>
  `, '当前 client 暂无最近请求。');
}

function buildLatestResult(item) {
  return [
    `请求 ID: ${item.requestId || '-'}`,
    `时间: ${formatDate(item.createdAt)}`,
    `Action: ${item.action}`,
    `状态: ${statusText(item.status)}`,
    `HTTP: ${Number(item.httpCode || 0)}`,
    `延迟: ${Number(item.latencyMs || 0)} ms`,
    '',
    '点击下方查看，可打开原始请求 JSON 和原始返回 JSON。'
  ].join('\n');
}
function jsonDetail(raw, label) {
  if (!raw) {
    return '<span class="muted">-</span>';
  }
  return `
    <details class="json-details">
      <summary>${escapeHTML(label)}</summary>
      <pre class="json-preview">${escapeHTML(prettyJSON(raw))}</pre>
    </details>
  `;
}

function jsonFullBlock(raw, emptyText = '-') {
  if (!raw) {
    return `<span class="muted">${escapeHTML(emptyText)}</span>`;
  }
  return `<pre class="json-preview request-inline-preview">${escapeHTML(prettyJSON(raw))}</pre>`;
}
function prettyJSON(raw) {
  const text = String(raw || '').trim();
  if (!text) {
    return '-';
  }
  try {
    return JSON.stringify(JSON.parse(text), null, 2);
  } catch {
    return text;
  }
}

async function fetchRequestFilterOptions(filters = {}) {
  const query = new URLSearchParams();
  Object.entries(filters || {}).forEach(([key, value]) => {
    if (value) {
      query.set(key, value);
    }
  });
  const suffix = query.toString() ? `?${query.toString()}` : '';
  return request(`/api/monitor/request-options${suffix}`, { method: 'GET' });
}

function fillSelectElement(select, items, placeholder) {
  if (!select) {
    return;
  }
  const currentValue = select.value;
  const values = Array.from(new Set((items || []).filter(Boolean)));
  const options = [`<option value="">${escapeHTML(placeholder)}</option>`].concat(
    values.map((item) => `<option value="${escapeHTML(item)}">${escapeHTML(item)}</option>`)
  );
  select.innerHTML = options.join('');
  if (values.includes(currentValue)) {
    select.value = currentValue;
  }
}

function openGroupClients(group) {
  const groupInput = el.clientExplorerGroupForm && el.clientExplorerGroupForm.elements
    ? el.clientExplorerGroupForm.elements.namedItem('groupKeyword')
    : null;
  if (groupInput) {
    groupInput.value = group || '';
  }
  state.selectedGroup = group || '';
  state.selectedClient = '';
  switchPage('clients');
}

async function loadRequestFilterOptions() {
  const groupSelect = el.requestFilterForm.elements.namedItem('group');
  const actionSelect = el.requestFilterForm.elements.namedItem('action');
  const clientSelect = el.requestFilterForm.elements.namedItem('client');
  const filters = {
    group: String(groupSelect ? groupSelect.value : '').trim(),
    action: String(actionSelect ? actionSelect.value : '').trim(),
    client: String(clientSelect ? clientSelect.value : '').trim()
  };
  const data = await fetchRequestFilterOptions(filters);
  state.requestFilterOptions = {
    groups: data.groups || [],
    actions: data.actions || [],
    clientIds: data.clientIds || []
  };
  fillRequestFilterSelect('group', state.requestFilterOptions.groups, '\u5168\u90e8 group');
  fillRequestFilterSelect('action', state.requestFilterOptions.actions, '\u5168\u90e8 action');
  fillRequestFilterSelect('client', state.requestFilterOptions.clientIds, '\u5168\u90e8 client');
  if (groupSelect) { groupSelect.value = filters.group; }
  if (actionSelect) { actionSelect.value = filters.action; }
  if (clientSelect) { clientSelect.value = filters.client; }
}

function fillRequestFilterSelect(name, items, placeholder) {
  const select = el.requestFilterForm.elements.namedItem(name);
  fillSelectElement(select, items, placeholder);
}

function renderRequestsPager() {
  const total = Number(state.requestsTotal || 0);
  const totalPages = Math.max(0, Number(state.requestsTotalPages || 0));
  const currentPage = Math.max(1, Number(state.requestsPage || 1));
  if (totalPages <= 0) {
    el.requestsPageInfo.textContent = '\u6682\u65e0\u8bf7\u6c42\u6570\u636e';
  } else {
    const start = (currentPage - 1) * state.requestsPageSize + 1;
    const end = Math.min(currentPage * state.requestsPageSize, total);
    el.requestsPageInfo.textContent = `\u7b2c ${currentPage} / ${totalPages} \u9875\uff0c\u5171 ${total} \u6761\uff0c\u5f53\u524d\u663e\u793a ${start}-${end}`;
  }
  el.requestsPrevBtn.disabled = currentPage <= 1;
  el.requestsNextBtn.disabled = totalPages <= 0 || currentPage >= totalPages;
}

async function loadRequests() {
  await loadRequestFilterOptions();
  const query = new URLSearchParams(objectFromForm(el.requestFilterForm));
  query.set('page', String(state.requestsPage || 1));
  query.set('pageSize', String(state.requestsPageSize || 20));
  const data = await request(`/api/monitor/requests?${query.toString()}`, { method: 'GET' });
  state.requests = data.items || [];
  state.requestsPage = Number(data.page || 1);
  state.requestsPageSize = Number(data.pageSize || 20);
  state.requestsTotal = Number(data.total || 0);
  state.requestsTotalPages = Number(data.totalPages || 0);
  if (state.requestsTotalPages > 0 && state.requestsPage > state.requestsTotalPages) {
    state.requestsPage = state.requestsTotalPages;
    await loadRequests();
    return;
  }
  el.requestsBody.innerHTML = renderRows(state.requests, 9, (item, index) => `
    <tr>
      <td>${formatDate(item.createdAt)}</td>
      <td>${formatDate(item.finishedAt)}</td>
      <td>${escapeHTML(item.group)}</td>
      <td>${escapeHTML(item.action)}</td>
      <td>${escapeHTML(item.clientId || '-')}</td>
      <td>${statusBadge(item.status)}</td>
      <td>${Number(item.httpCode || 0)}</td>
      <td>${Number(item.latencyMs || 0)} ms</td>
      <td><button class="ghost mini-btn" type="button" onclick="openRequestDetail(${index})">\u67e5\u770b</button></td>
    </tr>
  `, '\u6682\u65e0\u5339\u914d\u7684\u8bf7\u6c42\u8bb0\u5f55');
  renderRequestsPager();
}

async function loadDeviceMetrics() {
  const query = new URLSearchParams(objectFromForm(el.metricsFilterForm));
  const data = await request(`/api/metrics/clients/weekly?${query.toString()}`, { method: 'GET' });
  state.weekly = data.items || [];
  if (!state.weekly.some((item) => item.clientId === state.selectedClient)) {
    state.selectedClient = state.weekly.length > 0 ? state.weekly[0].clientId : '';
  }

  const totalClients = state.weekly.length;
  const totalRequests = state.weekly.reduce((sum, item) => sum + Number(item.totalRequests || 0), 0);
  const totalSuccess = state.weekly.reduce((sum, item) => sum + Number(item.successRequests || 0), 0);
  const avgSuccess = totalRequests ? ((totalSuccess * 100) / totalRequests).toFixed(1) : '0.0';

  el.deviceSummaryCards.innerHTML = [
    { label: '\u8bbe\u5907\u6570', value: totalClients, foot: '\u5f53\u524d\u7b5b\u9009\u547d\u4e2d\u7684 client' },
    { label: '7\u5929\u8bf7\u6c42', value: totalRequests, foot: `${totalSuccess} \u6210\u529f` },
    { label: '\u6210\u529f\u7387', value: `${avgSuccess}%`, foot: '\u57fa\u4e8e\u8bbe\u5907\u5468\u6307\u6807\u805a\u5408' }
  ].map((card) => cardTemplate(card)).join('');

  el.weeklyMetricsBody.innerHTML = renderRows(state.weekly, 8, (item) => `
    <tr>
      <td>${escapeHTML(item.clientId)}</td>
      <td>${escapeHTML(item.group)}</td>
      <td>${Number(item.totalRequests || 0)}</td>
      <td>${Number(item.successRequests || 0)}</td>
      <td>${Number(item.failedRequests || 0)}</td>
      <td>${Number(item.timeoutRequests || 0)}</td>
      <td>${Number(item.avgLatencyMs || 0)} ms</td>
      <td><button class="ghost mini-btn" type="button" onclick="showDaily('${escapeJS(item.clientId)}')">\u67e5\u770b</button></td>
    </tr>
  `, '\u6682\u65e0\u8bbe\u5907\u5468\u6307\u6807');

  if (state.selectedClient && el.deviceTrendModal && !el.deviceTrendModal.classList.contains('hidden')) {
    await showDaily(state.selectedClient, false);
  }
}

async function showDaily(clientId, announce = true) {
  state.selectedClient = clientId;
  const data = await request('/api/metrics/clients/' + encodeURIComponent(clientId) + '/daily?days=15', { method: 'GET' });
  state.daily = (data.items || []).slice().reverse();

  if (el.deviceTrendTitle) {
    el.deviceTrendTitle.textContent = '\u8bbe\u5907 15 \u5929\u8d8b\u52bf / ' + clientId;
  }
  if (el.deviceTrendMeta) {
    const requestCount = state.daily.reduce((sum, item) => sum + Number(item.totalRequests || 0), 0);
    const successCount = state.daily.reduce((sum, item) => sum + Number(item.successRequests || 0), 0);
    const successRate = requestCount ? ((successCount * 100) / requestCount).toFixed(1) : '0.0';
    el.deviceTrendMeta.textContent = `15\u5929\u8bf7\u6c42 ${requestCount}\uff0c\u6210\u529f ${successCount}\uff0c\u6210\u529f\u7387 ${successRate}%`;
  }
  if (el.deviceTrendBody) {
    el.deviceTrendBody.innerHTML = renderRows(state.daily, 7, (item) => `
      <tr>
        <td>${escapeHTML(item.statDate)}</td>
        <td>${escapeHTML(item.group)}</td>
        <td>${Number(item.totalRequests || 0)}</td>
        <td>${Number(item.successRequests || 0)}</td>
        <td>${Number(item.failedRequests || 0)}</td>
        <td>${Number(item.timeoutRequests || 0)}</td>
        <td>${Number(item.maxLatencyMs || 0)} ms</td>
      </tr>
    `, '\u6700\u8fd1 15 \u5929\u6682\u65e0\u65e5\u6307\u6807');
  }

  openDeviceTrendModal();
  requestAnimationFrame(() => renderDeviceTrendCharts());

  if (announce) {
    toast('\u5df2\u52a0\u8f7d ' + clientId + ' \u6700\u8fd1 15 \u5929\u6307\u6807');
  }
}

async function loadUsers() {
  const data = await request('/api/users', { method: 'GET' });
  state.users = data.items || [];
  el.usersBody.innerHTML = renderRows(state.users, 6, (item) => `
    <tr>
      <td>${escapeHTML(item.username)}</td>
      <td>${roleBadge(item.role)}</td>
      <td>${boolBadge(item.enabled, '\u542f\u7528', '\u7981\u7528')}</td>
      <td>${boolBadge(item.canRpc, 'RPC \u5141\u8bb8', 'RPC \u7981\u6b62')}</td>
      <td>${formatDate(item.lastLoginAt)}</td>
      <td>
        <div class="actions">
          <button class="ghost mini-btn" type="button" onclick="toggleUserEnabled(${Number(item.id)}, ${item.enabled ? 'false' : 'true'}, ${item.canRpc ? 'true' : 'false'})">${item.enabled ? '\u7981\u7528' : '\u542f\u7528'}</button>
          <button class="ghost mini-btn" type="button" onclick="toggleUserRPC(${Number(item.id)}, ${item.enabled ? 'true' : 'false'}, ${item.canRpc ? 'false' : 'true'})">${item.canRpc ? '\u7981\u6b62 RPC' : '\u5141\u8bb8 RPC'}</button>
          <button class="ghost mini-btn" type="button" onclick="resetUserPassword(${Number(item.id)}, '${escapeJS(item.username)}')">\u91cd\u7f6e\u5bc6\u7801</button>
        </div>
      </td>
    </tr>
  `, '\u6682\u65e0\u8d26\u6237\u6570\u636e');
}

async function onCreateUser(event) {
  event.preventDefault();
  const form = new FormData(el.createUserForm);
  await request('/api/users', {
    method: 'POST',
    body: JSON.stringify({
      username: String(form.get('username') || '').trim(),
      password: String(form.get('password') || ''),
      role: String(form.get('role') || 'client'),
      enabled: String(form.get('enabled') || 'true') === 'true',
      canRpc: String(form.get('canRpc') || 'true') === 'true',
      notes: String(form.get('notes') || '').trim()
    })
  });
  el.createUserForm.reset();
  toast('\u8d26\u6237\u5df2\u521b\u5efa');
  await loadUsers();
}

async function toggleUserEnabled(userId, enabled, canRpc) {
  await request(`/api/users/${userId}/status`, {
    method: 'PATCH',
    body: JSON.stringify({ enabled, canRpc })
  });
  toast('\u8d26\u6237\u72b6\u6001\u5df2\u66f4\u65b0');
  await loadUsers();
}

async function toggleUserRPC(userId, enabled, canRpc) {
  await request(`/api/users/${userId}/status`, {
    method: 'PATCH',
    body: JSON.stringify({ enabled, canRpc })
  });
  toast('RPC \u6743\u9650\u5df2\u66f4\u65b0');
  await loadUsers();
}

async function resetUserPassword(userId, username) {
  const password = window.prompt('\u8bf7\u4e3a ' + username + ' \u8f93\u5165\u65b0\u5bc6\u7801');
  if (!password) {
    return;
  }
  await request('/api/users/' + userId + '/password', {
    method: 'PATCH',
    body: JSON.stringify({ password })
  });
  toast(username + ' \u7684\u5bc6\u7801\u5df2\u91cd\u7f6e');
}

async function onInvoke(event) {
  event.preventDefault();
  const form = new FormData(el.invokeForm);
  let payload = {};
  const raw = String(form.get('payload') || '').trim();
  if (raw) {
    payload = JSON.parse(raw);
  }

  const group = String(form.get('group') || '').trim();
  const action = String(form.get('action') || '').trim();
  const requestUrl = `/rpc/invoke/${encodeURIComponent(group)}/${encodeURIComponent(action)}`;
  const requestBody = {
    clientId: String(form.get('clientId') || '').trim(),
    timeoutSeconds: Number(form.get('timeoutSeconds') || 20),
    payload
  };

  try {
    const result = await requestWithMeta(requestUrl, {
      method: 'POST',
      body: JSON.stringify(requestBody)
    });
    el.invokeResult.textContent = JSON.stringify(buildInvokeConsoleResult(requestUrl, requestBody, result.status, result.data), null, 2);
    toast('\\u8c03\\u7528\\u6210\\u529f');
  } catch (error) {
    const detail = error && typeof error === 'object' && typeof error.payload !== 'undefined'
      ? error.payload
      : { error: error?.message || String(error) };
    el.invokeResult.textContent = JSON.stringify(
      buildInvokeConsoleResult(requestUrl, requestBody, Number(error?.status || 0) || 500, detail),
      null,
      2
    );
    toast(detail.error || '\\u8c03\\u7528\\u5931\\u8d25');
  }
}

function buildInvokeConsoleResult(requestUrl, requestBody, status, responseBody) {
  return {
    request: {
      url: requestUrl,
      body: requestBody
    },
    response: {
      httpStatus: status,
      body: responseBody
    }
  };
}

async function requestWithMeta(url, options = {}, auth = true) {
  const headers = { ...(options.headers || {}) };
  if (options.body) {
    headers['Content-Type'] = 'application/json';
  }
  if (auth && state.token) {
    headers.Authorization = `Bearer ${state.token}`;
  }

  const response = await fetch(url, { ...options, headers, cache: 'no-store' });
  const rawText = await response.text();
  let data = {};
  if (rawText.trim()) {
    try {
      data = JSON.parse(rawText);
    } catch {
      data = rawText;
    }
  }
  if (!response.ok) {
    if (response.status === 401 && auth) {
      logout();
    }
    const errorMessage = data && typeof data === 'object' && data.error
      ? data.error
      : `HTTP ${response.status}`;
    const error = new Error(errorMessage);
    error.payload = data;
    error.rawText = rawText;
    error.status = response.status;
    throw error;
  }
  return { status: response.status, data, rawText };
}

async function request(url, options = {}, auth = true) {
  const result = await requestWithMeta(url, options, auth);
  const data = result.data;
  return data;
}

function objectFromForm(form) {
  const formData = new FormData(form);
  const result = {};
  for (const [key, value] of formData.entries()) {
    if (value !== '') {
      result[key] = value;
    }
  }
  return result;
}

function cardTemplate(card) {
  return `
    <article class="card">
      <div class="metric-label">${escapeHTML(card.label)}</div>
      <div class="metric-value">${escapeHTML(String(card.value ?? '-'))}</div>
      <div class="metric-foot">${escapeHTML(String(card.foot ?? ''))}</div>
    </article>
  `;
}

function renderRows(items, colspan, rowBuilder, emptyText) {
  if (!items || items.length === 0) {
    return renderEmptyRow(colspan, emptyText);
  }
  return items.map((item, index) => rowBuilder(item, index)).join('');
}

function renderEmptyRow(colspan, text) {
  return `<tr><td colspan="${colspan}" class="empty">${escapeHTML(text)}</td></tr>`;
}

function formatDate(value) {
  if (!value) {
    return '-';
  }
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) {
    return String(value);
  }
  const pad = (num) => String(num).padStart(2, '0');
  return `${date.getFullYear()}-${pad(date.getMonth() + 1)}-${pad(date.getDate())} ${pad(date.getHours())}:${pad(date.getMinutes())}:${pad(date.getSeconds())}`;
}

function statusText(status) {
  switch (String(status || '')) {
    case 'online': return 'Online';
    case 'offline': return 'Offline';
    case 'stale': return 'Stale';
    case 'no_device': return 'No device';
    case 'success': return 'Success';
    case 'timeout': return 'Timeout';
    case 'error': return 'Error';
    case 'rejected': return 'Rejected';
    case 'no_client': return 'No client';
    case 'enabled': return '\u542f\u7528';
    case 'disabled': return '\u7981\u7528';
    default: return String(status || '-');
  }
}

function statusBadge(status) {
  const value = String(status || 'unknown');
  return `<span class="badge ${escapeHTML(value)}">${escapeHTML(statusText(value))}</span>`;
}

function roleBadge(role) {
  const value = String(role || 'client');
  return `<span class="badge ${escapeHTML(value)}">${escapeHTML(value)}</span>`;
}

function boolBadge(value, yesText, noText) {
  return value
    ? `<span class="badge enabled">${escapeHTML(yesText)}</span>`
    : `<span class="badge disabled">${escapeHTML(noText)}</span>`;
}

function drawBarChart(canvas, labels, values, color, suffix) {
  if (!canvas || typeof canvas.getContext !== 'function') {
    return;
  }
  const ctx = canvas.getContext('2d');
  if (!ctx) {
    return;
  }
  drawChartBase(ctx, canvas, labels, values, (plot, max) => {
    const barWidth = plot.step * 0.62;
    values.forEach((value, index) => {
      const x = plot.left + (index + 0.5) * plot.step - barWidth / 2;
      const y = plot.bottom - (max ? (value / max) * plot.height : 0);
      ctx.fillStyle = color;
      ctx.fillRect(x, y, barWidth, plot.bottom - y);
    });
  }, suffix);
}

function drawLineChart(canvas, labels, values, color, suffix) {
  if (!canvas || typeof canvas.getContext !== 'function') {
    return;
  }
  const ctx = canvas.getContext('2d');
  if (!ctx) {
    return;
  }
  drawChartBase(ctx, canvas, labels, values, (plot, max) => {
    ctx.beginPath();
    values.forEach((value, index) => {
      const x = plot.left + (index + 0.5) * plot.step;
      const y = plot.bottom - (max ? (value / max) * plot.height : 0);
      if (index === 0) {
        ctx.moveTo(x, y);
      } else {
        ctx.lineTo(x, y);
      }
    });
    ctx.strokeStyle = color;
    ctx.lineWidth = 3;
    ctx.stroke();

    values.forEach((value, index) => {
      const x = plot.left + (index + 0.5) * plot.step;
      const y = plot.bottom - (max ? (value / max) * plot.height : 0);
      ctx.fillStyle = color;
      ctx.beginPath();
      ctx.arc(x, y, 4, 0, Math.PI * 2);
      ctx.fill();
    });
  }, suffix);
}

function drawChartBase(ctx, canvas, labels, values, painter, suffix) {
  const ratio = window.devicePixelRatio || 1;
  const width = canvas.clientWidth || canvas.width || 640;
  const height = canvas.clientHeight || canvas.height || 220;
  canvas.width = width * ratio;
  canvas.height = height * ratio;
  ctx.setTransform(ratio, 0, 0, ratio, 0, 0);
  ctx.clearRect(0, 0, width, height);

  const axis = buildValueAxis(values, suffix);
  const plot = { left: 58, top: 18, right: width - 16, bottom: height - 38 };
  plot.width = plot.right - plot.left;
  plot.height = plot.bottom - plot.top;
  plot.step = plot.width / Math.max(labels.length, 1);

  ctx.strokeStyle = 'rgba(255,255,255,0.08)';
  ctx.lineWidth = 1;
  axis.ticks.forEach((tick) => {
    const ratioY = axis.max ? (tick / axis.max) : 0;
    const y = plot.bottom - ratioY * plot.height;
    ctx.beginPath();
    ctx.moveTo(plot.left, y);
    ctx.lineTo(plot.right, y);
    ctx.stroke();
  });

  ctx.fillStyle = 'rgba(156,176,168,0.88)';
  ctx.font = '12px Aptos, sans-serif';
  ctx.textBaseline = 'middle';
  ctx.textAlign = 'right';

  if (!labels.length || !values.length) {
    ctx.fillText('\u6682\u65e0\u6570\u636e', plot.left, plot.top + 14);
    return;
  }

  axis.ticks.forEach((tick) => {
    const ratioY = axis.max ? (tick / axis.max) : 0;
    const y = plot.bottom - ratioY * plot.height;
    ctx.fillText(formatAxisValue(tick, suffix), plot.left - 10, y);
  });

  const axisTicks = buildAxisTicks(labels, plot.width);
  ctx.textAlign = 'center';
  ctx.textBaseline = 'alphabetic';
  axisTicks.forEach(({ label, index }) => {
    const x = plot.left + (index + 0.5) * plot.step;
    ctx.fillText(label, x, height - 10);

    ctx.strokeStyle = 'rgba(255,255,255,0.12)';
    ctx.beginPath();
    ctx.moveTo(x, plot.bottom + 2);
    ctx.lineTo(x, plot.bottom + 6);
    ctx.stroke();
  });

  painter(plot, axis.max || 1);
}

function buildValueAxis(values, suffix) {
  const numericValues = values
    .map((value) => Number(value))
    .filter((value) => Number.isFinite(value) && value >= 0);
  const rawMax = numericValues.length ? Math.max(...numericValues, 0) : 0;

  if (rawMax <= 0) {
    return { max: 1, ticks: [0, 1] };
  }

  const isPercent = suffix === '%';
  const isLatency = suffix === 'ms';
  let step = (isPercent || isLatency)
    ? niceAxisStep(rawMax / 4)
    : niceCountStep(rawMax / 4);
  let max = Math.ceil(rawMax / step) * step;
  if (max <= rawMax) {
    max += step;
  }
  if (isPercent && max > 100) {
    max = 100;
    step = niceAxisStep(max / 4);
  }

  const ticks = [];
  for (let value = 0; value <= max + (step / 2); value += step) {
    ticks.push(roundAxisValue(value));
  }
  if (ticks[ticks.length - 1] !== max) {
    ticks.push(roundAxisValue(max));
  }
  return { max, ticks };
}

function niceCountStep(rawStep) {
  const safeStep = Math.max(1, rawStep);
  const exponent = Math.floor(Math.log10(safeStep));
  const base = 10 ** exponent;
  const fraction = safeStep / base;

  let niceFraction = 1;
  if (fraction <= 1) {
    niceFraction = 1;
  } else if (fraction <= 2) {
    niceFraction = 2;
  } else if (fraction <= 3) {
    niceFraction = 3;
  } else if (fraction <= 5) {
    niceFraction = 5;
  } else {
    niceFraction = 10;
  }
  return Math.max(1, Math.round(niceFraction * base));
}

function niceAxisStep(rawStep) {
  const safeStep = rawStep > 0 ? rawStep : 1;
  const exponent = Math.floor(Math.log10(safeStep));
  const base = 10 ** exponent;
  const fraction = safeStep / base;

  let niceFraction = 1;
  if (fraction <= 1) {
    niceFraction = 1;
  } else if (fraction <= 2) {
    niceFraction = 2;
  } else if (fraction <= 2.5) {
    niceFraction = 2.5;
  } else if (fraction <= 5) {
    niceFraction = 5;
  } else {
    niceFraction = 10;
  }
  return niceFraction * base;
}

function roundAxisValue(value) {
  return Number(value.toFixed(2));
}

function formatAxisValue(value, suffix) {
  const text = Number.isInteger(value)
    ? String(value)
    : String(Number(value.toFixed(2))).replace(/\.0+$/, '');
  if (suffix === '%' || suffix === 'ms') {
    return text + suffix;
  }
  return text;
}

function compactDateLabel(value) {
  const text = String(value || '');
  if (!text) {
    return '';
  }
  if (/^\d{4}-\d{2}-\d{2}$/.test(text)) {
    return text.slice(5);
  }
  return text.length > 5 ? text.slice(-5) : text;
}

function buildAxisTicks(labels, width) {
  if (!labels.length) {
    return [];
  }
  const minSpacing = 58;
  const maxTickCount = Math.max(2, Math.floor(width / minSpacing));
  if (labels.length <= maxTickCount) {
    return labels.map((label, index) => ({ label, index }));
  }
  const step = Math.max(1, Math.ceil((labels.length - 1) / (maxTickCount - 1)));
  const ticks = [];
  for (let index = 0; index < labels.length; index += step) {
    ticks.push({ label: labels[index], index });
  }
  const lastIndex = labels.length - 1;
  if (ticks[ticks.length - 1]?.index !== lastIndex) {
    ticks.push({ label: labels[lastIndex], index: lastIndex });
  }
  return ticks;
}

function toast(message) {
  el.toast.textContent = message;
  el.toast.classList.remove('hidden');
  clearTimeout(toast.timer);
  toast.timer = setTimeout(() => el.toast.classList.add('hidden'), 2400);
}

function handleError(error) {
  toast(error.message || String(error));
}

function escapeHTML(value) {
  return String(value).replace(/[&<>"']/g, (char) => ({
    '&': '&amp;',
    '<': '&lt;',
    '>': '&gt;',
    '"': '&quot;',
    "'": '&#39;'
  }[char]));
}

function escapeJS(value) {
  return String(value).replace(/\\/g, '\\\\').replace(/'/g, "\\'");
}

window.openGroupClients = openGroupClients;
window.selectExplorerGroup = selectExplorerGroup;
window.selectExplorerClient = selectExplorerClient;
window.showDaily = showDaily;
window.toggleUserEnabled = toggleUserEnabled;
window.toggleUserRPC = toggleUserRPC;
window.resetUserPassword = resetUserPassword;
window.closeClientDetailModal = closeClientDetailModal;
window.openRequestDetail = openRequestDetail;
window.openClientRequestDetail = openClientRequestDetail;
window.closeRequestDetailModal = closeRequestDetailModal;
window.closeDeviceTrendModal = closeDeviceTrendModal;
window.addEventListener('resize', () => {
  if (state.page === 'overview') {
    renderOverview();
  }
  if (el.deviceTrendModal && !el.deviceTrendModal.classList.contains('hidden')) {
    renderDeviceTrendCharts();
  }
});










