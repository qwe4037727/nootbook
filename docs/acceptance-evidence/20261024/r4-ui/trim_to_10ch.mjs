const base = "http://127.0.0.1:8093";
const project = await (await fetch(base + "/api/project")).json();
const chapters = project.chapters || [];
const keep = 10;
const doomed = chapters.filter((c) => c.index > keep);
console.log("chapters=" + chapters.length + " delete=" + doomed.length);
for (const c of doomed) {
  const res = await fetch(base + "/api/chapters/" + c.id, { method: "DELETE" });
  if (!res.ok) console.log("delete failed", c.index, res.status, (await res.text()).slice(0, 120));
}
const after = await (await fetch(base + "/api/project")).json();
console.log("after=" + (after.chapters || []).length);
const cfg = await (await fetch(base + "/api/config", {
  method: "PUT",
  headers: { "Content-Type": "application/json" },
  body: JSON.stringify({ baseUrl: "http://127.0.0.1:8092/v1", model: "ui-walk-placeholder" }),
})).json();
console.log("config=" + JSON.stringify(cfg));
