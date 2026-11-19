(() => {
  const dialog = document.createElement("dialog");
  dialog.className = "dialog dialog-wide manuscript-dialog";
  dialog.setAttribute("aria-labelledby", "manuscriptToolsTitle");
  const node = (tag, text = "", className = "") => {
    const element = document.createElement(tag);
    element.textContent = text;
    if (className) element.className = className;
    return element;
  };
  const heading = node("h2");
  heading.id = "manuscriptToolsTitle";
  const close = node("button", "×", "icon-button");
  close.type = "button";
  close.setAttribute("aria-label", "关闭");
  const head = node("div", "", "dialog-head");
  head.append(heading, close);
  const status = node("p", "", "dialog-note");
  status.setAttribute("role", "status");
  const body = node("div", "", "manuscript-tool-body");
  dialog.append(head, status, body);
  document.body.append(dialog);
  close.onclick = () => dialog.close();
  let busy = false;
  dialog.addEventListener("cancel", (event) => { if (busy) event.preventDefault(); });

  function open(title) {
    heading.textContent = title;
    status.textContent = "";
    body.replaceChildren();
    showDialog(dialog);
  }

  function run(action) {
    if (busy) return;
    busy = true;
    body.inert = true;
    close.disabled = true;
    status.textContent = "处理中…";
    return (async () => {
      try {
        status.textContent = await action() || "";
      } catch (error) {
        status.textContent = error.message;
      } finally {
        busy = false;
        body.inert = false;
        close.disabled = false;
        if (dialog.open && !dialog.contains(document.activeElement)) close.focus();
      }
    })();
  }

  function button(label, action, primary = false) {
    const control = node("button", label, `btn ${primary ? "btn-primary" : "btn-quiet"}`);
    control.type = "button";
    control.onclick = action;
    return control;
  }

  function field(parent, label, control) {
    const wrapper = node("label", label, "field");
    wrapper.append(control);
    parent.append(wrapper);
    return control;
  }

  function select(options) {
    const control = node("select");
    for (const [value, label] of options) control.add(new Option(label, value));
    return control;
  }

  function textInput(maxLength = 120) {
    const control = node("input");
    control.type = "text";
    control.maxLength = maxLength;
    return control;
  }

  function section(title) {
    const panel = node("section", "", "manuscript-tool-section");
    panel.append(node("h3", title));
    body.append(panel);
    return panel;
  }

  function post(url, payload = {}) {
    return api(url, { method: "POST", body: JSON.stringify(payload) });
  }

  async function saveEditor() {
    if (!await flushChapterSave()) throw new Error("请先保存当前章节");
  }

  async function saveBook() {
    await saveEditor();
    if (titleRevision > titleSavedRevision) await saveTitleNow();
    if (titleRevision > titleSavedRevision) throw new Error("请先保存书名");
  }

  async function updateEditor(data, preferred = chapter.id) {
    applyProjectMeta(data.project);
    const id = project.chapters.some((item) => item.id === preferred) ? preferred : lastChapterID();
    if (id) await openChapter(id);
    else clearChapterSelection();
    ensureAnalysisPolling();
  }

  function runeOffset(text, offset) {
    if (offset > 0 && /[\uD800-\uDBFF]/.test(text[offset - 1]) && /[\uDC00-\uDFFF]/.test(text[offset] || "")) {
      throw new Error("请选择完整字符边界");
    }
    return Array.from(text.slice(0, offset)).length;
  }

  function openAdapt() {
    const { selectionStart, selectionEnd, value } = chapterContent;
    if (!chapter.id || selectionEnd <= selectionStart) return showToast("请先在正文中选中要改写的段落", true);
    const id = chapter.id;
    const original = value.slice(selectionStart, selectionEnd);
    open("选区改写");
    const options = section("改写指令");
    options.append(node("pre", original, "manuscript-preview"));
    const instruction = node("textarea");
    instruction.rows = 3;
    instruction.maxLength = 2000;
    field(options, "给模型的指令", instruction);
    const type = field(options, "改写类型", select([
      ["rewrite", "改写"], ["polish", "润色"], ["expand", "扩写"], ["shorten", "缩写"],
      ["style", "调整文风"], ["perspective", "切换视角"],
    ]));
    const mode = field(options, "世界影响", select([["A", "A · 仅改正文"], ["B", "B · 推演影响"]]));
    const result = node("div", "", "manuscript-tool-section");
    const generate = button("生成改编草稿", () => run(async () => {
      if (!instruction.value.trim()) throw new Error("请输入改写指令");
      await saveEditor();
      if (chapter.id !== id || chapter.content !== value) throw new Error("正文已变化，请重新选择改写范围");
      const data = await post("/api/adapt/rewrite", {
        chapterId: id, startOffset: runeOffset(value, selectionStart), endOffset: runeOffset(value, selectionEnd),
        instruction: instruction.value.trim(), instructionType: type.value, mode: mode.value,
      });
      result.replaceChildren(node("h3", "差异预览"));
      const diff = node("div", "", "manuscript-preview manuscript-diff");
      for (const part of data.diffParts || []) {
        diff.append(node(part.op === "delete" ? "del" : part.op === "insert" ? "ins" : "span", part.text));
      }
      result.append(diff, node("h3", "影响清单"), node("p", data.memoryModeNote, "dialog-note"));
      const impact = data.impact || {};
      for (const [key, label, display] of [
        ["changedEntities", "设定与人物", (item) => item.name],
        ["affectedChapters", "后续章节", (item) => `第 ${item.index} 章 ${item.title}`],
        ["foreshadows", "伏笔", (item) => item.title || item.quote],
        ["memoryEvents", "记忆事件", (item) => item.summary],
      ]) {
        const list = node("ul");
        for (const item of impact[key] || []) list.append(node("li", `${display(item)}${item.reason ? ` · ${item.reason}` : ""}`));
        result.append(node("h4", label), list.children.length ? list : node("p", "无条目", "dialog-note"));
      }
      for (const warning of impact.warnings || []) result.append(node("p", warning, "dialog-note"));
      result.append(button("在草稿台审阅", () => {
        dialog.close();
        void openDraftBox(data.draft.id);
      }, true));
      result.scrollIntoView({ block: "nearest" });
      return "改编草稿已保存，正文等待审阅确认。";
    }), true);
    options.append(generate);
    body.append(result);
    instruction.focus();
  }

  function openManuscript() {
    if (!chapter.id) return;
    const current = { ...chapter };
    const offset = chapterContent.selectionStart;
    const path = `/api/chapters/${encodeURIComponent(current.id)}`;
    open("章节管理");
    const order = section("排序");
    const items = [...project.chapters];
    const navigator = field(order, "章节顺序", select(items.map((item) => [item.id, chapterLabel(item)])));
    navigator.size = Math.min(8, Math.max(2, items.length));
    navigator.value = current.id;
    const move = (delta) => {
      const index = navigator.selectedIndex;
      const target = index + delta;
      if (index < 0 || target < 0 || target >= items.length) return;
      [items[index], items[target]] = [items[target], items[index]];
      const option = navigator.options[index];
      if (delta < 0) navigator.insertBefore(option, navigator.options[target]);
      else navigator.insertBefore(option, navigator.options[target].nextSibling);
      navigator.selectedIndex = target;
    };
    order.append(button("上移", () => move(-1)), button("下移", () => move(1)), button("保存顺序", () => run(async () => {
      await saveEditor();
      await updateEditor(await post("/api/chapters/reorder", { chapterIds: items.map((item) => item.id) }));
      dialog.close();
      showToast("章节顺序已保存");
    }), true));

    const merge = section("合并连续章节");
    const choices = project.chapters.map((item) => [item.id, chapterLabel(item)]);
    const from = field(merge, "从", select(choices));
    const to = field(merge, "合并至（含）", select(choices));
    from.value = current.id;
    to.selectedIndex = Math.min(from.selectedIndex + 1, choices.length - 1);
    const mergeTitle = field(merge, "合并后的标题（可选）", textInput());
    merge.append(button("合并章节", () => run(async () => {
      if (from.selectedIndex >= to.selectedIndex) throw new Error("请选择至少两个连续的章节");
      if (!window.confirm("确定合并所选范围内的章节？")) return;
      await saveEditor();
      const chapterIds = choices.slice(from.selectedIndex, to.selectedIndex + 1).map(([id]) => id);
      const data = await post("/api/chapters/merge", { chapterIds, title: mergeTitle.value.trim() });
      await updateEditor(data, data.chapter.id);
      dialog.close();
      showToast("章节已合并");
    })));

    const split = section("拆分当前章节");
    const splitOffset = node("input");
    splitOffset.type = "number";
    splitOffset.min = "1";
    splitOffset.max = String(Array.from(current.content).length - 1);
    splitOffset.value = String(Array.from(current.content.slice(0, offset)).length);
    field(split, "拆分位置（字符偏移，默认取编辑器光标）", splitOffset);
    const first = field(split, "上半章标题（可选）", textInput());
    const second = field(split, "下半章标题（可选）", textInput());
    split.append(button("拆分章节", () => run(async () => {
      if (!splitOffset.value || !splitOffset.checkValidity()) throw new Error("请将拆分位置设在正文首尾之间");
      await saveEditor();
      const data = await post(`${path}/split`, { offset: splitOffset.valueAsNumber, firstTitle: first.value.trim(), secondTitle: second.value.trim() });
      await updateEditor(data, data.second.id);
      dialog.close();
      showToast("章节已拆分");
    })));

    const history = section("当前章版本");
    const versions = node("div", "", "manuscript-tool-body");
    history.append(button("读取版本", () => run(async () => {
      await saveEditor();
      const data = await api(`${path}/versions`);
      versions.replaceChildren();
      for (const version of data.versions || []) {
        const details = node("details");
        details.append(node("summary", `版本 ${version.revision} · ${version.title} · ${dateLabel(version.createdAt)}`));
        details.append(node("pre", version.content, "manuscript-preview"));
        details.append(button("回滚到此版本", () => run(async () => {
          if (!window.confirm("确定回滚本章？系统会先保存当前全书快照。")) return;
          await updateEditor(await post(`${path}/versions/${version.revision}/restore`));
          dialog.close();
          showToast("章节已回滚，恢复前的全书快照已保存");
        })));
        versions.append(details);
      }
      if (!versions.children.length) versions.append(node("p", "暂无历史版本", "dialog-note"));
    })), versions);
  }

  function dateLabel(date) {
    return new Date(date).toLocaleString();
  }

  function openSnapshots() {
    open("全书快照");
    const create = section("创建快照");
    const name = field(create, "快照名称（可选）", textInput());
    const history = section("快照列表");
    const list = node("div", "", "manuscript-tool-body");
    async function load() {
      const data = await api("/api/snapshots");
      list.replaceChildren();
      for (const snapshot of data.snapshots || []) {
        const details = node("details");
        const path = `/api/snapshots/${encodeURIComponent(snapshot.id)}`;
        const preview = node("div", "", "manuscript-tool-body");
        details.append(node("summary", `${snapshot.name || snapshot.title} · ${snapshot.chapterCount} 章 · ${dateLabel(snapshot.createdAt)}`));
        details.append(button("查看快照章节", () => run(async () => {
          const detail = await api(path);
          preview.replaceChildren();
          for (const item of detail.chapters) {
            const copy = node("details");
            copy.append(node("summary", `第 ${item.index} 章 ${item.title}`), node("pre", item.content, "manuscript-preview"));
            preview.append(copy);
          }
        })), button("恢复此快照", () => run(async () => {
          if (!window.confirm("恢复快照将替换全书章节和知识库。系统会先尝试保存当前快照。确定恢复？")) return;
          await saveBook();
          await updateEditor(await post(`${path}/restore`));
          titleInput.value = project.title;
          dialog.close();
          showToast("全书快照已恢复");
        })), preview);
        list.append(details);
      }
      if (!list.children.length) list.append(node("p", "暂无全书快照", "dialog-note"));
    }
    create.append(button("保存全书快照", () => run(async () => {
      await saveBook();
      await post("/api/snapshots", { name: name.value.trim() });
      name.value = "";
      await load();
      return "全书快照已保存";
    }), true));
    history.append(button("刷新", () => run(load)), list);
    void run(load);
  }

  function openCandidates() {
    const current = { ...chapter };
    const offset = chapterContent.selectionStart;
    open("多候选续写");
    const controls = section("生成候选");
    controls.append(node("p", "使用续写区的方向、篇幅和创意度。选中的候选可存为待审阅的新章草稿。", "dialog-note"));
    const source = field(controls, "续写起点", select([
      ["full", "全书末尾"], ...(current.id ? [["chapter_offset", `本章光标（偏移 ${Array.from(current.content.slice(0, offset)).length}）`]] : []),
    ]));
    const count = field(controls, "候选数", select([["1", "1 个"], ["2", "2 个"], ["3", "3 个"]]));
    count.value = "2";
    const results = node("div", "", "manuscript-candidates");
    const history = section("历史候选组");
    const groups = field(history, "选择候选组", select([]));
    const view = button("查看候选组", () => run(async () => {
      render(await api(`/api/continuations/${encodeURIComponent(groups.value)}`));
    }));
    async function loadGroups() {
      const data = await api("/api/continuations");
      groups.replaceChildren();
      for (const group of data.groups || []) {
        groups.add(new Option(`${dateLabel(group.createdAt)} · ${group.direction || "自然续写"} · ${group.candidateCount} 个`, group.id));
      }
      view.disabled = !groups.value;
    }
    function render(data) {
      results.replaceChildren();
      for (const candidate of data.candidates || []) {
        const card = node("article", "", "manuscript-tool-section");
        card.append(node("h3", `候选 ${results.children.length + 1}`));
        if (candidate.status === "failed") card.append(node("p", candidate.error || "生成失败"));
        else {
          card.append(node("p", `${candidate.charCount} 字 · ${candidate.totalTokens} tokens`, "dialog-note"));
          card.append(node("pre", candidate.content, "manuscript-preview"));
          card.append(button("存为待审阅草稿", () => run(async () => {
            const draft = await post("/api/drafts", { content: candidate.content });
            dialog.close();
            void openDraftBox(draft.id);
          })));
        }
        results.append(card);
      }
      if (!results.children.length) results.append(node("p", "本组没有候选结果", "dialog-note"));
    }
    controls.append(button("生成候选", () => run(async () => {
      await saveEditor();
      const params = { ...memoryPreviewParams(), temperature: Number($("temperature").value), startMode: source.value, candidates: Number(count.value) };
      if (params.startMode === "chapter_offset") {
        params.chapterId = current.id;
        params.startOffset = runeOffset(current.content, offset);
      }
      try {
        render(await post("/api/continue/candidates", params));
      } catch (error) {
        if (error.status === 502) await loadGroups();
        throw error;
      }
      await loadGroups();
      return "候选组已保存";
    }), true));
    history.append(button("刷新列表", () => run(loadGroups)), view);
    body.append(results);
    void run(loadGroups);
  }

  $("adaptSelectionButton").onclick = openAdapt;
  $("manuscriptButton").onclick = openManuscript;
  $("snapshotButton").onclick = openSnapshots;
  $("candidateButton").onclick = openCandidates;
})();
