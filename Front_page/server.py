#!/usr/bin/env python3
# -*- coding: utf-8 -*-
import http.server
import socketserver
from functools import partial

class UTF8Handler(http.server.SimpleHTTPRequestHandler):
    def end_headers(self):
        # 为 HTML、CSS、JS 文件设置 UTF-8 编码
        if self.path.endswith('.html'):
            self.send_header('Content-Type', 'text/html; charset=utf-8')
        elif self.path.endswith('.css'):
            self.send_header('Content-Type', 'text/css; charset=utf-8')
        elif self.path.endswith('.js'):
            self.send_header('Content-Type', 'application/javascript; charset=utf-8')
        super().end_headers()

PORT = 8081

with socketserver.TCPServer(("", PORT), UTF8Handler) as httpd:
    print(f"服务器运行在 http://localhost:{PORT}")
    print("按 Ctrl+C 停止服务器")
    httpd.serve_forever()
