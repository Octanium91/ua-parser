// Benchmarks THIS project's Node.js client (koffi FFI -> Go shared library);
// the ua-parser-js side lives in parse.mjs. See ../README.md.
//
//   node ours.cjs ../corpus.json <N> [cacheSize] [libPath]
//
// libPath points at the platform driver (bundled in the published npm package;
// when running from the repo pass the file from GitHub Releases explicitly).
// koffi resolves from clients/node/node_modules via the client's own require.
const { readFileSync } = require('node:fs');
const path = require('node:path');

const UaParser = require(path.join(__dirname, '../../../clients/node/lib/index.js'));

async function main() {
    const [corpusPath, nArg, cacheArg, libPath] = process.argv.slice(2);
    const n = Number(nArg);
    const cacheSize = Number(cacheArg ?? 0);
    const corpus = JSON.parse(readFileSync(corpusPath, 'utf8'));

    const t0 = process.hrtime.bigint();
    const parser = new UaParser(libPath || undefined);
    await parser.init({ disable_auto_update: true, lru_cache_size: cacheSize });
    parser.parse(corpus[0].ua, corpus[0].headers ?? {});
    const initMs = Number(process.hrtime.bigint() - t0) / 1e6;
    console.log(`mode: ${parser.isWasm ? 'wasm' : 'native'}`);

    for (const e of corpus) parser.parse(e.ua, e.headers ?? {}); // warmup

    const t1 = process.hrtime.bigint();
    for (let i = 0; i < n; i++) {
        const e = corpus[i % corpus.length];
        parser.parse(e.ua, e.headers ?? {});
    }
    const elapsedMs = Number(process.hrtime.bigint() - t1) / 1e6;

    console.log(
        `impl=ours-node cache=${cacheSize} init=${initMs.toFixed(0)}ms | ` +
        `${n} parses in ${elapsedMs.toFixed(0)}ms — ` +
        `${(n / elapsedMs * 1000).toFixed(0)} ops/sec, ${(elapsedMs * 1000 / n).toFixed(2)} µs/op`
    );
    console.log(`process rss: ${(process.memoryUsage().rss / 1024 / 1024).toFixed(1)} MB`);
}

main().catch((e) => { console.error(e); process.exit(1); });
