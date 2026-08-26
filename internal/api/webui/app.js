const { createApp, ref, reactive, computed, onMounted, nextTick } = Vue;

const app = createApp({
  setup() {
    const view = ref('dashboard');
    const token = ref(localStorage.getItem('yaa_token') || '');
    const connected = ref(false);
    const loading = ref(false);

    const health = ref(null);
    const agents = ref([]);
    const sessions = ref([]);
    const messages = ref([]);
    const memoryItems = ref([]);
    const listData = ref([]);
    const listColumns = ref([]);
    const configText = ref('');

    const chat = reactive({ agent: '', session: '', text: '', sending: false });
    const chatSessions = ref([]);
    const sessionFilter = reactive({ agent: '', state: '' });
    const memory = reactive({ agent: '', query: '', put: { key: '', content: '', session_id: '' } });
    const memoryPutDialog = ref(false);

    const pageTitle = computed(() => {
      const map = {
        dashboard: '仪表盘', chat: '对话', agents: 'Agents', sessions: 'Sessions',
        memory: 'Memory', tools: 'Tools', skills: 'Skills', providers: 'Providers',
        mcp: 'MCP', config: 'Config'
      };
      return map[view.value] || 'Yaa!';
    });

    const statCards = computed(() => {
      const h = health.value || {};
      const agentsCount = (h.agents && h.agents.total) || 0;
      return [
        { label: '状态', value: h.status || '-', color: statusColor(h.status) },
        { label: 'Ready', value: h.ready ? '是' : '否', color: h.ready ? '#67c23a' : '#f56c6c' },
        { label: 'Agents', value: agentsCount, color: '#409eff' },
        { label: '运行时长', value: formatUptime(h.uptime_seconds), color: '#909399' },
      ];
    });

    const componentRows = computed(() => {
      const c = (health.value && health.value.components) || {};
      return Object.keys(c).sort().map(k => ({ name: k, status: c[k] }));
    });

    function statusColor(s) {
      if (!s) return '#909399';
      if (s === 'healthy' || s === 'running' || s === 'ready' || s === 'created') return '#67c23a';
      if (s === 'degraded' || s === 'paused') return '#e6a23c';
      if (s === 'not_ready' || s === 'unhealthy' || s === 'closed' || s === 'stopped') return '#f56c6c';
      return '#909399';
    }
    function statusType(s) {
      if (!s) return 'info';
      if (s === 'healthy' || s === 'running' || s === 'ready' || s === 'created') return 'success';
      if (s === 'degraded' || s === 'paused') return 'warning';
      if (s === 'not_ready' || s === 'unhealthy' || s === 'closed' || s === 'stopped') return 'danger';
      return 'info';
    }

    function headers() {
      const h = { 'Content-Type': 'application/json' };
      if (token.value) h['Authorization'] = 'Bearer ' + token.value;
      return h;
    }

    async function api(path, opts = {}) {
      const res = await fetch(path, { headers: headers(), ...opts });
      const body = await res.json().catch(() => ({}));
      if (!res.ok || body.code !== 0) {
        const msg = body.message || ('HTTP ' + res.status);
        throw new Error(msg);
      }
      return body.data;
    }

    function notify(msg, type = 'success') {
      ElMessage({ message: msg, type, grouping: true });
    }

    async function loadHealth() {
      try {
        health.value = await api('/api/v1/health');
        connected.value = true;
      } catch (e) {
        connected.value = false;
        health.value = null;
      }
    }

    async function loadAgents() {
      loading.value = true;
      try {
        const d = await api('/api/v1/agents?page_size=100');
        agents.value = d.items || [];
      } catch (e) {
        notify('加载 Agents 失败: ' + e.message, 'error');
      } finally {
        loading.value = false;
      }
    }

    async function loadSessions() {
      loading.value = true;
      try {
        let url = '/api/v1/agents/' + (sessionFilter.agent || '') + '/sessions?page_size=100';
        if (!sessionFilter.agent) url = '/api/v1/agents/' + (agents.value[0] ? agents.value[0].id : '') + '/sessions?page_size=100';
        if (sessionFilter.state) url += '&state=' + sessionFilter.state;
        const d = await api(url);
        sessions.value = d.items || [];
      } catch (e) {
        sessions.value = [];
        notify('加载 Sessions 失败: ' + e.message, 'error');
      } finally {
        loading.value = false;
      }
    }

    async function agentAction(id, action) {
      try {
        const d = await api('/api/v1/agents/' + id + '/' + action, { method: 'POST' });
        notify('Agent ' + id + ' → ' + (d.status || action));
        await loadAgents();
      } catch (e) {
        notify('操作失败: ' + e.message, 'error');
      }
    }

    async function sessionAction(id, action) {
      try {
        if (action === 'delete') {
          await api('/api/v1/sessions/' + id, { method: 'DELETE' });
        } else {
          await api('/api/v1/sessions/' + id + '/' + action, { method: 'POST' });
        }
        notify('Session ' + action + ' 成功');
        await loadSessions();
      } catch (e) {
        notify('操作失败: ' + e.message, 'error');
      }
    }

    function sessionLabel(s) {
      return s.id + ' (' + s.state + ', ' + s.message_count + ' msgs)';
    }

    async function onChatAgentChange() {
      chat.session = '';
      chatSessions.value = [];
      messages.value = [];
      if (chat.agent) {
        try {
          const d = await api('/api/v1/agents/' + chat.agent + '/sessions?page_size=100');
          chatSessions.value = d.items || [];
        } catch (e) {
          notify('加载会话失败: ' + e.message, 'error');
        }
      }
    }

    async function onChatSessionChange() {
      if (chat.session) await loadChatMessages();
    }

    async function createSessionForChat() {
      if (!chat.agent) {
        notify('请先选择 Agent', 'warning');
        return;
      }
      try {
        const d = await api('/api/v1/agents/' + chat.agent + '/sessions', {
          method: 'POST', body: JSON.stringify({})
        });
        chat.session = d.id;
        await onChatAgentChange();
        notify('会话已创建');
      } catch (e) {
        notify('创建会话失败: ' + e.message, 'error');
      }
    }

    async function loadChatMessages() {
      if (!chat.session) return;
      try {
        const d = await api('/api/v1/sessions/' + chat.session + '/messages?page_size=200');
        messages.value = (d.items || []).slice().reverse();
        await nextTick();
        scrollChatBottom();
      } catch (e) {
        notify('加载消息失败: ' + e.message, 'error');
      }
    }

    async function sendMessage() {
      if (!chat.session || !chat.text.trim()) {
        notify('请选择会话并输入内容', 'warning');
        return;
      }
      chat.sending = true;
      try {
        await api('/api/v1/sessions/' + chat.session + '/messages', {
          method: 'POST',
          body: JSON.stringify({ content: chat.text })
        });
        chat.text = '';
        await loadChatMessages();
      } catch (e) {
        notify('发送失败: ' + e.message, 'error');
      } finally {
        chat.sending = false;
      }
    }

    function scrollChatBottom() {
      const el = document.querySelector('.chat-messages');
      if (el) el.scrollTop = el.scrollHeight;
    }

    async function memorySearch() {
      if (!memory.agent) {
        notify('请选择 Agent', 'warning');
        return;
      }
      loading.value = true;
      try {
        const q = memory.query ? '?query=' + encodeURIComponent(memory.query) : '';
        const d = await api('/api/v1/agents/' + memory.agent + '/memory' + q);
        memoryItems.value = d.items || [];
      } catch (e) {
        memoryItems.value = [];
        notify('搜索失败: ' + e.message, 'error');
      } finally {
        loading.value = false;
      }
    }

    async function memoryPut() {
      if (!memory.agent || !memory.put.key || !memory.put.content) {
        notify('请填写 Agent、Key 和内容', 'warning');
        return;
      }
      try {
        await api('/api/v1/agents/' + memory.agent + '/memory', {
          method: 'POST',
          body: JSON.stringify({
            key: memory.put.key,
            content: memory.put.content,
            session_id: memory.put.session_id || ''
          })
        });
        notify('写入成功');
        memoryPutDialog.value = false;
        memory.put = { key: '', content: '', session_id: '' };
        await memorySearch();
      } catch (e) {
        notify('写入失败: ' + e.message, 'error');
      }
    }

    async function memoryDelete(key) {
      if (!memory.agent) return;
      try {
        await api('/api/v1/agents/' + memory.agent + '/memory/' + encodeURIComponent(key), { method: 'DELETE' });
        notify('删除成功');
        await memorySearch();
      } catch (e) {
        notify('删除失败: ' + e.message, 'error');
      }
    }

    async function loadList(kind) {
      loading.value = true;
      try {
        const urlMap = {
          tools: '/api/v1/tools',
          skills: '/api/v1/skills',
          providers: '/api/v1/providers',
          mcp: '/api/v1/mcp/servers'
        };
        const d = await api(urlMap[kind]);
        listData.value = d.items || [];
        listColumns.value = columnsFor(kind);
      } catch (e) {
        listData.value = [];
        notify('加载失败: ' + e.message, 'error');
      } finally {
        loading.value = false;
      }
    }

    function columnsFor(kind) {
      if (kind === 'tools') return [
        { prop: 'name', label: '名称' }, { prop: 'description', label: '描述' },
        { prop: 'source', label: '来源' }, { prop: 'enabled', label: '启用' }
      ];
      if (kind === 'skills') return [
        { prop: 'name', label: '名称' }, { prop: 'description', label: '描述' }
      ];
      if (kind === 'providers') return [
        { prop: 'id', label: 'ID' }, { prop: 'type', label: '类型' }, { prop: 'models', label: '模型' }
      ];
      if (kind === 'mcp') return [
        { prop: 'name', label: '名称' }, { prop: 'transport', label: '传输' }, { prop: 'status', label: '状态' }
      ];
      return [];
    }

    async function loadConfig() {
      try {
        const d = await api('/api/v1/config');
        configText.value = JSON.stringify(d, null, 2);
      } catch (e) {
        configText.value = '加载失败: ' + e.message;
      }
    }

    function saveToken() {
      localStorage.setItem('yaa_token', token.value);
    }

    function onMenuSelect(index) {
      view.value = index;
      routeView(index);
    }

    async function routeView(index) {
      switch (index) {
        case 'dashboard': await loadHealth(); break;
        case 'agents': await loadAgents(); break;
        case 'sessions': await loadAgents(); await loadSessions(); break;
        case 'chat': await loadAgents(); break;
        case 'memory': await loadAgents(); break;
        case 'tools': case 'skills': case 'providers': case 'mcp': await loadList(index); break;
        case 'config': await loadConfig(); break;
      }
    }

    function formatTime(t) {
      if (!t) return '';
      const d = new Date(t);
      return isNaN(d) ? t : d.toLocaleString();
    }
    function formatUptime(s) {
      if (s == null) return '-';
      const sec = Number(s);
      if (sec < 60) return sec + 's';
      if (sec < 3600) return Math.floor(sec / 60) + 'm ' + (sec % 60) + 's';
      return Math.floor(sec / 3600) + 'h ' + Math.floor((sec % 3600) / 60) + 'm';
    }

    onMounted(async () => {
      await loadHealth();
      await loadAgents();
    });

    return {
      view, token, connected, loading, health, agents, sessions, messages,
      memoryItems, listData, listColumns, configText, chat, chatSessions,
      sessionFilter, memory, memoryPutDialog, pageTitle, statCards, componentRows,
      onMenuSelect, saveToken, agentAction, sessionAction, sessionLabel,
      onChatAgentChange, onChatSessionChange, createSessionForChat, loadChatMessages,
      sendMessage, memorySearch, memoryPut, memoryDelete, formatTime, formatUptime,
      statusType
    };
  }
});

app.use(ElementPlus);
app.mount('#app');
