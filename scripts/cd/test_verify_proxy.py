import asyncio
import importlib.util
from pathlib import Path
import unittest


class ControllerProxyTests(unittest.IsolatedAsyncioTestCase):
    async def test_roundtrip_and_half_close(self):
        path = Path(__file__).with_name("verify_proxy.py")
        spec = importlib.util.spec_from_file_location("proxy", path)
        proxy = importlib.util.module_from_spec(spec)
        spec.loader.exec_module(proxy)

        async def echo(reader, writer):
            data = await reader.read()
            writer.write(data.upper())
            await writer.drain()
            writer.close()
            await writer.wait_closed()

        target = await asyncio.start_server(echo, "127.0.0.1", 0)
        port = target.sockets[0].getsockname()[1]
        front = await proxy.start_forwarder(0, "127.0.0.1", port)
        try:
            reader, writer = await asyncio.open_connection("127.0.0.1", front.sockets[0].getsockname()[1])
            writer.write(b"streamgate")
            await writer.drain()
            writer.write_eof()
            self.assertEqual(await asyncio.wait_for(reader.read(), 3), b"STREAMGATE")
            writer.close()
            await writer.wait_closed()
        finally:
            front.close()
            target.close()
            await front.wait_closed()
            await target.wait_closed()

    async def test_occupied_port_fails(self):
        path = Path(__file__).with_name("verify_proxy.py")
        spec = importlib.util.spec_from_file_location("proxy", path)
        proxy = importlib.util.module_from_spec(spec)
        spec.loader.exec_module(proxy)
        occupied = await asyncio.start_server(lambda r, w: w.close(), "127.0.0.1", 0)
        try:
            with self.assertRaises(OSError):
                await proxy.start_forwarder(occupied.sockets[0].getsockname()[1], "127.0.0.1", 1)
        finally:
            occupied.close()
            await occupied.wait_closed()


if __name__ == "__main__":
    unittest.main()
