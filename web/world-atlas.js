(() => {
  "use strict";

  // Each kind has a separate region. Fibonacci points give entries stable depth
  // and spacing within a region without implying physical story coordinates.
  const regions = new Map([
    ["character", { x: -0.64, y: 0.31, z: 0.14, color: "#78d7f3" }],
    ["setting", { x: 0.64, y: 0.31, z: -0.14, color: "#c8afff" }],
    ["faction", { x: -0.36, y: -0.47, z: -0.4, color: "#f4ab86" }],
    ["location", { x: 0.42, y: -0.32, z: 0.34, color: "#88dfb7" }],
    ["item", { x: 0.13, y: 0.54, z: 0.4, color: "#f6d681" }],
  ]);
  const otherRegion = { x: 0, y: 0, z: 0, color: "#d0dbe9" };
  const initialCamera = { yaw: -0.22, pitch: 0.18, zoom: 1 };
  const clamp = (value, min, max) => Math.min(max, Math.max(min, value));
  const rows = (value) => Array.isArray(value) ? value : [];
  const searchText = (value) => String(value ?? "").toLocaleLowerCase();

  function element(tag, text = "", className = "") {
    const node = document.createElement(tag);
    if (text) node.textContent = text;
    if (className) node.className = className;
    return node;
  }

  function button(text, action, className = "btn btn-quiet btn-sm") {
    const node = element("button", text, className);
    node.type = "button";
    node.addEventListener("click", action);
    return node;
  }

  function field(label, control) {
    const wrapper = element("label", "", "world-atlas-field");
    wrapper.append(element("span", label), control);
    return wrapper;
  }

  function fillOptions(select, options, value) {
    select.replaceChildren(...options.map(([id, label]) => new Option(label, id)));
    select.value = value;
  }

  function layoutEntries(entries) {
    const groups = new Map();
    for (const entry of entries) {
      const kind = regions.has(entry.kind) ? entry.kind : "";
      if (!groups.has(kind)) groups.set(kind, []);
      groups.get(kind).push(entry);
    }
    const nodes = new Map();
    for (const [kind, group] of groups) {
      const region = regions.get(kind) || otherRegion;
      group.sort((a, b) => a.id.localeCompare(b.id));
      const spread = group.length === 1 ? 0 : Math.min(0.55, 0.14 + Math.cbrt(group.length) * 0.078);
      group.forEach((entry, index) => {
        const height = 1 - 2 * (index + 0.5) / group.length;
        const radius = Math.sqrt(1 - height * height) * spread;
        const phase = index * Math.PI * (3 - Math.sqrt(5));
        nodes.set(entry.id, {
          entry, color: region.color,
          x: region.x + radius * Math.cos(phase),
          y: region.y + spread * height,
          z: region.z + radius * Math.sin(phase),
          keywords: [entry.name, ...rows(entry.aliases), entry.summary, ...Object.values(entry.fields || {})]
            .map(searchText).join("\n"),
        });
      });
    }
    return nodes;
  }

  function project(point, camera, width, height) {
    const cosYaw = Math.cos(camera.yaw);
    const sinYaw = Math.sin(camera.yaw);
    const cosPitch = Math.cos(camera.pitch);
    const sinPitch = Math.sin(camera.pitch);
    const x = point.x * cosYaw - point.z * sinYaw;
    const turn = point.x * sinYaw + point.z * cosYaw;
    const y = point.y * cosPitch - turn * sinPitch;
    const depth = point.y * sinPitch + turn * cosPitch;
    const perspective = 3.1 / (3.1 - depth);
    const scale = Math.min(width, height) * 0.43 * camera.zoom * perspective;
    return { x: width / 2 + x * scale, y: height / 2 - y * scale, depth, perspective };
  }

  function drawEdge(ctx, relation, from, to, highlighted) {
    const distance = Math.hypot(to.x - from.x, to.y - from.y);
    if (distance < from.radius + to.radius + 5) return;
    const dx = (to.x - from.x) / distance;
    const dy = (to.y - from.y) / distance;
    const endX = to.x - dx * (to.radius + 3);
    const endY = to.y - dy * (to.radius + 3);
    const invalid = relation.status === "invalidated";
    const color = invalid ? "#b2a1af" : "#9cbfe0";
    ctx.globalAlpha = highlighted ? 0.95 : invalid ? 0.35 : 0.46;
    ctx.strokeStyle = color;
    ctx.lineWidth = highlighted ? 1.7 : 1;
    ctx.setLineDash(invalid ? [4, 4] : []);
    ctx.beginPath();
    ctx.moveTo(from.x + dx * (from.radius + 2), from.y + dy * (from.radius + 2));
    ctx.lineTo(endX, endY);
    ctx.stroke();
    ctx.setLineDash([]);
    const size = highlighted ? 7 : 5;
    ctx.fillStyle = color;
    ctx.beginPath();
    ctx.moveTo(endX, endY);
    ctx.lineTo(endX - dx * size + dy * size * 0.48, endY - dy * size - dx * size * 0.48);
    ctx.lineTo(endX - dx * size - dy * size * 0.48, endY - dy * size + dx * size * 0.48);
    ctx.closePath();
    ctx.fill();
    ctx.globalAlpha = 1;
  }

  function mount(parent, { entries, relations, kinds, relationStatuses, view, openEntry, openRelation }) {
    const nodes = layoutEntries(entries);
    const edges = rows(relations).filter(({ fromEntryId, toEntryId }) => nodes.has(fromEntryId) && nodes.has(toEntryId));
    const camera = view.camera ??= { ...initialCamera };
    const root = element("div", "", "world-atlas");
    const filters = element("div", "", "world-atlas-filters");
    const search = element("input");
    search.type = "search";
    search.placeholder = "名称、别名、摘要或字段";
    search.value = view.query || "";
    const kindFilter = element("select");
    fillOptions(kindFilter, [["", "全部类型"], ...kinds], view.kind || "");
    const statusFilter = element("select");
    fillOptions(statusFilter, [["", "全部状态"], ...relationStatuses], view.status || "");
    statusFilter.disabled = relations === null;
    filters.append(field("搜索词条", search), field("词条类型", kindFilter), field("关系状态", statusFilter));

    const body = element("div", "", "world-atlas-body");
    const stage = element("div", "", "world-atlas-stage");
    const canvas = element("canvas", "", "world-atlas-canvas");
    canvas.tabIndex = 0;
    canvas.setAttribute("aria-label", "词条 3D 关系图，使用方向键旋转，使用加减键缩放");
    const ctx = canvas.getContext("2d");
    const summary = element("p", "", "world-atlas-count");
    summary.setAttribute("role", "status");
    const empty = element("p", "", "world-atlas-empty");
    const controls = element("div", "", "world-atlas-controls");
    const zoom = (factor) => {
      camera.zoom = clamp(camera.zoom * factor, 0.55, 2.6);
      scheduleDraw();
    };
    const reset = () => {
      Object.assign(camera, initialCamera);
      scheduleDraw();
    };
    const zoomOut = button("−", () => zoom(1 / 1.2), "world-atlas-tool");
    zoomOut.setAttribute("aria-label", "缩小图谱");
    const zoomIn = button("+", () => zoom(1.2), "world-atlas-tool");
    zoomIn.setAttribute("aria-label", "放大图谱");
    controls.append(zoomOut, zoomIn, button("重置视角", reset, "world-atlas-tool"));
    stage.append(canvas, summary, empty, controls);

    const legend = element("ul", "", "world-atlas-legend");
    legend.setAttribute("aria-label", "图例");
    for (const [kind, label] of kinds) {
      const item = element("li", label);
      item.style.setProperty("--atlas-key", regions.get(kind)?.color || otherRegion.color);
      legend.append(item);
    }
    legend.append(element("li", "有效关系", "world-atlas-key-edge"),
      element("li", "已失效关系", "world-atlas-key-edge world-atlas-key-invalid"));
    const chart = element("div", "", "world-atlas-chart");
    chart.append(stage, legend);

    const inspector = element("aside", "", "world-atlas-inspector");
    const pick = element("select");
    const detail = element("div", "", "world-atlas-detail");
    inspector.append(field("定位词条", pick), detail);
    body.append(chart, inspector);
    root.append(filters, body, element("p", "拖动旋转 · 滚轮缩放 · 点击节点打开词条。节点按词条类型分布。", "world-atlas-help"));
    parent.append(root);

    let visibleNodes = [];
    let visibleEdges = [];
    let projected = [];
    let highlightedId = "";
    let hoverId = "";
    let frame = 0;
    let drag;
    let destroyed = false;

    function updateDetail(id) {
      view.selectedId = id;
      detail.replaceChildren();
      const entry = nodes.get(id)?.entry;
      if (!entry) {
        detail.append(element("p", "选择一个词条可查看相关关系，或直接点击图中的节点打开词条。"));
        return;
      }
      detail.append(element("p", kinds.get(entry.kind) || entry.kind, "world-atlas-kind"), element("h4", entry.name));
      if (entry.summary) detail.append(element("p", entry.summary));
      detail.append(button("打开词条", () => openEntry(id), "btn btn-primary btn-sm"));
      const linked = visibleEdges.filter((edge) => edge.fromEntryId === id || edge.toEntryId === id);
      detail.append(element("h5", `当前筛选下的关系 · ${linked.length}`));
      if (!linked.length) {
        detail.append(element("p", relations === null ? "关系暂时无法读取。" : "没有可显示的关系。"));
        return;
      }
      const connections = element("ul", "", "world-atlas-links");
      for (const edge of linked) {
        const from = nodes.get(edge.fromEntryId).entry.name;
        const to = nodes.get(edge.toEntryId).entry.name;
        const relation = element("li");
        relation.append(button(`${from} → ${to} · ${edge.label || edge.type}`, () => openRelation(edge)));
        const status = relationStatuses.get(edge.status) || edge.status;
        const range = edge.startChapterIndex || edge.endChapterIndex
          ? ` · 章序 ${edge.startChapterIndex || "不限"}–${edge.endChapterIndex || "不限"}` : "";
        relation.append(element("small", status + range));
        connections.append(relation);
      }
      detail.append(connections);
    }

    function applyFilters() {
      const query = searchText(search.value.trim());
      view.query = search.value;
      view.kind = kindFilter.value;
      view.status = statusFilter.value;
      visibleNodes = [...nodes.values()].filter(({ entry, keywords }) =>
        (!view.kind || entry.kind === view.kind) && (!query || keywords.includes(query)));
      const visibleIds = new Set(visibleNodes.map(({ entry }) => entry.id));
      visibleEdges = edges.filter(({ fromEntryId, toEntryId, status }) =>
        visibleIds.has(fromEntryId) && visibleIds.has(toEntryId) && (!view.status || status === view.status));
      const id = visibleIds.has(view.selectedId) ? view.selectedId : "";
      highlightedId = id;
      hoverId = "";
      fillOptions(pick, [["", "选择词条"], ...visibleNodes
        .map(({ entry }) => [entry.id, `${entry.name} · ${kinds.get(entry.kind) || entry.kind}`])
        .sort((a, b) => a[1].localeCompare(b[1], "zh-CN"))], id);
      pick.disabled = visibleNodes.length === 0;
      summary.textContent = `${visibleNodes.length} / ${nodes.size} 个词条 · ${relations === null ? "关系未加载" : `${visibleEdges.length} / ${edges.length} 条关系`}`;
      empty.textContent = !nodes.size ? "此作品还没有世界词条。请在「词条」中添加。"
        : !visibleNodes.length ? "没有符合筛选条件的词条。"
          : !ctx ? "无法绘制 3D 图谱。可从旁边的词条选择器继续浏览。" : "";
      empty.hidden = !empty.textContent;
      controls.hidden = !ctx || !visibleNodes.length;
      canvas.hidden = !ctx;
      stage.classList.remove("has-hit");
      projected = [];
      updateDetail(id);
      scheduleDraw();
    }

    function scheduleDraw() {
      if (ctx && !frame && !destroyed) frame = requestAnimationFrame(draw);
    }

    function draw() {
      frame = 0;
      const { width, height } = canvas.getBoundingClientRect();
      if (!width || !height) return;
      const ratio = Math.min(2, window.devicePixelRatio || 1);
      const pixelWidth = Math.ceil(width * ratio);
      const pixelHeight = Math.ceil(height * ratio);
      if (canvas.width !== pixelWidth || canvas.height !== pixelHeight) {
        canvas.width = pixelWidth;
        canvas.height = pixelHeight;
      }
      ctx.setTransform(pixelWidth / width, 0, 0, pixelHeight / height, 0, 0);
      ctx.clearRect(0, 0, width, height);
      const activeId = hoverId || highlightedId;
      const byId = new Map();
      projected = visibleNodes.map((node) => {
        const point = project(node, camera, width, height);
        const item = { ...point, node, radius: clamp(5.5 * point.perspective * Math.sqrt(camera.zoom), 3.5, 11) };
        byId.set(node.entry.id, item);
        return item;
      }).sort((a, b) => a.depth - b.depth);

      for (const edge of visibleEdges) {
        const highlighted = edge.fromEntryId === activeId || edge.toEntryId === activeId;
        drawEdge(ctx, edge, byId.get(edge.fromEntryId), byId.get(edge.toEntryId), highlighted);
      }
      for (const { x, y, radius, node } of projected) {
        const selected = node.entry.id === activeId;
        ctx.fillStyle = node.color;
        ctx.globalAlpha = selected ? 0.27 : 0.14;
        ctx.beginPath();
        ctx.arc(x, y, radius + (selected ? 7 : 3), 0, Math.PI * 2);
        ctx.fill();
        ctx.globalAlpha = 1;
        ctx.beginPath();
        ctx.arc(x, y, radius, 0, Math.PI * 2);
        ctx.fill();
        if (selected) {
          ctx.strokeStyle = "#fff";
          ctx.lineWidth = 1.5;
          ctx.stroke();
        }
      }

      // Prioritize the hovered/selected label and omit collisions in dense graphs.
      const labels = [];
      const active = byId.get(activeId);
      const candidates = projected.slice(-85).reverse().filter((point) => point !== active);
      if (active) candidates.unshift(active);
      ctx.font = '12px system-ui, "Microsoft YaHei", sans-serif';
      ctx.textBaseline = "middle";
      for (const point of candidates) {
        const name = point.node.entry.name;
        if (point.x < 0 || point.x > width || point.y < 34 || point.y > height - 25) continue;
        const textWidth = Math.min(ctx.measureText(name).width, width * 0.45);
        const left = point.x + point.radius + 8;
        const x = left + textWidth < width - 6 ? left : Math.max(6, point.x - textWidth - point.radius - 8);
        const box = { x, y: point.y - 7, right: x + textWidth, bottom: point.y + 7 };
        if (labels.some((other) => box.x < other.right + 5 && box.right > other.x - 5
          && box.y < other.bottom + 3 && box.bottom > other.y - 3)) continue;
        labels.push(box);
        ctx.strokeStyle = "#101a2c";
        ctx.lineWidth = 3;
        ctx.strokeText(name, x, point.y, textWidth);
        ctx.fillStyle = point === active ? "#ffffff" : "#d8e5f3";
        ctx.fillText(name, x, point.y, textWidth);
      }
    }

    function hit(event) {
      const bounds = canvas.getBoundingClientRect();
      const x = event.clientX - bounds.left;
      const y = event.clientY - bounds.top;
      for (let index = projected.length - 1; index >= 0; index--) {
        const point = projected[index];
        if (Math.hypot(point.x - x, point.y - y) <= Math.max(10, point.radius + 4)) {
          return point.node.entry.id;
        }
      }
      return "";
    }

    function rotate(yaw, pitch) {
      camera.yaw = (camera.yaw + yaw) % (Math.PI * 2);
      camera.pitch = clamp(camera.pitch + pitch, -1.5, 1.5);
      scheduleDraw();
    }

    canvas.addEventListener("pointerdown", (event) => {
      if (!event.isPrimary || event.button !== 0) return;
      canvas.setPointerCapture(event.pointerId);
      drag = { id: event.pointerId, x: event.clientX, y: event.clientY, moved: false };
    });
    canvas.addEventListener("pointermove", (event) => {
      if (drag?.id === event.pointerId) {
        const dx = event.clientX - drag.x;
        const dy = event.clientY - drag.y;
        if (!drag.moved && Math.hypot(dx, dy) < 5) return;
        drag = { ...drag, x: event.clientX, y: event.clientY, moved: true };
        stage.classList.add("is-dragging");
        hoverId = "";
        rotate(dx * 0.009, dy * 0.009);
      } else if (!drag && event.pointerType !== "touch") {
        const id = hit(event);
        if (id === hoverId) return;
        hoverId = id;
        stage.classList.toggle("has-hit", Boolean(id));
        scheduleDraw();
      }
    });
    function endDrag(event) {
      if (drag?.id !== event.pointerId) return;
      const id = event.type === "pointerup" && !drag.moved ? hit(event) : "";
      drag = null;
      stage.classList.remove("is-dragging");
      if (id) {
        pick.value = id;
        highlightedId = id;
        updateDetail(id);
        scheduleDraw();
        openEntry(id);
      }
    }
    canvas.addEventListener("pointerup", endDrag);
    canvas.addEventListener("pointercancel", endDrag);
    canvas.addEventListener("lostpointercapture", endDrag);
    canvas.addEventListener("pointerleave", () => {
      hoverId = "";
      stage.classList.remove("has-hit");
      scheduleDraw();
    });
    canvas.addEventListener("wheel", (event) => {
      if (event.ctrlKey || event.metaKey || !visibleNodes.length || !event.deltaY) return;
      event.preventDefault();
      zoom(event.deltaY < 0 ? 1.12 : 1 / 1.12);
    }, { passive: false });
    canvas.addEventListener("keydown", (event) => {
      if (event.ctrlKey || event.metaKey || event.altKey) return;
      switch (event.key) {
        case "ArrowLeft": rotate(-0.13, 0); break;
        case "ArrowRight": rotate(0.13, 0); break;
        case "ArrowUp": rotate(0, -0.13); break;
        case "ArrowDown": rotate(0, 0.13); break;
        case "=":
        case "+": zoom(1.2); break;
        case "-": zoom(1 / 1.2); break;
        case "0": reset(); break;
        default: return;
      }
      event.preventDefault();
    });

    search.addEventListener("input", applyFilters);
    kindFilter.addEventListener("change", applyFilters);
    statusFilter.addEventListener("change", applyFilters);
    pick.addEventListener("change", () => {
      highlightedId = pick.value;
      updateDetail(pick.value);
      scheduleDraw();
    });
    const observer = ctx ? new ResizeObserver(scheduleDraw) : null;
    observer?.observe(stage);
    applyFilters();

    return {
      destroy() {
        destroyed = true;
        observer?.disconnect();
        cancelAnimationFrame(frame);
        root.remove();
      },
    };
  }

  window.worldAtlas = { mount };
})();
