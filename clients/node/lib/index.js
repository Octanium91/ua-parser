const koffi = require('koffi');
const path = require('path');

class UaParser {
    /**
     * @param {string} [libPath] Path to the shared library (.so or .dll)
     */
    constructor(libPath) {
        if (!libPath) {
            const isWindows = process.platform === 'win32';
            const arch = process.arch === 'arm64' ? 'arm64' : 'amd64';
            const ext = isWindows ? 'dll' : 'so';
            const platform = isWindows ? 'windows' : 'linux';
            libPath = path.join(__dirname, `ua-parser-${platform}-${arch}.${ext}`);
        }

        try {
            this.lib = koffi.load(libPath);
        } catch (e) {
            // Fallback to current working directory
            const isWindows = process.platform === 'win32';
            const arch = process.arch === 'arm64' ? 'arm64' : 'amd64';
            const ext = isWindows ? 'dll' : 'so';
            const platform = isWindows ? 'windows' : 'linux';
            const fallbackPath = path.join(process.cwd(), `ua-parser-${platform}-${arch}.${ext}`);
            try {
                this.lib = koffi.load(fallbackPath);
            } catch (e2) {
                throw new Error(`Failed to load shared library from ${libPath} or ${fallbackPath}`);
            }
        }
        
        // Define functions using koffi
        // Go: func Init(configJSON *C.char) *C.char
        this.initFunc = this.lib.func('Init', 'void *', ['string']);
        
        // Go: func Parse(payloadJSON *C.char) *C.char
        this.parseFunc = this.lib.func('Parse', 'void *', ['string']);
        
        // Go: func FreeString(ptr *C.char)
        this.freeFunc = this.lib.func('FreeString', 'void', ['void *']);
    }

    /**
     * Initializes the parser with optional configuration.
     * @param {Object} [config]
     * @param {boolean} [config.disable_auto_update]
     * @param {number} [config.lru_cache_size]
     */
    init(config = {}) {
        const configJson = JSON.stringify(config);
        const errPtr = this.initFunc(configJson);
        if (errPtr) {
            const errStr = koffi.decode(errPtr, 'string');
            this.freeFunc(errPtr);
            throw new Error(`Failed to initialize parser: ${errStr}`);
        }
    }

    /**
     * Parses a User-Agent string and optional Client Hint headers.
     * @param {string} ua User-Agent string
     * @param {Object} [headers] Map of HTTP headers (Client Hints)
     * @returns {Object} Parsed result
     */
    parse(ua, headers = {}) {
        const payload = JSON.stringify({ ua, headers });
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
        return null;
    }
}

module.exports = UaParser;
