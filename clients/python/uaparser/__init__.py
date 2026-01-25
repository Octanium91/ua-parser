import ctypes
import json
import os
import platform

class UaParser:
    """
    Universal User-Agent Parser Python Wrapper.
    Requires the shared library (ua-parser-linux.so or ua-parser-windows.dll) to be present.
    """
    def __init__(self, lib_path=None):
        if lib_path is None:
            system = platform.system()
            machine = platform.machine().lower()
            arch = "arm64" if machine in ["arm64", "aarch64"] else "amd64"
            
            if system == "Windows":
                lib_name = f"ua-parser-windows-{arch}.dll"
            else:
                lib_name = f"ua-parser-linux-{arch}.so"
            
            lib_path = os.path.join(os.path.dirname(__file__), lib_name)
        
        if not os.path.exists(lib_path):
            # Try current directory as fallback
            system = platform.system()
            machine = platform.machine().lower()
            arch = "arm64" if machine in ["arm64", "aarch64"] else "amd64"
            alt_path = f"ua-parser-windows-{arch}.dll" if system == "Windows" else f"ua-parser-linux-{arch}.so"
            if os.path.exists(alt_path):
                lib_path = alt_path
            else:
                raise FileNotFoundError(f"Shared library not found at {lib_path}. Please provide lib_path or ensure the library is in the package directory.")

        self.lib = ctypes.CDLL(lib_path)
        
        # Define argument and return types
        self.lib.Init.argtypes = [ctypes.c_char_p]
        self.lib.Init.restype = ctypes.c_char_p
        
        self.lib.Parse.argtypes = [ctypes.c_char_p]
        self.lib.Parse.restype = ctypes.c_char_p
        
        self.lib.FreeString.argtypes = [ctypes.c_char_p]
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

    def parse(self, ua, headers=None):
        """
        Parses a User-Agent string and optional Client Hint headers.
        Returns a dictionary with the parsed results.
        """
        payload = {
            "ua": ua,
            "headers": headers or {}
        }
        payload_json = json.dumps(payload).encode('utf-8')
        res_ptr = self.lib.Parse(payload_json)
        if res_ptr:
            res_str = ctypes.string_at(res_ptr).decode('utf-8')
            self.lib.FreeString(res_ptr)
            return json.loads(res_str)
        return None
