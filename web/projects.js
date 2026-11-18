(() => {
  const COOKIE = "project_id";
  const state = { projects: [], currentId: "default", runs: [] };

  function setProjectCookie(id) {
    document.cookie = `${COOKIE}=${encodeURIComponent(id)}; path=/; max-age=31536000; SameSite=Lax`;
  }

  async function request(url, options = {}) {
    const response = await fetch(url, {
      ...options,
      headers: { "Content-Type": "application/json", ...(options.headers || {}) },
    });
    const text = await response.text();
    let data = null;
    if (text.trim()) {
      try {
        data = JSON.parse(text);
      } catch {
        data = null;
      }
    }
    if (!response.ok) {
      throw new Error((data && data.error) || `请求失败（HTTP ${response.status}）`);
    }
    return data;
  }

  function el(tag, attrs = {}, text = "") {
    const node = document.createElement(tag);
    for (const [key, value] of Object.entries(attrs)) {
      if (key === "style") node.style.cssText = value;
      else node.setAttribute(key, value);
    }
    if (text) node.textContent = text;
    return node;
  }

  function currentProject() {
    return state.projects.find((item) => item.id === state.currentId) || state.projects[0] || null;
  }

  function projectLabel(info) {
    return (info && (info.title || info.name)) || "未命名作品";
  }

  async function loadProjects() {
    const data = await request("/api/projects");
    state.projects = Array.isArray(data.projects) ? data.projects : [];
    state.currentId = data.currentId || data.defaultId || "default";
    renderProjectsButton();
  }

  function renderProjectsButton() {
    const button = document.getElementById("projectSwitchButton");
    if (!button) return;
    const info = currentProject();
    button.textContent = `作品：${projectLabel(info)}`;
    button.title = "切换或新建作品";
  }

  function mountButtons() {
    const actions = document.querySelector(".top-actions");
    if (!actions) return;
    if (!document.getElementById("projectSwitchButton")) {
      const button = el("button", {
        id: "projectSwitchButton",
        class: "btn btn-ghost",
        type: "button",
        style: "max-width:240px;overflow:hidden;text-overflow:ellipsis;white-space:nowrap;",
      }, "作品…");
      button.addEventListener("click", openProjectDialog);
      actions.insertBefore(button, actions.firstChild);
    }
    if (!document.getElementById("agentRunsButton")) {
      const button = el("button", {
        id: "agentRunsButton",
        class: "btn btn-ghost",
        type: "button",
        title: "查看后台 Agent 的只读观察记录",
      }, "Echo 观察");
      button.addEventListener("click", openAgentDialog);
      const anchor = document.getElementById("planButton");
      actions.insertBefore(button, anchor || null);
    }
  }

  async function openProjectDialog() {
    let dialog = document.getElementById("projectManagerDialog");
    if (!dialog) {
      dialog = el("dialog", { id: "projectManagerDialog", class: "dialog" });
      const form = el("div", { class: "dialog-form" });
      const head = el("div", { class: "dialog-head" });
      head.append(
        el("h2", {}, "作品管理"),
        (() => {
          const close = el("button", { class: "icon-button", type: "button", "aria-label": "关闭" }, "×");
          close.addEventListener("click", () => dialog.close());
          return close;
        })(),
      );
      const note = el("p", { class: "dialog-note" }, "每部作品拥有独立的章节、知识库、计划、草稿、记忆与 Agent 记录。切换作品后页面会重新载入。");
      const list = el("div", { id: "projectList", style: "display:flex;flex-direction:column;gap:10px;" });
      const createRow = el("div", { style: "display:flex;gap:8px;align-items:center;padding-top:12px;border-top:1px solid var(--line);" });
      const input = el("input", {
        id: "newProjectName",
        type: "text",
        maxlength: "120",
        placeholder: "新作品名称",
        style: "flex:1;min-width:0;padding:9px 11px;border:1px solid var(--line-strong);border-radius:2px;background:var(--paper);color:var(--ink);",
      });
      const create = el("button", { class: "btn btn-primary", type: "button" }, "新建作品");
      create.addEventListener("click", createProject);
      input.addEventListener("keydown", (event) => {
        if (event.key === "Enter") createProject();
      });
      createRow.append(input, create);
      form.append(head, note, list, createRow);
      dialog.append(form);
      document.body.append(dialog);
    }
    try {
      await loadProjects();
    } catch {
      // 列表加载失败时保留上次数据，错误会由按钮状态体现。
    }
    renderProjectList();
    if (!dialog.open) dialog.showModal();
  }

  function renderProjectList() {
    const list = document.getElementById("projectList");
    if (!list) return;
    list.textContent = "";
    for (const info of state.projects) {
      const row = el("div", {
        style: "display:flex;align-items:center;gap:10px;padding:10px 12px;border:1px solid var(--line);background:var(--paper);",
      });
      const copy = el("div", { style: "flex:1;min-width:0;" });
      const title = el("div", { style: "font-family:var(--serif);font-size:15px;overflow:hidden;text-overflow:ellipsis;white-space:nowrap;" }, projectLabel(info));
      const meta = el("div", { style: "margin-top:3px;color:var(--muted);font-size:11px;" },
        `${info.chapterCount || 0} 章 · ${info.id === "default" ? "默认作品" : info.id}`);
      copy.append(title, meta);
      row.append(copy);

      if (info.id !== state.currentId) {
        const use = el("button", { class: "btn btn-quiet", type: "button" }, "切换");
        use.addEventListener("click", () => switchProject(info.id));
        row.append(use);
      } else {
        row.append(el("span", { style: "color:var(--accent-dark);font-size:11px;" }, "当前"));
      }

      const rename = el("button", { class: "btn btn-quiet", type: "button" }, "重命名");
      rename.addEventListener("click", () => renameProject(info));
      row.append(rename);

      if (info.id !== "default") {
        const remove = el("button", { class: "btn btn-quiet btn-delete", type: "button" }, "删除");
        remove.addEventListener("click", () => deleteProject(info));
        row.append(remove);
      }
      list.append(row);
    }
  }

  function switchProject(id) {
    setProjectCookie(id);
    location.reload();
  }

  async function createProject() {
    const input = document.getElementById("newProjectName");
    const name = (input && input.value || "").trim();
    if (!name) return;
    try {
      const info = await request("/api/projects", {
        method: "POST",
        body: JSON.stringify({ name }),
      });
      setProjectCookie(info.id);
      location.reload();
    } catch (error) {
      alert(error.message);
    }
  }

  async function renameProject(info) {
    const name = prompt("新的作品名称", projectLabel(info));
    if (name === null) return;
    const trimmed = name.trim();
    if (!trimmed) return;
    try {
      await request(`/api/projects/${encodeURIComponent(info.id)}`, {
        method: "PATCH",
        body: JSON.stringify({ name: trimmed }),
      });
      if (info.id === state.currentId) {
        location.reload();
        return;
      }
      await loadProjects();
      renderProjectList();
    } catch (error) {
      alert(error.message);
    }
  }

  async function deleteProject(info) {
    if (!confirm(`删除「${projectLabel(info)}」将永久移除该作品的全部章节、知识库、记忆和草稿。确定继续？`)) return;
    try {
      await request(`/api/projects/${encodeURIComponent(info.id)}`, { method: "DELETE" });
      if (info.id === state.currentId) {
        setProjectCookie("default");
        location.reload();
        return;
      }
      await loadProjects();
      renderProjectList();
    } catch (error) {
      alert(error.message);
    }
  }

  async function openAgentDialog() {
    let dialog = document.getElementById("agentRunsDialog");
    if (!dialog) {
      dialog = el("dialog", { id: "agentRunsDialog", class: "dialog" });
      const form = el("div", { class: "dialog-form" });
      const head = el("div", { class: "dialog-head" });
      head.append(
        el("h2", {}, "Echo 观察记录"),
        (() => {
          const close = el("button", { class: "icon-button", type: "button", "aria-label": "关闭" }, "×");
          close.addEventListener("click", () => dialog.close());
          return close;
        })(),
      );
      form.append(
        head,
        el("p", { class: "dialog-note" }, "Echo 是 M0 的只读贯穿线 Agent：章节保存/新建/删除/导入后记录一次观察，不修改正文、知识库或记忆。"),
        el("div", { id: "agentRunsList", style: "display:flex;flex-direction:column;gap:9px;" }),
      );
      dialog.append(form);
      document.body.append(dialog);
    }
    await refreshAgentRuns();
    if (!dialog.open) dialog.showModal();
  }

  async function refreshAgentRuns() {
    try {
      const data = await request("/api/agents/runs?limit=20");
      state.runs = Array.isArray(data.runs) ? data.runs : [];
      renderAgentRuns();
    } catch (error) {
      const list = document.getElementById("agentRunsList");
      if (list) list.textContent = error.message;
    }
  }

  function renderAgentRuns() {
    const list = document.getElementById("agentRunsList");
    const button = document.getElementById("agentRunsButton");
    if (button) button.textContent = state.runs.length ? `Echo 观察 · ${state.runs.length}` : "Echo 观察";
    if (!list) return;
    list.textContent = "";
    if (!state.runs.length) {
      list.append(el("p", { class: "empty-state" }, "还没有观察记录。保存一次章节后会出现。"));
      return;
    }
    for (const run of state.runs) {
      const card = el("div", { style: "padding:10px 12px;border:1px solid var(--line);background:var(--paper);" });
      const top = el("div", { style: "display:flex;align-items:center;gap:8px;font-size:11px;color:var(--muted);" });
      top.append(
        el("span", {
          style: `padding:1px 6px;border-radius:2px;background:${run.status === "done" ? "var(--surface-muted)" : "#f3dede"};color:${run.status === "done" ? "var(--muted)" : "var(--accent-dark)"};`,
        }, run.status === "done" ? "完成" : "失败"),
        el("span", {}, run.agent || "echo"),
        el("span", {}, run.eventType || ""),
        el("span", {}, run.startedAt ? new Date(run.startedAt).toLocaleTimeString("zh-CN") : ""),
        el("span", {}, `${run.durationMs || 0} ms`),
        el("span", {}, `${run.tokens || 0} tokens`),
      );
      const summary = el("div", { style: "margin-top:6px;font-size:13px;line-height:1.7;" }, run.summary || run.error || "无输出");
      card.append(top, summary);
      if (Array.isArray(run.details) && run.details.length) {
        card.append(el("div", { style: "margin-top:4px;color:var(--muted);font-size:11.5px;line-height:1.7;" }, run.details.join(" · ")));
      }
      list.append(card);
    }
  }

  async function bootstrap() {
    mountButtons();
    try {
      await loadProjects();
      const data = await request("/api/agents/runs?limit=20");
      state.runs = Array.isArray(data.runs) ? data.runs : [];
      renderAgentRuns();
    } catch (error) {
      const button = document.getElementById("projectSwitchButton");
      if (button) button.textContent = "作品：加载失败";
    }
    window.setInterval(async () => {
      if (document.hidden) return;
      try {
        const data = await request("/api/agents/runs?limit=20");
        state.runs = Array.isArray(data.runs) ? data.runs : [];
        renderAgentRuns();
      } catch {
        // 页面其他请求会显示更具体的错误；这里保持静默轮询。
      }
    }, 5000);
  }

  if (document.readyState === "loading") {
    document.addEventListener("DOMContentLoaded", bootstrap);
  } else {
    bootstrap();
  }
})();
