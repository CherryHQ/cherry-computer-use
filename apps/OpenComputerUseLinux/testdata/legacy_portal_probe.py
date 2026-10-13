"""Checks whether the RemoteDesktop portal's keyboard methods that predate libei
(NotifyKeyboardKeysym, NotifyKeyboardKeycode) type into a window, inside
testdata/gnome-session.sh. The portal dialog is approved through AT-SPI, which is
possible only because this is an isolated session no one else uses.

Usage: legacy_portal_probe.py <gtk_fixture.py>
Prints one JSON object with what the portal offers and what reached the field."""
import json
import subprocess
import sys
import threading
import time

import gi

gi.require_version("Atspi", "2.0")
from gi.repository import Atspi, Gio, GLib  # noqa: E402

FIXTURE = sys.argv[1]
PORTAL = "org.freedesktop.portal.Desktop"
PATH = "/org/freedesktop/portal/desktop"
RD = "org.freedesktop.portal.RemoteDesktop"
report = {}
bus = Gio.bus_get_sync(Gio.BusType.SESSION)


def call(interface, method, args, path=PATH, dest=PORTAL, timeout=30000):
    return bus.call_sync(dest, path, interface, method, args, None, Gio.DBusCallFlags.NONE, timeout, None)


def introspect(dest, path, interface):
    xml = call("org.freedesktop.DBus.Introspectable", "Introspect", None, path=path, dest=dest).unpack()[0]
    info = Gio.DBusNodeInfo.new_for_xml(xml).lookup_interface(interface)
    return sorted(m.name for m in info.methods) if info else None


def request(method, args, token):
    """Calls a portal method that answers through a Request object's Response signal."""
    sender = bus.get_unique_name()[1:].replace(".", "_")
    handle = f"/org/freedesktop/portal/desktop/request/{sender}/{token}"
    loop = GLib.MainLoop()
    result = {}

    def on_response(_conn, _sender, _path, _iface, _signal, params):
        result["code"], result["results"] = params.unpack()
        loop.quit()

    sub = bus.signal_subscribe(PORTAL, "org.freedesktop.portal.Request", "Response", handle, None,
                               Gio.DBusSignalFlags.NO_MATCH_RULE, on_response)
    call(RD, method, args)
    GLib.timeout_add_seconds(60, loop.quit)
    loop.run()
    bus.signal_unsubscribe(sub)
    return result.get("code"), result.get("results", {})


def find(node, pred, depth=0):
    try:
        if pred(node):
            return node
        for i in range(node.get_child_count() if depth < 25 else 0):
            child = node.get_child_at_index(i)
            hit = child and find(child, pred, depth + 1)
            if hit:
                return hit
    except GLib.Error:
        pass
    return None


def apps():
    desktop = Atspi.get_desktop(0)
    return [desktop.get_child_at_index(i) for i in range(desktop.get_child_count())]


def approve_dialog(stop):
    """Presses the confirming button of the portal dialog until the portal answers."""
    while not stop.is_set():
        seen = report.setdefault("apps_seen", [])
        for app in apps():
            if app and app.get_name() not in seen:
                seen.append(app.get_name())
            if not app or "portal" not in (app.get_name() or ""):
                continue
            buttons = []
            find(app, lambda n: n.get_role() in (Atspi.Role.PUSH_BUTTON, Atspi.Role.TOGGLE_BUTTON, Atspi.Role.CHECK_BOX,
                                                 Atspi.Role.SWITCH) and buttons.append(n))
            # "Share" stays insensitive until remote interaction is switched on.
            for switch in [b for b in buttons if b.get_role() == Atspi.Role.SWITCH]:
                if not switch.get_state_set().contains(Atspi.StateType.CHECKED) and Atspi.Action.get_n_actions(switch):
                    Atspi.Action.do_action(switch, 0)
                    report.setdefault("dialog", {"presses": 0})["switched"] = switch.get_name()
            if not buttons:
                continue
            dialog = report.setdefault("dialog", {"app": app.get_name(), "presses": 0})
            dialog["buttons"] = [f"{b.get_role_name()}:{b.get_name()}" for b in buttons]
            if "tree" not in dialog:
                tree = []
                def walk(n, d=0):
                    if d > 20 or not n:
                        return
                    states = n.get_state_set()
                    flags = [f for f, st in (("sensitive", Atspi.StateType.SENSITIVE), ("checked", Atspi.StateType.CHECKED),
                                             ("selected", Atspi.StateType.SELECTED), ("showing", Atspi.StateType.SHOWING)) if states.contains(st)]
                    tree.append(f"{'  ' * d}{n.get_role_name()}:{n.get_name()} {','.join(flags)}")
                    for i in range(n.get_child_count()):
                        walk(n.get_child_at_index(i), d + 1)
                walk(app)
                dialog["tree"] = tree
            for button in buttons:
                name = (button.get_name() or "").lower().replace("_", "")
                if button.get_role() == Atspi.Role.PUSH_BUTTON and name and name != "cancel":
                    actions = [Atspi.Action.get_action_name(button, i) for i in range(Atspi.Action.get_n_actions(button))]
                    dialog["pressed"], dialog["actions"] = button.get_name(), actions
                    dialog["presses"] += 1
                    Atspi.Action.do_action(button, actions.index("click") if "click" in actions else 0)
        time.sleep(1)


# What the portal and Mutter offer.
try:
    props = call("org.freedesktop.DBus.Properties", "GetAll", GLib.Variant("(s)", (RD,))).unpack()[0]
    report["portal"] = {"version": props.get("version"), "device_types": props.get("AvailableDeviceTypes"),
                        "methods": introspect(PORTAL, PATH, RD)}
except GLib.Error as e:
    report["portal"] = {"error": e.message}
try:
    report["mutter_session_methods"] = None
    path = call("org.gnome.Mutter.RemoteDesktop", "CreateSession", None, path="/org/gnome/Mutter/RemoteDesktop",
                dest="org.gnome.Mutter.RemoteDesktop").unpack()[0]
    report["mutter_session_methods"] = introspect("org.gnome.Mutter.RemoteDesktop", path,
                                                  "org.gnome.Mutter.RemoteDesktop.Session")
except GLib.Error as e:
    report["mutter_session_methods"] = {"error": e.message}

fixture = subprocess.Popen(["python3", FIXTURE, "Legacy Portal"], stdout=subprocess.PIPE, stderr=subprocess.PIPE, text=True)
if fixture.stdout.readline().strip() != "ready":
    report["fixture"] = fixture.stderr.read()[-1500:]
    print(json.dumps(report, default=str))
    sys.exit(1)
time.sleep(1)

try:
    code, results = request("CreateSession", GLib.Variant("(a{sv})", ({
        "handle_token": GLib.Variant("s", "ocu1"), "session_handle_token": GLib.Variant("s", "ocus")},)), "ocu1")
    session = results.get("session_handle")
    report["create_session"] = code
    code, _ = request("SelectDevices", GLib.Variant("(oa{sv})", (session, {
        "handle_token": GLib.Variant("s", "ocu2"), "types": GLib.Variant("u", 1)})), "ocu2")
    report["select_devices"] = code
    stop = threading.Event()
    threading.Thread(target=approve_dialog, args=(stop,), daemon=True).start()
    code, results = request("Start", GLib.Variant("(osa{sv})", (session, "", {
        "handle_token": GLib.Variant("s", "ocu3")})), "ocu3")
    stop.set()
    report["start"] = {"code": code, "devices": results.get("devices")}

    # Activate the fixture and focus its field, then type through the portal.
    subprocess.run(["gdbus", "call", "--session", "--dest", "org.gnome.Shell", "--object-path", "/org/gnome/Shell",
                    "--method", "org.gnome.Shell.Eval",
                    "global.get_window_actors().map(a => a.meta_window).find(w => w.get_title() === 'Legacy Portal')"
                    ".activate(global.get_current_time()); 'ok'"], capture_output=True)
    app = next(a for a in apps() if a and a.get_process_id() == fixture.pid)
    field = find(app, lambda n: n.get_name() == "Second field")
    Atspi.Component.grab_focus(field)
    time.sleep(0.5)
    text = lambda: Atspi.Text.get_text(field, 0, Atspi.Text.get_character_count(field))

    def keysym(sym):
        for pressed in (True, False):
            call(RD, "NotifyKeyboardKeysym", GLib.Variant("(oa{sv}iu)", (session, {}, sym, int(pressed))))
            time.sleep(0.02)

    # GNOME adds the keyboard to the seat only once it has sent a key; Shift types nothing.
    keysym(0xffe1)
    time.sleep(0.3)
    for sym in (0x68, 0x69):  # h i
        keysym(sym)
    time.sleep(0.5)
    report["after_keysym"] = text()
    for sym in (0x4e2d,):  # 中 has no key in the layout
        try:
            keysym(0x01000000 | sym)
            report["unicode_keysym"] = "sent"
        except GLib.Error as e:
            report["unicode_keysym"] = e.message
    time.sleep(0.5)
    report["after_unicode"] = text()
    for pressed in (True, False):  # evdev 30 is "a" on a US layout
        call(RD, "NotifyKeyboardKeycode", GLib.Variant("(oa{sv}iu)", (session, {}, 30, int(pressed))))
    time.sleep(0.5)
    report["after_keycode"] = text()
except GLib.Error as e:
    report["error"] = e.message
finally:
    fixture.kill()
print(json.dumps(report, default=str))
