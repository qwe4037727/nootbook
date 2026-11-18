const base = "http://127.0.0.1:8090";
const res = await fetch(base + "/api/config", {
  method: "PUT",
  headers: { "Content-Type": "application/json" },
  body: JSON.stringify({ baseUrl: "http://127.0.0.1:8091/v1", model: "ui-walk-placeholder" }),
});
console.log(res.status, JSON.stringify(await res.json()));
