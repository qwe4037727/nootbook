(() => {
  "use strict";

  const statuses = {
    pending: "排队中", running: "执行中", paused: "已暂停",
    completed: "已完成", failed: "失败", cancelled: "已取消",
  };
  const priorities = { low: "低", normal: "普通", high: "高" };
  const severities = { info: "信息", warning: "警告", critical: "严重", blocking: "阻断" };
  const agents = {
    echo: "Echo 观察", plot: "剧情推演", style: "文风", relation: "关系情绪",
    proofread: "校对润色", task: "任务执行", fuse: "伏笔", memory: "记忆", continuity: "连续性", image: "图像一致性",
  };
  const kinds = { continue_batch: "批量续写", agent_run: "Agent 审查" };
  const actions = { pause: "暂停", resume: "恢复", cancel: "取消" };
  const sources = { agent_run: "Agent 运行", adapt_run: "改编", continue_candidate: "续写候选" };
  const rowStyle = "display:flex;flex-wrap:wrap;gap:8px;align-items:end;";
  const stackStyle = "display:grid;gap:10px;min-width:0;";
  const cardStyle = "padding:10px 12px;border:1px solid var(--line);background:var(--paper);overflow-wrap:anywhere;";
  const count = (value) => (Number(value) || 0).toLocaleString("zh-CN");
  const array = (value) => Array.isArray(value) ? value : [];
  const named = (labels, key) => Object.hasOwn(labels, key) ? labels[key] : (key || "—");

  function element(tag, attrs = {}, text) {
    const node = document.createElement(tag);
    for (const [name, value] of Object.entries(attrs)) {
      if (name === "style") node.style.cssText = value;
      else node.setAttribute(name, value);
    }
    if (text != null) node.textContent = text;
    return node;
  }

  function button(text, click, className = "btn btn-quiet") {
    const node = element("button", { type: "button", class: className }, text);
    node.addEventListener("click", click);
    return node;
  }

  function select(options) {
    const node = element("select");
    for (const [value, label] of options) node.append(element("option", { value }, label));
    return node;
  }

  function field(label, control) {
    const wrapper = element("label", { class: "field" });
    wrapper.append(element("span", {}, label), control);
    return wrapper;
  }

  function timeLabel(value) {
    const date = new Date(value);
    return value && date.getFullYear() > 1 ? date.toLocaleString("zh-CN") : "—";
  }

  function projectCookie() {
    // Keep the raw cookie for change detection. The router resolves the actual ID.
    return document.cookie.split(";").map((part) => part.trim())
      .find((part) => part.startsWith("project_id=")) || "";
  }

  function contextKey() {
    return `${projectCookie()}\n${typeof projectEpoch === "number" ? projectEpoch : 0}`;
  }

  function friendlyError(error) {
    if (error.status === 404 && !error.code && error.message === "请求失败（HTTP 404）") {
      return "M5 接口不可用，请检查是否启用了 M5。";
    }
    return error.message || "请求失败，请重试。";
  }

  function openCenter() {
    if (document.getElementById("taskCenterDialog")) return;
    const page = element("section", { id: "taskCenterDialog", class: "dialog dialog-wide", "aria-labelledby": "taskCenterTitle" });
    page.classList.add("studio-page");
    page.hidden = true;
    const body = element("div", { class: "dialog-form", style: "overflow-wrap:anywhere;" });
    const title = element("h2", { id: "taskCenterTitle" }, "任务与成本");
    const closePage = () => window.studioPages.close(page);
    const close = button("×", closePage, "icon-button");
    close.setAttribute("aria-label", "关闭任务与成本");
    const head = element("div", { class: "dialog-head" });
    head.append(title, close);
    const scopeLabel = element("p", { class: "dialog-note" }, "正在读取当前作品…");
    const navigation = element("div", { role: "group", "aria-label": "任务中心视图", style: rowStyle });
    const contextStatus = element("div", { role: "status" });
    const panels = {};
    const session = new AbortController();
    const context = contextKey();
    let currentView = "tasks";
    let projectId = "";
    let readController;
    let timer;
    let writing = false;
    let confirming = false;
    let closingForContext = false;
    let lastTaskSnapshot = "";

    async function closeForContextChange() {
      closingForContext = true;
      try {
        await closePage();
        if (session.signal.aborted) showToast("作品已变更，请重新打开任务中心");
      } finally {
        closingForContext = false;
      }
    }

    function isCurrent() {
      if (page.hidden || session.signal.aborted || closingForContext) return false;
      if (contextKey() === context) return true;
      void closeForContextChange();
      return false;
    }

    function request(url, options = {}, scoped = true) {
      return api(url, {
        cache: "no-store",
        signal: session.signal,
        ...options,
        ...(scoped ? { headers: { "X-Project-ID": projectId } } : {}),
      });
    }

    function makePanel(key, name) {
      const navButton = button(name, () => switchView(key));
      navButton.setAttribute("aria-controls", `task-center-${key}`);
      const node = element("section", { id: `task-center-${key}`, "aria-label": name });
      node.hidden = true;
      const controls = element("div", { style: stackStyle });
      const status = element("div", { role: "status" });
      const results = element("div", { style: stackStyle });
      node.append(controls, status, results);
      panels[key] = { node, controls, status, results, navButton };
      navigation.append(navButton);
      return panels[key];
    }

    function switchView(key) {
      if (!isCurrent() || !projectId || key === currentView) return;
      currentView = key;
      activateView();
    }

    function activateView() {
      for (const [key, panel] of Object.entries(panels)) {
        const active = key === currentView;
        panel.node.hidden = !active;
        panel.navButton.setAttribute("aria-pressed", String(active));
        panel.navButton.className = active ? "btn btn-primary" : "btn btn-quiet";
      }
      void load();
    }

    const tasks = makePanel("tasks", "任务队列");
    const audit = makePanel("audit", "审计报告");
    const cost = makePanel("cost", "成本看板");
    const description = element("textarea", {
      rows: "2", maxlength: "2000", required: "", placeholder: "例如：续写3章，让主角进入秘境；或：校对最后一章",
    });
    const priority = select(Object.entries(priorities));
    priority.value = "normal";
    const submit = element("button", { type: "submit", class: "btn btn-primary" }, "提交任务");
    const taskNote = element("div", { role: "status" });
    const taskForm = element("form", { style: stackStyle });
    const submitRow = element("div", { style: rowStyle });
    submitRow.append(field("优先级", priority), submit);
    taskForm.append(field("任务描述", description), submitRow,
      element("p", { class: "dialog-note" }, "批量续写生成待确认草稿，Agent 生成审计记录或建议。描述中的紧急程度可覆盖所选优先级。"), taskNote);
    taskForm.addEventListener("submit", (event) => {
      event.preventDefault();
      const text = description.value.trim();
      if (!text) {
        taskNote.textContent = "请输入任务描述。";
        description.focus();
        return;
      }
      void mutate("/api/tasks", "提交任务", { description: text, priority: priority.value });
    });
    const statusFilter = select([["", "全部状态"], ...Object.entries(statuses)]);
    statusFilter.addEventListener("change", () => void load());
    const taskFilters = element("div", { style: rowStyle });
    taskFilters.append(field("任务状态", statusFilter), button("刷新任务", () => void load()));
    tasks.controls.append(taskForm, taskFilters);

    const auditForm = element("form", { style: stackStyle });
    const chapterFilter = element("input", { list: "task-center-chapters", placeholder: "章节 ID" });
    const chapterOptions = element("datalist", { id: "task-center-chapters" });
    const auditFilters = [
      ["agent", "Agent", select([["", "全部 Agent"], ...Object.entries(agents)])],
      ["chapterId", "章节", chapterFilter],
      ["entryId", "人物 / 词条 ID", element("input", { placeholder: "例如 c_lin" })],
      ["severity", "严重程度", select([["", "全部严重度"], ...Object.entries(severities)])],
      ["chapterIndexMin", "起始章序", element("input", { type: "number", min: "1", max: "1000000" })],
      ["chapterIndexMax", "结束章序", element("input", { type: "number", min: "1", max: "1000000" })],
      ["limit", "最多报告数", element("input", { type: "number", min: "1", max: "500", value: "100", required: "" })],
    ];
    const auditGrid = element("div", { style: "display:grid;gap:8px;grid-template-columns:repeat(auto-fit,minmax(min(145px,100%),1fr));" });
    for (const [, label, control] of auditFilters) auditGrid.append(field(label, control));
    const auditButtons = element("div", { style: rowStyle });
    auditButtons.append(element("button", { type: "submit", class: "btn btn-primary" }, "筛选报告"),
      button("重置筛选", () => {
        auditForm.reset();
        void load();
      }));
    auditForm.append(auditGrid, chapterOptions, auditButtons);
    auditForm.addEventListener("submit", (event) => {
      event.preventDefault();
      void load();
    });
    audit.controls.append(auditForm);

    const costScope = select([["project", "当前作品"], ["global", "全部作品"]]);
    const days = select([["7", "最近 7 天"], ["30", "最近 30 天"], ["90", "最近 90 天"], ["", "全部时间"]]);
    const costFilters = element("div", { style: rowStyle });
    costFilters.append(field("统计范围", costScope), field("用量时间", days), button("刷新成本", () => void load()));
    for (const control of [costScope, days]) control.addEventListener("change", () => void load());
    cost.controls.append(costFilters,
      element("p", { class: "dialog-note" }, "时间范围仅用于调用和 token 用量。任务数按所有时间的当前状态统计。"));

    function taskCard(task) {
      const card = element("article", { style: cardStyle });
      const heading = element("strong", {}, task.description || named(kinds, task.kind));
      card.append(heading, element("p", { class: "field-hint" },
        `${named(kinds, task.kind)} · ${named(statuses, task.status)} · ${named(priorities, task.priority)}优先级 · 进度 ${count(task.progress)}/${count(task.total)}`));
      if (task.total > 0) {
        card.append(element("progress", { max: task.total, value: task.progress || 0, "aria-label": `${heading.textContent}的进度` }));
      }
      if (task.error) card.append(element("p", {}, `任务提示：${task.error}`));
      const details = element("details");
      details.append(element("summary", {}, "任务详情"),
        element("div", { class: "field-hint" }, `ID：${task.id} · 创建：${timeLabel(task.createdAt)} · 更新：${timeLabel(task.updatedAt)}`));
      const drafts = array(task.result?.draftIds);
      const reports = [...array(task.result?.reportIds), ...(task.result?.reportId ? [task.result.reportId] : [])];
      if (task.params?.agent) details.append(element("p", {}, `Agent：${named(agents, task.params.agent)}`));
      if (drafts.length) details.append(element("p", {}, `待确认草稿：${drafts.join("、")}（可到草稿箱查看）`));
      if (reports.length) details.append(element("p", {}, `审计报告：${reports.join("、")}`));
      if (task.result?.tokens != null) details.append(element("p", {}, `用量：${count(task.result.tokens)} tokens`));
      card.append(details);
      const available = {
        pending: ["pause", "cancel"], running: ["pause", "cancel"], paused: ["resume", "cancel"],
      }[task.status] || [];
      if (available.length) {
        const controls = element("div", { style: rowStyle });
        for (const action of available) {
          const actionButton = button(actions[action], async () => {
            if (!isCurrent() || writing || confirming) return;
            if (action === "cancel") {
              confirming = true;
              try {
                if (!await window.studioPages.confirm(`取消任务「${heading.textContent}」？已生成的草稿会保留。`)) return;
              } finally {
                confirming = false;
              }
            }
            void mutate(`/api/tasks/${encodeURIComponent(task.id)}/${action}`, `${actions[action]}任务`);
          });
          actionButton.setAttribute("aria-label", `${actions[action]}任务：${heading.textContent}`);
          controls.append(actionButton);
        }
        card.append(controls);
      }
      return card;
    }

    function auditCard(report) {
      const card = element("article", { style: cardStyle });
      const chapter = report.chapterIndex ? `第 ${report.chapterIndex} 章` : (report.chapterId || "未关联章节");
      card.append(element("strong", {}, report.agentName || named(agents, report.agent)),
        element("p", { class: "field-hint" }, `${named(severities, report.severity)} · ${chapter} · ${timeLabel(report.createdAt)}`),
        element("p", {}, report.summary || "无摘要"));
      const findings = array(report.findings);
      const details = element("details");
      details.append(element("summary", {}, `报告详情（${findings.length} 项发现）`),
        element("div", { class: "field-hint" }, `报告 ID：${report.id}${report.chapterId ? ` · 章节 ID：${report.chapterId}` : ""}`));
      if (findings.length) {
        const list = element("ul");
        for (const finding of findings) {
          const item = element("li", {}, `${named(severities, finding.severity)} · ${finding.detail || finding.type || "发现"}`);
          if (finding.entryId) item.append(element("span", { class: "field-hint" }, `（条目 ${finding.entryId}）`));
          if (finding.quote) item.append(element("blockquote", {}, finding.quote));
          list.append(item);
        }
        details.append(list);
      }
      card.append(details);
      return card;
    }

    function taskCounts(summary) {
      return element("p", { class: "field-hint" }, `任务 · ${Object.entries(statuses)
        .map(([key, label]) => `${label} ${count(summary.taskCounts?.[key])}`).join(" · ")}`);
    }

    function costTable(label, rows) {
      const table = element("table", { style: "width:100%;border-collapse:collapse;text-align:left;font-size:13px;" });
      const heading = element("thead");
      const headerRow = element("tr");
      for (const name of ["来源", "调用", "失败", "输入 tokens", "输出 tokens", "合计 tokens"]) {
        headerRow.append(element("th", { scope: "col", style: "padding:5px;" }, name));
      }
      heading.append(headerRow);
      const content = element("tbody");
      for (const [name, bucket] of rows) {
        const row = element("tr", { style: "border-top:1px solid var(--line);" });
        row.append(element("th", { scope: "row", style: "padding:5px;" }, name));
        for (const key of ["calls", "failed", "promptTokens", "completionTokens", "totalTokens"]) {
          row.append(element("td", { style: "padding:5px;" }, count(bucket?.[key])));
        }
        content.append(row);
      }
      table.append(element("caption", { style: "text-align:left;font-weight:600;padding:5px;" }, label), heading, content);
      return table;
    }

    function renderCost(summary, global) {
      const projectRows = (data) => array(data.buckets).map((bucket) => [named(sources, bucket.source), bucket]);
      const nodes = [costTable(global ? "全部作品用量" : "当前作品用量", [
        ["合计", summary.total], ...projectRows(summary),
      ]), taskCounts(summary)];
      if (global) {
        if (!array(summary.projects).length) nodes.push(element("p", { class: "empty-state" }, "没有可用的作品成本数据。"));
        for (const entry of array(summary.projects)) {
          if (!entry.summary) continue;
          const projectCost = element("details", { style: cardStyle });
          projectCost.append(element("summary", {}, `${entry.name || entry.projectId} · ${count(entry.summary.total?.totalTokens)} tokens`),
            costTable("作品用量", [["合计", entry.summary.total], ...projectRows(entry.summary)]), taskCounts(entry.summary));
          nodes.push(projectCost);
        }
      }
      return nodes;
    }

    async function load({ poll = false } = {}) {
      if (!isCurrent() || !projectId || (poll && (readController || writing || confirming))) return;
      if (currentView === "tasks" && writing) return;
      const view = currentView;
      const panel = panels[view];
      const inspectingTasks = () => panel.results.contains(document.activeElement) || panel.results.querySelector("details[open]");
      if (poll && inspectingTasks()) return;
      readController?.abort();
      readController = undefined;
      let path;
      const global = view === "cost" && costScope.value === "global";
      if (view === "tasks") {
        const query = new URLSearchParams({ limit: "50" });
        if (statusFilter.value) query.set("status", statusFilter.value);
        path = `/api/tasks?${query}`;
      } else if (view === "audit") {
        panel.results.replaceChildren();
        if (!auditForm.reportValidity()) {
          panel.status.textContent = "请检查筛选条件。";
          return;
        }
        const query = new URLSearchParams();
        for (const [key, , control] of auditFilters) {
          if (control.value.trim()) query.set(key, control.value.trim());
        }
        const min = Number(query.get("chapterIndexMin"));
        const max = Number(query.get("chapterIndexMax"));
        if (min && max && min > max) {
          panel.status.textContent = "起始章序不能大于结束章序。";
          return;
        }
        path = `/api/audit/reports?${query}`;
      } else {
        path = global ? "/api/cost/global" : "/api/audit/cost";
        if (days.value) path += `?days=${days.value}`;
      }
      const controller = new AbortController();
      readController = controller;
      const relevant = () => isCurrent() && readController === controller && !controller.signal.aborted;
      if (!poll) {
        if (view === "tasks") lastTaskSnapshot = "";
        panel.results.replaceChildren();
        panel.status.textContent = "正在加载…";
      }
      try {
        const data = await request(path, { signal: controller.signal }, !global);
        if (!relevant()) return;
        if (!data) throw new Error("接口未返回有效数据。");
        if (view === "tasks") {
          const items = array(data.tasks);
          const snapshot = JSON.stringify(items);
          submit.disabled = false;
          if (poll && inspectingTasks()) return;
          if (snapshot !== lastTaskSnapshot) {
            lastTaskSnapshot = snapshot;
            panel.results.replaceChildren(...(items.length ? items.map(taskCard) : [element("p", { class: "empty-state" }, "没有符合条件的任务。")]));
          }
        } else if (view === "audit") {
          const reports = array(data.reports);
          panel.results.replaceChildren(...(reports.length ? reports.map(auditCard) : [element("p", { class: "empty-state" }, "没有符合条件的审计报告。") ]));
        } else {
          panel.results.replaceChildren(...renderCost(data, global));
        }
        panel.status.textContent = "";
      } catch (error) {
        if (relevant()) {
          panel.status.textContent = friendlyError(error);
          if (view === "tasks" && error.status === 404) {
            submit.disabled = true;
          }
        }
      } finally {
        if (readController === controller) readController = undefined;
      }
    }

    async function mutate(path, label, payload) {
      if (!isCurrent() || !projectId || writing) return;
      writing = true;
      if (currentView === "tasks") readController?.abort();
      submit.disabled = true;
      description.readOnly = true;
      priority.disabled = true;
      statusFilter.disabled = true;
      const actionButtons = [...tasks.results.querySelectorAll("button")];
      if (actionButtons.includes(document.activeElement)) tasks.navButton.focus();
      for (const control of actionButtons) control.disabled = true;
      taskNote.textContent = `${label}中…`;
      try {
        await request(path, { method: "POST", ...(payload ? { body: JSON.stringify(payload) } : {}) });
        if (!isCurrent()) return;
        taskNote.textContent = `${label}成功。`;
        if (payload) {
          description.value = "";
          statusFilter.value = "";
        }
      } catch (error) {
        if (isCurrent()) taskNote.textContent = `${label}失败。${friendlyError(error)}`;
      } finally {
        if (isCurrent()) {
          writing = false;
          submit.disabled = false;
          description.readOnly = false;
          priority.disabled = false;
          statusFilter.disabled = false;
          for (const control of actionButtons) control.disabled = false;
          if (currentView === "tasks") void load();
        }
      }
    }

    async function resolveProject() {
      contextStatus.textContent = "";
      try {
        const data = await request("/api/projects", {}, false);
        if (!isCurrent()) return;
        if (!data?.currentId) throw new Error("接口未返回当前作品。");
        projectId = data.currentId;
        const info = array(data.projects).find((item) => item.id === projectId);
        scopeLabel.textContent = `当前作品：${info?.title || info?.name || projectId}`;
        for (const panel of Object.values(panels)) panel.navButton.disabled = false;
        activateView();
        // Chapter suggestions are optional. The audit API also accepts a typed ID.
        void request("/api/project").then((data) => {
          if (!isCurrent()) return;
          for (const chapter of array(data?.chapters)) {
            chapterOptions.append(element("option", { value: chapter.id }, chapter.title || chapter.id));
          }
        }).catch(() => {});
      } catch (error) {
        if (!isCurrent()) return;
        scopeLabel.textContent = "无法确认当前作品。";
        contextStatus.replaceChildren(element("span", {}, `${friendlyError(error)} `),
          button("重试", () => void resolveProject()));
      }
    }

    for (const panel of Object.values(panels)) panel.navButton.disabled = true;
    body.append(head, scopeLabel, contextStatus, navigation, tasks.node, audit.node, cost.node);
    page.append(body);
    function cleanup() {
      session.abort();
      readController?.abort();
      window.clearInterval(timer);
      page.remove();
    }
    document.body.append(page);
    if (!window.studioPages?.open("tasks", page, { onClose: cleanup })) {
      cleanup();
      return;
    }
    timer = window.setInterval(() => {
      if (isCurrent() && !document.hidden && currentView === "tasks") void load({ poll: true });
    }, 4000);
    window.addEventListener("pagehide", () => {
      cleanup();
      closePage();
    }, { signal: session.signal });
    void resolveProject();
  }

  function bind() {
    const trigger = document.getElementById("task-center-btn");
    if (!trigger) return;
    trigger.setAttribute("aria-controls", "taskCenterDialog");
    trigger.addEventListener("click", openCenter);
  }

  if (document.readyState === "loading") document.addEventListener("DOMContentLoaded", bind, { once: true });
  else bind();
})();
