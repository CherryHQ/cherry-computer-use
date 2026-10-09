import importlib.util
import pathlib
import sys
import types
import unittest
from unittest import mock


class FakeAtspi:
    KeySynthType = types.SimpleNamespace(PRESS="press", RELEASE="release", PRESSRELEASE="pressrelease", STRING="string")

    def __init__(self):
        self.events = []
        self.fail_on = None

    def generate_keyboard_event(self, value, text, kind):
        if self.fail_on == kind:
            raise RuntimeError("synthesis failed")
        self.events.append(("key", value, text, kind))

    def generate_mouse_event(self, x, y, kind):
        self.events.append(("mouse", x, y, kind))


def load_runtime():
    gi = types.ModuleType("gi")
    gi.require_version = lambda *_args: None
    repository = types.ModuleType("gi.repository")
    repository.Atspi = FakeAtspi()
    repository.Gdk = types.SimpleNamespace(keyval_from_name=lambda name: {"Control_L": 1, "Shift_L": 2, "Return": 3}.get(name, 0))
    gi.repository = repository

    runtime_path = pathlib.Path(__file__).with_name("runtime.py")
    spec = importlib.util.spec_from_file_location("open_computer_use_linux_runtime", runtime_path)
    module = importlib.util.module_from_spec(spec)
    with mock.patch.dict(sys.modules, {"gi": gi, "gi.repository": repository}):
        spec.loader.exec_module(module)
    return module


class GlobalInputHelperTests(unittest.TestCase):
    def setUp(self):
        self.runtime = load_runtime()
        self.atspi = self.runtime.Atspi
        self.runtime.time.sleep = lambda _seconds: None

    def test_modifiers_are_released_when_the_main_key_fails(self):
        self.atspi.fail_on = "pressrelease"
        with self.assertRaises(RuntimeError):
            self.runtime.send_key("ctrl+shift+Return")
        self.assertEqual(
            [event[1:] for event in self.atspi.events],
            [(1, None, "press"), (2, None, "press"), (2, None, "release"), (1, None, "release")],
        )

    def test_unknown_modifier_is_refused_before_any_event(self):
        with self.assertRaises(RuntimeError):
            self.runtime.send_key("hyper+a")
        self.assertEqual(self.atspi.events, [])

    def test_click_uses_caller_screen_coordinates(self):
        self.assertEqual(self.runtime.perform_operation({"tool": "click", "x": 10.4, "y": 20.6}), {"ok": True})
        self.assertEqual(self.atspi.events[0], ("mouse", 10, 21, "abs"))

    def test_helper_does_not_observe_or_resolve_targets(self):
        for tool in ("list_apps", "get_app_state", "set_value", "perform_secondary_action"):
            with self.assertRaises(RuntimeError):
                self.runtime.perform_operation({"tool": tool, "app": "Example"})
        self.assertEqual(self.atspi.events, [])


if __name__ == "__main__":
    unittest.main()
