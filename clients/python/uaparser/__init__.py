import ctypes
import json
import os
import platform


def _is_musl():
    """Detect a musl-based Linux system (e.g. Alpine Linux)."""
    return (
        os.path.exists("/lib/ld-musl-x86_64.so.1")
        or os.path.exists("/lib/ld-musl-aarch64.so.1")
    )


def _get_arch():
    """Map the current machine architecture to a supported binary suffix."""
    machine = platform.machine().lower()
    if machine in ("x86_64", "amd64"):
        return "amd64"
    if machine in ("aarch64", "arm64"):
        return "arm64"
    raise RuntimeError(
        f"Unsupported architecture: {platform.machine()!r}. "
        "ua-parser ships native binaries for x86_64/amd64 and aarch64/arm64 only."
    )


def _default_lib_name():
    """Return the bundled shared library filename for the current platform."""
    system = platform.system()
    arch = _get_arch()
    if system == "Windows":
        return f"ua-parser-windows-{arch}.dll"
    if system == "Darwin":
        return f"libua-parser-darwin-{arch}.dylib"
    if _is_musl():
        return f"libua-parser-linux-{arch}-musl.so"
    return f"libua-parser-linux-{arch}.so"


_MUSL_ERROR_MESSAGE = (
    "Failed to load the native ua-parser shared library on this musl-based system "
    "(e.g. Alpine Linux). Native Go shared libraries currently cannot be loaded on "
    "Alpine/musl due to a Go toolchain limitation "
    "(https://github.com/golang/go/issues/54805). Working alternatives: "
    "1) run the ua-parser REST server container (ghcr.io/octanium91/ua-parser) "
    "next to your application; "
    "2) use a glibc-based image (e.g. python:3.12-slim); "
    "3) use the Node.js or Java clients, which have an automatic WebAssembly fallback."
)


class UaParser:
    """
    Universal User-Agent Parser Python Wrapper.
    Requires the platform-specific shared library (bundled with the package)
    to be present, e.g. libua-parser-linux-amd64.so or ua-parser-windows-amd64.dll.
    """
    def __init__(self, lib_path=None):
        if lib_path is None:
            lib_path = os.path.join(os.path.dirname(__file__), _default_lib_name())

        if not os.path.exists(lib_path):
            # Try current directory as fallback
            alt_path = _default_lib_name()
            if os.path.exists(alt_path):
                lib_path = alt_path
            else:
                raise FileNotFoundError(f"Shared library not found at {lib_path}. Please provide lib_path or ensure the library is in the package directory.")

        try:
            self.lib = ctypes.CDLL(lib_path)
        except OSError as e:
            if platform.system() == "Linux" and _is_musl():
                raise RuntimeError(_MUSL_ERROR_MESSAGE) from e
            raise

        # Define argument and return types
        # Use c_void_p for return types to preserve the original C pointer.
        # Using c_char_p would auto-convert to Python bytes and lose the pointer,
        # causing a memory leak since FreeString would never free the real allocation.
        self.lib.Init.argtypes = [ctypes.c_char_p]
        self.lib.Init.restype = ctypes.c_void_p

        self.lib.Parse.argtypes = [ctypes.c_char_p]
        self.lib.Parse.restype = ctypes.c_void_p

        self.lib.FreeString.argtypes = [ctypes.c_void_p]
        self.lib.FreeString.restype = None

    def init(self, config=None):
        """
        Initializes the parser with an optional config dict.
        Example: {"disable_auto_update": True, "lru_cache_size": 1000}
        """
        config_json = json.dumps(config).encode('utf-8') if config else None
        err_ptr = self.lib.Init(config_json)
        if err_ptr:
            err_str = ctypes.string_at(err_ptr).decode('utf-8')
            self.lib.FreeString(err_ptr)
            raise Exception(f"Failed to initialize parser: {err_str}")

    def parse(self, ua, headers=None, signals=None):
        """
        Parses a User-Agent string with optional Client Hint headers and an
        optional browser-signals dict (max_touch_points, platform,
        webgl_renderer, screen, ...) that unmasks what UA and Client Hints
        cannot — e.g. iPads posing as Macs in Safari.
        Returns a dictionary with the parsed results.
        """
        payload = {
            "ua": ua,
            "headers": headers or {}
        }
        if signals:
            payload["signals"] = signals
        payload_json = json.dumps(payload).encode('utf-8')
        res_ptr = self.lib.Parse(payload_json)
        if res_ptr:
            res_bytes = ctypes.string_at(res_ptr)
            self.lib.FreeString(res_ptr)
            return json.loads(res_bytes.decode('utf-8'))
        return None
