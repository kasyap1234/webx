"""webx — Python SDK for the webx web toolkit.

Zero-dependency client for any webx/webxd server (local `webx serve` or a
hosted deployment). Method names mirror the REST surface and Firecrawl's
shape so migration is trivial.

    from webx import WebX

    wx = WebX("http://localhost:8080", api_key="...")   # api_key optional
    doc = wx.scrape("https://example.com")
    print(doc["data"]["markdown"])
"""

from .client import WebX, WebXError, Job

__all__ = ["WebX", "WebXError", "Job"]
__version__ = "0.1.0"
