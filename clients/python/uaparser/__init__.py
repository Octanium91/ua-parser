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
            if system == "Windows":
                lib_path = os.path.join(os.path.dirname(__file__), "ua-parser-windows.dll")
            else:
                lib_path = os.path.join(os.path.dirname(__file__), "ua-parser-linux.so")
        
        if not os.path.exists(lib_path):
            # Try current directory as fallback
            alt_path = "ua-parser-windows.dll" if platform.system() == "Windows" else "ua-parser-linux.so"
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
