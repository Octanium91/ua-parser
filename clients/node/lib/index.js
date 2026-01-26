const isBrowser = typeof window !== 'undefined' && typeof window.document !== 'undefined';

let koffi;
let path;
if (!isBrowser) {
    koffi = require('koffi');
    path = require('path');
}

class UaParser {
    /**
     * @param {string} [libPath] Path to the shared library (.so, .dll, .dylib) or .wasm file URL (for browser)
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
            this._initNode(config);
        }
        this.isInitialized = true;
    }

    _initNode(config) {
        if (!this.libPath) {
            const isWindows = process.platform === 'win32';
            const isMac = process.platform === 'darwin';
            const arch = process.arch === 'arm64' ? 'arm64' : 'amd64';
            let ext = 'so';
            let platform = 'linux';

            if (isWindows) {
                ext = 'dll';
                platform = 'windows';
            } else if (isMac) {
                ext = 'dylib';
                platform = 'darwin';
            }
            this.libPath = path.join(__dirname, `ua-parser-${platform}-${arch}.${ext}`);
        }

        try {
            this.lib = koffi.load(this.libPath);
        } catch (e) {
            // Fallback to current working directory
            const isWindows = process.platform === 'win32';
            const isMac = process.platform === 'darwin';
            const arch = process.arch === 'arm64' ? 'arm64' : 'amd64';
            let ext = 'so';
            let platform = 'linux';

            if (isWindows) {
                ext = 'dll';
                platform = 'windows';
            } else if (isMac) {
                ext = 'dylib';
                platform = 'darwin';
            }
            const fallbackPath = path.join(process.cwd(), `ua-parser-${platform}-${arch}.${ext}`);
            try {
                this.lib = koffi.load(fallbackPath);
            } catch (e2) {
                throw new Error(`Failed to load shared library from ${this.libPath} or ${fallbackPath}`);
            }
        }

        this.initFunc = this.lib.func('Init', 'void *', ['string']);
        this.parseFunc = this.lib.func('Parse', 'void *', ['string']);
        this.freeFunc = this.lib.func('FreeString', 'void', ['void *']);

        const configJson = JSON.stringify(config);
        const errPtr = this.initFunc(configJson);
        if (errPtr) {
            const errStr = koffi.decode(errPtr, 'string');
            this.freeFunc(errPtr);
            throw new Error(`Failed to initialize parser: ${errStr}`);
        }
    }

    async _initWasm(config) {
        if (typeof Go === 'undefined') {
            throw new Error('wasm_exec.js must be loaded before initializing UaParser in the browser');
        }

        const go = new Go();
        const wasmPath = this.libPath || '/ua-parser.wasm';
        
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
     * @param {Object} [headers] Map of HTTP headers (Client Hints)
     * @returns {Object} Parsed result
     */
    parse(ua, headers = {}) {
        if (!this.isInitialized) {
            throw new Error('Parser not initialized. Call init() first.');
        }

        const payload = JSON.stringify({ ua, headers });

        if (isBrowser) {
            const resStr = globalThis.parseUA(payload);
            const result = JSON.parse(resStr);
            if (result.error) {
                throw new Error(result.error);
            }
            return result;
        } else {
            const resPtr = this.parseFunc(payload);
            if (resPtr) {
                const resStr = koffi.decode(resPtr, 'string');
                this.freeFunc(resPtr);
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

if (typeof module !== 'undefined' && module.exports) {
    module.exports = UaParser;
}
if (isBrowser) {
    globalThis.UaParser = UaParser;
}
