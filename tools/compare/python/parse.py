"""Python side of the comparison harness (see ../README.md).

Each invocation measures exactly ONE library configuration in a fresh
interpreter, so memory numbers are attributable. RSS is reported in-process
(psapi on Windows, /proc/self/status on Linux) because the venv python.exe
shim on Windows breaks external working-set sampling.

    python parse.py <corpus.json> ours       <N> [cacheSize] [libPath]
    python parse.py <corpus.json> uap        <N>   # uncached basic.Resolver
    python parse.py <corpus.json> uap-cached <N>   # default global parse()

- ours: this project's Python client (ctypes -> Go shared library). libPath
  points at the platform driver (bundled in the release .whl; when running from
  the repo pass the file from GitHub Releases explicitly).
- uap: uap-python (PyPI `ua-parser`), the reference uap-core implementation,
  pure-Python backend that a plain `pip install ua-parser` gets. UA string
  only: no Client Hints, no bot flags. Its default global parse() wraps the
  resolver in a cache, hence the two modes.
"""

import json
import os
import sys
import time

REPO_ROOT = os.path.abspath(os.path.join(os.path.dirname(__file__), "..", "..", ".."))


def rss_mb():
    """(settled, peak) working set of the current process, in MB."""
    if sys.platform == "win32":
        import ctypes
        from ctypes import wintypes

        class PMC(ctypes.Structure):
            _fields_ = [
                ("cb", wintypes.DWORD),
                ("PageFaultCount", wintypes.DWORD),
                ("PeakWorkingSetSize", ctypes.c_size_t),
                ("WorkingSetSize", ctypes.c_size_t),
                ("QuotaPeakPagedPoolUsage", ctypes.c_size_t),
                ("QuotaPagedPoolUsage", ctypes.c_size_t),
                ("QuotaPeakNonPagedPoolUsage", ctypes.c_size_t),
                ("QuotaNonPagedPoolUsage", ctypes.c_size_t),
                ("PagefileUsage", ctypes.c_size_t),
                ("PeakPagefileUsage", ctypes.c_size_t),
            ]

        pmc = PMC()
        pmc.cb = ctypes.sizeof(PMC)
        kernel32 = ctypes.windll.kernel32
        # GetCurrentProcess returns a 64-bit HANDLE; the default c_int restype
        # truncates it and GetProcessMemoryInfo fails with ERROR_INVALID_HANDLE.
        kernel32.GetCurrentProcess.restype = ctypes.c_void_p
        ctypes.windll.psapi.GetProcessMemoryInfo(
            ctypes.c_void_p(kernel32.GetCurrentProcess()), ctypes.byref(pmc), pmc.cb
        )
        return pmc.WorkingSetSize / 1048576, pmc.PeakWorkingSetSize / 1048576

    settled = peak = 0.0
    with open("/proc/self/status", encoding="ascii") as f:
        for line in f:
            if line.startswith("VmRSS:"):
                settled = int(line.split()[1]) / 1024
            elif line.startswith("VmHWM:"):
                peak = int(line.split()[1]) / 1024
    return settled, peak


def build(impl, cache_size, lib_path):
    if impl == "ours":
        sys.path.insert(0, os.path.join(REPO_ROOT, "clients", "python"))
        from uaparser import UaParser

        parser = UaParser(lib_path=lib_path)
        parser.init({"disable_auto_update": True, "lru_cache_size": cache_size})
        return lambda e: parser.parse(e["ua"], e.get("headers"))

    if impl == "uap":
        import ua_parser
        import ua_parser.basic
        import ua_parser.loaders

        parser = ua_parser.Parser(
            ua_parser.basic.Resolver(ua_parser.loaders.load_builtins())
        )

        def fn(e):
            r = parser.parse(e["ua"])
            return r.user_agent.family if r.user_agent else None

        return fn

    if impl == "uap-cached":
        from ua_parser import parse as uap_parse

        def fn(e):
            r = uap_parse(e["ua"])
            return r.user_agent.family if r.user_agent else None

        return fn

    raise ValueError(f"unknown impl: {impl}")


def main():
    corpus_path, impl, n = sys.argv[1], sys.argv[2], int(sys.argv[3])
    cache_size = int(sys.argv[4]) if len(sys.argv) > 4 else 0
    lib_path = sys.argv[5] if len(sys.argv) > 5 else None

    with open(corpus_path, encoding="utf-8") as f:
        corpus = json.load(f)

    t0 = time.perf_counter()
    fn = build(impl, cache_size, lib_path)
    fn(corpus[0])  # include lazy-init paths in the init measurement
    init_ms = (time.perf_counter() - t0) * 1000

    for e in corpus:  # warmup (also fills the cache when enabled)
        fn(e)

    t1 = time.perf_counter()
    for i in range(n):
        fn(corpus[i % len(corpus)])
    elapsed_ms = (time.perf_counter() - t1) * 1000

    import gc

    gc.collect()
    settled, peak = rss_mb()

    print(
        f"impl={impl} cache={cache_size} init={init_ms:.0f}ms | "
        f"{n} parses in {elapsed_ms:.0f}ms — {n / elapsed_ms * 1000:.0f} ops/sec, "
        f"{elapsed_ms * 1000 / n:.2f} us/op | settled RSS {settled:.1f} MB, peak {peak:.1f} MB"
    )


if __name__ == "__main__":
    main()
