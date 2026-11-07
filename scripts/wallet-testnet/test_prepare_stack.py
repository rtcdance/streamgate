import importlib.util
from pathlib import Path
import unittest


spec = importlib.util.spec_from_file_location('prepare_stack', Path(__file__).with_name('prepare_stack.py'))
prepare_stack = importlib.util.module_from_spec(spec)
spec.loader.exec_module(prepare_stack)


class H5NginxTests(unittest.TestCase):
    def test_both_modes_declare_the_module_type_the_panel_requires(self):
        # panel.mjs imports fixture.mjs; Chrome refuses to execute a module
        # served as application/octet-stream, which is the image's default
        # because its mime.types maps only `js`.
        for upstream in ('monolith', 'api-gateway'):
            with self.subTest(upstream=upstream):
                config = prepare_stack.nginx(upstream)
                self.assertRegex(config, r'types\s*\{\s*application/javascript\s+mjs;\s*\}')
                self.assertIn('default_type application/octet-stream;', config)

    def test_unknown_mode_is_still_rejected(self):
        with self.assertRaises(ValueError):
            prepare_stack.nginx('elsewhere')


if __name__ == '__main__':
    unittest.main()
