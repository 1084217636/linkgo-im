"""Local Windows TCP bridge for WSL mirrored networking when localhost fails.

The browser speaks HTTPS/WSS to this loopback listener. Bytes pass unchanged to
WSL Nginx through ``wsl.exe``; this process never sees decrypted credentials.
"""

import argparse
import os
import socket
import socketserver
import subprocess
import sys
import threading


class BridgeHandler(socketserver.BaseRequestHandler):
    def handle(self) -> None:
        server = self.server
        command = [
            "wsl.exe",
            "--distribution",
            server.distro,
            "--exec",
            "/usr/bin/python3",
            "-u",
            server.bridge_path,
            "--port",
            str(server.target_port),
        ]
        process = subprocess.Popen(
            command, stdin=subprocess.PIPE, stdout=subprocess.PIPE,
            stderr=subprocess.DEVNULL, bufsize=0,
        )

        def to_wsl() -> None:
            try:
                while data := self.request.recv(65536):
                    process.stdin.write(data)
            except (BrokenPipeError, ConnectionError, OSError):
                pass
            finally:
                process.stdin.close()

        reader = threading.Thread(target=to_wsl, daemon=True)
        reader.start()
        try:
            while data := os.read(process.stdout.fileno(), 65536):
                self.request.sendall(data)
        except (BrokenPipeError, ConnectionError, OSError):
            pass
        finally:
            self.request.close()
            if process.poll() is None:
                process.terminate()
            reader.join(timeout=1)
            try:
                process.wait(timeout=5)
            except subprocess.TimeoutExpired:
                process.kill()
                process.wait()


class BridgeServer(socketserver.ThreadingTCPServer):
    allow_reuse_address = True
    daemon_threads = True


def main() -> None:
    parser = argparse.ArgumentParser()
    parser.add_argument("--distro", required=True)
    parser.add_argument("--bridge-path", required=True)
    parser.add_argument("--target-port", type=int, default=8444)
    parser.add_argument("--listen-port", type=int, default=8443)
    args = parser.parse_args()
    with BridgeServer(("127.0.0.1", args.listen_port), BridgeHandler) as server:
        server.distro = args.distro
        server.bridge_path = args.bridge_path
        server.target_port = args.target_port
        print(f"Windows localhost:{args.listen_port} -> WSL localhost:{args.target_port}", flush=True)
        server.serve_forever(poll_interval=0.5)


if __name__ == "__main__":
    main()
