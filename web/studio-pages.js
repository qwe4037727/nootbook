(() => {
  const main = document.getElementById("studio-main");
  const host = document.getElementById("studio-pages");
  const editor = document.getElementById("writingPage");
  const unavailable = document.getElementById("unavailablePage");
  const nav = document.querySelector(".flight-nav");
  const triggers = {
    adapt: "adaptSelectionButton", outline: "planButton", drafts: "draftBoxButton",
    chapters: "manuscriptButton", world: "world-center-btn", memory: "memoryReviewButton",
    images: "imageCenterButton", "image-model": "imageModelButton", projects: "projectSwitchButton",
    import: "importButton", export: "exportButton", snapshots: "snapshotButton",
    agents: "agentCenterButton", echo: "agentRunsButton", tasks: "task-center-btn",
    settings: "settingsButton", candidates: "candidateButton",
  };
  const extraTitles = { echo: "Echo 观察", "image-model": "图像模型", candidates: "续写候选" };
  const decision = document.createElement("form");
  decision.className = "studio-decision";
  decision.hidden = true;
  decision.setAttribute("aria-label", "确认操作");
  main.prepend(decision);

  let current = { route: "editor", page: editor };
  let previousRoute = "editor";
  let historyIndex = 0;
  let activeURL = location.href;
  let ready = false;
  let pendingRoute;
  let navigating = false;
  let activating = false;
  let cancelDecision;
  let restoreIndex;
  let chapterID = "";

  function urlFor(route) {
    const url = new URL(activeURL);
    if (studioProjectID) url.searchParams.set("projectId", studioProjectID);
    if (chapterID) url.searchParams.set("chapter", chapterID);
    else url.searchParams.delete("chapter");
    url.hash = route === "editor" ? "" : route;
    return url;
  }

  function writeHistory(route, replace = false) {
    const url = urlFor(route);
    if (!replace && url.href === activeURL) return;
    if (!replace) historyIndex += 1;
    history[replace ? "replaceState" : "pushState"]({ studioIndex: historyIndex }, "", url);
    activeURL = url.href;
  }

  function display(route, page, options = {}) {
    if (current.page !== page) {
      cancelDecision?.();
      const old = current;
      old.page.hidden = true;
      old.onClose?.();
      previousRoute = old.route;
      if (page !== editor && page.parentNode !== host) host.append(page);
      current = { route, page, ...options };
      page.hidden = false;
      window.scrollTo(0, 0);
    }
    const active = nav.querySelector(`[data-route="${route}"]`);
    const title = active?.textContent.trim().slice(1).trim() || extraTitles[route] || "写作工作台";
    nav.querySelectorAll("[aria-current]").forEach((link) => link.removeAttribute("aria-current"));
    active?.setAttribute("aria-current", "page");
    document.getElementById("studio-route-label").textContent = title;
    document.title = `${title} · The World`;
    for (const link of nav.querySelectorAll("[data-route]")) link.href = urlFor(link.dataset.route).href;
  }

  function open(route, page, options) {
    if (!ready || !page) return false;
    if (!activating && (decisionOpen() || navigating || current.canLeave)) return false;
    display(route, page, options);
    if (!activating) writeHistory(route);
    return true;
  }

  function decisionOpen() { return !decision.hidden; }

  async function navigate(route, { chapter, popIndex, popURL, initial = false } = {}) {
    if (navigating || (decisionOpen() && popIndex === undefined)) return false;
    if (decisionOpen()) cancelDecision();
    navigating = true;
    const priorIndex = historyIndex;
    try {
      if (route !== current.route && current.canLeave && !await current.canLeave()) return false;
      if (chapter && chapter !== chapterID && !await selectChapter(chapter)) return false;
      if (popURL) activeURL = popURL;
      if (popIndex !== undefined) historyIndex = popIndex;
      if (route !== current.route) {
        if (route === "editor") display(route, editor);
        else if (route === "continue") display(route, document.getElementById("continuationPage"));
        else {
          const trigger = document.getElementById(triggers[route] || "");
          activating = true;
          try {
            if (trigger && !trigger.hidden && !trigger.disabled) trigger.click();
          } finally {
            activating = false;
          }
          if (current.route !== route) display(route, unavailable);
        }
        if (!pageContainsFocus()) main.focus({ preventScroll: true });
      } else if (initial) {
        display(route, current.page);
      }
      writeHistory(route, initial || popURL !== undefined);
      return true;
    } finally {
      navigating = false;
      if (popURL && activeURL !== popURL && location.href === popURL && popIndex !== undefined && popIndex !== priorIndex) {
        restoreIndex = priorIndex;
        history.go(priorIndex - popIndex);
      }
    }
  }

  function pageContainsFocus() { return current.page.contains(document.activeElement); }

  function close(page) {
    if (current.page !== page || page === editor) return Promise.resolve(false);
    const returnRoute = current.route === "image-model" && previousRoute === "images" ? "images" : "editor";
    return navigate(returnRoute);
  }

  function ask(message, choices, { input, cancel = null } = {}) {
    if (decisionOpen()) return Promise.resolve(cancel);
    const origin = document.activeElement;
    return new Promise((resolve) => {
      const description = document.createElement("p");
      description.id = "studio-decision-text";
      description.textContent = message;
      decision.setAttribute("aria-describedby", description.id);
      decision.replaceChildren(description);
      let field;
      if (input !== undefined) {
        field = document.createElement("input");
        field.value = input;
        field.setAttribute("aria-labelledby", description.id);
        decision.append(field);
      }
      const buttons = document.createElement("div");
      buttons.className = "studio-decision-actions";
      decision.append(buttons);
      function finish(value) {
        if (!cancelDecision) return;
        cancelDecision = null;
        decision.hidden = true;
        decision.replaceChildren();
        decision.onsubmit = null;
        decision.onkeydown = null;
        if (origin instanceof HTMLElement && origin.isConnected) origin.focus({ preventScroll: true });
        resolve(value);
      }
      cancelDecision = () => finish(cancel);
      const dismiss = document.createElement("button");
      dismiss.type = "button";
      dismiss.className = "btn btn-quiet";
      dismiss.textContent = "取消";
      dismiss.onclick = cancelDecision;
      buttons.append(dismiss);
      for (const [label, value] of choices) {
        const button = document.createElement("button");
        button.type = "button";
        button.className = "btn btn-primary";
        button.textContent = label;
        button.onclick = () => finish(field ? field.value : value);
        buttons.append(button);
      }
      decision.onsubmit = (event) => {
        event.preventDefault();
        if (field) finish(field.value);
      };
      decision.onkeydown = (event) => {
        if (event.key !== "Escape") return;
        event.preventDefault();
        finish(cancel);
      };
      decision.hidden = false;
      (field || dismiss).focus();
      field?.select();
    });
  }

  document.querySelector(".studio-skip").addEventListener("click", (event) => {
    event.preventDefault();
    main.focus();
    main.scrollIntoView({ block: "start" });
  });

  document.addEventListener("click", (event) => {
    const link = event.target.closest("a[data-route]");
    if (!link || event.button !== 0 || event.metaKey || event.ctrlKey || event.shiftKey || event.altKey) return;
    event.preventDefault();
    if (ready) void navigate(link.dataset.route);
    else pendingRoute = link.dataset.route;
  });

  window.addEventListener("popstate", (event) => {
    if (!ready) return;
    const popIndex = event.state?.studioIndex ?? historyIndex;
    if (restoreIndex === popIndex) {
      restoreIndex = undefined;
      return;
    }
    if (location.hash === "#studio-main") return;
    const url = new URL(location.href);
    void navigate(readRoute(url), { chapter: url.searchParams.get("chapter"), popIndex, popURL: url.href });
  });

  function readRoute(url) {
    const route = url.hash.slice(1);
    return Object.hasOwn(triggers, route) || route === "continue" ? route : "editor";
  }

  window.studioPages = {
    open, close, ask,
    confirm: (message) => ask(message, [["确认", true]], { cancel: false }),
    prompt: (message, initial = "") => ask(message, [["确认", true]], { input: initial }),
    showEditor: (chapter) => navigate("editor", { chapter }),
    showContinuation: () => navigate("continue"),
    isCurrent: (route) => current.route === route,
    decisionOpen,
    updateChapter(id) {
      chapterID = id;
      if (ready && !navigating) {
        writeHistory(current.route, true);
        display(current.route, current.page);
      }
    },
    init() {
      ready = true;
      historyIndex = history.state?.studioIndex ?? 0;
      for (const route of ["images", "export"]) {
        nav.querySelector(`[data-route="${route}"]`).hidden = document.getElementById(triggers[route]).hidden;
      }
      return navigate(pendingRoute || readRoute(new URL(location.href)), { initial: true });
    },
  };
})();
