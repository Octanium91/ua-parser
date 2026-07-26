// Joins go.json + js.json into a compact side-by-side TSV for eyeballing
// differences: one line per corpus entry, our result vs ua-parser-js.
//   node report.mjs ../out/go.json ../out/js.json
import { readFileSync } from 'node:fs';

const [goPath, jsPath] = process.argv.slice(2);
const go = new Map(JSON.parse(readFileSync(goPath, 'utf8')).map((e) => [e.id, e.result]));
const js = new Map(JSON.parse(readFileSync(jsPath, 'utf8')).map((e) => [e.id, e.result]));

const fmt = (r, isGo) => {
    if (!r) return 'MISSING';
    const b = r.browser ?? {};
    const os = r.os ?? {};
    const d = r.device ?? {};
    const cpu = r.cpu ?? {};
    const eng = r.engine ?? {};
    const bot = isGo ? r.is_bot : r.is_bot;
    const ai = isGo ? r.is_ai_crawler : r.is_ai_crawler;
    return [
        `${b.name ?? '-'} ${b.version ?? '-'}`,
        `${os.name ?? '-'} ${os.version ?? '-'}`,
        `dev:${d.type ?? '-'}/${d.vendor ?? '-'}/${d.model ?? '-'}`,
        `cpu:${cpu.architecture ?? '-'}`,
        `eng:${eng.name ?? '-'} ${eng.version ?? '-'}`,
        `bot:${bot ? 1 : 0}${ai ? '+AI' : ''}`,
    ].join(' | ');
};

for (const id of go.keys()) {
    console.log(`### ${id}`);
    console.log(`  go: ${fmt(go.get(id), true)}`);
    console.log(`  js: ${fmt(js.get(id), false)}`);
}
