#!/usr/bin/env python3
"""GTK 3 fixture for opt-in real-desktop tests of the Linux engine.

The runtime itself never needs Python; this fixture only supplies controls whose
real state changes the tests can assert. It prints "ready" once mapped.
"""

import sys

import gi

gi.require_version("Gtk", "3.0")
from gi.repository import GLib, Gtk  # noqa: E402

TITLE = sys.argv[1] if len(sys.argv) > 1 else "Cherry Linux Fixture"


def named(widget, name):
    widget.get_accessible().set_name(name)
    return widget


def main():
    GLib.set_prgname("cherry-linux-fixture")
    GLib.set_application_name("Cherry Linux Fixture")
    window = Gtk.Window(title=TITLE)
    window.set_default_size(360, 320)
    window.connect("destroy", Gtk.main_quit)
    box = Gtk.Box(orientation=Gtk.Orientation.VERTICAL, spacing=6)
    window.add(box)

    count = {"value": 0}
    button = Gtk.Button(label="Count: 0")

    def increment(widget):
        count["value"] += 1
        widget.set_label("Count: {}".format(count["value"]))

    button.connect("clicked", increment)
    box.pack_start(button, False, False, 0)
    box.pack_start(named(Gtk.Entry(text="alpha"), "First field"), False, False, 0)
    second = named(Gtk.Entry(text="beta"), "Second field")
    box.pack_start(second, False, False, 0)
    locked = named(Gtk.Entry(text="locked"), "Locked field")
    locked.set_editable(False)
    box.pack_start(locked, False, False, 0)
    spin = named(Gtk.SpinButton.new_with_range(0, 10, 1), "Level")
    box.pack_start(spin, False, False, 0)
    box.pack_start(Gtk.CheckButton(label="Enabled option"), False, False, 0)
    disabled = Gtk.Button(label="Disabled action")
    disabled.set_sensitive(False)
    box.pack_start(disabled, False, False, 0)
    scale = named(Gtk.Scale.new_with_range(Gtk.Orientation.HORIZONTAL, 0, 100, 1), "Slider")
    scale.set_draw_value(False)
    box.pack_start(scale, False, False, 0)
    expander = Gtk.Expander(label="Details")
    expander.add(Gtk.Label(label="Hidden details"))
    box.pack_start(expander, False, False, 0)

    window.show_all()
    # Focus the second field so typing has one unambiguous target.
    second.grab_focus()
    window.present()
    print("ready", flush=True)
    Gtk.main()


if __name__ == "__main__":
    main()
