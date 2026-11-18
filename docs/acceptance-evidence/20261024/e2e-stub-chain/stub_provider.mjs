// Local OpenAI-compatible stub provider for acceptance runs (no model key involved).
// Serves POST /v1/chat/completions and logs every inbound request (full prompt) to JSONL,
// so the prompt actually handed to the "model" is itself evidence.
import http from 'node:http';
import fs from 'node:fs';

const PORT = Number(process.env.STUB_PORT || 8124);
const LOG = process.env.STUB_LOG || 'output/stub-requests.jsonl';
fs.writeFileSync(LOG, '');

// One object valid for every non-streaming parser in the pipeline:
// chapter analysis (summary/endsAt/characters/world/style), knowledge synthesis (overview),
// deep memory extraction (events/states/cognitions/threads/entityHints), plan generation (global/volumes).
const ANSWER = {
  summary: '本章里林砚在县衙外停留，握紧青玉符并想起三年前的旧案。',
  endsAt: '林砚转身走进雨里。',
  characters: [
    { name: '林砚', role: '县衙捕快', personality: '沉默克制', state: '左臂旧伤未愈', confidence: 0.9, evidence: ['握紧青玉符'] },
    { name: '沈青禾', role: '医者', personality: '谨慎', confidence: 0.8 },
  ],
  world: [{ title: '县衙', category: '地点', content: '县城的衙门，夜里有值房灯火。', confidence: 0.8 }],
  style: { perspective: '第三人称限知', samples: ['雨落在青石板上。'] },
  overview: '林砚在县城追查三年前的旧案，左臂带伤，手中一枚青玉符牵出伏笔。',
  events: [
    {
      summary: '林砚在县衙外握紧青玉符',
      eventType: 'item_transfer',
      participants: ['林砚'],
      participantNamesProvided: true,
      quote: '握紧青玉符',
      preconditions: ['林砚到达县衙外'],
      consequences: ['旧案线索被重新提起'],
    },
  ],
  states: [
    { subject: '林砚', category: '身体', attribute: '伤势', value: '左臂旧伤开始发凉', quote: '左臂旧伤开始发凉' },
  ],
  cognitions: [
    { subject: '林砚', content: '认定青玉符能打开旧案缺口', status: 'known', quote: '握紧青玉符' },
  ],
  threads: [
    { title: '青玉符的来历', action: 'new', clue: '青玉符上的刻痕', quote: '青玉符上的刻痕' },
  ],
  entityHints: [{ name: '黑衣人', aliases: [] }],
  global: {
    title: '旧案主线',
    objective: '让林砚沿青玉符追查三年前旧案，不提前揭露真凶。',
    constraints: ['不提前收束', '不改变已确认事实'],
    candidateBeats: ['在县衙重验卷宗', '沈青禾提出伤药线索'],
    characters: ['林砚', '沈青禾'],
    flexibility: '中',
  },
  volumes: [
    {
      title: '第一卷：符与伤',
      objective: '把青玉符与左臂旧伤绑成同一追查动机。',
      constraints: ['保留伏笔未解'],
      candidateBeats: ['夜访卷房', '雨夜对峙'],
      characters: ['林砚'],
      flexibility: '高',
    },
  ],
};

const PROSE =
  '门外的雨停了三息，脚步声才落到青石板上。\n\n林砚把青玉符收回袖中，指节在门框上叩了两下。\n\n"卷宗不在值房。"沈青禾的声音隔着雨传来。\n';

const server = http.createServer((req, res) => {
  let raw = '';
  req.on('data', (c) => (raw += c));
  req.on('end', () => {
    let body = {};
    try {
      body = JSON.parse(raw);
    } catch {}
    const messages = Array.isArray(body.messages) ? body.messages : [];
    fs.appendFileSync(
      LOG,
      JSON.stringify({
        at: new Date().toISOString(),
        path: req.url,
        model: body.model,
        stream: !!body.stream,
        temperature: body.temperature,
        messageCount: messages.length,
        roles: messages.map((m) => m.role),
        chars: messages.map((m) => (m.content || '').length),
        messages,
      }) + '\n',
    );

    if (body.stream) {
      res.writeHead(200, { 'Content-Type': 'text/event-stream' });
      for (const part of PROSE.match(/[\s\S]{1,40}/g) || []) {
        res.write('data: ' + JSON.stringify({ choices: [{ delta: { content: part }, finish_reason: null }] }) + '\n\n');
      }
      res.write('data: ' + JSON.stringify({ choices: [{ delta: {}, finish_reason: 'stop' }] }) + '\n\n');
      res.end('data: [DONE]\n\n');
      return;
    }
    res.writeHead(200, { 'Content-Type': 'application/json' });
    res.end(JSON.stringify({ choices: [{ message: { role: 'assistant', content: JSON.stringify(ANSWER) }, finish_reason: 'stop' }] }));
  });
});

server.listen(PORT, '127.0.0.1', () => console.log('stub provider on http://127.0.0.1:' + PORT + '/v1 log=' + LOG));
