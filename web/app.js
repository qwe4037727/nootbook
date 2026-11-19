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
const useMemoryInput = $("useMemory");
const usePlanInput = $("usePlan");
const useRecallSelect = $("useRecall");
const memoryPreviewButton = $("memoryPreviewButton");
const memoryPreview = $("memoryPreview");
const memoryPreviewBody = $("memoryPreviewBody");
const memoryPreviewClose = $("memoryPreviewClose");

const draftPanel = $("draftPanel");
const draftStatus = $("draftStatus");
const draftTarget = $("draftTarget");
const draftText = $("draftText");
const appendButton = $("appendButton");
const newChapterButton = $("newChapterButton");
const persistTargetWrap = $("persistTargetWrap");
const persistTarget = $("persistTarget");
const persistDraftButton = $("persistDraftButton");
const retryButton = $("retryButton");
const discardButton = $("discardButton");
const stopButton = $("stopButton");

const draftBoxButton = $("draftBoxButton");
const draftBoxCount = $("draftBoxCount");
const draftBoxDialog = $("draftBoxDialog");
const draftStatusFilter = $("draftStatusFilter");
const draftRefreshButton = $("draftRefreshButton");
const draftList = $("draftList");
const draftDetailSection = $("draftDetailSection");
const draftDetailTitle = $("draftDetailTitle");
const draftDetailBadge = $("draftDetailBadge");
const draftTitleInput = $("draftTitleInput");
const draftContentInput = $("draftContentInput");
const draftDetailMeta = $("draftDetailMeta");
const draftSaveEditButton = $("draftSaveEditButton");
const draftExtractButton = $("draftExtractButton");
const draftConfirmButton = $("draftConfirmButton");
const draftRejectButton = $("draftRejectButton");
const draftOpenChapterButton = $("draftOpenChapterButton");
const draftRejectField = $("draftRejectField");
const draftRejectReason = $("draftRejectReason");
const draftChangeSet = $("draftChangeSet");
const draftDetailStatus = $("draftDetailStatus");

const agentCenterButton = $("agentCenterButton");
const agentCenterDialog = $("agentCenterDialog");
const agentRefreshButton = $("agentRefreshButton");
const agentConfigList = $("agentConfigList");
const agentStatsList = $("agentStatsList");
const agentRunList = $("agentRunList");
const agentRunHint = $("agentRunHint");
const agentReportList = $("agentReportList");
const foreshadowList = $("foreshadowList");
const agentProposalList = $("agentProposalList");
const draftMemorySyncDialog = $("draftMemorySyncDialog");
const draftMemorySyncReextractButton = $("draftMemorySyncReextractButton");
const draftMemorySyncBodyOnlyButton = $("draftMemorySyncBodyOnlyButton");
const draftMemorySyncCancelButton = $("draftMemorySyncCancelButton");
const draftMemorySyncCancel = $("draftMemorySyncCancel");

const imageCenterButton = $("imageCenterButton");
const imageCenterDialog = $("imageCenterDialog");
const imageKindInput = $("imageKindInput");
const imageEntryField = $("imageEntryField");
const imageEntryInput = $("imageEntryInput");
const imageChapterField = $("imageChapterField");
const imageChapterInput = $("imageChapterInput");
const imageStyleInput = $("imageStyleInput");
const imageNegativeInput = $("imageNegativeInput");
const imageSizeInput = $("imageSizeInput");
const imageStepsInput = $("imageStepsInput");
const imageSeedInput = $("imageSeedInput");
const imagePromptInput = $("imagePromptInput");
const imagePreviewButton = $("imagePreviewButton");
const imageGenerateButton = $("imageGenerateButton");
const imageRefreshButton = $("imageRefreshButton");
const imageAssetKindFilter = $("imageAssetKindFilter");
const imageAssetList = $("imageAssetList");
const imageStatusText = $("imageStatusText");
const imagePromptPreview = $("imagePromptPreview");
const imageModelButton = $("imageModelButton");
const imageModelDialog = $("imageModelDialog");
const imageModelForm = $("imageModelForm");
const imageModelBackend = $("imageModelBackend");
const imageModelBaseUrl = $("imageModelBaseUrl");
const imageModelName = $("imageModelName");
const imageModelApiKey = $("imageModelApiKey");
const imageModelKeyHint = $("imageModelKeyHint");
const imageModelClearKey = $("imageModelClearKey");
const imageModelStyle = $("imageModelStyle");
const imageModelNegative = $("imageModelNegative");
const imageModelWidth = $("imageModelWidth");
const imageModelHeight = $("imageModelHeight");
const imageModelSteps = $("imageModelSteps");
const imageModelTimeout = $("imageModelTimeout");

const exportButton = $("exportButton");
const exportDialog = $("exportDialog");
const exportFormat = $("exportFormat");
const exportTitleInput = $("exportTitleInput");
const exportImagesField = $("exportImagesField");
const exportIncludeImages = $("exportIncludeImages");
const exportSubmitButton = $("exportSubmitButton");
const exportRefreshButton = $("exportRefreshButton");
const exportStatus = $("exportStatus");
const exportList = $("exportList");

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

const planButton = $("planButton");
const planDialog = $("planDialog");
const planVolumeCount = $("planVolumeCount");
const planEndingTone = $("planEndingTone");
const planAvoidTropes = $("planAvoidTropes");
const planExtra = $("planExtra");
const planGenerateButton = $("planGenerateButton");
const planRefreshButton = $("planRefreshButton");
const planStatusText = $("planStatusText");
const planTree = $("planTree");
const planCountBadge = $("planCountBadge");
const planRevisionBadge = $("planRevisionBadge");

const memoryReviewButton = $("memoryReviewButton");
const memoryReviewDialog = $("memoryReviewDialog");
const extractForceInput = $("extractForceInput");
const extractStartButton = $("extractStartButton");
const reconcileButton = $("reconcileButton");
const extractStatusText = $("extractStatusText");
const changesetList = $("changesetList");
const changesetDetail = $("changesetDetail");
const candidateList = $("candidateList");
const refreshReviewButton = $("refreshReviewButton");

let reviewExtractTimer = 0;

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
  persisted: false,
  persistedDraftID: "",
  controller: null,
  targetChapterID: "",
  targetLabel: "",
};
let toastTimer = 0;

let persistedDraft = null;
let draftDetailBusy = false;

let features = { images: false, export: false };
let imageEntries = [];
let imageAssetsTimer = 0;
let imageBusy = false;

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
    const error = new Error(message);
    error.code = data && typeof data.code === "string" ? data.code : "";
    error.status = response.status;
    throw error;
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
      if (chapterDirtyRevision > chapterSavedRevision) {
        meta.title = chapter.title;
        meta.charCount = countChars(chapter.content);
      }
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
  $("importMode").value = project.chapters.length ? "append" : "overwrite";
  importText.value = "";
  updateImportHint();
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
  const append = $("importMode").value === "append";
  const hasChapters = project.chapters.length > 0;
  importTitleInput.disabled = append && hasChapters;
  if (!importSubmitButton.disabled) importSubmitButton.textContent = append ? "追加并分析" : "导入并分析";
  importHint.textContent = chars
    ? `已读取 ${formatNumber(chars)} 字。${append ? "新章节将追加到书末。" : "导入后会自动拆分章节并开始分析。"}`
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
  const noSave = draft.appended || draft.persisted;
  stopButton.hidden = phase !== "streaming";
  updateGenerateAvailability();
  appendButton.hidden = !(phase === "done" || phase === "stopped") || noSave || !hasText;
  newChapterButton.hidden = appendButton.hidden;
  persistDraftButton.hidden = appendButton.hidden;
  persistTargetWrap.hidden = appendButton.hidden;
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

// ---- 续写记忆注入（I3-5，iter3-详设 §8）----

const memoryKindLabels = {
  world_state: "世界规则",
  state: "人物当前状态",
  cognition: "人物认知",
  thread: "未解决伏笔",
  event: "关键事件",
  plan: "近期计划",
};

const memoryKindOrder = ["world_state", "state", "cognition", "thread", "event", "plan"];

function memoryDisabledText(reason) {
  switch (reason) {
    case "safe_state":
      return "记忆链校验异常，本次未注入";
    case "read_error":
      return "记忆文件读取失败，本次未注入";
    case "memory_absent":
      return "还没有已确认的剧情记忆，本次未注入";
    default:
      return "本次未注入记忆";
  }
}

function hideMemoryPreview() {
  memoryPreview.hidden = true;
  memoryPreviewBody.textContent = "";
}

function recallParam() {
  switch (useRecallSelect.value) {
    case "on":
      return true;
    case "off":
      return false;
    default:
      return null;
  }
}

function memoryPreviewParams() {
  return {
    direction: directionInput.value.trim(),
    targetChars: Number(targetCharsSelect.value) || 800,
    useMemory: useMemoryInput.checked,
    usePlan: usePlanInput.checked,
    useRecall: recallParam(),
  };
}

function renderMemoryPreview(data) {
  memoryPreviewBody.textContent = "";
  memoryPreview.hidden = false;
  const memoryEnabled = data && data.memoryEnabled === true;
  const planEnabled = data && data.planEnabled === true;
  const recallEnabled = data && data.recallEnabled === true;
  if (!memoryEnabled && !planEnabled && !recallEnabled) {
    const empty = document.createElement("p");
    empty.className = "memory-empty";
    const reason = readString(data && data.memoryDisabledReason);
    if (reason) empty.textContent = memoryDisabledText(reason);
    else if (!useMemoryInput.checked && !usePlanInput.checked) empty.textContent = "记忆注入与计划对齐均已关闭，前文也没有可召回的片段。";
    else if (usePlanInput.checked && useMemoryInput.checked) empty.textContent = "还没有已确认的剧情记忆、已激活的计划或可召回的前文片段。";
    else if (usePlanInput.checked) empty.textContent = "还没有已激活的计划；激活后才会参与续写。";
    else empty.textContent = "本次不注入记忆，前文也没有检索到相关片段。";
    if (useRecallSelect.value === "on") empty.textContent += " 已要求前文召回但无注入：旧章节没有命中片段，或正文窗口不让位。";
    memoryPreviewBody.append(empty);
    return;
  }

  if (planEnabled) {
    const group = document.createElement("div");
    group.className = "memory-group";
    const title = document.createElement("h4");
    title.textContent = `已激活计划（${Number(data.planCount) || 0} 条，仅约束走向）`;
    const text = document.createElement("pre");
    text.className = "memory-plan-section";
    text.textContent = readString(data.planSection);
    group.append(title, text);
    memoryPreviewBody.append(group);
  }

  if (memoryEnabled) {
    const groups = new Map(memoryKindOrder.map((kind) => [kind, []]));
    const records = Array.isArray(data.records) ? data.records : [];
    for (const record of records) {
      if (!record) continue;
      const kind = readString(record.kind);
      if (!groups.has(kind)) groups.set(kind, []);
      groups.get(kind).push(record);
    }
    for (const [kind, items] of groups) {
      if (!items.length) continue;
      const group = document.createElement("div");
      group.className = "memory-group";
      const title = document.createElement("h4");
      title.textContent = memoryKindLabels[kind] || kind;
      group.append(title);
      for (const item of items) {
        const row = document.createElement("div");
        row.className = "memory-item";
        const text = document.createElement("span");
        text.className = "memory-item-text";
        text.textContent = readString(item.text);
        row.append(text);
        if (item.needsReview) {
          const badge = document.createElement("span");
          badge.className = "memory-badge";
          badge.textContent = "待复核";
          row.append(badge);
        }
        const id = document.createElement("span");
        id.className = "memory-item-id";
        id.textContent = readString(item.id);
        row.append(id);
        group.append(row);
      }
      memoryPreviewBody.append(group);
    }
  }

  if (recallEnabled) {
    const group = document.createElement("div");
    group.className = "memory-group";
    const title = document.createElement("h4");
    const chapters = (Array.isArray(data.recallChapters) ? data.recallChapters : [])
      .map((value) => Number(value) || 0)
      .filter((value) => value > 0);
    title.textContent = chapters.length
      ? `前文相关片段（来自第 ${chapters.join("、")} 章）`
      : "前文相关片段";
    const text = document.createElement("pre");
    text.className = "memory-plan-section memory-recall-section";
    text.textContent = readString(data.recallSection);
    group.append(title, text);
    memoryPreviewBody.append(group);
  }

  const parts = [];
  if (memoryEnabled) {
    const counts = data.counts || {};
    const recalled = ["states", "events", "threads", "cognitions", "plans"]
      .map((key) => Number(counts[key]) || 0)
      .reduce((sum, value) => sum + value, 0);
    parts.push(`已用 ${formatNumber(Number(data.used) || 0)} / ${formatNumber(Number(data.budget) || 0)} 字`);
    parts.push(`检索 ${recalled} 条`);
    if (Number(data.droppedByBudget)) parts.push(`预算裁掉 ${Number(data.droppedByBudget)} 条`);
    if (Number(data.skippedReview)) parts.push(`待复核跳过 ${Number(data.skippedReview)} 条`);
    if (Number(data.skippedAmbiguous)) parts.push(`歧义提及跳过 ${Number(data.skippedAmbiguous)} 条`);
    if (Number(data.memoryRevision)) parts.push(`revision ${Number(data.memoryRevision)}`);
  }
  if (planEnabled) parts.push(`计划 ${Number(data.planCount) || 0} 条`);
  if (recallEnabled) {
    parts.push(`前文召回 ${Number(data.recallHits) || 0} 段 · ${formatNumber(Number(data.recallUsedChars) || 0)}/${formatNumber(Number(data.recallBudgetChars) || 0)} 字`);
  }
  if (useRecallSelect.value === "off") parts.push("前文召回已关闭");
  else if (useRecallSelect.value === "on" && !recallEnabled) {
    parts.push("已要求前文召回但无注入：旧章节没有命中片段，或正文窗口不让位");
  }
  const foot = document.createElement("p");
  foot.className = "memory-preview-foot";
  foot.textContent = parts.join(" · ");
  memoryPreviewBody.append(foot);
}

async function loadMemoryPreview({ quiet = false } = {}) {
  if (!useMemoryInput.checked && !usePlanInput.checked && useRecallSelect.value !== "on") {
    hideMemoryPreview();
    if (!quiet) showToast("记忆注入与计划对齐均已关闭，本次不附加上下文");
    return;
  }
  memoryPreviewButton.disabled = true;
  try {
    const data = await api("/api/continue/preview", {
      method: "POST",
      body: JSON.stringify(memoryPreviewParams()),
    });
    renderMemoryPreview(data);
  } catch (error) {
    hideMemoryPreview();
    if (!quiet) showToast(error.message || "无法预览本次记忆", true);
  } finally {
    memoryPreviewButton.disabled = false;
  }
}

function memoryMetaSummary(meta) {
  const planNote = meta && meta.planAligned === true ? `计划 ${Number(meta.planCount) || 0} 条` : "";
  const recallChapters = meta && Array.isArray(meta.recallChapters) ? meta.recallChapters.filter((value) => Number(value) > 0) : [];
  const recallNote = meta && meta.recallEnabled === true
    ? `前文召回 ${recallChapters.length} 段 · ${formatNumber(Number(meta.recallUsedChars) || 0)} 字`
    : "";
  if (!meta || meta.memoryEnabled !== true) {
    const reason = readString(meta && meta.memoryDisabledReason);
    const base = reason ? memoryDisabledText(reason) : planNote;
    return [base, recallNote].filter(Boolean).join(" · ");
  }
  const counts = meta.recalledByKind || {};
  const total = ["states", "events", "threads", "cognitions", "plans"]
    .map((key) => Number(counts[key]) || 0)
    .reduce((sum, value) => sum + value, 0);
  let text = `记忆 ${total} 条`;
  const used = Number(meta.memoryUsedChars) || 0;
  if (used) text += ` · ${formatNumber(used)} 字`;
  const dropped = Number(meta.droppedByBudget) || 0;
  if (dropped) text += ` · 裁掉 ${dropped} 条`;
  const skipped = Number(meta.skippedReview) || 0;
  if (skipped) text += ` · 待复核跳过 ${skipped} 条`;
  if (planNote) text += ` · ${planNote}`;
  if (recallNote) text += ` · ${recallNote}`;
  return text;
}

// ---- 大纲规划（F20–F23）----

const planLevelLabels = { global: "全书", volume: "分卷", near_term: "近期" };
const planStatusLabels = {
  proposed: "草案",
  active: "生效",
  paused: "暂停",
  completed: "完成",
  superseded: "已取代",
  cancelled: "已取消",
};
const planReviewReasons = {
  evidence_stale: "依据章节已改动，请复核计划",
  quote_mismatch: "原文引用与正文不符，请复核",
  parent_completed: "上级计划已完成，子计划需要复核",
};

let planBusy = false;

function openPlanDialog() {
  showDialog(planDialog);
  void loadPlanTree();
}

async function loadPlanTree() {
  planRefreshButton.disabled = true;
  try {
    const tree = await api("/api/plans");
    renderPlanTree(tree || {});
  } catch (error) {
    planTree.replaceChildren();
    const empty = document.createElement("p");
    empty.className = "empty-state small";
    empty.textContent = "读取计划失败：" + error.message;
    planTree.append(empty);
    showToast("读取计划失败：" + error.message, true);
  } finally {
    planRefreshButton.disabled = false;
  }
}

function renderPlanTree(tree) {
  planTree.replaceChildren();
  const roots = Array.isArray(tree.roots) ? tree.roots : [];
  const count = Number(tree.count) || 0;
  planCountBadge.textContent = count ? `${count} 条计划` : "";
  planRevisionBadge.textContent = `revision ${Number(tree.memoryRevision) || 0}`;
  if (!roots.length) {
    const empty = document.createElement("p");
    empty.className = "empty-state small";
    empty.textContent = "还没有计划。生成草案后，确认激活才会参与续写对齐。";
    planTree.append(empty);
    return;
  }
  for (const root of roots) planTree.append(planNodeElement(root));
}

function planNodeElement(node) {
  const plan = (node && node.plan) || {};
  const status = readString(plan.status);
  const card = document.createElement("article");
  card.className = "plan-node" + (status === "active" ? " is-active" : "");

  const head = document.createElement("div");
  head.className = "plan-node-head";
  const level = document.createElement("span");
  level.className = "plan-level";
  level.textContent = planLevelLabels[readString(plan.level)] || readString(plan.level);
  const badge = document.createElement("span");
  badge.className = "plan-status is-" + (status || "proposed");
  badge.textContent = planStatusLabels[status] || status;
  head.append(level, badge);

  const title = document.createElement("h4");
  title.className = "plan-title";
  title.textContent = readString(plan.title) || "未命名计划";
  card.append(head, title);

  const objective = readString(plan.objective);
  if (objective) {
    const p = document.createElement("p");
    p.className = "plan-objective";
    p.textContent = objective;
    card.append(p);
  }

  const constraints = Array.isArray(plan.constraints) ? plan.constraints.filter(Boolean) : [];
  const beats = Array.isArray(plan.candidateBeats) ? plan.candidateBeats.filter(Boolean) : [];
  if (constraints.length || beats.length) {
    const meta = document.createElement("div");
    meta.className = "plan-meta";
    if (constraints.length) {
      const row = document.createElement("p");
      row.textContent = "约束：" + constraints.join("；");
      meta.append(row);
    }
    if (beats.length) {
      const row = document.createElement("p");
      row.textContent = "候选发展：" + beats.join("；");
      meta.append(row);
    }
    card.append(meta);
  }

  if (plan.needsReview) {
    const review = document.createElement("p");
    review.className = "plan-review";
    const reason = readString(plan.reviewReason);
    review.textContent = planReviewReasons[reason] || "计划依据已变化，请复核后再使用";
    card.append(review);
  }

  const actions = [];
  if (status === "proposed" || status === "paused") actions.push(["activate", "激活", "btn-primary"]);
  if (status === "active") actions.push(["pause", "暂停", "btn-quiet"]);
  if (status === "active" || status === "paused") actions.push(["complete", "完成", "btn-ghost"]);
  if (status === "proposed" || status === "active" || status === "paused") actions.push(["cancel", "取消", "btn-quiet"]);
  if (actions.length) {
    const row = document.createElement("div");
    row.className = "plan-node-actions";
    for (const [action, label, className] of actions) {
      const button = document.createElement("button");
      button.type = "button";
      button.className = "btn btn-sm " + className;
      button.textContent = label;
      button.addEventListener("click", () => {
        void updatePlanStatus(plan.id, action, button);
      });
      row.append(button);
    }
    card.append(row);
  }

  const children = Array.isArray(node && node.children) ? node.children : [];
  if (children.length) {
    const list = document.createElement("div");
    list.className = "plan-children";
    for (const child of children) list.append(planNodeElement(child));
    card.append(list);
  }
  return card;
}

async function updatePlanStatus(id, action, button) {
  if (planBusy) return;
  const status = action === "activate" ? "active" : action;
  planBusy = true;
  button.disabled = true;
  try {
    const url =
      action === "activate"
        ? `/api/plans/${encodeURIComponent(id)}/activate`
        : `/api/plans/${encodeURIComponent(id)}/status`;
    const body = action === "activate" ? "{}" : JSON.stringify({ status });
    await api(url, { method: "POST", body });
    showToast(action === "activate" ? "计划已激活，将参与续写对齐" : `计划已标记为「${planStatusLabels[status] || status}」`);
    hideMemoryPreview();
    await loadPlanTree();
  } catch (error) {
    showToast("更新计划失败：" + error.message, true);
    button.disabled = false;
  } finally {
    planBusy = false;
  }
}

async function generatePlanDrafts() {
  if (planGenerateButton.disabled) return;
  if (!configured()) {
    showToast("请先配置模型接口", true);
    openSettings();
    return;
  }
  if (!project.chapters.length) {
    showToast("请先导入文稿或新建章节，再生成计划", true);
    return;
  }
  const volumeCount = Number(planVolumeCount.value) || 3;
  if (volumeCount < 1 || volumeCount > 12) {
    showToast("分卷数量需要在 1 到 12 之间", true);
    planVolumeCount.focus();
    return;
  }

  planGenerateButton.disabled = true;
  planGenerateButton.textContent = "生成中…";
  planStatusText.textContent = "正在调用模型生成全书与分卷草案，通常需要十几秒…";
  try {
    const data = await api("/api/plans/generate", {
      method: "POST",
      body: JSON.stringify({
        volumeCount,
        endingTone: planEndingTone.value.trim(),
        avoidTropes: planAvoidTropes.value.trim(),
        extra: planExtra.value.trim(),
      }),
    });
    const created = Array.isArray(data && data.plans) ? data.plans.length : 0;
    planStatusText.textContent = `已生成 ${created} 条草案（revision ${Number(data && data.memoryRevision) || 0}）。激活后才会参与续写。`;
    showToast(`已生成 ${created} 条计划草案`);
    await loadPlanTree();
  } catch (error) {
    planStatusText.textContent = "";
    showToast("生成计划失败：" + error.message, true);
  } finally {
    planGenerateButton.disabled = false;
    planGenerateButton.textContent = "生成计划草案";
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

  await loadMemoryPreview({ quiet: true });

  const target = project.chapters[project.chapters.length - 1];
  draft = {
    text: "",
    streaming: true,
    appended: false,
    persisted: false,
    persistedDraftID: "",
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
        useMemory: useMemoryInput.checked,
        usePlan: usePlanInput.checked,
        useRecall: recallParam(),
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
    let doneTruncated = false;

    await readSSE(response.body, (event, data) => {
      switch (event) {
        case "meta": {
          applyAnalysisFromMeta(data);
          const contextChars = Number(data.contextChars) || 0;
          let message = `已连接 · 带入正文 ${formatNumber(contextChars)} 字`;
          if (data.truncated) message += "（截取结尾）";
          const memoryNote = memoryMetaSummary(data);
          if (memoryNote) message += ` · ${memoryNote}`;
          setDraftStatus(message);
          const disabledReason = readString(data.memoryDisabledReason);
          if (disabledReason === "safe_state" || disabledReason === "read_error") {
            showToast(memoryDisabledText(disabledReason), true);
          }
          const skippedReview = Number(data.skippedReview) || 0;
          if (skippedReview > 0) {
            showToast(`有 ${skippedReview} 条记忆因待复核未注入，请在「记忆审阅」中处理`);
          }
          if (useRecallSelect.value === "on" && data.recallEnabled !== true) {
            showToast("已要求前文召回，但本次没有注入：旧章节没有命中片段，或正文窗口不让位", true);
          }
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
          doneTruncated = Boolean(data && data.truncated);
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
    finishGeneration(
      "done",
      doneTruncated
        ? `已生成 ${formatNumber(countChars(draft.text))} 字 · 输出达到模型长度上限，已截断`
        : `已生成 ${formatNumber(countChars(draft.text))} 字`,
    );
    if (doneTruncated) {
      showToast("输出达到模型长度上限，已截断；请调大输出上限或缩短章节后重试", true);
    }
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
    persisted: false,
    persistedDraftID: "",
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
  persistDraftButton.hidden = true;
  persistTargetWrap.hidden = true;
  retryButton.hidden = true;
  discardButton.hidden = true;
  stopButton.hidden = true;
  updateGenerateAvailability();
}

// ---- 草稿台（G6）：正文草稿与记忆变更联合确认 ----

function normalizeDraft(raw) {
  if (!raw || typeof raw !== "object" || !readString(raw.id)) return null;
  const status = readString(raw.status);
  return {
    id: readString(raw.id),
    title: readString(raw.title),
    content: readString(raw.content),
    status: ["pending_confirmation", "committed", "rejected"].includes(status) ? status : "pending_confirmation",
    draftRevision: Math.max(1, Number(raw.draftRevision) || 1),
    targetChapterId: readString(raw.targetChapterId),
    newChapter: Boolean(raw.newChapter),
    baseChapterRevision: Number(raw.baseChapterRevision) || 0,
    baseMemoryRevision: Number(raw.baseMemoryRevision) || 0,
    memoryChangeSetId: readString(raw.memoryChangeSetId),
    commitId: readString(raw.commitId),
    chapterRevision: Number(raw.chapterRevision) || 0,
    rejectReason: readString(raw.rejectReason),
    createdAt: readString(raw.createdAt),
    updatedAt: readString(raw.updatedAt),
  };
}

function draftStatusText(status) {
  switch (status) {
    case "pending_confirmation":
      return "待确认";
    case "committed":
      return "已提交";
    case "rejected":
      return "已拒绝";
    default:
      return String(status || "未知");
  }
}

function draftTargetLabel(d) {
  if (!d) return "";
  if (d.newChapter) return "新章节";
  const meta = project.chapters.find((item) => item.id === d.targetChapterId);
  return meta ? "并入" + chapterLabel(meta) : "章节 " + d.targetChapterId;
}

async function refreshPendingDraftCount() {
  try {
    const pending = await api("/api/drafts?status=pending_confirmation");
    const count = Array.isArray(pending) ? pending.length : 0;
    draftBoxCount.textContent = String(count);
    draftBoxCount.hidden = count === 0;
    draftBoxButton.title = count ? `有 ${count} 份待确认草稿` : "查看草稿台";
  } catch {
    // 服务未就绪时保持上次计数。
  }
}

async function openDraftBox(focusID = "") {
  if (!draftBoxDialog.open) showDialog(draftBoxDialog);
  persistedDraft = null;
  draftDetailSection.hidden = true;
  draftStatusFilter.value = "pending_confirmation";
  await loadDraftList();
  if (focusID) {
    await openDraftDetail(focusID);
  } else {
    const first = draftList.querySelector("[data-draft-id]");
    if (first) await openDraftDetail(first.dataset.draftId);
  }
  await refreshPendingDraftCount();
}

async function loadDraftList() {
  const status = draftStatusFilter.value;
  draftList.replaceChildren();
  try {
    const summaries = await api("/api/drafts" + (status ? "?status=" + encodeURIComponent(status) : ""));
    renderDraftList(Array.isArray(summaries) ? summaries : []);
  } catch (error) {
    showToast("读取草稿列表失败：" + error.message, true);
  }
}

function renderDraftList(summaries) {
  draftList.replaceChildren();
  if (!summaries.length) {
    const empty = document.createElement("p");
    empty.className = "empty-state small";
    empty.textContent = draftStatusFilter.value === "pending_confirmation"
      ? "没有待确认稿。生成续写后可「存入草稿台」，在这里提取记忆并联合提交。"
      : "没有符合条件的草稿。";
    draftList.append(empty);
    return;
  }
  const showStatus = draftStatusFilter.value === "";
  const fragment = document.createDocumentFragment();
  for (const raw of summaries) {
    const d = normalizeDraft(raw);
    if (!d) continue;
    const row = document.createElement("button");
    row.type = "button";
    row.className = "changeset-row";
    row.dataset.draftId = d.id;
    if (persistedDraft && persistedDraft.id === d.id) row.classList.add("is-active");

    const meta = document.createElement("span");
    meta.className = "changeset-meta";
    const flags = [draftTargetLabel(d), `r${d.draftRevision}`];
    flags.push(d.memoryChangeSetId ? "已提取记忆" : "未提取记忆");
    if (showStatus) flags.push(draftStatusText(d.status));
    meta.textContent = `${d.title || "未命名章节"} · ${flags.join(" · ")}`;

    const time = document.createElement("span");
    time.className = "changeset-time";
    time.textContent = formatTime(d.updatedAt || d.createdAt);
    row.append(meta, time);
    row.addEventListener("click", () => void openDraftDetail(d.id));
    fragment.append(row);
  }
  draftList.append(fragment);
}

function markActiveDraftRow() {
  for (const row of draftList.querySelectorAll("[data-draft-id]")) {
    row.classList.toggle("is-active", Boolean(persistedDraft && row.dataset.draftId === persistedDraft.id));
  }
}

async function openDraftDetail(id) {
  if (draftDetailBusy) return;
  draftDetailStatus.textContent = "读取中…";
  draftChangeSet.hidden = true;
  draftChangeSet.replaceChildren();
  try {
    const data = await api("/api/drafts/" + encodeURIComponent(id));
    persistedDraft = normalizeDraft(data);
    if (!persistedDraft) throw new Error("草稿数据无效");
    renderDraftDetail();
    markActiveDraftRow();
  } catch (error) {
    draftDetailStatus.textContent = "";
    showToast("读取草稿失败：" + error.message, true);
  }
}

function renderDraftDetail() {
  if (!persistedDraft) return;
  const d = persistedDraft;
  const editable = d.status === "pending_confirmation";
  draftDetailSection.hidden = false;
  draftDetailTitle.textContent = d.title || "未命名章节";
  draftDetailBadge.textContent = `${draftStatusText(d.status)} · r${d.draftRevision}`;
  draftTitleInput.value = d.title;
  draftContentInput.value = d.content;
  draftTitleInput.disabled = !editable;
  draftContentInput.disabled = !editable;
  draftSaveEditButton.hidden = !editable;
  draftExtractButton.hidden = !editable;
  draftConfirmButton.hidden = !editable;
  draftRejectButton.hidden = !editable;
  draftRejectField.hidden = !editable;
  draftOpenChapterButton.hidden = d.status !== "committed";
  draftRejectReason.value = "";

  const parts = [
    `目标：${draftTargetLabel(d)}`,
    `正文基线 r${d.newChapter ? 0 : d.baseChapterRevision}`,
    `记忆基线 revision ${d.baseMemoryRevision}`,
    `更新于 ${formatTime(d.updatedAt || d.createdAt)}`,
  ];
  draftDetailMeta.textContent = parts.join(" · ");

  if (d.status === "committed") {
    draftDetailStatus.textContent = `已联合提交 · commit ${d.commitId} · 章节版本 r${d.chapterRevision}`;
  } else if (d.status === "rejected") {
    draftDetailStatus.textContent = d.rejectReason ? "已拒绝：" + d.rejectReason : "已拒绝";
  } else if (!d.memoryChangeSetId) {
    draftDetailStatus.textContent = "尚未提取记忆变更：建议先提取核对，再联合提交。";
  } else {
    draftDetailStatus.textContent = "已关联记忆变更：确认时会与正文放入同一次提交。";
  }

  if (d.memoryChangeSetId) {
    void loadDraftChangeSet(d.memoryChangeSetId);
  } else {
    draftChangeSet.hidden = true;
    draftChangeSet.replaceChildren();
  }
}

async function loadDraftChangeSet(id) {
  try {
    const cs = await api("/api/memory/changesets/" + encodeURIComponent(id));
    if (!persistedDraft || persistedDraft.memoryChangeSetId !== id) return;
    renderDraftChangeSet(cs || {});
  } catch (error) {
    if (!persistedDraft || persistedDraft.memoryChangeSetId !== id) return;
    draftChangeSet.hidden = false;
    draftChangeSet.replaceChildren();
    const note = document.createElement("p");
    note.className = "review-note";
    note.textContent = "读取关联记忆变更失败：" + error.message;
    draftChangeSet.append(note);
  }
}

function renderDraftChangeSet(cs) {
  draftChangeSet.hidden = false;
  draftChangeSet.replaceChildren();

  const head = document.createElement("div");
  head.className = "detail-head";
  const title = document.createElement("h4");
  title.textContent = "关联记忆变更 " + (cs.id || "");
  head.append(title);
  const badge = document.createElement("span");
  badge.className = "detail-status is-" + (cs.status || "proposed");
  badge.textContent = cs.status || "proposed";
  head.append(badge);
  draftChangeSet.append(head);

  const ops = Array.isArray(cs.ops) ? cs.ops : [];
  const conflicts = Array.isArray(cs.conflicts) ? cs.conflicts : [];
  const note = document.createElement("p");
  note.className = "review-note";
  note.textContent = `${ops.length} 操作 · ${conflicts.length} 冲突 · 基于记忆 revision ${Number(cs.basedOnMemoryRevision) || 0}`;
  draftChangeSet.append(note);

  for (const cf of conflicts) draftChangeSet.append(renderConflict(cf));
  if (ops.length) {
    const group = document.createElement("div");
    group.className = "op-group";
    const label = document.createElement("p");
    label.className = "group-title";
    label.textContent = `变更操作（${ops.length}）`;
    group.append(label);
    for (const op of ops) group.append(renderOp(op));
    draftChangeSet.append(group);
  } else if (!conflicts.length) {
    const empty = document.createElement("p");
    empty.className = "empty-state small";
    empty.textContent = "本次提取无记忆变更。";
    draftChangeSet.append(empty);
  }
}

function setDraftDetailBusy(busy, label) {
  draftDetailBusy = busy;
  const editable = !busy && persistedDraft && persistedDraft.status === "pending_confirmation";
  draftTitleInput.disabled = !editable;
  draftContentInput.disabled = !editable;
  draftSaveEditButton.disabled = busy;
  draftExtractButton.disabled = busy;
  draftConfirmButton.disabled = busy;
  draftRejectButton.disabled = busy;
  draftRefreshButton.disabled = busy;
  if (busy && label) draftDetailStatus.textContent = label;
}

async function handleDraftActionError(error, fallbackPrefix) {
  const friendly = {
    chapter_conflict: "目标章节已变化：草稿基于旧正文，请重新创建或重新提取草稿",
    revision_conflict: "记忆基线已变化：请重新提取后再确认",
    draft_needs_recheck: "草稿内容与记忆变更不一致：请重新提取",
    draft_conflict: "草稿版本已变化：已刷新到最新版本",
    draft_terminal: "草稿已处于终态，不能继续操作",
    analysis_running: "故事分析进行中，请稍后再试",
    config_missing: "请先在「模型设置」中填写 Base URL、模型名与 API Key",
  };
  const message = friendly[error.code] || error.message || fallbackPrefix;
  showToast(message, true);
  draftDetailBusy = false;
  await loadDraftList();
  if (persistedDraft) await openDraftDetail(persistedDraft.id);
}

async function saveDraftEdit() {
  if (!persistedDraft || draftDetailBusy || persistedDraft.status !== "pending_confirmation") return;
  const content = draftContentInput.value.trim();
  if (!content) {
    showToast("草稿正文不能为空", true);
    return;
  }
  setDraftDetailBusy(true, "保存中…");
  try {
    const data = await api("/api/drafts/" + encodeURIComponent(persistedDraft.id), {
      method: "PATCH",
      body: JSON.stringify({
        draftRevision: persistedDraft.draftRevision,
        title: draftTitleInput.value.trim(),
        content,
      }),
    });
    persistedDraft = normalizeDraft(data);
    renderDraftDetail();
    markActiveDraftRow();
    await loadDraftList();
    showToast("草稿已更新；已提取的记忆变更需重新提取");
  } catch (error) {
    await handleDraftActionError(error, "保存草稿失败");
  } finally {
    setDraftDetailBusy(false);
  }
}

async function extractDraftMemory() {
  if (!persistedDraft || draftDetailBusy || persistedDraft.status !== "pending_confirmation") return;
  if (!configured()) {
    showToast("请先配置模型接口", true);
    openSettings();
    return;
  }
  if (draftTitleInput.value.trim() !== persistedDraft.title || draftContentInput.value.trim() !== persistedDraft.content) {
    showToast("正文有未保存的修改，请先「保存修改」再提取", true);
    return;
  }
  setDraftDetailBusy(true, "正在提取记忆变更…");
  draftExtractButton.textContent = "提取中…";
  try {
    const data = await api("/api/drafts/" + encodeURIComponent(persistedDraft.id) + "/extract", {
      method: "POST",
      body: "{}",
    });
    if (data && data.draft) persistedDraft = normalizeDraft(data.draft);
    renderDraftDetail();
    if (data && data.changeset) renderDraftChangeSet(data.changeset);
    const ops = data && data.changeset && Array.isArray(data.changeset.ops) ? data.changeset.ops.length : 0;
    const conflicts = data && data.changeset && Array.isArray(data.changeset.conflicts) ? data.changeset.conflicts.length : 0;
    draftDetailStatus.textContent = `已提取 ${ops} 操作 · ${conflicts} 冲突；核对后确认提交。`;
    showToast(`已提取记忆变更：${ops} 操作 · ${conflicts} 冲突`);
    await loadDraftList();
  } catch (error) {
    await handleDraftActionError(error, "提取记忆变更失败");
  } finally {
    draftExtractButton.textContent = "提取记忆变更";
    setDraftDetailBusy(false);
  }
}

async function confirmPersistedDraft(memorySync = "") {
  if (!persistedDraft || draftDetailBusy || persistedDraft.status !== "pending_confirmation") return;
  if (!memorySync) {
    const saved = await flushChapterSave();
    if (!saved) {
      showToast("当前章节尚未保存，暂不能确认提交", true);
      return;
    }
    const action = persistedDraft.newChapter
      ? "将新建章节"
      : "将覆盖" + draftTargetLabel(persistedDraft).replace(/^并入/, "");
    if (!window.confirm(`确认联合提交？${action}，并同时写入已提取的记忆变更。`)) return;
  }
  setDraftDetailBusy(true, memorySync === "reextract" ? "正在重新提取记忆并提交…" : "正在联合提交…");
  try {
    const result = await api("/api/drafts/" + encodeURIComponent(persistedDraft.id) + "/confirm", {
      method: "POST",
      body: JSON.stringify({
        draftRevision: persistedDraft.draftRevision,
        expectedChapterRevision: persistedDraft.newChapter ? 0 : persistedDraft.baseChapterRevision,
        expectedMemoryRevision: 0,
        memorySync,
      }),
    });
    if (result && result.draft) persistedDraft = normalizeDraft(result.draft);
    if (result && result.project) applyProjectMeta(result.project);
    if (draft.persisted && draft.persistedDraftID && draft.persistedDraftID === persistedDraft.id) {
      finishGeneration("appended", "已联合提交 · 正文与记忆已写入");
    }
    renderDraftDetail();
    await loadDraftList();
    markActiveDraftRow();
    await refreshPendingDraftCount();
    const chapterID = result && result.chapter ? result.chapter.id : persistedDraft.targetChapterId;
    const memoryRevision = result ? Number(result.memoryRevision) || 0 : 0;
    const conflicts = result && Array.isArray(result.conflicts) ? result.conflicts.length : 0;
    const syncState = result && result.memorySyncState;
    const syncLabel = syncState === "body_only" ? "正文已提交（记忆未更新）" : "正文与记忆已提交";
    showToast(`${syncLabel} · commit ${result.commitId} · 记忆 revision ${memoryRevision}` + (conflicts ? ` · ${conflicts} 冲突` : ""));
    if (chapterID) {
      closeDialog(draftBoxDialog);
      await openChapter(chapterID);
    }
    ensureAnalysisPolling();
  } catch (error) {
    if (error.code === "draft_memory_stale" && !memorySync) {
      setDraftDetailBusy(false);
      const choice = await askDraftMemorySync();
      if (choice) await confirmPersistedDraft(choice);
      return;
    }
    await handleDraftActionError(error, "确认提交失败");
  } finally {
    setDraftDetailBusy(false);
  }
}

async function rejectPersistedDraft() {
  if (!persistedDraft || draftDetailBusy || persistedDraft.status !== "pending_confirmation") return;
  if (!window.confirm("拒绝后该草稿及其记忆变更将不再可用，确定拒绝？")) return;
  setDraftDetailBusy(true, "正在拒绝…");
  try {
    const data = await api("/api/drafts/" + encodeURIComponent(persistedDraft.id) + "/reject", {
      method: "POST",
      body: JSON.stringify({ reason: draftRejectReason.value.trim() }),
    });
    persistedDraft = normalizeDraft(data);
    if (draft.persisted && draft.persistedDraftID && draft.persistedDraftID === persistedDraft.id) {
      draft.persisted = false;
      draft.persistedDraftID = "";
      setDraftStatus("草稿已拒绝 · 可重新存入草稿台");
      setDraftPhase("done");
    }
    renderDraftDetail();
    markActiveDraftRow();
    await loadDraftList();
    await refreshPendingDraftCount();
    showToast("已拒绝草稿");
  } catch (error) {
    await handleDraftActionError(error, "拒绝草稿失败");
  } finally {
    setDraftDetailBusy(false);
  }
}

async function openCommittedDraftChapter() {
  if (!persistedDraft) return;
  const saved = await flushChapterSave();
  if (!saved) {
    showToast("当前章节尚未保存，暂不能切换", true);
    return;
  }
  closeDialog(draftBoxDialog);
  await openChapter(persistedDraft.targetChapterId);
}

async function saveGenerationToDraftBox() {
  if (draft.streaming || draft.appended || draft.persisted) return;
  const addition = draft.text.replace(/\s+$/u, "");
  if (!addition.trim()) {
    showToast("草稿为空，无法存入草稿台", true);
    return;
  }
  const saved = await flushChapterSave();
  if (!saved) {
    showToast("当前章节尚未保存，暂不能存入草稿台", true);
    return;
  }
  const asNewChapter = persistTarget.value === "new";
  let chapterID = "";
  let title = "";
  let content = addition;
  if (!asNewChapter) {
    const targetID = draft.targetChapterID || lastChapterID();
    if (!targetID) {
      showToast("没有可并入的章节", true);
      return;
    }
    const base = chapter.id === targetID
      ? { title: chapter.title, content: chapter.content }
      : await api("/api/chapters/" + encodeURIComponent(targetID));
    const baseContent = readString(base && base.content);
    content = baseContent.trim() ? baseContent.replace(/\s+$/u, "") + "\n\n" + addition : addition;
    chapterID = targetID;
    title = readString(base && base.title);
  }

  persistDraftButton.disabled = true;
  persistTarget.disabled = true;
  try {
    const created = await api("/api/drafts", {
      method: "POST",
      body: JSON.stringify({ chapterId: chapterID, title, content }),
    });
    draft.persisted = true;
    draft.persistedDraftID = created && created.id ? created.id : "";
    persistDraftButton.hidden = true;
    persistTargetWrap.hidden = true;
    appendButton.hidden = true;
    newChapterButton.hidden = true;
    setDraftStatus("已存入草稿台 · 请在草稿台提取记忆并确认");
    showToast("已存入草稿台：确认前不会改动正文与记忆");
    await openDraftBox(created && created.id ? created.id : "");
  } catch (error) {
    showToast("存入草稿台失败：" + error.message, true);
  } finally {
    persistDraftButton.disabled = false;
    persistTarget.disabled = false;
  }
}


function agentEl(tag, className, text) {
  const node = document.createElement(tag);
  if (className) node.className = className;
  if (text !== undefined) node.textContent = text;
  return node;
}

function agentEmpty(container, message) {
  container.replaceChildren();
  container.append(agentEl("p", "empty-state small", message));
}

function formatAgentTime(value) {
  if (!value) return "—";
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return "—";
  return date.toLocaleString("zh-CN", { hour12: false });
}

function openAgentCenter() {
  showDialog(agentCenterDialog);
  void refreshAgentCenter();
}

async function refreshAgentCenter() {
  agentRefreshButton.disabled = true;
  await Promise.all([
    loadAgentConfig(),
    loadAgentStats(),
    loadAgentRuns(),
    loadAgentReports(),
    loadForeshadowLedger(),
    loadAgentProposals(),
  ]);
  agentRefreshButton.disabled = false;
}

async function loadAgentConfig() {
  try {
    const data = await api("/api/agents/config");
    const agents = Array.isArray(data && data.agents) ? data.agents : [];
    agentConfigList.replaceChildren();
    if (!agents.length) {
      agentEmpty(agentConfigList, "本里程碑没有可配置的 Agent。");
      return;
    }
    for (const agent of agents) {
      const row = agentEl("div", "agent-config-row");
      const toggle = document.createElement("input");
      toggle.type = "checkbox";
      toggle.checked = Boolean(agent.enabled);
      toggle.setAttribute("aria-label", (agent.name || agent.key) + " 开关");
      const meta = agentEl("div", "agent-meta");
      meta.append(agentEl("strong", "", agent.name || agent.key));
      const source = agent.source === "project" ? "项目配置" : "里程碑默认";
      meta.append(agentEl("small", "", `${agent.key} · ${source} · 触发：${(agent.triggers || []).join(" / ")}`));
      const level = document.createElement("select");
      for (const [value, label] of [["observe", "observe 只报告"], ["propose", "propose 提案送审"], ["autonomous", "autonomous 低风险自动"]]) {
        const option = document.createElement("option");
        option.value = value;
        option.textContent = label;
        option.selected = agent.level === value;
        level.append(option);
      }
      const save = async () => {
        toggle.disabled = true;
        level.disabled = true;
        try {
          await api("/api/agents/config/" + encodeURIComponent(agent.key), {
            method: "PUT",
            body: JSON.stringify({ enabled: toggle.checked, level: level.value }),
          });
          showToast(`${agent.name || agent.key} 已更新`);
          await loadAgentConfig();
        } catch (error) {
          showToast("更新 Agent 配置失败：" + error.message, true);
          await loadAgentConfig();
        }
      };
      toggle.addEventListener("change", () => void save());
      level.addEventListener("change", () => void save());
      row.append(toggle, meta, level);
      agentConfigList.append(row);
    }
  } catch (error) {
    agentEmpty(agentConfigList, "读取 Agent 配置失败：" + error.message);
  }
}

async function loadAgentStats() {
  try {
    const data = await api("/api/agents/stats?days=7");
    const stats = Array.isArray(data && data.stats) ? data.stats : [];
    agentStatsList.replaceChildren();
    if (!stats.length) {
      agentEmpty(agentStatsList, "近 7 天还没有 Agent 运行记录。保存章节或导入文稿后会自动触发。");
      return;
    }
    for (const item of stats) {
      const row = agentEl("div", "agent-list-row");
      const meta = agentEl("div", "agent-meta");
      meta.append(agentEl("strong", "", item.agentName || item.agent));
      meta.append(agentEl("small", "", `运行 ${item.runs} 次 · 失败 ${item.failed} 次 · 平均耗时 ${item.avgDurationMs} ms · 输入/输出 token ${item.promptTokens}/${item.completionTokens}`));
      row.append(meta, agentEl("span", "agent-severity is-info", `总 token ${item.totalTokens}`));
      agentStatsList.append(row);
    }
  } catch (error) {
    agentEmpty(agentStatsList, "读取成本统计失败：" + error.message);
  }
}

async function loadAgentRuns() {
  try {
    const data = await api("/api/agents/runs?limit=40");
    const runs = Array.isArray(data && data.runs) ? data.runs : [];
    agentRunList.replaceChildren();
    agentRunHint.textContent = runs.length ? `最近 ${runs.length} 条` : "";
    if (!runs.length) {
      agentEmpty(agentRunList, "暂无运行日志。");
      return;
    }
    for (const run of runs) {
      const row = agentEl("div", "agent-log-row");
      row.append(agentEl("span", "", formatAgentTime(run.startedAt)));
      const meta = agentEl("span", "");
      meta.append(agentEl("strong", "", `${run.agentName || run.agent} · ${run.status}`));
      meta.append(document.createTextNode(" " + (run.summary || run.error || "")));
      row.append(meta);
      const tokens = Number(run.tokens) || 0;
      row.append(agentEl("span", "", `${run.durationMs} ms · ${tokens} token`));
      agentRunList.append(row);
    }
  } catch (error) {
    agentEmpty(agentRunList, "读取运行日志失败：" + error.message);
  }
}

async function loadAgentReports() {
  try {
    const data = await api("/api/agents/reports?limit=30");
    const reports = Array.isArray(data && data.reports) ? data.reports : [];
    agentReportList.replaceChildren();
    if (!reports.length) {
      agentEmpty(agentReportList, "暂无审计报告。");
      return;
    }
    for (const report of reports) {
      const row = agentEl("div", "agent-list-row");
      const meta = agentEl("div", "agent-meta");
      meta.append(agentEl("strong", "", `${report.agentName || report.agent} · ${formatAgentTime(report.createdAt)}`));
      meta.append(agentEl("small", "", report.summary || ""));
      const findings = Array.isArray(report.findings) ? report.findings : [];
      for (const finding of findings.slice(0, 4)) {
        const detail = agentEl("small", "", `· [${finding.severity || "info"}] ${finding.detail || finding.type || ""}${finding.quote ? "｜" + finding.quote : ""}`);
        meta.append(detail);
        if (finding.chapterId) {
          const link = agentEl("button", "btn btn-quiet btn-sm", `打开第 ${finding.chapterIndex || "?"} 章证据`);
          link.type = "button";
          link.addEventListener("click", () => {
            closeDialog(agentCenterDialog);
            void openChapter(finding.chapterId);
          });
          meta.append(link);
        }
      }
      const severity = agentEl("span", "agent-severity is-" + (report.severity || "info"), report.severity || "info");
      row.append(meta, severity);
      agentReportList.append(row);
    }
  } catch (error) {
    agentEmpty(agentReportList, "读取审计报告失败：" + error.message);
  }
}

async function loadForeshadowLedger() {
  try {
    const data = await api("/api/foreshadows?limit=80");
    const records = Array.isArray(data && data.foreshadows) ? data.foreshadows : [];
    foreshadowList.replaceChildren();
    if (!records.length) {
      agentEmpty(foreshadowList, "暂无伏笔候选。A3 会在章节保存后自动扫描。");
      return;
    }
    for (const record of records) {
      const row = agentEl("div", "agent-list-row");
      const meta = agentEl("div", "agent-meta");
      meta.append(agentEl("strong", "", `第 ${record.chapterIndex} 章「${record.title}」 · ${record.status}`));
      meta.append(agentEl("small", "", `${record.kind} · ${record.quote || ""}`));
      const actions = agentEl("div", "agent-actions");
      if (record.status !== "confirmed") {
        const confirm = agentEl("button", "btn btn-quiet btn-sm", "确认");
        confirm.type = "button";
        confirm.addEventListener("click", () => void updateForeshadow(record.id, "confirmed"));
        actions.append(confirm);
      }
      if (record.status !== "dismissed") {
        const dismiss = agentEl("button", "btn btn-quiet btn-sm", "废弃");
        dismiss.type = "button";
        dismiss.addEventListener("click", () => void updateForeshadow(record.id, "dismissed"));
        actions.append(dismiss);
      }
      row.append(meta, actions);
      foreshadowList.append(row);
    }
  } catch (error) {
    agentEmpty(foreshadowList, "读取伏笔台账失败：" + error.message);
  }
}

async function updateForeshadow(id, status) {
  try {
    await api("/api/foreshadows/" + encodeURIComponent(id), { method: "PATCH", body: JSON.stringify({ status }) });
    showToast(status === "confirmed" ? "伏笔已确认" : "伏笔已废弃");
    await loadForeshadowLedger();
  } catch (error) {
    showToast("更新伏笔失败：" + error.message, true);
  }
}

async function loadAgentProposals() {
  try {
    const data = await api("/api/agents/proposals?status=pending&limit=50");
    const proposals = Array.isArray(data && data.proposals) ? data.proposals : [];
    agentProposalList.replaceChildren();
    if (!proposals.length) {
      agentEmpty(agentProposalList, "没有待审提案。");
      return;
    }
    for (const proposal of proposals) {
      const row = agentEl("div", "agent-list-row");
      const meta = agentEl("div", "agent-meta");
      meta.append(agentEl("strong", "", `${proposal.kind} · ${proposal.risk} 风险`));
      meta.append(agentEl("small", "", proposal.summary || ""));
      const actions = agentEl("div", "agent-actions");
      const accept = agentEl("button", "btn btn-quiet btn-sm", "接受");
      accept.type = "button";
      accept.addEventListener("click", () => void updateProposal(proposal.id, "accepted"));
      const reject = agentEl("button", "btn btn-quiet btn-sm", "驳回");
      reject.type = "button";
      reject.addEventListener("click", () => void updateProposal(proposal.id, "rejected"));
      actions.append(accept, reject);
      row.append(meta, actions);
      agentProposalList.append(row);
    }
  } catch (error) {
    agentEmpty(agentProposalList, "读取提案失败：" + error.message);
  }
}

async function updateProposal(id, status) {
  try {
    await api("/api/agents/proposals/" + encodeURIComponent(id), { method: "PATCH", body: JSON.stringify({ status }) });
    showToast(status === "accepted" ? "提案已接受（仍需在记忆审阅中人工处理）" : "提案已驳回");
    await loadAgentProposals();
  } catch (error) {
    showToast("更新提案失败：" + error.message, true);
  }
}

function askDraftMemorySync() {
  return new Promise((resolve) => {
    let settled = false;
    const finish = (value) => {
      if (settled) return;
      settled = true;
      closeDialog(draftMemorySyncDialog);
      resolve(value);
    };
    draftMemorySyncReextractButton.onclick = () => finish("reextract");
    draftMemorySyncBodyOnlyButton.onclick = () => finish("body_only");
    draftMemorySyncCancelButton.onclick = () => finish("");
    draftMemorySyncCancel.onclick = () => finish("");
    draftMemorySyncDialog.oncancel = (event) => {
      event.preventDefault();
      finish("");
    };
    showDialog(draftMemorySyncDialog);
  });
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

// ---- 记忆审阅面板（I2-7，iter2-详设 §7）----

function openMemoryReview() {
  showDialog(memoryReviewDialog);
  void loadMemoryReview();
}

async function loadMemoryReview() {
  await Promise.all([loadExtractStatus(), loadChangeSets(), loadCandidates()]);
}

function formatTime(value) {
  if (!value) return "";
  const date = new Date(value);
  if (isNaN(date.getTime())) return String(value);
  return date.toLocaleString("zh-CN", { hour12: false });
}

// 提取任务：状态轮询（复用分析进度轮询模式）。
async function loadExtractStatus() {
  try {
    const state = await api("/api/memory/extract/status");
    renderExtractStatus(state || {});
    ensureExtractPolling(state || {});
  } catch (error) {
    extractStatusText.textContent = "读取提取状态失败：" + error.message;
  }
}

function renderExtractStatus(state) {
  const running = state.status === "extracting";
  const stats = state.stats || {};
  let text = "";
  switch (state.status) {
    case "idle":
      text = "空闲";
      break;
    case "extracting":
      text = `提取中 · 已处理 ${stats.chaptersDone || 0} 章 · 暂存 ${stats.changeSetsCreated || 0} 变更集 · 候选 ${stats.candidatesCreated || 0}`;
      break;
    case "done":
      text = `完成 · 变更集 ${stats.changeSetsCreated || 0} · 候选 ${stats.candidatesCreated || 0} · 跳过 ${stats.skippedUpToDate || 0}`;
      break;
    case "failed":
      text = "失败：" + (state.lastError || "未知错误");
      break;
    case "partial":
      text = `部分完成 · 失败 ${stats.failedChapters || 0} 章`;
      break;
    default:
      text = String(state.status || "未知");
  }
  extractStatusText.textContent = text;
  extractStartButton.disabled = running;
}

function ensureExtractPolling(state) {
  if (state.status === "extracting") {
    if (reviewExtractTimer) return;
    reviewExtractTimer = window.setTimeout(() => {
      reviewExtractTimer = 0;
      void loadExtractStatus();
    }, 2500);
  } else if (reviewExtractTimer) {
    clearTimeout(reviewExtractTimer);
    reviewExtractTimer = 0;
  }
}

async function startExtract() {
  if (!configured()) {
    showToast("请先配置模型接口", true);
    openSettings();
    return;
  }
  extractStartButton.disabled = true;
  try {
    const data = await api("/api/memory/extract", {
      method: "POST",
      body: JSON.stringify({ force: extractForceInput.checked }),
    });
    showToast(data.extractStatus === "extracting" ? "已启动深度提取" : "无需提取（已是最新）");
    void loadExtractStatus();
  } catch (error) {
    showToast("启动提取失败：" + error.message, true);
    extractStartButton.disabled = false;
  }
}

async function reconcileMemory() {
  reconcileButton.disabled = true;
  try {
    const data = await api("/api/memory/reconcile", { method: "POST", body: "{}" });
    showToast(`证据复核完成：标记 ${data.marked} 处（revision ${data.memoryRevision}）`);
  } catch (error) {
    showToast("证据复核失败：" + error.message, true);
  } finally {
    reconcileButton.disabled = false;
  }
}

// 变更集列表与详情。
async function loadChangeSets() {
  try {
    const sets = await api("/api/memory/changesets?status=proposed");
    renderChangeSetList(sets || []);
  } catch (error) {
    showToast("读取变更集失败：" + error.message, true);
  }
}

function renderChangeSetList(sets) {
  changesetList.replaceChildren();
  changesetDetail.hidden = true;
  if (!sets.length) {
    const empty = document.createElement("p");
    empty.className = "empty-state small";
    empty.textContent = "没有待审变更集。";
    changesetList.append(empty);
    return;
  }
  const fragment = document.createDocumentFragment();
  for (const cs of sets) {
    const row = document.createElement("button");
    row.type = "button";
    row.className = "changeset-row";
    row.dataset.id = cs.id;
    const meta = document.createElement("span");
    meta.className = "changeset-meta";
    meta.textContent = `${cs.reason || cs.id} · ${cs.opCount} 操作 · ${cs.conflictCount} 冲突`;
    const time = document.createElement("span");
    time.className = "changeset-time";
    time.textContent = formatTime(cs.createdAt);
    row.append(meta, time);
    row.addEventListener("click", () => void openChangeSet(cs.id));
    fragment.append(row);
  }
  changesetList.append(fragment);
}

async function openChangeSet(id) {
  try {
    const cs = await api("/api/memory/changesets/" + encodeURIComponent(id));
    renderChangeSetDetail(cs);
  } catch (error) {
    showToast("读取变更集详情失败：" + error.message, true);
  }
}

function renderChangeSetDetail(cs) {
  changesetDetail.hidden = false;
  changesetDetail.replaceChildren();

  const head = document.createElement("div");
  head.className = "detail-head";
  const title = document.createElement("h4");
  title.textContent = cs.reason || cs.id;
  head.append(title);
  if (cs.status === "proposed") {
    const actions = document.createElement("div");
    actions.className = "review-actions";
    const accept = document.createElement("button");
    accept.type = "button";
    accept.className = "btn btn-primary btn-sm";
    accept.textContent = "采纳";
    accept.addEventListener("click", () => void acceptChangeSet(cs.id));
    const reject = document.createElement("button");
    reject.type = "button";
    reject.className = "btn btn-danger btn-sm";
    reject.textContent = "驳回";
    reject.addEventListener("click", () => void rejectChangeSet(cs.id));
    const rebase = document.createElement("button");
    rebase.type = "button";
    rebase.className = "btn btn-quiet btn-sm";
    rebase.textContent = "重算基线";
    rebase.addEventListener("click", () => void rebaseChangeSet(cs.id));
    actions.append(accept, reject, rebase);
    head.append(actions);
  } else {
    const badge = document.createElement("span");
    badge.className = "detail-status is-" + cs.status;
    badge.textContent = cs.status;
    head.append(badge);
  }
  changesetDetail.append(head);

  if (cs.conflicts && cs.conflicts.length) {
    const group = document.createElement("div");
    group.className = "conflict-group";
    const label = document.createElement("p");
    label.className = "group-title conflict-title";
    label.textContent = `冲突项（${cs.conflicts.length} · 不自动提交）`;
    group.append(label);
    for (const cf of cs.conflicts) group.append(renderConflict(cf));
    changesetDetail.append(group);
  }

  if (cs.ops && cs.ops.length) {
    const group = document.createElement("div");
    group.className = "op-group";
    const label = document.createElement("p");
    label.className = "group-title";
    label.textContent = `变更操作（${cs.ops.length}）`;
    group.append(label);
    for (const op of cs.ops) group.append(renderOp(op));
    changesetDetail.append(group);
  } else if (!cs.conflicts || !cs.conflicts.length) {
    const empty = document.createElement("p");
    empty.className = "empty-state small";
    empty.textContent = "无操作、无冲突。";
    changesetDetail.append(empty);
  }
}

function renderConflict(cf) {
  const wrap = document.createElement("div");
  wrap.className = "conflict-card";
  const head = document.createElement("div");
  head.className = "conflict-head";
  const kind = document.createElement("span");
  kind.className = "conflict-kind";
  kind.textContent = cf.kind || "conflict";
  head.append(kind);
  if (cf.targetId) {
    const target = document.createElement("span");
    target.className = "op-target";
    target.textContent = cf.targetId;
    head.append(target);
  }
  if (cf.detail) {
    const detail = document.createElement("span");
    detail.className = "conflict-detail";
    detail.textContent = cf.detail;
    head.append(detail);
  }
  wrap.append(head);
  if (cf.proposed) {
    wrap.append(renderJsonBlock(cf.proposed, "提议值"));
  }
  if (cf.old) {
    wrap.append(renderJsonBlock(cf.old, "现值"));
  }
  return wrap;
}

function renderOp(op) {
  const wrap = document.createElement("div");
  wrap.className = "op-card";
  const head = document.createElement("div");
  head.className = "op-head";
  const type = document.createElement("span");
  type.className = "op-type";
  type.textContent = op.op || "op";
  head.append(type);
  if (op.targetId) {
    const target = document.createElement("span");
    target.className = "op-target";
    target.textContent = "→ " + op.targetId + (op.expectedRecordVersion ? " @v" + op.expectedRecordVersion : "");
    head.append(target);
  }
  wrap.append(head);

  let record = null;
  try {
    record = JSON.parse(op.record);
  } catch {
    record = null;
  }
  if (record && typeof record === "object") {
    wrap.append(renderRecordSummary(record));
    if (Array.isArray(record.sourceRefs)) {
      const refs = record.sourceRefs.filter((ref) => ref && ref.quote);
      if (refs.length) {
        const ev = document.createElement("div");
        ev.className = "op-evidence";
        for (const ref of refs) {
          const quote = document.createElement("blockquote");
          quote.className = "evidence-quote";
          quote.textContent = ref.quote;
          const cap = document.createElement("span");
          cap.className = "evidence-cap";
          cap.textContent = (ref.chapterId || "") + (ref.chapterRevision ? " @r" + ref.chapterRevision : "");
          quote.append(cap);
          ev.append(quote);
        }
        wrap.append(ev);
      }
    }
  } else {
    wrap.append(renderJsonBlock(op.record, null));
  }
  if (op.evidenceNote) {
    const note = document.createElement("p");
    note.className = "op-note";
    note.textContent = "标记：" + op.evidenceNote;
    wrap.append(note);
  }
  return wrap;
}

function renderRecordSummary(record) {
  const div = document.createElement("div");
  div.className = "record-summary";
  const lines = [];
  if (typeof record.summary === "string" && record.summary) lines.push(record.summary);
  if (typeof record.title === "string" && record.title) {
    lines.push("伏笔：" + record.title + (record.status ? "（" + record.status + "）" : ""));
  }
  if (record.category || record.attribute || record.value) {
    lines.push(`${record.subjectId || "?"} · ${record.category || ""} · ${record.attribute || ""} = ${record.value || ""}`);
  }
  if (Array.isArray(record.participants) && record.participants.length) {
    lines.push("参与者：" + record.participants.join("、"));
  }
  if (record.participantsKnown === false) lines.push("（参与者未识别）");
  if (record.needsReview) lines.push("待复核：" + (record.reviewReason || "needsReview"));
  const text = document.createElement("p");
  text.className = "record-text";
  text.textContent = lines.join("　|　");
  div.append(text);
  div.append(renderJsonBlock(record, "完整记录"));
  return div;
}

function renderJsonBlock(raw, caption) {
  const pre = document.createElement("pre");
  pre.className = "op-json";
  let value = raw;
  if (typeof raw === "string") {
    try {
      value = JSON.stringify(JSON.parse(raw), null, 2);
    } catch {
      value = raw;
    }
  } else {
    value = JSON.stringify(raw, null, 2);
  }
  pre.textContent = caption ? caption + "\n" + value : value;
  return pre;
}

async function acceptChangeSet(id) {
  try {
    const data = await api("/api/memory/changesets/" + encodeURIComponent(id) + "/accept", { method: "POST" });
    showToast("已采纳：" + (data.commitId || "") + "（revision " + data.memoryRevision + "）");
    await loadChangeSets();
    changesetDetail.hidden = true;
  } catch (error) {
    if (error.message.indexOf("revision_conflict") >= 0) {
      showToast("基线已过期：请先「重算基线」再采纳", true);
    } else {
      showToast("采纳失败：" + error.message, true);
    }
  }
}

async function rejectChangeSet(id) {
  const reason = window.prompt("驳回原因（可选，≤200 字）：", "");
  if (reason === null) return;
  try {
    await api("/api/memory/changesets/" + encodeURIComponent(id) + "/reject", {
      method: "POST",
      body: JSON.stringify({ reason: reason.trim() }),
    });
    showToast("已驳回");
    await loadChangeSets();
    changesetDetail.hidden = true;
  } catch (error) {
    showToast("驳回失败：" + error.message, true);
  }
}

async function rebaseChangeSet(id) {
  try {
    await api("/api/memory/changesets/" + encodeURIComponent(id) + "/rebase", { method: "POST" });
    showToast("已重算基线，可重新采纳");
    await openChangeSet(id);
  } catch (error) {
    showToast("重算失败：" + error.message, true);
  }
}

// 实体候选。
async function loadCandidates() {
  try {
    const cands = await api("/api/memory/candidates?status=pending");
    renderCandidateList(cands || []);
  } catch (error) {
    showToast("读取候选失败：" + error.message, true);
  }
}

function renderCandidateList(cands) {
  candidateList.replaceChildren();
  if (!cands.length) {
    const empty = document.createElement("p");
    empty.className = "empty-state small";
    empty.textContent = "没有待确认实体候选。";
    candidateList.append(empty);
    return;
  }
  const fragment = document.createDocumentFragment();
  for (const cand of cands) fragment.append(renderCandidate(cand));
  candidateList.append(fragment);
}

function renderCandidate(cand) {
  const wrap = document.createElement("div");
  wrap.className = "candidate-card";
  const head = document.createElement("div");
  head.className = "candidate-head";
  const name = document.createElement("span");
  name.className = "candidate-name";
  name.textContent = cand.name || cand.id;
  head.append(name);
  if (cand.reason) {
    const reason = document.createElement("span");
    reason.className = "candidate-reason";
    reason.textContent = cand.reason;
    head.append(reason);
  }
  wrap.append(head);
  if (Array.isArray(cand.aliases) && cand.aliases.length) {
    const aliases = document.createElement("p");
    aliases.className = "candidate-aliases";
    aliases.textContent = "别名：" + cand.aliases.join("、");
    wrap.append(aliases);
  }
  if (Array.isArray(cand.possibleIds) && cand.possibleIds.length) {
    const ids = document.createElement("p");
    ids.className = "candidate-aliases";
    ids.textContent = "可能命中：" + cand.possibleIds.join("、");
    wrap.append(ids);
  }
  const actions = document.createElement("div");
  actions.className = "review-actions";
  const register = document.createElement("button");
  register.type = "button";
  register.className = "btn btn-primary btn-sm";
  register.textContent = "注册为新实体";
  register.addEventListener("click", () => void resolveCandidate(cand.id, "register", ""));
  const merge = document.createElement("button");
  merge.type = "button";
  merge.className = "btn btn-ghost btn-sm";
  merge.textContent = "并入实体";
  merge.addEventListener("click", () => {
    const target = window.prompt("并入目标实体 ID（c_ 或 w_ 开头）：", "");
    if (!target || !target.trim()) return;
    void resolveCandidate(cand.id, "merge", target.trim());
  });
  const dismiss = document.createElement("button");
  dismiss.type = "button";
  dismiss.className = "btn btn-quiet btn-sm";
  dismiss.textContent = "忽略";
  dismiss.addEventListener("click", () => void resolveCandidate(cand.id, "dismiss", ""));
  actions.append(register, merge, dismiss);
  wrap.append(actions);
  return wrap;
}

async function resolveCandidate(id, action, resolution) {
  try {
    const data = await api("/api/memory/candidates/" + encodeURIComponent(id) + "/resolve", {
      method: "POST",
      body: JSON.stringify({ action, resolution }),
    });
    showToast("候选已处理：" + data.status);
    await loadCandidates();
  } catch (error) {
    showToast("确认失败：" + error.message, true);
  }
}

// ---- 图像中心（M6）----

const imageKindLabels = {
  character: "角色",
  scene: "场景",
  item: "物品",
  illustration: "插图",
  cover: "封面",
};

const imageStatusLabels = {
  pending: "排队中",
  running: "生成中",
  done: "已完成",
  failed: "失败",
};

async function loadFeatures() {
  try {
    const data = await api("/api/features");
    features = data || features;
  } catch {
    features = { images: false, export: false };
  }
  imageCenterButton.hidden = !features.images;
  exportButton.hidden = !features.export;
}

async function openImageCenter() {
  if (!features.images) {
    showToast("图像功能未启用", true);
    return;
  }
  showDialog(imageCenterDialog);
  await Promise.all([loadImageEntries(), loadImageAssets()]);
}

async function loadImageEntries() {
  try {
    const entries = await api("/api/world/entries");
    imageEntries = Array.isArray(entries) ? entries : [];
  } catch {
    imageEntries = [];
  }
  renderImageEntryOptions();
  renderImageChapterOptions();
  updateImageKindFields();
}

function imageEntryKinds(kind) {
  switch (kind) {
    case "character":
      return ["character"];
    case "scene":
      return ["location", "setting"];
    case "item":
      return ["item"];
    default:
      return [];
  }
}

function renderImageEntryOptions() {
  const kinds = imageEntryKinds(imageKindInput.value);
  const previous = imageEntryInput.value;
  imageEntryInput.replaceChildren();
  const matched = imageEntries.filter((entry) => kinds.includes(entry.kind));
  if (!matched.length) {
    const option = document.createElement("option");
    option.value = "";
    option.textContent = "没有可选词条（请先在大世界维护）";
    imageEntryInput.append(option);
    return;
  }
  for (const entry of matched) {
    const option = document.createElement("option");
    option.value = entry.id;
    option.textContent = `${entry.name || entry.id}（${entry.id}）`;
    imageEntryInput.append(option);
  }
  if (matched.some((entry) => entry.id === previous)) imageEntryInput.value = previous;
}

function renderImageChapterOptions() {
  const previous = imageChapterInput.value;
  imageChapterInput.replaceChildren();
  const chapters = project.chapters || [];
  if (!chapters.length) {
    const option = document.createElement("option");
    option.value = "";
    option.textContent = "还没有章节";
    imageChapterInput.append(option);
    return;
  }
  for (const meta of chapters) {
    const option = document.createElement("option");
    option.value = meta.id;
    option.textContent = `${meta.index}. ${meta.title || "未命名"}`;
    imageChapterInput.append(option);
  }
  if (chapters.some((meta) => meta.id === previous)) imageChapterInput.value = previous;
}

function updateImageKindFields() {
  const kind = imageKindInput.value;
  imageEntryField.hidden = kind === "illustration" || kind === "cover";
  imageChapterField.hidden = kind !== "illustration";
  renderImageEntryOptions();
}

function imageRequestPayload() {
  const kind = imageKindInput.value;
  const payload = {
    kind,
    styleTag: imageStyleInput.value.trim(),
    negative: imageNegativeInput.value.trim(),
    prompt: imagePromptInput.value.trim(),
  };
  if (kind === "illustration") {
    payload.chapterId = imageChapterInput.value;
  } else if (kind !== "cover") {
    payload.entryId = imageEntryInput.value;
  }
  const size = String(imageSizeInput.value || "").split("x");
  const width = Number(size[0]);
  const height = Number(size[1]);
  if (width && height) {
    payload.width = width;
    payload.height = height;
  }
  const steps = Number(imageStepsInput.value);
  if (steps > 0) payload.steps = steps;
  const seed = Number(imageSeedInput.value);
  if (seed > 0) payload.seed = seed;
  return payload;
}

async function previewImagePrompt() {
  imagePreviewButton.disabled = true;
  try {
    const spec = await api("/api/images/preview-prompt", {
      method: "POST",
      body: JSON.stringify(imageRequestPayload()),
    });
    const featuresText = Array.isArray(spec.features) && spec.features.length
      ? spec.features.join(" / ")
      : "无（可在词条外观字段中补充特征）";
    imagePromptPreview.hidden = false;
    imagePromptPreview.textContent = [
      `画风：${spec.styleTag || "未设置"}`,
      `正向：${spec.positive || "（空）"}`,
      `负面：${spec.negative || "（空）"}`,
      `特征：${featuresText}`,
    ].join("\n");
  } catch (error) {
    showToast("预览提示词失败：" + error.message, true);
  } finally {
    imagePreviewButton.disabled = false;
  }
}

async function submitImageGeneration() {
  if (imageBusy) return;
  const payload = imageRequestPayload();
  if (payload.kind === "illustration" && !payload.chapterId) {
    showToast("请先选择要配图的章节", true);
    return;
  }
  if (payload.kind !== "illustration" && payload.kind !== "cover" && !payload.entryId) {
    showToast("请先选择词条", true);
    return;
  }
  imageBusy = true;
  imageGenerateButton.disabled = true;
  try {
    const asset = await api("/api/images/generate", {
      method: "POST",
      body: JSON.stringify(payload),
    });
    showToast(`已加入队列：${asset.id}`);
    await loadImageAssets();
  } catch (error) {
    showToast("提交生成失败：" + error.message, true);
  } finally {
    imageBusy = false;
    imageGenerateButton.disabled = false;
  }
}

async function loadImageAssets({ quiet = false } = {}) {
  const params = new URLSearchParams();
  if (imageAssetKindFilter.value) params.set("kind", imageAssetKindFilter.value);
  params.set("limit", "100");
  try {
    const data = await api("/api/images/assets?" + params.toString());
    renderImageAssets(data.assets || []);
    const pending = Number(data.pending || 0);
    imageStatusText.textContent = pending ? `队列中 ${pending} 个` : "队列空闲";
    ensureImagePolling(pending);
  } catch (error) {
    if (!quiet) showToast("读取图像资产失败：" + error.message, true);
  }
}

function ensureImagePolling(pending) {
  if (pending > 0) {
    if (imageAssetsTimer) return;
    imageAssetsTimer = window.setTimeout(() => {
      imageAssetsTimer = 0;
      void loadImageAssets({ quiet: true });
    }, 2000);
  } else if (imageAssetsTimer) {
    clearTimeout(imageAssetsTimer);
    imageAssetsTimer = 0;
  }
}

function renderImageAssets(assets) {
  imageAssetList.replaceChildren();
  if (!assets.length) {
    const empty = document.createElement("p");
    empty.className = "empty-state small";
    empty.textContent = "还没有图像资产。选择词条后开始生成。";
    imageAssetList.append(empty);
    return;
  }
  const fragment = document.createDocumentFragment();
  for (const asset of [...assets].reverse()) fragment.append(renderImageCard(asset, assets));
  imageAssetList.append(fragment);
}

function renderImageCard(asset, assets) {
  const card = document.createElement("article");
  card.className = "image-card";

  const thumb = document.createElement("div");
  thumb.className = "image-card-thumb";
  if (asset.status === "done" && (asset.thumbKey || asset.fileKey)) {
    const img = document.createElement("img");
    img.loading = "lazy";
    img.alt = `${imageKindLabels[asset.kind] || asset.kind} ${asset.entryId || asset.chapterId || ""}`;
    img.src = asset.thumbKey ? `/api/images/${asset.id}/thumb` : `/api/images/${asset.id}/file`;
    thumb.append(img);
  } else {
    const placeholder = document.createElement("span");
    placeholder.className = "image-placeholder";
    placeholder.textContent = imageStatusLabels[asset.status] || asset.status || "—";
    if (asset.error) placeholder.title = asset.error;
    thumb.append(placeholder);
  }
  if (asset.isAnchor) {
    const anchorBadge = document.createElement("span");
    anchorBadge.className = "image-badge is-anchor";
    anchorBadge.textContent = "基准";
    thumb.append(anchorBadge);
  }
  card.append(thumb);

  const body = document.createElement("div");
  body.className = "image-card-body";
  const title = document.createElement("p");
  title.className = "image-card-title";
  title.textContent = `${imageKindLabels[asset.kind] || asset.kind} · ${asset.entryId || asset.chapterId || "全书"}`;
  const meta = document.createElement("p");
  meta.className = "image-card-meta";
  const bits = [imageStatusLabels[asset.status] || asset.status];
  if (asset.status === "done") bits.push(`提示词与特征一致性 ${Number(asset.consistencyScore || 0).toFixed(2)}`);
  if (asset.seed) bits.push(`seed ${asset.seed}`);
  meta.textContent = bits.join(" · ");
  if (asset.consistencyNote) meta.title = asset.consistencyNote;
  body.append(title, meta);
  if (asset.kind === "character" && asset.status === "done") {
    const review = asset.appearance;
    const labels = { consistent: "未发现明显外观漂移", drift: "疑似外观漂移，请复核", uncertain: "证据不足，无法判断", unavailable: "未能核验" };
    const reasons = { disabled: "自动核验未启用", agent_disabled: "图像 Agent 已停用", no_anchor: "生成时未设基准图", model_unconfigured: "未配置视觉模型", image_unavailable: "原图缺失或无法读取", model_error: "视觉模型调用失败或结果无效", interrupted: "核验中断" };
    const summary = document.createElement("p");
    summary.className = "image-card-meta";
    summary.textContent = `角色外观：${labels[review?.status] || "未核验"}${reasons[review?.reason] ? ` · ${reasons[review.reason]}` : ""}`;
    body.append(summary);
    if (review?.evidence?.length) {
      const details = document.createElement("details");
      const heading = document.createElement("summary");
      heading.textContent = "查看核验依据";
      details.append(heading);
      const evidence = document.createElement("ul");
      for (const observation of review.evidence) {
        const item = document.createElement("li");
        item.textContent = observation;
        evidence.append(item);
      }
      details.append(evidence);
      if (review.anchorId) {
        const anchor = document.createElement("a");
        anchor.href = `/api/images/${encodeURIComponent(review.anchorId)}/file`;
        anchor.target = "_blank";
        anchor.rel = "noopener";
        anchor.textContent = "查看核验时的基准图";
        details.append(anchor);
      }
      body.append(details);
    }
  }

  const actions = document.createElement("div");
  actions.className = "image-card-actions";
  if (asset.status === "done") {
    const view = document.createElement("a");
    view.className = "btn btn-quiet btn-sm";
    view.href = `/api/images/${asset.id}/file`;
    view.target = "_blank";
    view.rel = "noopener";
    view.textContent = "查看";
    actions.append(view);
    if (!asset.isAnchor) {
      actions.append(imageActionButton("设为基准", "btn btn-ghost btn-sm", () => void imageAction(asset.id, "anchor", "已设为基准图")));
    }
  }
  if (asset.status === "failed" || asset.status === "done") {
    actions.append(imageActionButton("重试", "btn btn-quiet btn-sm", () => void imageAction(asset.id, "retry", "已重新排队")));
  }
  actions.append(imageActionButton("删除", "btn btn-danger btn-sm", () => void deleteImageAsset(asset)));
  body.append(actions);
  if (asset.kind === "character" && asset.entryId && asset.status === "done") {
    const variants = assets.filter((other) => other.id !== asset.id && other.kind === "character" && other.entryId === asset.entryId && other.status === "done");
    if (variants.length) body.append(imageSimilarityControls(asset, variants));
  }
  card.append(body);
  return card;
}

function imageSimilarityControls(asset, variants) {
  const panel = document.createElement("div");
  const label = document.createElement("label");
  label.className = "field";
  label.textContent = "同角色对比图";
  const select = document.createElement("select");
  for (const other of variants) {
    select.add(new Option(`${other.isAnchor ? "基准图 · " : ""}${other.id}`, other.id));
    if (other.isAnchor) select.value = other.id;
  }
  const result = document.createElement("p");
  result.className = "image-card-meta";
  result.setAttribute("role", "status");
  const reasons = {
    asset_not_done: "图像尚未生成完成",
    file_unavailable: "无法读取原图",
    image_too_large: "原图超过比较尺寸限制",
    unsupported_or_invalid_image: "原图格式不支持或文件损坏",
  };
  const compare = imageActionButton("比较实际图像", "btn btn-quiet btn-sm", async () => {
    select.disabled = compare.disabled = true;
    result.textContent = "正在比较原图像素…";
    try {
      const params = new URLSearchParams({ otherId: select.value });
      const data = await api(`/api/images/${encodeURIComponent(asset.id)}/similarity?${params}`);
      result.textContent = data.available
        ? `像素相似度 ${data.score.toFixed(2)} / 1。${data.note}`
        : `像素相似度不可用。${reasons[data.reason] || "请尝试其他图像"}。`;
    } catch (error) {
      result.textContent = "比较失败：" + error.message;
    } finally {
      select.disabled = compare.disabled = false;
    }
  });
  select.addEventListener("change", () => { result.textContent = ""; });
  label.append(select);
  panel.append(label, compare, result);
  return panel;
}

function imageActionButton(label, className, handler) {
  const button = document.createElement("button");
  button.type = "button";
  button.className = className;
  button.textContent = label;
  button.addEventListener("click", handler);
  return button;
}

async function imageAction(id, action, message) {
  try {
    await api(`/api/images/${encodeURIComponent(id)}/${action}`, { method: "POST" });
    showToast(message);
    await loadImageAssets();
  } catch (error) {
    showToast("操作失败：" + error.message, true);
  }
}

async function deleteImageAsset(asset) {
  if (!window.confirm("删除这张图像资产？文件会一并删除。")) return;
  try {
    await api(`/api/images/${encodeURIComponent(asset.id)}`, { method: "DELETE" });
    showToast("已删除");
    await loadImageAssets();
  } catch (error) {
    showToast("删除失败：" + error.message, true);
  }
}

async function openImageModelSettings() {
  try {
    const cfg = await api("/api/config/image-model");
    imageModelBackend.value = cfg.backend === "sdwebui" ? "sdwebui" : "openai";
    imageModelBaseUrl.value = cfg.baseUrl || "";
    imageModelName.value = cfg.model || "";
    imageModelStyle.value = cfg.styleTag || "";
    imageModelNegative.value = cfg.negativePrompt || "";
    imageModelWidth.value = String(cfg.width || 1024);
    imageModelHeight.value = String(cfg.height || 1024);
    imageModelSteps.value = String(cfg.steps || 20);
    imageModelTimeout.value = String(cfg.timeoutSeconds || 120);
    $("imageAppearanceEnabled").checked = cfg.appearanceEnabled === true;
    $("imageAppearanceModel").value = cfg.appearanceModel || "";
    imageModelApiKey.value = "";
    imageModelApiKey.disabled = false;
    imageModelClearKey.checked = false;
    if (cfg.apiKeyFromEnv) {
      imageModelKeyHint.textContent = "已从环境变量 IMAGE_API_KEY 读取，将优先使用。";
    } else if (cfg.hasApiKey) {
      imageModelKeyHint.textContent = cfg.apiKeyHint ? `已保存：${cfg.apiKeyHint}。留空表示不修改。` : "已保存 API Key。留空表示不修改。";
    } else {
      imageModelKeyHint.textContent = "尚未保存 API Key。本地 WebUI 可以留空。";
    }
  } catch (error) {
    showToast("读取图像模型设置失败：" + error.message, true);
    return;
  }
  showDialog(imageModelDialog);
}

async function saveImageModelSettings(event) {
  event.preventDefault();
  const backend = imageModelBackend.value;
  const baseUrl = imageModelBaseUrl.value.trim().replace(/\/+$/, "");
  const model = imageModelName.value.trim();
  if (baseUrl) {
    let parsed = null;
    try {
      parsed = new URL(baseUrl);
    } catch {
      parsed = null;
    }
    if (!parsed || (parsed.protocol !== "http:" && parsed.protocol !== "https:") || !parsed.host) {
      showToast("Base URL 需要是 http:// 或 https:// 地址", true);
      return;
    }
  }
  if (backend === "openai" && baseUrl && !model) {
    showToast("OpenAI 兼容后端需要填写模型名", true);
    return;
  }
  const submitButton = imageModelForm.querySelector('button[type="submit"]');
  submitButton.disabled = true;
  try {
    const saved = await api("/api/config/image-model", {
      method: "PUT",
      body: JSON.stringify({
        backend,
        baseUrl,
        model,
        apiKey: imageModelApiKey.value,
        clearApiKey: imageModelClearKey.checked,
        styleTag: imageModelStyle.value.trim(),
        negativePrompt: imageModelNegative.value.trim(),
        width: Number(imageModelWidth.value) || 0,
        height: Number(imageModelHeight.value) || 0,
        steps: Number(imageModelSteps.value) || 0,
        timeoutSeconds: Number(imageModelTimeout.value) || 0,
        appearanceEnabled: $("imageAppearanceEnabled").checked,
        appearanceModel: $("imageAppearanceModel").value.trim(),
      }),
    });
    if (saved && saved.styleTag) imageStyleInput.value = saved.styleTag;
    closeDialog(imageModelDialog);
    showToast("图像模型设置已保存");
  } catch (error) {
    showToast("保存图像设置失败：" + error.message, true);
  } finally {
    submitButton.disabled = false;
  }
}

// ---- 导出（M7）----

const exportFormatLabels = { txt: "TXT", md: "Markdown", epub: "EPUB3" };

function formatBytes(bytes) {
  const value = Number(bytes || 0);
  if (value < 1024) return `${value} B`;
  if (value < 1024 * 1024) return `${(value / 1024).toFixed(1)} KB`;
  return `${(value / 1024 / 1024).toFixed(2)} MB`;
}

async function openExportDialog() {
  if (!features.export) {
    showToast("导出功能未启用", true);
    return;
  }
  exportStatus.textContent = "";
  updateExportImageField();
  showDialog(exportDialog);
  await loadExports();
}

function updateExportImageField() {
  exportImagesField.hidden = exportFormat.value === "epub";
}

async function loadExports() {
  try {
    const data = await api("/api/exports");
    renderExportList(data.exports || []);
  } catch (error) {
    showToast("读取导出记录失败：" + error.message, true);
  }
}

function renderExportList(records) {
  exportList.replaceChildren();
  if (!records.length) {
    const empty = document.createElement("p");
    empty.className = "empty-state small";
    empty.textContent = "还没有导出记录。选择格式后开始导出。";
    exportList.append(empty);
    return;
  }
  const fragment = document.createDocumentFragment();
  for (const record of records) {
    const row = document.createElement("div");
    row.className = "export-row";
    const info = document.createElement("div");
    info.className = "export-row-info";
    const title = document.createElement("strong");
    title.textContent = record.title || record.fileName;
    const meta = document.createElement("span");
    meta.className = "export-row-meta";
    const bits = [
      exportFormatLabels[record.format] || record.format,
      `${record.chapterCount} 章`,
    ];
    if (record.imageCount) bits.push(`${record.imageCount} 张图`);
    bits.push(formatBytes(record.sizeBytes));
    if (record.createdAt) bits.push(formatTime(record.createdAt));
    meta.textContent = bits.join(" · ");
    info.append(title, meta);
    const download = document.createElement("a");
    download.className = "btn btn-quiet btn-sm";
    download.href = `/api/exports/${encodeURIComponent(record.id)}/download`;
    download.textContent = "下载";
    row.append(info, download);
    fragment.append(row);
  }
  exportList.append(fragment);
}

async function createExport() {
  const format = exportFormat.value;
  exportSubmitButton.disabled = true;
  exportStatus.textContent = "导出中…";
  try {
    const record = await api("/api/export", {
      method: "POST",
      body: JSON.stringify({
        format,
        title: exportTitleInput.value.trim(),
        includeImages: format !== "epub" && exportIncludeImages.checked,
      }),
    });
    exportStatus.textContent = "";
    showToast(`已生成 ${exportFormatLabels[record.format] || record.format}（${formatBytes(record.sizeBytes)}）`);
    await loadExports();
  } catch (error) {
    exportStatus.textContent = "";
    showToast("导出失败：" + error.message, true);
  } finally {
    exportSubmitButton.disabled = false;
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
  $("importMode").addEventListener("change", updateImportHint);

  importForm.addEventListener("submit", async (event) => {
    event.preventDefault();
    const text = importText.value;
    if (!text.trim()) {
      showToast("请选择文件或粘贴正文", true);
      return;
    }
    if (importSubmitButton.disabled) return;
    const modeInput = $("importMode");
    const mode = modeInput.value;
    if (mode === "overwrite" && project.chapters.length && !window.confirm("导入会替换当前全部章节，确定继续？")) return;
    importSubmitButton.disabled = true;
    modeInput.disabled = true;
    importSubmitButton.textContent = "导入中…";
    try {
      if (!await flushChapterSave()) throw new Error("请先保存当前章节");
      if (titleRevision > titleSavedRevision) await saveTitleNow();
      if (titleRevision > titleSavedRevision) throw new Error("请先保存书名");
      const data = await api("/api/import", {
        method: "POST",
        body: JSON.stringify({ title: importTitleInput.disabled ? "" : importTitleInput.value.trim(), text, mode }),
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
      showToast(`已${mode === "append" ? "追加" : "导入"} ${formatNumber(data?.importedChapters || 0)} 章`);
    } catch (error) {
      showToast("导入失败：" + error.message, true);
    } finally {
      importSubmitButton.disabled = false;
      modeInput.disabled = false;
      updateImportHint();
    }
  });

  modelBadge.addEventListener("click", openSettings);
  settingsButton.addEventListener("click", openSettings);

  planButton.addEventListener("click", openPlanDialog);
  agentCenterButton.addEventListener("click", openAgentCenter);
  agentRefreshButton.addEventListener("click", () => void refreshAgentCenter());
  planRefreshButton.addEventListener("click", () => {
    void loadPlanTree();
  });
  planGenerateButton.addEventListener("click", () => {
    void generatePlanDrafts();
  });

  memoryReviewButton.addEventListener("click", openMemoryReview);
  imageCenterButton.addEventListener("click", () => void openImageCenter());
  imageKindInput.addEventListener("change", updateImageKindFields);
  imageAssetKindFilter.addEventListener("change", () => void loadImageAssets());
  imageRefreshButton.addEventListener("click", () => void loadImageAssets());
  imagePreviewButton.addEventListener("click", () => void previewImagePrompt());
  imageGenerateButton.addEventListener("click", () => void submitImageGeneration());
  imageModelButton.addEventListener("click", () => void openImageModelSettings());
  imageModelForm.addEventListener("submit", (event) => void saveImageModelSettings(event));
  imageModelClearKey.addEventListener("change", () => {
    imageModelApiKey.disabled = imageModelClearKey.checked;
    if (imageModelClearKey.checked) imageModelApiKey.value = "";
  });
  imageCenterDialog.addEventListener("close", () => {
    if (imageAssetsTimer) {
      clearTimeout(imageAssetsTimer);
      imageAssetsTimer = 0;
    }
  });
  exportButton.addEventListener("click", () => void openExportDialog());
  exportFormat.addEventListener("change", updateExportImageField);
  exportRefreshButton.addEventListener("click", () => void loadExports());
  exportSubmitButton.addEventListener("click", () => void createExport());
  extractStartButton.addEventListener("click", () => {
    void startExtract();
  });
  reconcileButton.addEventListener("click", () => {
    void reconcileMemory();
  });
  refreshReviewButton.addEventListener("click", () => {
    void loadChangeSets();
  });
  memoryReviewDialog.addEventListener("close", () => {
    if (reviewExtractTimer) {
      clearTimeout(reviewExtractTimer);
      reviewExtractTimer = 0;
    }
  });

  draftBoxButton.addEventListener("click", () => {
    void openDraftBox();
  });
  draftRefreshButton.addEventListener("click", () => {
    void loadDraftList();
  });
  draftStatusFilter.addEventListener("change", () => {
    void loadDraftList();
  });
  draftSaveEditButton.addEventListener("click", () => {
    void saveDraftEdit();
  });
  draftExtractButton.addEventListener("click", () => {
    void extractDraftMemory();
  });
  draftConfirmButton.addEventListener("click", () => {
    void confirmPersistedDraft();
  });
  draftRejectButton.addEventListener("click", () => {
    void rejectPersistedDraft();
  });
  draftOpenChapterButton.addEventListener("click", () => {
    void openCommittedDraftChapter();
  });
  draftBoxDialog.addEventListener("close", () => {
    void refreshPendingDraftCount();
  });

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
  memoryPreviewButton.addEventListener("click", () => {
    void loadMemoryPreview();
  });
  memoryPreviewClose.addEventListener("click", hideMemoryPreview);
  useMemoryInput.addEventListener("change", () => {
    if (useMemoryInput.checked) return;
    hideMemoryPreview();
    showToast("记忆注入已关闭，本次不会检索记忆");
  });
  usePlanInput.addEventListener("change", () => {
    if (usePlanInput.checked) return;
    hideMemoryPreview();
    showToast("计划对齐已关闭，本次不会注入已激活计划");
  });
  useRecallSelect.addEventListener("change", () => {
    hideMemoryPreview();
    if (useRecallSelect.value === "off") showToast("前文召回已关闭，本次不会检索旧章节");
    else if (useRecallSelect.value === "on") showToast("前文召回已强制开启；不满 20 章也会检索");
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
  persistDraftButton.addEventListener("click", () => {
    void saveGenerationToDraftBox();
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
  await loadFeatures();
  await loadConfig();
  await loadProject();
  updateConfigUI();
  updateContextInfo();
  await refreshPendingDraftCount();
}

void init();
