(() => {
  "use strict";

  const kinds = new Map([
    ["character", "角色"], ["setting", "设定"], ["faction", "势力"], ["location", "地点"], ["item", "物品"],
  ]);
  const extendedKinds = new Map([...kinds].filter(([kind]) => kind !== "character" && kind !== "setting"));
  const characterFields = new Map([
    ["role", "身份"], ["personality", "性格"], ["appearance", "外貌"],
    ["relationships", "人物关系描述"], ["state", "当前状态"], ["notes", "备注"],
  ]);
  const settingFields = new Map([["category", "分类"], ["content", "内容"]]);
  const relationStatuses = new Map([["active", "有效"], ["invalidated", "已失效"]]);
  const sections = new Map([["atlas", "3D 图谱"], ["entries", "词条"], ["relations", "关系"], ["timeline", "时间线"]]);
  const list = (value) => Array.isArray(value) ? value : [];
  const endpoint = (section, id) => `/api/world/${section}${id ? `/${encodeURIComponent(id)}` : ""}`;

  function element(tag, text = "", className = "") {
    const node = document.createElement(tag);
    if (className) node.className = className;
    if (text) node.textContent = text;
    return node;
  }

  function button(text, handler, className = "btn btn-quiet btn-sm") {
    const node = element("button", text, className);
    node.type = "button";
    if (handler) node.addEventListener("click", handler);
    return node;
  }

  function optionControl(options, value) {
    const control = element("select", "", "direction");
    for (const [key, label] of options) control.add(new Option(label, key));
    control.value = value;
    return control;
  }

  function field(parent, label, control, value = "") {
    const wrapper = element("label", "", "field");
    wrapper.append(element("span", label), control);
    control.value = value ?? "";
    parent.append(wrapper);
    return control;
  }

  function textField(parent, label, value, { required = false, rows = 0 } = {}) {
    const control = element(rows ? "textarea" : "input");
    if (rows) control.rows = rows;
    control.required = required;
    return field(parent, label, control, value);
  }

  function numberField(parent, label, value) {
    const control = element("input");
    control.type = "number";
    control.min = "0";
    control.step = "1";
    control.max = String(Number.MAX_SAFE_INTEGER);
    return field(parent, label, control, value || 0);
  }

  function selectField(parent, label, options, value, required = false) {
    const control = optionControl(options, value);
    control.required = required;
    return field(parent, label, control, value);
  }

  function actionRow(parent) {
    const row = element("div", "", "review-actions");
    parent.append(row);
    return row;
  }

  function note(parent, text) {
    const node = element("p", text, "review-note");
    node.style.overflowWrap = "anywhere";
    node.style.whiteSpace = "pre-wrap";
    parent.append(node);
    return node;
  }

  function records(parent, items, empty, render) {
    if (!items.length) {
      note(parent, empty);
      return;
    }
    const rows = element("ul", "", "changeset-list");
    rows.style.listStyle = "none";
    rows.style.margin = "0";
    rows.style.padding = "0";
    for (const item of items) {
      const row = element("li", "", "review-section");
      row.style.overflowWrap = "anywhere";
      render(row, item);
      rows.append(row);
    }
    parent.append(rows);
  }

  function mount() {
    const launcher = document.getElementById("world-center-btn");
    if (!launcher || document.getElementById("world-center-dialog")) return;

    const page = element("section", "", "dialog dialog-wide studio-page");
    page.id = "world-center-dialog";
    page.hidden = true;
    page.setAttribute("aria-labelledby", "world-center-title");
    launcher.setAttribute("aria-controls", page.id);
    const layout = element("div", "", "dialog-form world-center-layout");
    const header = element("header", "", "dialog-head");
    const titleBlock = element("div");
    const title = element("h2", "大世界");
    title.id = "world-center-title";
    titleBlock.append(title);
    const projectLine = note(titleBlock, "");
    const closeButton = button("返回编辑器", () => window.studioPages.close(page));
    header.append(titleBlock, closeButton);
    const navigation = element("nav");
    navigation.setAttribute("aria-label", "大世界分类");
    navigation.className = "review-actions";
    const status = element("div");
    status.setAttribute("role", "status");
    status.setAttribute("aria-atomic", "true");
    const content = element("section", "", "world-center-content");
    content.setAttribute("aria-labelledby", "world-center-view-title");
    layout.append(header, navigation, status, content);
    page.append(layout);
    document.body.append(page);

    let session;
    const navButtons = new Map();
    for (const [section, label] of sections) {
      if (section === "atlas" && !window.worldAtlas) continue;
      const navButton = button(label, () => navigate(section));
      navButtons.set(section, navButton);
      navigation.append(navButton);
    }

    function setStatus(message = "", error = false) {
      status.className = message ? (error ? "dialog-note" : "review-note") : "";
      status.textContent = message;
    }

    async function request(path, method = "GET", body) {
      const { controller: { signal }, projectId } = session;
      const result = await api(path, {
        method,
        cache: "no-store",
        signal,
        headers: projectId ? { "X-Project-ID": projectId } : {},
        ...(body === undefined ? {} : { body: JSON.stringify(body) }),
      });
      signal.throwIfAborted();
      return result;
    }

    async function run(message, work, { writing = false } = {}) {
      const current = session;
      if (!current || current.busy || current.confirming) return;
      const previousFocus = document.activeElement;
      current.busy = true;
      current.writing = writing;
      content.inert = true;
      content.setAttribute("aria-busy", "true");
      setStatus(message);
      try {
        await work();
        if (session === current) setStatus(current.feedback);
      } catch (error) {
        if (session === current) setStatus(error.message || "操作失败", true);
      } finally {
        if (session === current) {
          current.busy = false;
          current.writing = false;
          current.feedback = "";
          content.inert = false;
          content.removeAttribute("aria-busy");
          const target = current.focusTarget || previousFocus;
          current.focusTarget = null;
          if (target?.isConnected && page.contains(target)) target.focus();
        }
      }
    }

    async function confirmAction(message) {
      const current = session;
      if (!current || current.busy || current.confirming) return false;
      current.confirming = true;
      try {
        const accepted = await window.studioPages.confirm(message);
        return accepted && session === current && !current.busy;
      } finally {
        current.confirming = false;
      }
    }

    function canLeave() {
      if (session?.confirming || session?.writing) return false;
      return session?.dirty ? confirmAction("放弃当前未保存的修改？") : true;
    }

    function onClose() {
      session?.controller.abort();
      session?.atlas?.destroy();
      session = null;
      content.inert = false;
      content.removeAttribute("aria-busy");
      content.replaceChildren();
      setStatus();
    }

    launcher.addEventListener("click", () => {
      if (!page.hidden || !window.studioPages) return;
      if (!window.studioPages.open("world", page, { onClose, canLeave })) return;
      session = {
        controller: new AbortController(), section: window.worldAtlas ? "atlas" : "entries",
        kind: "", query: "", relationStatus: "", atlasView: {},
        entries: [], chapters: [], ready: false, busy: false, dirty: false, feedback: "", focusTarget: null,
      };
      projectLine.textContent = "";
      navigation.hidden = true;
      content.replaceChildren();
      void initialize();
    });

    async function initialize() {
      const current = session;
      await run("正在读取当前作品…", async () => {
        const projects = await request("/api/projects");
        if (!projects?.currentId) throw new Error("无法确定当前作品");
        // Pin all world reads and writes to the project resolved by the router on open.
        session.projectId = projects.currentId;
        const features = await request("/api/features");
        if (!features?.m2) {
          setView("大世界未启用");
          note(content, "当前服务未启用 M2 大世界功能。");
          return;
        }
        const [project, entries] = await Promise.all([request("/api/project"), request(endpoint("entries"))]);
        session.chapters = list(project.chapters);
        session.entries = list(entries);
        projectLine.textContent = `作品：${project.title || projects.projects?.find((item) => item.id === session.projectId)?.name || session.projectId}`;
        session.ready = true;
        navigation.hidden = false;
        if (session.section === "atlas") await loadAtlas(session.entries);
        else renderEntries(session.entries);
      });
      if (session && session === current && !session.ready && !content.childElementCount) {
        setView("无法打开大世界");
        content.append(button("重试", initialize));
        content.querySelector("button").focus();
      }
    }

    function setView(heading) {
      session.atlas?.destroy();
      session.atlas = null;
      session.dirty = false;
      for (const [name, navButton] of navButtons) {
        const selected = name === session.section;
        navButton.setAttribute("aria-pressed", String(selected));
        navButton.classList.toggle("btn-primary", selected);
        navButton.classList.toggle("btn-quiet", !selected);
      }
      const viewTitle = element("h3", heading);
      viewTitle.id = "world-center-view-title";
      viewTitle.tabIndex = -1;
      viewTitle.style.margin = "0";
      viewTitle.style.overflowWrap = "anywhere";
      content.replaceChildren(viewTitle);
      content.style.display = "grid";
      content.style.gap = "12px";
      session.focusTarget = viewTitle;
      if (!session.busy) {
        viewTitle.focus();
        session.focusTarget = null;
      }
    }

    function entryListPath() {
      const query = new URLSearchParams();
      if (session.kind) query.set("kind", session.kind);
      if (session.query.trim()) query.set("q", session.query.trim());
      return endpoint("entries") + (query.size ? `?${query}` : "");
    }

    async function loadAtlas(entries) {
      const current = session;
      const relations = await request(endpoint("relations")).catch((error) => {
        current.feedback = `关系读取失败：${error.message}`;
        return null;
      });
      current.controller.signal.throwIfAborted();
      session.entries = entries;
      session.section = "atlas";
      setView("空间关系图谱");
      session.atlas = window.worldAtlas.mount(content, {
        entries, relations, kinds, relationStatuses, view: session.atlasView,
        openEntry, openRelation: (relation) => {
          session.section = "relations";
          renderRelationForm(relation, "atlas");
        },
      });
    }

    async function loadSection(section) {
      if (section === "atlas") {
        await loadAtlas(list(await request(endpoint("entries"))));
      } else if (section === "entries") {
        const entries = list(await request(entryListPath()));
        if (!session.kind && !session.query.trim()) session.entries = entries;
        session.section = section;
        renderEntries(entries);
      } else if (section === "relations") {
        const query = session.relationStatus ? `?status=${encodeURIComponent(session.relationStatus)}` : "";
        const relations = list(await request(endpoint("relations") + query));
        session.section = section;
        renderRelations(relations);
      } else {
        const timeline = list(await request(endpoint("timeline")));
        session.section = section;
        renderTimeline(timeline);
      }
    }

    function navigate(section) {
      const current = session;
      if (!current || current.busy || current.confirming) return;
      const load = () => run("正在加载…", () => loadSection(section));
      if (!current.dirty) return load();
      return confirmAction("放弃当前未保存的修改？").then((accepted) => {
        if (accepted) return load();
      });
    }

    function openChapter(chapterId) {
      if (!chapterId || session?.busy || session?.confirming) return;
      return window.studioPages.showEditor(chapterId);
    }

    function appendSourceRefs(parent, refs) {
      for (const ref of list(refs)) {
        const chapter = session.chapters.find((item) => item.id === ref.chapterId);
        if (!chapter) continue;
        const row = actionRow(parent);
        row.append(button(`打开第 ${chapter.index} 章 · ${chapter.title}`, () => openChapter(chapter.id)));
        if (ref.quote) note(parent, ref.quote);
      }
    }

    function renderEntries(entries) {
      setView("世界词条");
      const filters = element("form", "", "review-actions");
      const kind = selectField(filters, "词条类型", [["", "全部类型"], ...kinds], session.kind);
      const query = textField(filters, "检索词条", session.query);
      query.type = "search";
      query.placeholder = "名称、别名、摘要或字段";
      const search = button("筛选");
      search.type = "submit";
      filters.append(search);
      filters.addEventListener("submit", (event) => {
        event.preventDefault();
        if (session.busy) return;
        session.kind = kind.value;
        session.query = query.value;
        navigate("entries");
      });
      kind.addEventListener("change", () => filters.requestSubmit());
      content.append(filters);
      const actions = actionRow(content);
      actions.append(
        button("新建角色", () => renderEntryForm(null, [], "character")),
        button("新建设定", () => renderEntryForm(null, [], "setting")),
        button("新建扩展词条", () => renderEntryForm()),
      );
      records(content, entries, "没有符合条件的词条。", (row, entry) => {
        const open = button(`${entry.name} · ${kinds.get(entry.kind) || entry.kind}`, () => openEntry(entry.id), "btn btn-ghost");
        open.style.whiteSpace = "normal";
        open.style.maxWidth = "100%";
        row.append(open);
        if (entry.summary) note(row, entry.summary);
        if (list(entry.lockedFields).length) note(row, `已锁定 ${entry.lockedFields.length} 个字段`);
      });
    }

    function openEntry(id) {
      void run("正在读取词条…", async () => {
        const detail = await request(endpoint("entries", id));
        const returnSection = session.section;
        session.section = "entries";
        renderEntryForm(detail.entry, list(detail.relations), undefined, returnSection);
      });
    }

    function editor(section, item, heading, savePayload, removeMessage, returnSection = section) {
      setView(heading);
      actionRow(content).append(button(`返回${sections.get(returnSection)}`, () => navigate(returnSection)));
      const form = element("form");
      form.style.display = "grid";
      form.style.gap = "12px";
      form.addEventListener("submit", (event) => event.preventDefault());
      const track = () => { session.dirty = true; };
      if (savePayload) {
        form.addEventListener("input", track);
        form.addEventListener("change", track);
      }
      content.append(form);
      return {
        form,
        finish() {
          if (!savePayload && !removeMessage) return;
          const actions = element("div", "", "dialog-actions");
          form.append(actions);
          if (removeMessage) {
            actions.append(button("删除", async () => {
              if (await confirmAction(removeMessage)) {
                return run("正在删除…", async () => {
                  await request(endpoint(section, item.id), "DELETE");
                  await afterWrite(section, returnSection, "已删除", null, item.id);
                }, { writing: true });
              }
            }, "btn btn-danger"));
          }
          if (savePayload) {
            const save = button("保存", null, "btn btn-primary");
            save.type = "submit";
            actions.append(save);
            form.addEventListener("submit", (event) => {
              event.preventDefault();
              void run("正在保存…", async () => {
                const payload = savePayload();
                if (payload === null) {
                  session.dirty = false;
                  session.feedback = "没有需要保存的修改";
                  return;
                }
                const saved = await request(endpoint(section, item?.id), item ? "PUT" : "POST", payload);
                await afterWrite(section, returnSection, "已保存", saved);
              }, { writing: true });
            });
          }
        },
      };
    }

    async function afterWrite(section, returnSection, message, saved, removedId) {
      session.dirty = false;
      if (section === "entries") {
        session.entries = session.entries.filter((entry) => entry.id !== (saved?.id || removedId));
        if (saved) session.entries.push(saved);
      }
      // Leave the submitted form immediately so a failed refresh cannot repeat the write.
      setView(message);
      actionRow(content).append(button(`返回${sections.get(returnSection)}`, () => navigate(returnSection)));
      try {
        await loadSection(returnSection);
        session.feedback = [message, session.feedback].filter(Boolean).join("。");
      } catch (error) {
        throw new Error(`${message}，但列表刷新失败：${error.message}`);
      }
    }

    function readRequired(control, message) {
      if (!control.value.trim()) throw new Error(message);
      return control.value;
    }

    function renderEntryForm(entry, relations = [], createKind = extendedKinds.has(session.kind) ? session.kind : "faction", returnSection = "entries") {
      const entryKind = entry?.kind || createKind;
      const isSetting = entryKind === "setting";
      const isExtended = extendedKinds.has(entryKind);
      const currentFields = entry?.fields || {};
      const customFields = [];
      const controls = new Map();
      let kind;
      let name;
      let summary;
      let aliases;
      const readPatch = () => {
        // Send only changed fields. The endpoint merges them over the stored entry.
        const fields = Object.create(null);
        const existingKeys = new Set(Object.keys(currentFields));
        const newKeys = new Set();
        for (const [key, control] of controls) {
          if (control.value !== control.defaultValue) fields[key] = control.value;
        }
        for (const row of customFields) {
          const key = row.key.value.trim();
          if (!key) throw new Error("请填写自定义字段名或移除该行");
          if (Array.from(key).length > 40) throw new Error("自定义字段名不能超过 40 个字符");
          if (existingKeys.has(key) || newKeys.has(key)) throw new Error(`字段名重复：${key}`);
          newKeys.add(key);
          fields[key] = row.value.value;
        }
        const title = readRequired(name, "请填写词条名称");
        if (!entry) {
          return {
            kind: kind?.value || entryKind, name: title, fields, ...(summary ? { summary: summary.value } : {}),
            aliases: aliases.value.split(/\r?\n/u).map((alias) => alias.trim()).filter(Boolean),
          };
        }
        const patch = { fields };
        if (title !== name.defaultValue) patch.name = title;
        return patch.name || Object.keys(fields).length ? patch : null;
      };
      const heading = entry ? entry.name : isExtended ? "新建扩展词条" : `新建${kinds.get(entryKind)}`;
      const edit = editor("entries", entry, heading, readPatch,
        entry ? `删除「${entry.name}」及其关系与字段锁？` : "", returnSection);
      if (entry) {
        note(edit.form, `${kinds.get(entry.kind) || entry.kind} · ${entry.id}`);
      } else if (isExtended) {
        kind = selectField(edit.form, "类型", extendedKinds, entryKind);
      }
      name = textField(edit.form, "名称", entry?.name, { required: true });
      name.defaultValue = name.value;
      if (!entry) {
        if (isExtended) summary = textField(edit.form, "摘要", "", { rows: 3 });
        aliases = textField(edit.form, "别名（每行一个）", "", { rows: 2 });
      } else {
        if (list(entry.aliases).length) note(edit.form, `别名：${entry.aliases.join("、")}`);
        if (isExtended && entry.summary) note(edit.form, `摘要：${entry.summary}`);
        if (entry.lastSeen) note(edit.form, `最近出现：第 ${entry.lastSeen} 章`);
      }
      const labels = isExtended ? new Map(Object.keys(currentFields).map((key) => [key, key]))
        : isSetting ? settingFields : characterFields;
      const lockButtons = new Map();
      let locks = new Set(list(entry?.lockedFields));
      const refreshLocks = () => {
        for (const [key, lock] of lockButtons) {
          const locked = locks.has(key);
          lock.button.textContent = locked ? "解锁" : "加锁";
          lock.button.setAttribute("aria-label", `${locked ? "解锁" : "锁定"}字段：${lock.label}`);
          lock.status.textContent = locked ? "已锁定" : "未锁定";
        }
      };
      const addLock = (parent, key, label) => {
        const actions = actionRow(parent);
        const lockStatus = element("span", "", "review-note");
        const toggle = button("", () => {
          void run("正在更新字段锁…", async () => {
            const result = await request(`${endpoint("entries", entry.id)}/locks`, "POST", {
              fields: [key], lock: !locks.has(key),
            });
            locks = new Set(list(result.lockedFields));
            const cached = session.entries.find((item) => item.id === entry.id);
            if (cached) cached.lockedFields = [...locks];
            refreshLocks();
            session.feedback = locks.has(key) ? `已锁定「${label}」` : `已解锁「${label}」`;
          }, { writing: true });
        });
        lockButtons.set(key, { button: toggle, status: lockStatus, label });
        actions.append(lockStatus, toggle);
      };
      for (const [key, label] of labels) {
        const group = element("div");
        const control = textField(group, label, currentFields[key] ?? "", { rows: key === "role" || key === "category" ? 0 : 2 });
        control.defaultValue = control.value;
        controls.set(key, control);
        if (entry) addLock(group, key, label);
        edit.form.append(group);
      }
      if (entry) {
        const otherLocks = [...locks].filter((key) => !lockButtons.has(key));
        if (otherLocks.length) {
          const extra = element("fieldset", "", "review-section");
          extra.append(element("legend", "其他已锁定字段"));
          for (const key of otherLocks) addLock(extra, key, key);
          edit.form.append(extra);
        }
        if (lockButtons.size) note(edit.form, "字段锁只影响后续分析，已锁定的字段仍可人工编辑。");
        refreshLocks();
      }
      if (isExtended) {
        const addField = button("添加自定义字段", () => {
          const group = element("fieldset", "", "review-section");
          group.style.minWidth = "0";
          group.append(element("legend", "自定义字段"));
          const row = { key: textField(group, "字段名", "", { required: true }), value: textField(group, "字段值", "", { rows: 2 }) };
          customFields.push(row);
          actionRow(group).append(button("移除此行", () => {
            customFields.splice(customFields.indexOf(row), 1);
            group.remove();
            session.dirty = true;
            addField.focus();
          }));
          edit.form.insertBefore(group, addField);
          session.dirty = true;
          row.key.focus();
        });
        edit.form.append(addField);
      }
      edit.finish();
      if (entry) renderEntryContext(entry, relations);
    }

    function renderEntryContext(entry, relations) {
      const evidence = element("details", "", "review-section");
      evidence.append(element("summary", "证据与来源"));
      for (const quote of list(entry.evidence)) note(evidence, quote);
      appendSourceRefs(evidence, entry.sourceRefs);
      const resolved = element("div");
      const loadEvidence = button("定位证据章节", () => {
        void run("正在定位证据…", async () => {
          const items = list(await request(`${endpoint("entries", entry.id)}/evidence`));
          resolved.replaceChildren();
          records(resolved, items, "未找到可定位的章节引文。", (row, item) => {
            note(row, `第 ${item.chapterIndex} 章 · ${item.chapterTitle}（字符位置 ${item.startOffset}–${item.endOffset}）`);
            note(row, item.quote);
            if (item.chapterId) row.append(button("在编辑器打开此章", () => openChapter(item.chapterId)));
          });
        });
      });
      evidence.append(loadEvidence, resolved);
      content.append(evidence);
      const links = element("details", "", "review-section");
      links.append(element("summary", `关联关系（${relations.length}）`));
      records(links, relations, "此词条暂无关联关系。", (row, relation) => {
        note(row, relationTitle(relation));
        note(row, relationStatuses.get(relation.status) || relation.status);
        appendSourceRefs(row, relation.sourceRefs);
      });
      content.append(links);
    }

    function entryName(id) {
      return session.entries.find((item) => item.id === id)?.name || id;
    }

    function entryOptions() {
      return session.entries.map((item) => [item.id, `${item.name} · ${kinds.get(item.kind) || item.kind}（${item.id}）`]);
    }

    function chapterOptions() {
      return session.chapters.map((chapter) => [chapter.id, `第 ${chapter.index} 章 · ${chapter.title}`]);
    }

    function withSavedOption(options, id) {
      return id && !options.some(([key]) => key === id) ? [...options, [id, `不可用（${id}）`]] : options;
    }

    function relationTitle(relation) {
      return `${entryName(relation.fromEntryId)} → ${entryName(relation.toEntryId)} · ${relation.label || relation.type}`;
    }

    function renderRelations(relations) {
      setView("词条关系");
      const actions = actionRow(content);
      const filter = selectField(actions, "关系状态", [["", "全部状态"], ...relationStatuses], session.relationStatus);
      filter.addEventListener("change", () => {
        session.relationStatus = filter.value;
        navigate("relations");
      });
      const create = button("新建关系", () => renderRelationForm());
      create.disabled = session.entries.length < 2;
      actions.append(create);
      if (create.disabled) note(content, "新建关系需要至少两个词条。");
      records(content, relations, "没有符合条件的关系。", (row, relation) => {
        row.append(button("编辑关系", () => renderRelationForm(relation)));
        note(row, relationTitle(relation));
        note(row, `${relationStatuses.get(relation.status) || relation.status} · ${relation.type}`);
        if (relation.startChapterIndex || relation.endChapterIndex) {
          note(row, `起止章序：${relation.startChapterIndex || "不限"} – ${relation.endChapterIndex || "不限"}`);
        }
        if (relation.description) note(row, relation.description);
        appendSourceRefs(row, relation.sourceRefs);
      });
    }

    function renderRelationForm(relation, returnSection = "relations") {
      const edit = editor("relations", relation, relation ? "编辑关系" : "新建关系", () => {
        if (from.value === to.value) throw new Error("关系的起点与终点必须不同");
        if (Number(end.value) > 0 && Number(end.value) < Number(start.value)) {
          throw new Error("结束章序不能早于开始章序");
        }
        return {
          fromEntryId: from.value, toEntryId: to.value, type: readRequired(type, "请填写关系类型"),
          label: label.value, description: description.value, status: state.value,
          startChapterIndex: Number(start.value), endChapterIndex: Number(end.value),
          ...(relation?.sourceRefs ? { sourceRefs: relation.sourceRefs } : {}),
        };
      }, relation ? `删除「${relationTitle(relation)}」？` : "", returnSection);
      const options = [["", "请选择词条"], ...entryOptions()];
      const fromId = relation?.fromEntryId || "";
      const toId = relation?.toEntryId || "";
      const from = selectField(edit.form, "起点词条", withSavedOption(options, fromId), fromId, true);
      const to = selectField(edit.form, "终点词条", withSavedOption(options, toId), toId, true);
      const type = textField(edit.form, "关系类型", relation?.type, { required: true });
      const label = textField(edit.form, "显示名称", relation?.label);
      const description = textField(edit.form, "说明", relation?.description, { rows: 3 });
      const state = selectField(edit.form, "状态", relationStatuses, relation?.status || "active");
      const start = numberField(edit.form, "起始章序（0 为不限）", relation?.startChapterIndex);
      const end = numberField(edit.form, "结束章序（0 为不限）", relation?.endChapterIndex);
      if (list(relation?.sourceRefs).length) {
        note(edit.form, `保存时保留原有 ${relation.sourceRefs.length} 条来源引用。`);
        appendSourceRefs(edit.form, relation.sourceRefs);
      }
      edit.finish();
    }

    function renderTimeline(events) {
      setView("时间线");
      actionRow(content).append(button("新建人工事件", () => renderTimelineForm()));
      records(content, events, "暂无时间线事件。", (row, event) => {
        note(row, event.title);
        note(row, `${event.source === "manual" ? "人工事件" : "记忆事件 · 只读"} · ${event.chapterIndex ? `第 ${event.chapterIndex} 章` : "无章节"} · 故事顺序 ${event.storyOrder}`);
        if (event.eventType) note(row, `类型：${event.eventType}`);
        if (event.summary && event.summary !== event.title) note(row, event.summary);
        if (list(event.participants).length) note(row, `参与者：${event.participants.map(entryName).join("、")}`);
        if (list(event.entities).length) note(row, `关联词条：${event.entities.map(entryName).join("、")}`);
        if (event.chapterId) row.append(button("在编辑器打开此章", () => openChapter(event.chapterId)));
        if (event.source === "manual") row.append(button("编辑事件", () => renderTimelineForm(event)));
      });
    }

    function entryPicker(parent, label, savedIds) {
      const group = element("fieldset", "", "review-section");
      group.style.minWidth = "0";
      group.append(element("legend", label));
      const choices = [["", "请选择词条"], ...entryOptions()];
      const rows = [];
      const add = button(`添加${label}`, () => appendRow("").focus());
      group.append(add);
      parent.append(group);
      function appendRow(id) {
        const row = element("div", "", "review-actions");
        const select = selectField(row, label, withSavedOption(choices, id), id, true);
        const item = { select };
        rows.push(item);
        row.append(button("移除", () => {
          rows.splice(rows.indexOf(item), 1);
          row.remove();
          session.dirty = true;
          add.focus();
        }));
        group.insertBefore(row, add);
        return select;
      }
      for (const id of list(savedIds)) appendRow(id);
      add.addEventListener("click", () => { session.dirty = true; });
      return () => [...new Set(rows.map(({ select }) => select.value))];
    }

    function renderTimelineForm(event) {
      const edit = editor("timeline", event, event ? "编辑人工事件" : "新建人工事件", () => ({
        title: readRequired(title, "请填写事件标题"), chapterId: chapter.value, summary: summary.value,
        eventType: type.value, storyOrder: Number(order.value), participants: participants(), entities: entities(),
      }), event ? `删除事件「${event.title}」？` : "");
      const title = textField(edit.form, "事件标题", event?.title, { required: true });
      const chapterId = event?.chapterId || "";
      const chapters = withSavedOption([["", "不指定章节"], ...chapterOptions()], chapterId);
      const chapter = selectField(edit.form, "所属章节", chapters, chapterId);
      const type = textField(edit.form, "事件类型", event?.eventType);
      const summary = textField(edit.form, "摘要", event?.summary, { rows: 3 });
      const order = numberField(edit.form, "故事顺序", event?.storyOrder);
      const participants = entryPicker(edit.form, "参与者", event?.participants);
      const entities = entryPicker(edit.form, "关联词条", event?.entities);
      edit.finish();
    }
  }

  if (document.readyState === "loading") document.addEventListener("DOMContentLoaded", mount, { once: true });
  else mount();
})();
