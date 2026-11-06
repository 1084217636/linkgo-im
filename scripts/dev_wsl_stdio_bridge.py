"""Bridge one raw TCP stream from Windows wsl.exe stdio to local WSL Nginx."""

import argparse
import os
import socket
import sys
import threading


def copy_input(target: socket.socket) -> None:
    try:
        while data := os.read(sys.stdin.fileno(), 65536):
            target.sendall(data)
    except (BrokenPipeError, ConnectionError, OSError):
        pass
    finally:
        try:
            target.shutdown(socket.SHUT_WR)
        except OSError:
            pass


def main() -> None:
    parser = argparse.ArgumentParser()
    parser.add_argument("--port", type=int, required=True)
    args = parser.parse_args()
    with socket.create_connection(("127.0.0.1", args.port), timeout=10) as target:
        target.settimeout(None)
        writer = threading.Thread(target=copy_input, args=(target,), daemon=True)
        writer.start()
        try:
            while data := target.recv(65536):
                os.write(sys.stdout.fileno(), data)
        except (BrokenPipeError, ConnectionError, OSError):
            pass
        writer.join(timeout=1)


if __name__ == "__main__":
    main()
