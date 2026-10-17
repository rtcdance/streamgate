#!/usr/bin/env python3
"""Expose project services on the controller's loopback, never the host."""
import argparse
import asyncio
import json
from pathlib import Path
import signal


async def start_forwarder(listen_port, target_host, target_port):
    async def forward(reader, writer):
        remote = None
        try:
            remote_reader, remote = await asyncio.wait_for(
                asyncio.open_connection(target_host, target_port), 10
            )

            async def copy(source, destination):
                while True:
                    data = await asyncio.wait_for(source.read(65536), 90)
                    if not data:
                        if destination.can_write_eof():
                            destination.write_eof()
                        return
                    destination.write(data)
                    await destination.drain()

            await asyncio.wait_for(asyncio.gather(copy(reader, remote), copy(remote_reader, writer)), 300)
        except (OSError, asyncio.TimeoutError, ConnectionError):
            pass
        finally:
            writer.close()
            if remote:
                remote.close()

    return await asyncio.start_server(forward, "127.0.0.1", listen_port)


async def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("config", type=Path)
    parser.add_argument("ready", type=Path)
    args = parser.parse_args()
    servers = []
    stop = asyncio.Event()
    for sig in (signal.SIGINT, signal.SIGTERM):
        asyncio.get_running_loop().add_signal_handler(sig, stop.set)
    try:
        ports = {}
        for mapping in json.loads(args.config.read_text()):
            server = await start_forwarder(mapping["listen"], mapping["host"], mapping["port"])
            servers.append(server)
            ports[mapping["host"]] = server.sockets[0].getsockname()[1]
        temporary = args.ready.with_suffix(".tmp")
        temporary.write_text(json.dumps(ports) + "\n")
        temporary.replace(args.ready)
        await stop.wait()
    finally:
        args.ready.unlink(missing_ok=True)
        for server in servers:
            server.close()
            await server.wait_closed()


if __name__ == "__main__":
    asyncio.run(main())
