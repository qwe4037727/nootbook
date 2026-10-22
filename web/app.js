"use strict";

const $ = (id) => document.getElementById(id);

const headerTitle = $("headerTitle");
const modelBadge = $("modelBadge");
const modelBadgeText = $("modelBadgeText");
const saveStatus = $("saveStatus");
const settingsButton = $("settingsButton");

const titleInput = $("titleInput");
const importButton = $("importButton");
const addChapterButton = $("addChapterButton");
const chapterTotalCount = $("chapterTotalCount");
const chapterList = $("chapterList");
const chapterEmpty = $("chapterEmpty");

const analysisBadge = $("analysisBadge");
const analysisText = $("analysisText");
const analysisProgress = $("analysisProgress");
const analysisBar = $("analysisBar");
const retryAnalysisButton = $("retryAnalysisButton");

const chapterEditor = $("chapterEditor");
const chapterTitleInput = $("chapterTitleInput");
const chapterCharCount = $("chapterCharCount");
const chapterContent = $("chapterContent");
const saveButton = $("saveButton");
const deleteChapterButton = $("deleteChapterButton");
const noChapter = $("noChapter");
const emptyImportButton = $("emptyImportButton");
const emptyAddChapterButton = $("emptyAddChapterButton");

const contextInfo = $("contextInfo");
const directionInput = $("direction");
const targetCharsSelect = $("targetChars");
const temperatureSelect = $("temperature");
const generateButton = $("generateButton");

const draftPanel = $("draftPanel");
const draftStatus = $("draftStatus");
const draftTarget = $("draftTarget");
const draftText = $("draftText");
const appendButton = $("appendButton");
const newChapterButton = $("newChapterButton");
const retryButton = $("retryButton");
const discardButton = $("discardButton");
const stopButton = $("stopButton");

const toast = $("toast");

const settingsDialog = $("settingsDialog");
const settingsForm = $("settingsForm");
const baseUrlInput = $("baseUrlInput");
const modelInput = $("modelInput");
const apiKeyInput = $("apiKeyInput");
const apiKeyHint = $("apiKeyHint");
const clearKeyInput = $("clearKeyInput");
const contextCharsInput = $("contextCharsInput");

const importDialog = $("importDialog");
const importForm = $("importForm");
const importFile = $("importFile");
const importTitleInput = $("importTitleInput");
const importText = $("importText");
const importHint = $("importHint");
const importSubmitButton = $("importSubmitButton");

let project = {
  title: "",
  chapters: [],
  analysis: { status: "idle", analyzedCount: 0, totalCount: 0, message: "", updatedAt: "" },
  updatedAt: "",
};
let config = {
  baseUrl: "",
  model: "",
  contextChars: 12000,
  hasApiKey: false,
  apiKeyHint: "",
  apiKeyFromEnv: false,
};

let chapter = { id: "", index: 0, title: "", content: "", charCount: 0 };
let selectedChapterID = "";
let chapterDirtyRevision = 0;
let chapterSavedRevision = 0;
let chapterSaveTimer = 0;
let chapterSaveChain = Promise.resolve();
let chapterSwitchToken = 0;

let titleSaveTimer = 0;
let titleRevision = 0;
let titleSavedRevision = 0;
let analysisTimer = 0;
let projectEpoch = 0;

let draft = {
  text: "",
  streaming: false,
  appended: false,
  controller: null,
  targetChapterID: "",
  targetLabel: "",
};
let toastTimer = 0;

function readString(value) {
  return typeof value === "string" ? value : "";
}

function formatNumber(value) {
  return new Intl.NumberFormat("zh-CN").format(value);
}

function countChars(text) {
  return Array.from(String(text || "").replace(/\s+/gu, "")).length;
}

async function api(url, options = {}) {
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
    const message = data && typeof data.error === "string" ? data.error : `请求失败（HTTP ${response.status}）`;
    throw new Error(message);
  }
  return data;
}

function showToast(message, isError = false) {
  toast.textContent = message;
  toast.classList.toggle("is-error", isError);
  toast.hidden = false;
  clearTimeout(toastTimer);
  toastTimer = window.setTimeout(() => {
    toast.hidden = true;
  }, isError ? 5200 : 2800);
}

function setSaveStatus(kind) {
  const labels = {
    saved: "已保存",
    dirty: "未保存",
    saving: "保存中…",
    error: "保存失败",
  };
  saveStatus.className = "save-status is-" + kind;
  saveStatus.textContent = labels[kind] || labels.saved;
}

function normalizeMeta(raw) {
  if (!raw || typeof raw !== "object") return null;
  const id = readString(raw.id);
  if (!id) return null;
  return {
    id,
    index: Number(raw.index) || 0,
    title: readString(raw.title),
    charCount: Number(raw.charCount) || 0,
    updatedAt: readString(raw.updatedAt),
  };
}

function normalizeAnalysis(raw) {
  const statuses = new Set(["idle", "pending", "analyzing", "ready", "error"]);
  const status = readString(raw && raw.status);
  return {
    status: statuses.has(status) ? status : "idle",
    analyzedCount: Math.max(0, Number(raw && raw.analyzedCount) || 0),
    totalCount: Math.max(0, Number(raw && raw.totalCount) || 0),
    message: readString(raw && raw.message),
    updatedAt: readString(raw && raw.updatedAt),
  };
}

function lastChapterID() {
  return project.chapters.length ? project.chapters[project.chapters.length - 1].id : "";
}

function chapterLabel(meta) {
  if (!meta) return "";
  const title = readString(meta.title).trim();
  return title ? `第 ${meta.index} 章 ${title}` : `第 ${meta.index} 章`;
}

function totalBookChars() {
  let total = 0;
  for (const meta of project.chapters) {
    total += meta.id === chapter.id ? countChars(chapter.content) : meta.charCount;
  }
  return total;
}

function configured() {
  return Boolean(config.baseUrl && config.model);
}

function refreshHeader() {
  const title = (readString(project.title).trim() || readString(titleInput.value).trim() || "未命名小说");
  headerTitle.textContent = title;
  document.title = title + " · 续写";
}

function updateContextInfo() {
  const limit = config.contextChars || 12000;
  if (!project.chapters.length) {
    contextInfo.textContent = "导入文稿后开始";
    return;
  }
  const total = totalBookChars();
  if (total <= limit) {
    contextInfo.textContent = `全书 ${formatNumber(total)} 字，将全部带入`;
  } else {
    contextInfo.textContent = `全书 ${formatNumber(total)} 字，将截取最近约 ${formatNumber(limit)} 字`;
  }
}

function updateConfigUI() {
  const isConfigured = configured();
  modelBadge.classList.toggle("is-configured", isConfigured);
  modelBadgeText.textContent = isConfigured ? config.model : "模型未配置";
  modelBadge.title = isConfigured ? config.baseUrl + " · 点击修改设置" : "点击配置模型接口";
  updateContextInfo();
  renderAnalysis();
}

function applyConfig(raw) {
  config = {
    baseUrl: readString(raw.baseUrl),
    model: readString(raw.model),
    contextChars: Number(raw.contextChars) > 0 ? Number(raw.contextChars) : 12000,
    hasApiKey: Boolean(raw.hasApiKey),
    apiKeyHint: readString(raw.apiKeyHint),
    apiKeyFromEnv: Boolean(raw.apiKeyFromEnv),
  };
  updateConfigUI();
}

async function loadConfig() {
  try {
    const data = await api("/api/config");
    applyConfig(data || {});
  } catch (error) {
    showToast("读取模型设置失败：" + error.message, true);
  }
}

function applyProjectMeta(raw) {
  const localTitle = project.title;
  const keepLocalTitle = titleRevision > titleSavedRevision || document.activeElement === titleInput;
  project = {
    title: readString(raw.title) || "未命名小说",
    chapters: Array.isArray(raw.chapters) ? raw.chapters.map(normalizeMeta).filter(Boolean) : [],
    analysis: normalizeAnalysis(raw.analysis || {}),
    updatedAt: readString(raw.updatedAt),
  };
  if (keepLocalTitle) project.title = localTitle;
  if (!keepLocalTitle && document.activeElement !== titleInput) titleInput.value = project.title;
  if (chapter.id) {
    const meta = project.chapters.find((item) => item.id === chapter.id);
    if (meta) {
      chapter.index = meta.index;
      meta.title = chapter.title;
      meta.charCount = countChars(chapter.content);
    }
  }
  refreshHeader();
  renderChapterList();
  renderAnalysis();
  updateContextInfo();
  updateGenerateAvailability();
}

async function loadProject() {
  try {
    const data = await api("/api/project");
    applyProjectMeta(data || {});
    if (project.chapters.length) {
      await openChapter(lastChapterID());
    } else {
      clearChapterSelection();
    }
    ensureAnalysisPolling();
  } catch (error) {
    setSaveStatus("error");
    showToast("读取作品失败：" + error.message, true);
  }
}

async function refreshProject() {
  const epoch = projectEpoch;
  try {
    const previousStatus = project.analysis.status;
    const data = await api("/api/project");
    if (epoch !== projectEpoch) return;
    applyProjectMeta(data || {});
    if (previousStatus !== project.analysis.status) {
      if (project.analysis.status === "ready") {
        showToast("文稿分析完成");
      } else if (project.analysis.status === "error") {
        showToast(project.analysis.message || "文稿分析中断，请检查模型设置", true);
      }
    }
    if (selectedChapterID && !project.chapters.some((item) => item.id === selectedChapterID)) {
      await openChapter(lastChapterID());
    }
  } catch {
    // Polling is best-effort; the next tick will retry.
  }
  ensureAnalysisPolling();
}

function analysisIsActive() {
  return project.analysis.status === "pending" || project.analysis.status === "analyzing";
}

function ensureAnalysisPolling() {
  if (!analysisIsActive()) {
    if (analysisTimer) {
      clearTimeout(analysisTimer);
      analysisTimer = 0;
    }
    return;
  }
  if (analysisTimer) return;
  analysisTimer = window.setTimeout(() => {
    analysisTimer = 0;
    void refreshProject();
  }, 2600);
}

function renderAnalysis() {
  const state = project.analysis;
  const running = state.status === "pending" || state.status === "analyzing";
  const total = state.totalCount || project.chapters.length;
  let label = "未开始";
  let text = "导入文稿后会自动开始。";

  switch (state.status) {
    case "pending":
      label = "排队中";
      text = total ? `等待开始 · 共 ${total} 章` : "等待开始…";
      break;
    case "analyzing":
      label = "分析中";
      text = total ? `正在理解 ${Math.min(state.analyzedCount, total)} / ${total} 章` : "正在理解文稿…";
      break;
    case "ready":
      label = "已就绪";
      text = `已完成 ${total} 章 · 续写会自动使用相关设定`;
      break;
    case "error":
      label = "已中断";
      text = state.message || "分析中断，请检查模型设置后重试。";
      break;
    default:
      if (project.chapters.length) {
        text = configured() ? "等待分析开始。" : "配置模型后会自动开始分析。";
      }
      break;
  }

  analysisBadge.className = "analysis-badge is-" + state.status;
  analysisBadge.textContent = label;
  analysisText.textContent = text;
  analysisProgress.hidden = !running;
  analysisProgress.classList.toggle("is-indeterminate", running && state.analyzedCount === 0);
  if (running && total > 0) {
    const percent = Math.round((Math.min(state.analyzedCount, total) / total) * 100);
    analysisBar.style.width = Math.max(2, percent) + "%";
  } else {
    analysisBar.style.width = "0%";
  }
  retryAnalysisButton.hidden = state.status !== "error" || project.chapters.length === 0;
}

function renderChapterList() {
  chapterTotalCount.textContent = String(project.chapters.length);
  chapterEmpty.hidden = project.chapters.length > 0;
  chapterList.replaceChildren();

  if (!project.chapters.length) {
    setEditorVisibility(false);
    updateGenerateAvailability();
    return;
  }

  const fragment = document.createDocumentFragment();
  for (const meta of project.chapters) {
    const row = document.createElement("div");
    row.className = "chapter-row" + (meta.id === selectedChapterID ? " is-active" : "");
    row.dataset.id = meta.id;
    row.tabIndex = 0;
    row.setAttribute("role", "button");
    row.setAttribute("aria-label", "打开" + chapterLabel(meta));

    const index = document.createElement("span");
    index.className = "chapter-index";
    index.textContent = String(meta.index).padStart(2, "0");

    const info = document.createElement("span");
    info.className = "chapter-info";
    const name = document.createElement("span");
    name.className = "chapter-name";
    name.textContent = meta.title || `第${meta.index}章`;
    const chars = document.createElement("span");
    chars.className = "chapter-chars";
    chars.textContent = formatNumber(meta.charCount) + " 字";
    info.append(name, chars);

    const remove = document.createElement("button");
    remove.type = "button";
    remove.className = "chapter-remove";
    remove.dataset.remove = "true";
    remove.setAttribute("aria-label", "删除" + chapterLabel(meta));
    remove.textContent = "×";

    row.append(index, info, remove);
    fragment.append(row);
  }
  chapterList.append(fragment);
  setEditorVisibility(Boolean(chapter.id && selectedChapterID === chapter.id));
  updateGenerateAvailability();
}

function setEditorVisibility(visible) {
  chapterEditor.hidden = !visible;
  noChapter.hidden = visible;
  chapterTitleInput.disabled = !visible;
  chapterContent.disabled = !visible;
  saveButton.disabled = !visible;
  deleteChapterButton.disabled = !visible;
  if (!visible) chapterCharCount.textContent = "0 字";
}

function updateGenerateAvailability() {
  generateButton.disabled = draft.streaming || project.chapters.length === 0;
}

function updateChapterStats() {
  chapterCharCount.textContent = formatNumber(countChars(chapter.content)) + " 字";
}

function updateChapterRowStats() {
  if (!chapter.id) return;
  const meta = project.chapters.find((item) => item.id === chapter.id);
  if (meta) {
    meta.title = chapter.title;
    meta.charCount = countChars(chapter.content);
  }
  const row = Array.from(chapterList.children).find((element) => element.dataset.id === chapter.id);
  if (!row) return;
  const name = row.querySelector(".chapter-name");
  const chars = row.querySelector(".chapter-chars");
  if (name) name.textContent = chapter.title || `第${chapter.index}章`;
  if (chars) chars.textContent = formatNumber(countChars(chapter.content)) + " 字";
}

function applyChapterData(raw, id) {
  chapterSwitchToken += 1;
  selectedChapterID = id;
  chapter = {
    id,
    index: Number(raw.index) || 0,
    title: readString(raw.title),
    content: readString(raw.content),
    charCount: Number(raw.charCount) || 0,
  };
  chapterDirtyRevision = 0;
  chapterSavedRevision = 0;
  clearTimeout(chapterSaveTimer);
  chapterSaveTimer = 0;
  chapterTitleInput.value = chapter.title;
  chapterContent.value = chapter.content;
  setSaveStatus("saved");
  updateChapterStats();
  updateContextInfo();
  renderChapterList();
  revealSelectedChapter();
}

function revealSelectedChapter() {
  const row = Array.from(chapterList.children).find((element) => element.dataset.id === selectedChapterID);
  if (!row) return;
  window.requestAnimationFrame(() => row.scrollIntoView({ block: "nearest", inline: "nearest" }));
}

async function openChapter(id, token = ++chapterSwitchToken) {
  if (!id) {
    if (token === chapterSwitchToken) clearChapterSelection();
    return;
  }
  try {
    const data = await api("/api/chapters/" + encodeURIComponent(id));
    if (token !== chapterSwitchToken) return;
    applyChapterData(data || {}, id);
  } catch (error) {
    if (token !== chapterSwitchToken) return;
    showToast("读取章节失败：" + error.message, true);
    clearChapterSelection();
  }
}

function clearChapterSelection() {
  chapterSwitchToken += 1;
  selectedChapterID = "";
  chapter = { id: "", index: 0, title: "", content: "", charCount: 0 };
  chapterDirtyRevision = 0;
  chapterSavedRevision = 0;
  clearTimeout(chapterSaveTimer);
  chapterSaveTimer = 0;
  chapterTitleInput.value = "";
  chapterContent.value = "";
  setSaveStatus("saved");
  updateChapterStats();
  updateContextInfo();
  renderChapterList();
}

async function selectChapter(id) {
  if (!id || id === selectedChapterID) return;
  const token = ++chapterSwitchToken;
  const saved = await flushChapterSave();
  if (token !== chapterSwitchToken) return;
  if (!saved) {
    showToast("当前章节尚未保存，已取消切换", true);
    return;
  }
  await openChapter(id, token);
}

function markChapterDirty() {
  chapterDirtyRevision += 1;
  setSaveStatus("dirty");
  clearTimeout(chapterSaveTimer);
  chapterSaveTimer = window.setTimeout(() => {
    chapterSaveTimer = 0;
    void saveChapterNow();
  }, 900);
}

function saveChapterNow() {
  if (!chapter.id) return Promise.resolve(true);
  if (chapterDirtyRevision <= chapterSavedRevision) {
    setSaveStatus("saved");
    return Promise.resolve(true);
  }
  const id = chapter.id;
  const revision = chapterDirtyRevision;
  const epoch = projectEpoch;
  const payload = JSON.stringify({ title: chapter.title, content: chapter.content });
  setSaveStatus("saving");

  const run = chapterSaveChain.then(async () => {
    try {
      const data = await api("/api/chapters/" + encodeURIComponent(id), { method: "PUT", body: payload });
      if (epoch !== projectEpoch) return true;
      if (data && data.project) applyProjectMeta(data.project);
      if (chapter.id === id) {
        if (data && data.chapter) {
          chapter.index = Number(data.chapter.index) || chapter.index;
          chapter.charCount = Number(data.chapter.charCount) || 0;
        }
        chapterSavedRevision = Math.max(chapterSavedRevision, revision);
        if (chapterDirtyRevision > chapterSavedRevision) setSaveStatus("dirty");
        else setSaveStatus("saved");
      }
      ensureAnalysisPolling();
      return true;
    } catch (error) {
      if (epoch !== projectEpoch) return true;
      if (chapter.id === id) setSaveStatus("error");
      showToast("保存章节失败：" + error.message, true);
      return false;
    }
  });
  chapterSaveChain = run.then(() => undefined, () => undefined);
  return run;
}

async function flushChapterSave() {
  clearTimeout(chapterSaveTimer);
  chapterSaveTimer = 0;
  await chapterSaveChain;
  for (let attempt = 0; attempt < 20 && chapterSavedRevision < chapterDirtyRevision; attempt += 1) {
    const ok = await saveChapterNow();
    if (!ok) return false;
    if (chapterSaveTimer) {
      clearTimeout(chapterSaveTimer);
      chapterSaveTimer = 0;
    }
  }
  return chapterSavedRevision >= chapterDirtyRevision;
}

async function manualSave() {
  if (!chapter.id) return;
  saveButton.disabled = true;
  const saved = await flushChapterSave();
  saveButton.disabled = false;
  if (saved) showToast("章节已保存");
}

async function saveTitleNow() {
  if (titleSaveTimer) {
    clearTimeout(titleSaveTimer);
    titleSaveTimer = 0;
  }
  const revision = titleRevision;
  const value = project.title;
  const epoch = projectEpoch;
  try {
    const data = await api("/api/project", {
      method: "PUT",
      body: JSON.stringify({ title: value }),
    });
    if (epoch !== projectEpoch) return;
    if (data) {
      titleSavedRevision = Math.max(titleSavedRevision, revision);
      if (titleSavedRevision >= titleRevision) {
        project.title = readString(data.title) || value;
      }
      if (document.activeElement !== titleInput) titleInput.value = project.title;
      refreshHeader();
    }
  } catch (error) {
    if (epoch !== projectEpoch) return;
    showToast("保存书名失败：" + error.message, true);
  }
}

function scheduleTitleSave() {
  clearTimeout(titleSaveTimer);
  titleSaveTimer = window.setTimeout(() => {
    titleSaveTimer = 0;
    void saveTitleNow();
  }, 900);
}

async function addChapter() {
  const token = ++chapterSwitchToken;
  const saved = await flushChapterSave();
  if (token !== chapterSwitchToken) return;
  if (!saved) {
    showToast("当前章节尚未保存，无法新建章节", true);
    return;
  }
  try {
    const data = await api("/api/chapters", {
      method: "POST",
      body: JSON.stringify({ title: "", content: "" }),
    });
    if (data && data.project) applyProjectMeta(data.project);
    if (data && data.chapter) {
      await openChapter(data.chapter.id);
      chapterTitleInput.focus();
      chapterTitleInput.select();
    }
    showToast("已新建章节");
  } catch (error) {
    showToast("新建章节失败：" + error.message, true);
  }
}

async function deleteChapter(id) {
  const meta = project.chapters.find((item) => item.id === id);
  if (!meta) return;
  if (!window.confirm(`确定删除「${chapterLabel(meta)}」？删除后无法恢复。`)) return;

  chapterSwitchToken += 1;
  if (chapter.id === id) {
    clearTimeout(chapterSaveTimer);
    chapterSaveTimer = 0;
    chapterDirtyRevision = 0;
    chapterSavedRevision = 0;
  }
  try {
    const data = await api("/api/chapters/" + encodeURIComponent(id), { method: "DELETE" });
    if (data && data.project) applyProjectMeta(data.project);
    if (selectedChapterID === id) {
      await openChapter(lastChapterID());
    } else {
      renderChapterList();
    }
    ensureAnalysisPolling();
    showToast("章节已删除");
  } catch (error) {
    showToast("删除章节失败：" + error.message, true);
  }
}

function openImportDialog() {
  importForm.reset();
  importText.value = "";
  importHint.textContent = "尚未读取内容。";
  showDialog(importDialog);
  importFile.focus();
}

async function readTextFile(file) {
  const buffer = await file.arrayBuffer();
  const utf8 = new TextDecoder("utf-8").decode(buffer);
  if (utf8.includes("\uFFFD")) {
    try {
      return new TextDecoder("gbk").decode(buffer);
    } catch {
      return utf8;
    }
  }
  return utf8;
}

function updateImportHint() {
  const chars = countChars(importText.value);
  importHint.textContent = chars
    ? `已读取 ${formatNumber(chars)} 字，导入后会自动拆分章节并开始分析。`
    : "尚未读取内容。";
}

async function saveDraftAsChapter() {
  if (draft.streaming || draft.appended) return;
  const content = draft.text.replace(/\s+$/u, "");
  if (!content.trim()) {
    showToast("草稿为空，无法保存", true);
    return;
  }
  const saved = await flushChapterSave();
  if (!saved) {
    showToast("当前章节尚未保存，暂不能保存草稿", true);
    return;
  }
  appendButton.disabled = true;
  newChapterButton.disabled = true;
  try {
    const data = await api("/api/chapters", {
      method: "POST",
      body: JSON.stringify({ title: "", content }),
    });
    if (data && data.project) applyProjectMeta(data.project);
    draft.appended = true;
    finishGeneration("appended", `已保存 ${formatNumber(countChars(content))} 字`);
    showToast("已存为新章节");
    if (data && data.chapter) await openChapter(data.chapter.id);
    ensureAnalysisPolling();
  } catch (error) {
    showToast("保存新章节失败：" + error.message, true);
  } finally {
    appendButton.disabled = false;
    newChapterButton.disabled = false;
  }
}

async function appendDraft() {
  if (draft.streaming || draft.appended) return;
  const addition = draft.text.replace(/\s+$/u, "");
  if (!addition.trim()) {
    showToast("草稿为空，无法追加", true);
    return;
  }
  const targetID = draft.targetChapterID || lastChapterID();
  if (!targetID) {
    showToast("没有可追加的章节", true);
    return;
  }
  const saved = await flushChapterSave();
  if (!saved) {
    showToast("当前章节尚未保存，暂不能追加", true);
    return;
  }

  const wasCurrent = chapter.id === targetID;
  const localTitle = chapter.title;
  const localContent = chapter.content;
  appendButton.disabled = true;
  newChapterButton.disabled = true;
  chapterContent.disabled = true;
  chapterTitleInput.disabled = true;
  try {
    const base = wasCurrent
      ? { title: localTitle, content: localContent }
      : await api("/api/chapters/" + encodeURIComponent(targetID));
    const baseContent = readString(base.content);
    const joined = baseContent.trim() ? baseContent.replace(/\s+$/u, "") + "\n\n" + addition : addition;
    const data = await api("/api/chapters/" + encodeURIComponent(targetID), {
      method: "PUT",
      body: JSON.stringify({ title: readString(base.title), content: joined }),
    });
    if (wasCurrent && data && data.chapter) {
      chapter.content = joined;
      chapter.title = readString(data.chapter.title) || chapter.title;
      chapter.index = Number(data.chapter.index) || chapter.index;
      chapter.charCount = Number(data.chapter.charCount) || 0;
      chapterContent.value = joined;
      chapterDirtyRevision = 0;
      chapterSavedRevision = 0;
      updateChapterStats();
      setSaveStatus("saved");
    }
    if (data && data.project) applyProjectMeta(data.project);
    draft.appended = true;
    finishGeneration("appended", `已追加 ${formatNumber(countChars(addition))} 字`);
    ensureAnalysisPolling();
    showToast("已追加到章节");
  } catch (error) {
    showToast("追加失败：" + error.message, true);
  } finally {
    appendButton.disabled = false;
    newChapterButton.disabled = false;
    if (chapter.id) {
      setEditorVisibility(true);
    }
  }
}

function setDraftStatus(message) {
  draftStatus.textContent = message;
}

function setDraftPhase(phase) {
  const settled = phase === "done" || phase === "stopped" || phase === "error" || phase === "appended";
  const hasText = draft.text.trim() !== "";
  stopButton.hidden = phase !== "streaming";
  updateGenerateAvailability();
  appendButton.hidden = !(phase === "done" || phase === "stopped") || draft.appended || !hasText;
  newChapterButton.hidden = appendButton.hidden;
  retryButton.hidden = !(phase === "done" || phase === "stopped" || phase === "error");
  discardButton.hidden = !settled;
  draftText.classList.toggle("is-streaming", phase === "streaming");
}

function finishGeneration(phase, message) {
  draft.streaming = false;
  draft.controller = null;
  if (typeof message === "string") setDraftStatus(message);
  setDraftPhase(phase);
}

function finishStopped() {
  const chars = countChars(draft.text);
  finishGeneration("stopped", chars > 0 ? `已停止 · 保留 ${formatNumber(chars)} 字` : "已停止");
}

function appendDraftText(text) {
  const nearBottom = draftText.scrollHeight - draftText.scrollTop - draftText.clientHeight < 80;
  draft.text += text;
  draftText.append(document.createTextNode(text));
  if (nearBottom) draftText.scrollTop = draftText.scrollHeight;
}

function parseSSEBlock(block) {
  let event = "message";
  const dataLines = [];
  for (const line of block.split("\n")) {
    if (line.startsWith(":")) continue;
    if (line.startsWith("event:")) {
      event = line.slice(6).trim();
    } else if (line.startsWith("data:")) {
      dataLines.push(line.slice(5).replace(/^ /, ""));
    }
  }
  if (dataLines.length === 0) return null;
  return { event, data: dataLines.join("\n") };
}

async function readSSE(body, onEvent) {
  const reader = body.getReader();
  const decoder = new TextDecoder();
  let buffer = "";

  const dispatch = (block) => {
    if (!block.trim()) return;
    const parsed = parseSSEBlock(block);
    if (!parsed) return;
    let payload;
    try {
      payload = JSON.parse(parsed.data);
    } catch {
      return;
    }
    onEvent(parsed.event, payload);
  };

  try {
    while (true) {
      const { value, done } = await reader.read();
      if (done) break;
      buffer += decoder.decode(value, { stream: true });
      buffer = buffer.replace(/\r\n/g, "\n");
      let index;
      while ((index = buffer.indexOf("\n\n")) >= 0) {
        dispatch(buffer.slice(0, index));
        buffer = buffer.slice(index + 2);
      }
    }
    buffer += decoder.decode();
    buffer = buffer.replace(/\r\n/g, "\n");
    if (buffer.trim()) dispatch(buffer);
  } finally {
    reader.releaseLock();
  }
}

function applyAnalysisFromMeta(data) {
  const previous = project.analysis.status;
  const next = normalizeAnalysis({
    status: data.analysisStatus,
    analyzedCount: data.analyzedChapters,
    totalCount: data.totalChapters,
  });
  project.analysis = {
    ...project.analysis,
    status: next.status,
    analyzedCount: next.analyzedCount,
    totalCount: next.totalCount,
  };
  renderAnalysis();
  ensureAnalysisPolling();
  if (previous !== next.status && next.status === "ready") {
    showToast("文稿分析完成");
  }
}

async function startGeneration() {
  if (draft.streaming) return;
  if (!project.chapters.length) {
    showToast("请先导入文稿或新建章节", true);
    openImportDialog();
    return;
  }
  if (!configured()) {
    showToast("请先配置模型接口", true);
    openSettings();
    return;
  }

  const saved = await flushChapterSave();
  if (!saved) {
    showToast("当前章节尚未保存，无法开始续写", true);
    return;
  }
  if (totalBookChars() === 0) {
    showToast("请先在章节中写入正文，再开始续写", true);
    if (chapter.id) {
      chapterContent.focus();
    }
    return;
  }

  const target = project.chapters[project.chapters.length - 1];
  draft = {
    text: "",
    streaming: true,
    appended: false,
    controller: new AbortController(),
    targetChapterID: target ? target.id : "",
    targetLabel: chapterLabel(target),
  };
  const controller = draft.controller;
  draftPanel.hidden = false;
  draftText.textContent = "";
  draftTarget.hidden = false;
  draftTarget.textContent = draft.targetLabel ? `续写位置：${draft.targetLabel}` : "";
  appendButton.textContent = "追加到本章";
  setDraftStatus("正在连接模型…");
  setDraftPhase("streaming");

  try {
    const response = await fetch("/api/continue", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({
        direction: directionInput.value.trim(),
        targetChars: Number(targetCharsSelect.value) || 800,
        temperature: Number(temperatureSelect.value) || 0.85,
      }),
      signal: controller.signal,
    });

    if (!response.ok) {
      let message = `请求失败（HTTP ${response.status}）`;
      try {
        const data = await response.json();
        if (data && typeof data.error === "string") message = data.error;
      } catch {
        // Keep the status-based message.
      }
      throw new Error(message);
    }
    if (!response.body) throw new Error("当前浏览器不支持流式输出");

    let serverError = "";
    let sawDone = false;

    await readSSE(response.body, (event, data) => {
      switch (event) {
        case "meta": {
          applyAnalysisFromMeta(data);
          const contextChars = Number(data.contextChars) || 0;
          let message = `已连接 · 带入正文 ${formatNumber(contextChars)} 字`;
          if (data.truncated) message += "（截取结尾）";
          setDraftStatus(message);
          break;
        }
        case "delta": {
          const text = readString(data.text);
          if (!text) break;
          appendDraftText(text);
          setDraftStatus(`正在续写 · ${formatNumber(countChars(draft.text))} 字`);
          break;
        }
        case "done":
          sawDone = true;
          break;
        case "error":
          serverError = readString(data.error) || "模型调用失败";
          break;
        default:
          break;
      }
    });

    if (serverError) {
      finishGeneration("error", "生成失败：" + serverError);
      showToast(serverError, true);
      return;
    }
    if (controller.signal.aborted) {
      finishStopped();
      return;
    }
    if (!sawDone) throw new Error("连接意外中断，请重试");
    finishGeneration("done", `已生成 ${formatNumber(countChars(draft.text))} 字`);
  } catch (error) {
    if (controller.signal.aborted) {
      finishStopped();
      return;
    }
    const message =
      error instanceof TypeError ? "无法连接本机服务，请确认程序仍在运行" : error.message || "续写失败";
    finishGeneration("error", "生成失败：" + message);
    showToast(message, true);
  }
}

function discardDraft() {
  if (draft.controller) draft.controller.abort();
  draft = {
    text: "",
    streaming: false,
    appended: false,
    controller: null,
    targetChapterID: "",
    targetLabel: "",
  };
  draftPanel.hidden = true;
  draftText.textContent = "";
  draftStatus.textContent = "";
  draftTarget.textContent = "";
  draftTarget.hidden = true;
  appendButton.hidden = true;
  newChapterButton.hidden = true;
  retryButton.hidden = true;
  discardButton.hidden = true;
  stopButton.hidden = true;
  updateGenerateAvailability();
}

function showDialog(dialog) {
  if (typeof dialog.showModal === "function") dialog.showModal();
  else dialog.setAttribute("open", "");
}

function closeDialog(dialog) {
  if (typeof dialog.close === "function") dialog.close();
  else dialog.removeAttribute("open");
}

function openSettings() {
  baseUrlInput.value = config.baseUrl;
  modelInput.value = config.model;
  apiKeyInput.value = "";
  apiKeyInput.disabled = false;
  clearKeyInput.checked = false;
  contextCharsInput.value = String(config.contextChars || 12000);
  updateKeyHint();
  showDialog(settingsDialog);
  baseUrlInput.focus();
}

function updateKeyHint() {
  if (config.apiKeyFromEnv) {
    apiKeyHint.textContent = "已从环境变量 OPENAI_API_KEY 读取，将优先使用。";
  } else if (config.hasApiKey) {
    apiKeyHint.textContent = config.apiKeyHint
      ? `已保存：${config.apiKeyHint}。留空表示不修改。`
      : "已保存 API Key。留空表示不修改。";
  } else {
    apiKeyHint.textContent = "尚未保存 API Key。本地模型服务可以留空。";
  }
}

function bindEvents() {
  titleInput.addEventListener("input", () => {
    project.title = titleInput.value;
    titleRevision += 1;
    refreshHeader();
    scheduleTitleSave();
  });
  titleInput.addEventListener("blur", () => {
    if (titleSaveTimer || titleRevision > titleSavedRevision) void saveTitleNow();
  });

  chapterTitleInput.addEventListener("input", () => {
    if (!chapter.id) return;
    chapter.title = chapterTitleInput.value;
    updateChapterRowStats();
    markChapterDirty();
  });

  chapterContent.addEventListener("input", () => {
    if (!chapter.id) return;
    chapter.content = chapterContent.value;
    updateChapterStats();
    updateChapterRowStats();
    updateContextInfo();
    markChapterDirty();
  });

  chapterList.addEventListener("click", (event) => {
    if (!(event.target instanceof Element)) return;
    const row = event.target.closest(".chapter-row");
    if (!row) return;
    const id = row.dataset.id || "";
    if (event.target.closest(".chapter-remove")) {
      void deleteChapter(id);
      return;
    }
    void selectChapter(id);
  });

  chapterList.addEventListener("keydown", (event) => {
    if (event.key !== "Enter" && event.key !== " ") return;
    if (!(event.target instanceof Element)) return;
    const row = event.target.closest(".chapter-row");
    if (!row || event.target !== row) return;
    event.preventDefault();
    void selectChapter(row.dataset.id || "");
  });

  saveButton.addEventListener("click", () => {
    void manualSave();
  });
  deleteChapterButton.addEventListener("click", () => {
    if (chapter.id) void deleteChapter(chapter.id);
  });

  importButton.addEventListener("click", openImportDialog);
  emptyImportButton.addEventListener("click", openImportDialog);
  addChapterButton.addEventListener("click", () => {
    void addChapter();
  });
  emptyAddChapterButton.addEventListener("click", () => {
    void addChapter();
  });

  retryAnalysisButton.addEventListener("click", async () => {
    retryAnalysisButton.disabled = true;
    try {
      const data = await api("/api/analyze", { method: "POST" });
      applyProjectMeta(data || {});
      ensureAnalysisPolling();
      showToast("已重新提交分析");
    } catch (error) {
      showToast("提交分析失败：" + error.message, true);
    } finally {
      retryAnalysisButton.disabled = false;
    }
  });

  importFile.addEventListener("change", async () => {
    const file = importFile.files && importFile.files[0];
    if (!file) return;
    importHint.textContent = "正在读取文件…";
    try {
      const text = await readTextFile(file);
      importText.value = text;
      if (!importTitleInput.value.trim()) {
        importTitleInput.value = file.name.replace(/\.(txt|md|markdown)$/i, "");
      }
      updateImportHint();
    } catch (error) {
      importHint.textContent = "读取文件失败。";
      showToast("读取文件失败：" + error.message, true);
    }
  });
  importText.addEventListener("input", updateImportHint);

  importForm.addEventListener("submit", async (event) => {
    event.preventDefault();
    const text = importText.value;
    if (!text.trim()) {
      showToast("请选择文件或粘贴正文", true);
      return;
    }
    if (project.chapters.length && !window.confirm("导入会替换当前全部章节，确定继续？")) {
      return;
    }
    importSubmitButton.disabled = true;
    importSubmitButton.textContent = "导入中…";
    try {
      const data = await api("/api/import", {
        method: "POST",
        body: JSON.stringify({ title: importTitleInput.value.trim(), text }),
      });
      projectEpoch += 1;
      chapterSwitchToken += 1;
      clearChapterSelection();
      clearTimeout(titleSaveTimer);
      titleSaveTimer = 0;
      titleRevision = 0;
      titleSavedRevision = 0;
      if (data && data.project) applyProjectMeta(data.project);
      closeDialog(importDialog);
      await openChapter(lastChapterID());
      ensureAnalysisPolling();
      showToast(`已导入 ${formatNumber(data && data.importedChapters ? data.importedChapters : 0)} 章`);
    } catch (error) {
      showToast("导入失败：" + error.message, true);
    } finally {
      importSubmitButton.disabled = false;
      importSubmitButton.textContent = "导入并分析";
    }
  });

  modelBadge.addEventListener("click", openSettings);
  settingsButton.addEventListener("click", openSettings);

  clearKeyInput.addEventListener("change", () => {
    apiKeyInput.disabled = clearKeyInput.checked;
    if (clearKeyInput.checked) apiKeyInput.value = "";
  });

  settingsForm.addEventListener("submit", async (event) => {
    event.preventDefault();
    const baseUrl = baseUrlInput.value.trim().replace(/\/+$/, "");
    const model = modelInput.value.trim();
    const contextChars = Number(contextCharsInput.value);

    if ((baseUrl === "") !== (model === "")) {
      showToast("Base URL 和模型名需要同时填写", true);
      return;
    }
    if (baseUrl) {
      let parsed = null;
      try {
        parsed = new URL(baseUrl);
      } catch {
        parsed = null;
      }
      if (!parsed || (parsed.protocol !== "http:" && parsed.protocol !== "https:") || !parsed.host) {
        showToast("Base URL 需要是 http:// 或 https:// 地址", true);
        baseUrlInput.focus();
        return;
      }
    }
    if (!Number.isFinite(contextChars) || contextChars < 1000 || contextChars > 100000) {
      showToast("正文带入上限需要在 1,000 到 100,000 字之间", true);
      contextCharsInput.focus();
      return;
    }

    const submitButton = settingsForm.querySelector('button[type="submit"]');
    submitButton.disabled = true;
    try {
      const saved = await api("/api/config", {
        method: "PUT",
        body: JSON.stringify({
          baseUrl,
          model,
          apiKey: apiKeyInput.value,
          clearApiKey: clearKeyInput.checked,
          contextChars,
        }),
      });
      applyConfig(saved || {});
      closeDialog(settingsDialog);
      showToast("模型设置已保存");
      window.setTimeout(() => {
        void refreshProject();
      }, 900);
    } catch (error) {
      showToast("保存设置失败：" + error.message, true);
    } finally {
      submitButton.disabled = false;
    }
  });

  document.querySelectorAll("[data-close-dialog]").forEach((button) => {
    button.addEventListener("click", () => {
      const dialog = document.getElementById(button.dataset.closeDialog || "");
      if (dialog) closeDialog(dialog);
    });
  });

  document.querySelectorAll("dialog").forEach((dialog) => {
    dialog.addEventListener("click", (event) => {
      if (event.target === dialog) closeDialog(dialog);
    });
  });

  generateButton.addEventListener("click", () => {
    void startGeneration();
  });
  stopButton.addEventListener("click", () => {
    if (draft.controller) {
      draft.controller.abort();
      setDraftStatus("正在停止…");
    }
  });
  retryButton.addEventListener("click", () => {
    void startGeneration();
  });
  appendButton.addEventListener("click", () => {
    void appendDraft();
  });
  newChapterButton.addEventListener("click", () => {
    void saveDraftAsChapter();
  });
  discardButton.addEventListener("click", discardDraft);

  document.addEventListener("keydown", (event) => {
    const modifier = event.ctrlKey || event.metaKey;
    if (!modifier) return;
    if (event.key === "Enter") {
      if (document.querySelector("dialog[open]")) return;
      event.preventDefault();
      void startGeneration();
    } else if (event.key.toLowerCase() === "s") {
      event.preventDefault();
      void manualSave();
    }
  });

  document.addEventListener("visibilitychange", () => {
    if (!document.hidden) void refreshProject();
  });

  window.addEventListener("beforeunload", (event) => {
    if (chapterDirtyRevision > chapterSavedRevision || titleRevision > titleSavedRevision) {
      event.preventDefault();
      event.returnValue = "";
    }
  });
}

async function init() {
  bindEvents();
  await loadConfig();
  await loadProject();
  updateConfigUI();
  updateContextInfo();
}

void init();
