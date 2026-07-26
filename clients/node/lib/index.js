const isBrowser = typeof window !== 'undefined' && typeof window.document !== 'undefined';

let koffi;
let koffiLoadError = null;
let path;
let fs;
if (!isBrowser) {
    path = require('path');
    fs = require('fs');
    try {
        koffi = require('koffi');
    } catch (e) {
        // koffi itself may fail to load (unsupported platform, missing prebuilt
        // binding, etc.). Remember the error and let init() fall back to the
        // bundled WebAssembly module instead of crashing at require time.
        koffiLoadError = e;
    }
}

let nativeFallbackWarned = false;

class UaParser {
    /**
     * @param {string} [libPath] Path to the shared library (.so, .dll, .dylib) or, in the browser, the URL of the js/wasm module (ua-parser-js.wasm)
     */
    constructor(libPath) {
        this.libPath = libPath;
        this.isInitialized = false;
        this.isWasm = isBrowser;
        this.lib = null;
    }

    /**
     * Initializes the parser. In browser, this loads the WebAssembly module.
     * @param {Object} [config]
     * @param {boolean} [config.disable_auto_update]
     * @param {number} [config.lru_cache_size]
     * @returns {Promise<void>|void}
     */
    async init(config = {}) {
        if (this.isInitialized) return;

        if (isBrowser) {
            await this._initWasm(config);
        } else {
            try {
                this._initNode(config);
            } catch (nativeError) {
                // Native library failed — try WASM fallback
                const wasmFile = path.join(__dirname, 'ua-parser.wasm');
                if (fs.existsSync(wasmFile)) {
                    if (!nativeFallbackWarned) {
                        nativeFallbackWarned = true;
                        console.warn('WARN: Native UA-Parser library failed to load: ' + nativeError.message);
                        if (this._isMusl()) {
                            console.warn('WARN: Native mode is unavailable on Alpine Linux / musl until the upstream Go toolchain fix lands (https://github.com/golang/go/issues/54805). WASM mode engages automatically.');
                        }
                        console.warn('WARN: Falling back to WebAssembly (WASM) mode.');
                    }
                    this.isWasm = true;
                    await this._initWasmNode(config, wasmFile);
                } else {
                    throw nativeError;
                }
            }
        }
        this.isInitialized = true;
    }

    _isMusl() {
        if (process.platform !== 'linux') return false;
        const arch = process.arch === 'arm64' ? 'aarch64' : 'x86_64';
        return fs.existsSync(`/lib/ld-musl-${arch}.so.1`);
    }

    _getLibName() {
        const isWindows = process.platform === 'win32';
        const isMac = process.platform === 'darwin';
        const arch = process.arch === 'arm64' ? 'arm64' : 'amd64';
        let ext = 'so';
        let platform = 'linux';
        let prefix = 'lib';
        let variant = '';

        if (isWindows) {
            ext = 'dll';
            platform = 'windows';
            prefix = '';
        } else if (isMac) {
            ext = 'dylib';
            platform = 'darwin';
            prefix = 'lib';
        } else if (this._isMusl()) {
            variant = '-musl';
        }
        return `${prefix}ua-parser-${platform}-${arch}${variant}.${ext}`;
    }

    _initNode(config) {
        if (!koffi) {
            const reason = koffiLoadError ? koffiLoadError.message : 'unknown error';
            throw new Error(`Failed to load the koffi FFI module: ${reason}`);
        }

        if (!this.libPath) {
            this.libPath = path.join(__dirname, this._getLibName());
        }

        if (!fs.existsSync(this.libPath)) {
            const fallbackPath = path.join(process.cwd(), this._getLibName());
            if (fs.existsSync(fallbackPath)) {
                this.libPath = fallbackPath;
            } else {
                throw new Error(`Shared library not found at ${this.libPath} or ${fallbackPath}. Please ensure the library is installed correctly.`);
            }
        }

        try {
            this.lib = koffi.load(this.libPath);
        } catch (e) {
            throw new Error(`Failed to load shared library: ${e.message}`);
        }

        this.freeFunc = this.lib.func('FreeString', 'void', ['void *']);

        // The library returns malloc'd char* strings that must be released via
        // its own FreeString (not the host allocator — matters on Windows where
        // the DLL links a static CRT). A koffi disposable type converts the
        // returned char* to a JS string and calls FreeString automatically.
        const goStr = koffi.disposable(`UaGoStr${UaParser._typeSeq++}`, 'str', this.freeFunc);
        this.initFunc = this.lib.func('Init', goStr, ['str']);
        this.parseFunc = this.lib.func('Parse', goStr, ['str']);

        const configJson = JSON.stringify(config);
        const err = this.initFunc(configJson);
        if (err) {
            throw new Error(`Failed to initialize parser: ${err}`);
        }
    }

    async _initWasmNode(config, wasmFile) {
        const { WASI } = require('wasi');
        const wasi = new WASI({ version: 'preview1' });

        const wasmBytes = fs.readFileSync(wasmFile);
        const wasmModule = await WebAssembly.compile(wasmBytes);
        const instance = await WebAssembly.instantiate(wasmModule, wasi.getImportObject());
        wasi.initialize(instance);

        this._wasmExports = instance.exports;
        this._wasmMemory = instance.exports.memory;

        const configJson = JSON.stringify(config);
        const configBytes = Buffer.from(configJson, 'utf-8');
        const ptr = this._wasmExports.malloc(configBytes.length);
        new Uint8Array(this._wasmMemory.buffer, ptr, configBytes.length).set(configBytes);
        const result = this._wasmExports.initUA(ptr, configBytes.length);
        this._wasmExports.free(ptr);

        if (result !== 0) {
            throw new Error('Failed to initialize WASM parser');
        }

        // WASI preview1 has no sockets, so the WASM engine cannot fetch
        // correction-rule updates itself — the host does it: one fetch now,
        // then daily (unref'd so it never keeps the process alive). Failures
        // are non-fatal; the embedded snapshot keeps serving.
        if (!config.disable_corrections_update && typeof this._wasmExports.updateCorrections === 'function') {
            const url = config.corrections_url ||
                'https://raw.githubusercontent.com/Octanium91/ua-parser/main/pkg/core/resources/corrections.yaml';
            this._pushCorrectionsFromURL(url);
            const timer = setInterval(() => this._pushCorrectionsFromURL(url), 24 * 60 * 60 * 1000);
            if (typeof timer.unref === 'function') timer.unref();
        }
    }

    _pushCorrectionsFromURL(url) {
        const https = require('https');
        const req = https.get(url, { timeout: 30000 }, (res) => {
            if (res.statusCode !== 200) {
                res.resume();
                console.warn(`WARN: ua-parser corrections fetch status ${res.statusCode} (embedded rules stay active)`);
                return;
            }
            const chunks = [];
            let total = 0;
            res.on('data', (chunk) => {
                total += chunk.length;
                if (total > 1 << 20) { // 1 MB cap, mirrors the engine
                    res.destroy(new Error('corrections payload exceeds 1 MB'));
                    return;
                }
                chunks.push(chunk);
            });
            res.on('end', () => {
                try {
                    const yaml = Buffer.concat(chunks);
                    const ptr = this._wasmExports.malloc(yaml.length);
                    new Uint8Array(this._wasmMemory.buffer, ptr, yaml.length).set(yaml);
                    const rc = this._wasmExports.updateCorrections(ptr, yaml.length);
                    this._wasmExports.free(ptr);
                    if (rc !== 0) {
                        console.warn('WARN: ua-parser corrections rejected by engine (keeping last good)');
                    }
                } catch (e) {
                    console.warn('WARN: ua-parser corrections push failed: ' + e.message);
                }
            });
        }).on('error', (e) => {
            console.warn('WARN: ua-parser corrections fetch failed (embedded rules stay active): ' + e.message);
        });
        // Don't let this background fetch keep an otherwise-idle process alive.
        req.on('socket', (s) => { if (typeof s.unref === 'function') s.unref(); });
    }

    _parseWasmNode(payload) {
        const payloadBytes = Buffer.from(payload, 'utf-8');
        const ptr = this._wasmExports.malloc(payloadBytes.length);
        new Uint8Array(this._wasmMemory.buffer, ptr, payloadBytes.length).set(payloadBytes);

        const packed = this._wasmExports.parseUA(ptr, payloadBytes.length);
        this._wasmExports.free(ptr);

        // parseUA returns (length << 32) | ptr as i64 (BigInt in JS)
        const resLength = Number(packed >> 32n);
        const resPtr = Number(packed & 0xFFFFFFFFn);

        if (resPtr === 0) return null;

        const resBytes = new Uint8Array(this._wasmMemory.buffer, resPtr, resLength);
        const resStr = Buffer.from(resBytes).toString('utf-8');
        this._wasmExports.free(resPtr);

        return JSON.parse(resStr);
    }

    async _initWasm(config) {
        if (typeof Go === 'undefined') {
            try {
                require('./wasm_exec.js');
            } catch (e) {
                // Ignore if not in a bundler environment
            }
        }

        if (typeof Go === 'undefined') {
            throw new Error('wasm_exec.js must be loaded before initializing UaParser in the browser');
        }

        const go = new Go();
        
        let wasmPath = this.libPath;
        if (!wasmPath) {
            try {
                // js/wasm ABI build (loaded via Go's wasm_exec.js). The wasip1
                // build (ua-parser.wasm) is only used by the Node.js WASI fallback.
                const resolved = require('./ua-parser-js.wasm');
                wasmPath = resolved.default || resolved;
            } catch (e) {
                wasmPath = '/ua-parser-js.wasm';
            }
        }
        
        let result;
        if (WebAssembly.instantiateStreaming) {
            result = await WebAssembly.instantiateStreaming(fetch(wasmPath), go.importObject);
        } else {
            const response = await fetch(wasmPath);
            const bytes = await response.arrayBuffer();
            result = await WebAssembly.instantiate(bytes, go.importObject);
        }

        go.run(result.instance);

        const configJson = JSON.stringify(config);
        const err = globalThis.initUA(configJson);
        if (err) {
            throw new Error(`Failed to initialize Wasm parser: ${err}`);
        }
    }

    /**
     * Parses a User-Agent string and optional Client Hint headers.
     * @param {string} ua User-Agent string
     * @param {Object} [headers] Map of HTTP headers (Client Hints, X-Requested-With)
     * @param {Object} [signals] Optional browser signals collected on the page
     *   ({max_touch_points, platform, webgl_renderer, screen: {w,h,dpr}, ...});
     *   in the browser build the engine auto-collects them when omitted.
     * @returns {Object} Parsed result
     */
    parse(ua, headers = {}, signals = undefined) {
        if (!this.isInitialized) {
            throw new Error('Parser not initialized. Call init() first.');
        }

        const payload = JSON.stringify(signals ? { ua, headers, signals } : { ua, headers });

        if (this.isWasm && isBrowser) {
            const resStr = globalThis.parseUA(payload);
            const result = JSON.parse(resStr);
            if (result.error) {
                throw new Error(result.error);
            }
            return result;
        } else if (this.isWasm) {
            const result = this._parseWasmNode(payload);
            if (result && result.error) {
                throw new Error(result.error);
            }
            return result;
        } else {
            // parseFunc returns a JS string (disposable type frees the C memory)
            const resStr = this.parseFunc(payload);
            if (resStr) {
                const result = JSON.parse(resStr);
                if (result.error) {
                    throw new Error(result.error);
                }
                return result;
            }
        }
        return null;
    }
}

// Unique suffix for koffi named types (koffi forbids re-registering a name)
UaParser._typeSeq = 0;

if (typeof module !== 'undefined' && module.exports) {
    module.exports = UaParser;
    // Support both `import UaParser from ...` and `import { UaParser } from ...`
    module.exports.UaParser = UaParser;
    module.exports.default = UaParser;
}
if (isBrowser) {
    globalThis.UaParser = UaParser;
}
