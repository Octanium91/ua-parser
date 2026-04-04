const isBrowser = typeof window !== 'undefined' && typeof window.document !== 'undefined';

let koffi;
let path;
let fs;
if (!isBrowser) {
    koffi = require('koffi');
    path = require('path');
    fs = require('fs');
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
            try {
                this._initNode(config);
            } catch (nativeError) {
                // Native library failed — try WASM fallback
                const wasmFile = path.join(__dirname, 'ua-parser.wasm');
                if (fs.existsSync(wasmFile)) {
                    console.warn('WARN: Native UA-Parser library failed to load: ' + nativeError.message);
                    if (this._isMusl()) {
                        console.warn('WARN: Detected musl libc (Alpine Linux). Go c-shared libraries require glibc.');
                    }
                    console.warn('WARN: Falling back to WebAssembly (WASM) mode.');
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
                const resolved = require('./ua-parser.wasm');
                wasmPath = resolved.default || resolved;
            } catch (e) {
                wasmPath = '/ua-parser.wasm';
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
     * @param {Object} [headers] Map of HTTP headers (Client Hints)
     * @returns {Object} Parsed result
     */
    parse(ua, headers = {}) {
        if (!this.isInitialized) {
            throw new Error('Parser not initialized. Call init() first.');
        }

        const payload = JSON.stringify({ ua, headers });

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
