import http from "node:http";

// 只用于 UI 走查：接受续写请求，延迟后再失败，好让 SSE meta 在界面上停留。
const server = http.createServer((req, res) => {
  const chunks = [];
  req.on("data", (c) => chunks.push(c));
  req.on("end", () => {
    const body = Buffer.concat(chunks).toString("utf8");
    let model = "?";
    try {
      model = JSON.parse(body).model;
    } catch {}
    console.log(`stub-llm <- ${req.method} ${req.url} model=${model} bytes=${body.length}`);
    setTimeout(() => {
      res.writeHead(502, { "Content-Type": "application/json" });
      res.end(JSON.stringify({ error: { message: "stub-llm: no model channel (UI walk only)" } }));
    }, 6000);
  });
});

server.listen(Number(process.env.STUB_PORT) || 8092, "127.0.0.1", () => {
  console.log("stub-llm listening on http://127.0.0.1:" + (Number(process.env.STUB_PORT) || 8092) + "/v1");
});
