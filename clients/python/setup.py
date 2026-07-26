from setuptools import setup, find_packages

setup(
    name="ua-parser-core",
    version="1.1.7",  # Placeholder, managed by CI
    packages=find_packages(),
    package_data={
        "uaparser": ["*.so", "*.dll", "*.dylib", "*.h"],
    },
    description="Python wrapper for the Universal User-Agent Parser",
    long_description=open("README.md", encoding="utf-8").read(),
    long_description_content_type="text/markdown",
    author="uaparser",
    url="https://github.com/Octanium91/ua-parser",
    python_requires=">=3.6",
    classifiers=[
        "Programming Language :: Python :: 3",
        "License :: OSI Approved :: Apache Software License",
    ],
)
