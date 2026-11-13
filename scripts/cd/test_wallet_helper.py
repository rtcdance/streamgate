import json
from pathlib import Path
import struct
import tempfile
import unittest

import wallet_helper as helper

CANDIDATE = 'a' * 40


def elf(machine=62, kind=1):
    raw = bytearray(120)
    raw[:7] = b'\x7fELF\x02\x01\x01'
    struct.pack_into('<HHI', raw, 16, 2, machine, 1)
    struct.pack_into('<Q', raw, 32, 64)
    struct.pack_into('<HH', raw, 54, 56, 1)
    struct.pack_into('<I', raw, 64, kind)
    return bytes(raw)


class OriginalWalletClientTests(unittest.TestCase):
    def test_original_export_is_reusable_and_modified_bytes_never_overwritten(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary); binary = root / 'compiled'; directory = root / 'bundle'
            binary.write_bytes(elf())
            helper.export(binary, directory, CANDIDATE, b'original source')
            helper.export(binary, directory, CANDIDATE, b'original source')
            self.assertEqual(2, len(helper.validate(directory, CANDIDATE, b'original source')))
            self.assertEqual(0o755, (directory / 'walletgrpc-linux').stat().st_mode & 0o777)
            original = (directory / 'walletgrpc.json').read_bytes()
            binary.write_bytes(elf() + b'foreign compile')
            with self.assertRaises(ValueError): helper.export(binary, directory, CANDIDATE, b'original source')
            self.assertEqual(elf(), (directory / 'walletgrpc-linux').read_bytes())
            self.assertEqual(original, (directory / 'walletgrpc.json').read_bytes())

    def test_script_wrong_architecture_dynamic_or_truncated_binary_rejected(self):
        for raw in (b'#!/bin/sh\n', elf(183), elf(kind=3), elf()[:110], elf()[:16]):
            with self.subTest(raw=raw[:8]), self.assertRaises(ValueError):
                helper.static_amd64(raw)

    def test_foreign_candidate_source_binary_or_claims_are_rejected(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary); binary = root / 'compiled'; directory = root / 'bundle'
            binary.write_bytes(elf()); helper.export(binary, directory, CANDIDATE, b'source')
            for candidate, source in [('b' * 40, b'source'), (CANDIDATE, b'edited')]:
                with self.subTest(candidate=candidate, source=source), self.assertRaises(ValueError):
                    helper.validate(directory, candidate, source)
            metadata = directory / 'walletgrpc.json'; original = json.loads(metadata.read_text())
            for key, value in [('compilerExit', 1), ('platform', 'darwin/arm64'), ('completeAcceptance', True), ('binaryPath', '../other')]:
                metadata.write_text(json.dumps(dict(original, **{key: value})))
                with self.subTest(field=key), self.assertRaises(ValueError): helper.validate(directory, CANDIDATE, b'source')
            metadata.write_text(json.dumps(original)); (directory / 'walletgrpc-linux').write_bytes(elf() + b'changed')
            with self.assertRaises(ValueError): helper.validate(directory, CANDIDATE, b'source')

    def test_symlink_outputs_are_rejected_without_touching_target(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary); binary = root / 'compiled'; directory = root / 'bundle'; directory.mkdir()
            binary.write_bytes(elf()); target = root / 'foreign'; target.write_bytes(b'keep')
            (directory / 'walletgrpc-linux').symlink_to(target)
            with self.assertRaises(ValueError): helper.export(binary, directory, CANDIDATE, b'source')
            with self.assertRaises(ValueError): helper.validate(directory, CANDIDATE, b'source')
            self.assertEqual(b'keep', target.read_bytes())


if __name__ == '__main__':
    unittest.main()
