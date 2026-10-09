#!/usr/bin/env python3
"""Transitional global input helper for the Linux CLI/MCP service.

Observation and semantic actions run in the native Go engine. This helper only
synthesizes pointer and keyboard input at screen coordinates the caller has
already validated; it never resolves applications or elements.
"""

import json
import math
import os
import sys
import time
import traceback
import warnings

warnings.filterwarnings("ignore", category=DeprecationWarning)

import gi

gi.require_version("Atspi", "2.0")

try:
    gi.require_version("Gdk", "3.0")
    from gi.repository import Gdk
except (ImportError, ValueError):
    Gdk = None

from gi.repository import Atspi


def require_desktop_session():
    missing = []
    if not os.environ.get("XDG_RUNTIME_DIR"):
        missing.append("XDG_RUNTIME_DIR")
    if not os.environ.get("DBUS_SESSION_BUS_ADDRESS"):
        missing.append("DBUS_SESSION_BUS_ADDRESS")
    if missing:
        raise RuntimeError(
            "Linux runtime requires an active desktop session; missing "
            + ", ".join(missing)
        )


def screen_point(operation, x_key, y_key):
    x, y = operation.get(x_key), operation.get(y_key)
    if x is None or y is None:
        raise RuntimeError("{} and {} are required".format(x_key, y_key))
    return float(x), float(y)


def mouse_button_events(button):
    normalized = (button or "left").lower()
    if normalized == "right":
        return "b3p", "b3r"
    if normalized == "middle":
        return "b2p", "b2r"
    return "b1p", "b1r"


def send_mouse_click(x, y, button, count):
    down, up = mouse_button_events(button)
    repeat = max(1, int(count or 1))
    for _ in range(repeat):
        Atspi.generate_mouse_event(int(round(x)), int(round(y)), "abs")
        Atspi.generate_mouse_event(int(round(x)), int(round(y)), down)
        time.sleep(0.035)
        Atspi.generate_mouse_event(int(round(x)), int(round(y)), up)
        time.sleep(0.05)


def send_drag(from_x, from_y, to_x, to_y):
    Atspi.generate_mouse_event(int(round(from_x)), int(round(from_y)), "abs")
    Atspi.generate_mouse_event(int(round(from_x)), int(round(from_y)), "b1p")
    x, y = from_x, from_y
    try:
        steps = 12
        for step in range(1, steps + 1):
            x = from_x + ((to_x - from_x) * step / steps)
            y = from_y + ((to_y - from_y) * step / steps)
            Atspi.generate_mouse_event(int(round(x)), int(round(y)), "abs")
            time.sleep(0.02)
    finally:
        Atspi.generate_mouse_event(int(round(x)), int(round(y)), "b1r")


KEY_ALIASES = {
    "return": "Return",
    "enter": "Return",
    "tab": "Tab",
    "escape": "Escape",
    "esc": "Escape",
    "backspace": "BackSpace",
    "back_space": "BackSpace",
    "delete": "Delete",
    "space": "space",
    "left": "Left",
    "up": "Up",
    "right": "Right",
    "down": "Down",
    "home": "Home",
    "end": "End",
    "page_up": "Page_Up",
    "prior": "Page_Up",
    "page_down": "Page_Down",
    "next": "Page_Down",
}

MODIFIER_KEYS = {
    "ctrl": "Control_L",
    "control": "Control_L",
    "shift": "Shift_L",
    "alt": "Alt_L",
    "super": "Super_L",
    "win": "Super_L",
    "cmd": "Super_L",
}


def keyval(name):
    if Gdk is not None:
        value = Gdk.keyval_from_name(name)
        if value:
            return int(value)
    if len(name) == 1:
        return ord(name)
    raise RuntimeError("Unsupported key: " + name)


def send_key(key):
    parts = [part for part in str(key).split("+") if part]
    if not parts:
        raise RuntimeError("Unsupported key: " + str(key))
    main = parts[-1]
    modifiers = parts[:-1]
    unknown = [m for m in modifiers if m.lower() not in MODIFIER_KEYS]
    if unknown:
        raise RuntimeError("Unsupported modifier: " + ", ".join(unknown))
    values = [keyval(MODIFIER_KEYS[m.lower()]) for m in modifiers]
    normalized = KEY_ALIASES.get(main.lower(), main)
    main_value = None if len(normalized) == 1 else keyval(normalized)
    pressed = []
    try:
        for value in values:
            Atspi.generate_keyboard_event(value, None, Atspi.KeySynthType.PRESS)
            pressed.append(value)
        if main_value is None:
            Atspi.generate_keyboard_event(0, normalized, Atspi.KeySynthType.STRING)
        else:
            Atspi.generate_keyboard_event(
                main_value, None, Atspi.KeySynthType.PRESSRELEASE
            )
    finally:
        # Modifiers are released even when a later event fails.
        for value in reversed(pressed):
            Atspi.generate_keyboard_event(value, None, Atspi.KeySynthType.RELEASE)


def send_text(text):
    Atspi.generate_keyboard_event(0, str(text), Atspi.KeySynthType.STRING)


def scroll_element(direction, pages):
    key = "Page_Down"
    if direction == "up":
        key = "Page_Up"
    elif direction == "left":
        key = "Left"
    elif direction == "right":
        key = "Right"
    repeat = max(1, int(math.ceil(float(pages or 1))))
    for _ in range(repeat):
        send_key(key)
        time.sleep(0.04)


def perform_operation(operation):
    tool = operation.get("tool")
    if tool == "click":
        x, y = screen_point(operation, "x", "y")
        send_mouse_click(
            x, y, operation.get("mouse_button", "left"), operation.get("click_count", 1)
        )
    elif tool == "drag":
        from_x, from_y = screen_point(operation, "from_x", "from_y")
        to_x, to_y = screen_point(operation, "to_x", "to_y")
        send_drag(from_x, from_y, to_x, to_y)
    elif tool == "scroll":
        scroll_element(operation.get("direction", "down"), operation.get("pages", 1))
    elif tool == "type_text":
        send_text(operation.get("text", ""))
    elif tool == "press_key":
        send_key(operation.get("key", ""))
    else:
        raise RuntimeError('unsupportedTool("{}")'.format(tool))
    time.sleep(0.12)
    return {"ok": True}


def main():
    if len(sys.argv) != 2:
        raise RuntimeError("runtime.py requires an operation JSON path")
    require_desktop_session()
    Atspi.init()
    with open(sys.argv[1], "r", encoding="utf-8") as file:
        operation = json.load(file)
    try:
        response = perform_operation(operation)
    except Exception as exc:
        response = {"ok": False, "error": str(exc)}
    print(json.dumps(response, separators=(",", ":")))


if __name__ == "__main__":
    try:
        main()
    except Exception:
        print(json.dumps({"ok": False, "error": traceback.format_exc()}))
