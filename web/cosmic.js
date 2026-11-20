(() => {
  const get = (id) => document.getElementById(id);
  const element = (tag, text, className) => {
    const node = document.createElement(tag);
    if (text) node.textContent = text;
    if (className) node.className = className;
    return node;
  };
  const search = get('work-search');
  const motion = matchMedia('(prefers-reduced-motion: reduce)');
  const dateFormat = new Intl.DateTimeFormat('zh-CN', { month: 'numeric', day: 'numeric' });
  let projects = [];
  let current;
  let departing = false;
  let loadingAgent = false;
  let effects = true;
  try { effects = localStorage.getItem('cosmic-effects') !== 'off'; } catch {}

  async function request(path, options) {
    const response = await fetch(path, options);
    if (!response.ok) {
      const error = new Error(`请求失败（HTTP ${response.status}）`);
      error.status = response.status;
      const body = await response.json().catch(() => null);
      if (typeof body?.error === 'string') error.message = body.error;
      throw error;
    }
    return response.json();
  }

  const title = (project) => project.title || project.name || '未命名作品';
  const studio = (id) => `/studio.html?projectId=${encodeURIComponent(id)}`;

  function enter(id, url) {
    if (departing) return;
    departing = true;
    if (id) document.cookie = `project_id=${encodeURIComponent(id)}; path=/; max-age=31536000; SameSite=Lax`;
    if (effects && !motion.matches) {
      document.body.classList.add('departing');
      window.setTimeout(() => location.assign(url), 380);
    } else location.assign(url);
  }

  function wireEntry(link, id) {
    link.addEventListener('click', (event) => {
      if (event.button || event.ctrlKey || event.metaKey || event.shiftKey || event.altKey) return;
      event.preventDefault();
      enter(id(), link.href);
    });
  }

  function renderWorks() {
    const query = search.value.trim().toLocaleLowerCase();
    const matches = projects.map((project, index) => ({ project, index })).filter(({ project }) => title(project).toLocaleLowerCase().includes(query));
    const cards = matches.map(({ project, index }) => {
      const card = element('a', '', `work-card cover-${index % 4}`);
      card.href = studio(project.id);
      card.append(element('strong', title(project)), element('span', `${project.chapterCount || 0} 章${project.id === current?.id ? ' · 当前作品' : ''}`, 'card-stats'));
      wireEntry(card, () => project.id);
      return card;
    });
    get('work-grid').replaceChildren(...cards);
    get('works-status').hidden = cards.length > 0;
    get('works-status').textContent = query ? '没有匹配的作品。' : '还没有作品，可以新建后导入文稿。';
  }

  function renderRecent() {
    const recent = projects.filter((project) => project.chapterCount > 0 && Number.isFinite(Date.parse(project.updatedAt))).slice(0, 3);
    get('recent-works').replaceChildren(...recent.map((project) => {
      const row = element('li');
      const link = element('a');
      link.href = studio(project.id);
      link.append(element('span', title(project)), element('small', dateFormat.format(new Date(project.updatedAt))));
      wireEntry(link, () => project.id);
      row.append(link);
      return row;
    }));
    if (!recent.length) get('recent-works').append(element('li', '暂无创作记录', 'muted'));
  }

  async function refreshAgent() {
    if (!current || loadingAgent || document.hidden) return;
    loadingAgent = true;
    const headers = { 'X-Project-ID': current.id };
    const badge = get('agent-state');
    const list = get('agent-runs');
    try {
      const [runs, tasks] = await Promise.allSettled([
        request('/api/agents/runs?limit=3', { headers }),
        request('/api/tasks?limit=3&status=running', { headers }),
      ]);
      const live = tasks.status === 'fulfilled' ? tasks.value.tasks || [] : [];
      const records = runs.status === 'fulfilled' ? runs.value.runs || [] : [];
      badge.toggleAttribute('data-running', live.length > 0);
      badge.textContent = live.length ? '运行中' : tasks.status === 'fulfilled' ? '空闲' : '状态不可用';
      const rows = [
        ...live.map((task) => [task.description || task.kind, task.total > 0 ? `${task.progress}/${task.total}` : '运行中']),
        ...records.map((run) => [run.agentName || run.agent, ({ done: '完成', failed: '失败', skipped: '已跳过' })[run.status] || run.status]),
      ].slice(0, 3);
      list.replaceChildren(...rows.map(([name, status]) => {
        const row = element('li', '', 'run');
        row.append(element('span', name), element('small', status));
        return row;
      }));
      if (!rows.length) list.append(element('li', runs.status === 'fulfilled' ? '暂无 Agent 执行记录' : '暂时无法读取 Agent 记录', 'muted'));
    } finally { loadingAgent = false; }
  }

  async function loadProjects() {
    try {
      const result = await request('/api/projects');
      projects = (result.projects || []).slice().sort((a, b) => (Date.parse(b.updatedAt) || 0) - (Date.parse(a.updatedAt) || 0));
      current = projects.find((project) => project.id === result.currentId);
      if (current) {
        get('start').href = studio(current.id);
        get('agent-project').textContent = `当前作品：${title(current)}`;
        void refreshAgent();
      } else get('agent-state').textContent = '无作品';
      get('work-count').textContent = projects.length;
      get('chapter-count').textContent = projects.reduce((sum, project) => sum + (project.chapterCount || 0), 0);
      renderWorks();
      renderRecent();
    } catch {
      get('agent-state').textContent = '状态不可用';
      get('recent-works').replaceChildren(element('li', '暂时无法读取创作记录', 'muted'));
      const retry = element('button', '重试', 'quiet');
      retry.type = 'button';
      retry.addEventListener('click', loadProjects, { once: true });
      get('works-status').replaceChildren(document.createTextNode('无法读取作品。 '), retry);
    }
  }

  wireEntry(get('start'), () => current?.id);
  search.addEventListener('input', () => { if (current || projects.length) renderWorks(); });
  window.addEventListener('keydown', (event) => {
    if ((event.ctrlKey || event.metaKey) && event.key.toLowerCase() === 'k') {
      event.preventDefault();
      search.focus();
    }
  });

  const canvas = get('starfield');
  const context = canvas.getContext('2d');
  const galaxy = document.querySelector('.galaxy');
  const stars = Array.from({ length: 106 }, () => [Math.random(), Math.random(), Math.random(), Math.random()]);
  let width = 0, height = 0, frame = 0, lastPaint = 0;
  let x = 0, y = 0, targetX = 0, targetY = 0;

  function paint(time) {
    if (!context) return;
    context.clearRect(0, 0, width, height);
    for (const [sx, sy, size, phase] of stars) {
      const opacity = .24 + size * .38 + (time ? Math.sin(time / 950 + phase * 7) * .19 : 0);
      context.fillStyle = `rgba(189,213,255,${opacity})`;
      context.beginPath();
      context.arc(sx * width - x * size, sy * height - y * size, .4 + size * 1.35, 0, Math.PI * 2);
      context.fill();
    }
  }

  function animate(time) {
    if (time - lastPaint > 32) {
      x += (targetX - x) * .14;
      y += (targetY - y) * .14;
      galaxy.style.setProperty('--drift-x', `${x}px`);
      galaxy.style.setProperty('--drift-y', `${y}px`);
      paint(time);
      lastPaint = time;
    }
    frame = requestAnimationFrame(animate);
  }

  function resize() {
    width = window.innerWidth;
    height = window.innerHeight;
    const ratio = Math.min(devicePixelRatio || 1, 1.5);
    canvas.width = Math.round(width * ratio);
    canvas.height = Math.round(height * ratio);
    context?.setTransform(ratio, 0, 0, ratio, 0, 0);
    paint(0);
  }

  function updateEffects() {
    const enabled = effects && !motion.matches;
    get('effects').setAttribute('aria-pressed', String(enabled));
    get('effects').disabled = motion.matches;
    get('effects').textContent = enabled ? '动效开启' : '动效关闭';
    document.body.classList.toggle('effects-on', enabled);
    cancelAnimationFrame(frame);
    x = y = targetX = targetY = 0;
    galaxy.style.removeProperty('--drift-x');
    galaxy.style.removeProperty('--drift-y');
    if (enabled && !document.hidden) frame = requestAnimationFrame(animate);
    else paint(0);
  }

  window.addEventListener('pointermove', (event) => {
    if (event.pointerType !== 'mouse') return;
    targetX = (event.clientX / width - .5) * 20;
    targetY = (event.clientY / height - .5) * 14;
  }, { passive: true });
  window.addEventListener('resize', resize);
  document.addEventListener('visibilitychange', updateEffects);
  window.addEventListener('pageshow', () => { departing = false; document.body.classList.remove('departing'); });
  motion.addEventListener('change', updateEffects);
  get('effects').addEventListener('click', () => {
    effects = !effects;
    try { localStorage.setItem('cosmic-effects', effects ? 'on' : 'off'); } catch {}
    updateEffects();
  });
  resize();
  updateEffects();
  void loadProjects();
  window.setInterval(refreshAgent, 5000);
})();
