// ua-parser-js side of the comparison harness (see ../README.md).
// Runs the shared corpus through ua-parser-js v2 in its strongest server-side
// configuration: Bots extension + withClientHints() + isBot/isAIBot helpers,
// so the comparison is against the best the library can do, not a strawman.
//
//   node parse.mjs ../corpus.json ../out/js.json      # dump results
//   node parse.mjs ../corpus.json --bench 200000      # benchmark
import { readFileSync, writeFileSync, mkdirSync } from 'node:fs';
import { dirname } from 'node:path';
import { UAParser } from 'ua-parser-js';
import { Bots } from 'ua-parser-js/extensions';
import { isBot, isAIBot } from 'ua-parser-js/helpers';

const [corpusPath, outArg, benchArg] = process.argv.slice(2);
const corpus = JSON.parse(readFileSync(corpusPath, 'utf8'));

// Full pipeline equivalent to core.Parser.Parse: structured result with
// Client Hints applied, plus the bot / AI-crawler booleans.
function parseEntry(entry) {
    const headers = { 'user-agent': entry.ua, ...(entry.headers ?? {}) };
    const result = UAParser(Bots, headers).withClientHints();
    return {
        browser: result.browser,
        os: result.os,
        device: result.device,
        cpu: result.cpu,
        engine: result.engine,
        is_bot: isBot(entry.ua),
        is_ai_crawler: isAIBot(entry.ua),
    };
}

if (outArg === '--bench') {
    const n = Number(benchArg ?? 100000);

    for (const entry of corpus) parseEntry(entry); // warmup

    global.gc?.();
    const start = process.hrtime.bigint();
    for (let i = 0; i < n; i++) {
        parseEntry(corpus[i % corpus.length]);
    }
    const elapsedMs = Number(process.hrtime.bigint() - start) / 1e6;

    const opsSec = (n / elapsedMs) * 1000;
    console.log(
        `ua-parser-js: ${n} parses in ${elapsedMs.toFixed(0)}ms — ` +
        `${opsSec.toFixed(0)} ops/sec, ${((elapsedMs * 1000) / n).toFixed(2)} µs/op`
    );
    console.log(`process rss: ${(process.memoryUsage().rss / 1024 / 1024).toFixed(1)} MB`);
} else {
    const report = corpus.map((entry) => ({ id: entry.id, result: parseEntry(entry) }));
    const json = JSON.stringify(report, null, 2);
    if (outArg) {
        mkdirSync(dirname(outArg), { recursive: true });
        writeFileSync(outArg, json);
    } else {
        console.log(json);
    }
}
