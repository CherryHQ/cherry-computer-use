// Cherry Computer Use: window identity, per-window capture and targeted input
// for GNOME Shell, served on the session bus to the runtime's Wayland helper.
//
// Every request names its target by Mutter window id plus the observed PID and
// title, and is refused unless all three still match. Input is checked against
// the target in the same call that sends it: the window under the point for the
// pointer, the focus window for keys and text. Nothing is served until the user
// allows the calling executable in a Shell dialog; the grant lasts until the
// screen locks (GNOME disables extensions there) or the session ends, and can be
// revoked from the top bar. See
// docs/design-docs/linux-display-backends.md ("GNOME Shell 扩展").

import Clutter from 'gi://Clutter';
import Gio from 'gi://Gio';
import GLib from 'gi://GLib';
import Shell from 'gi://Shell';
import St from 'gi://St';
import * as Main from 'resource:///org/gnome/shell/ui/main.js';
import * as ModalDialog from 'resource:///org/gnome/shell/ui/modalDialog.js';
import * as Dialog from 'resource:///org/gnome/shell/ui/dialog.js';
import * as PanelMenu from 'resource:///org/gnome/shell/ui/panelMenu.js';
import * as PopupMenu from 'resource:///org/gnome/shell/ui/popupMenu.js';
import {Extension} from 'resource:///org/gnome/shell/extensions/extension.js';

Gio._promisify(Gio.DBusConnection.prototype, 'call');
Gio._promisify(Shell.Screenshot, 'composite_to_stream');

const BUS_NAME = 'com.cherryai.ComputerUse.Shell';
const OBJECT_PATH = '/com/cherryai/ComputerUse/Shell';
const INTERFACE = 'com.cherryai.ComputerUse.Shell1';
// Bumped when the D-Bus interface changes incompatibly.
const PROTOCOL = 1;
const TARGET = '<arg type="u" direction="in" name="id"/><arg type="u" direction="in" name="pid"/><arg type="s" direction="in" name="title"/>';
const IFACE = `<node><interface name="${INTERFACE}">
<method name="GetVersion"><arg type="u" direction="out" name="protocol"/></method>
<method name="GetAuthorization"><arg type="s" direction="out" name="status"/></method>
<method name="Authorize"><arg type="s" direction="out" name="status"/></method>
<method name="ListWindows"><arg type="s" direction="out" name="windows"/></method>
<method name="CaptureWindow">${TARGET}<arg type="ay" direction="out" name="png"/><arg type="u" direction="out" name="width"/><arg type="u" direction="out" name="height"/></method>
<method name="Activate">${TARGET}<arg type="b" direction="out" name="focused"/></method>
<method name="WindowAt"><arg type="d" direction="in" name="x"/><arg type="d" direction="in" name="y"/><arg type="s" direction="out" name="window"/></method>
<method name="Click">${TARGET}<arg type="d" direction="in" name="x"/><arg type="d" direction="in" name="y"/><arg type="u" direction="in" name="button"/><arg type="u" direction="in" name="count"/></method>
<method name="Drag">${TARGET}<arg type="d" direction="in" name="from_x"/><arg type="d" direction="in" name="from_y"/><arg type="d" direction="in" name="to_x"/><arg type="d" direction="in" name="to_y"/></method>
<method name="KeyChord">${TARGET}<arg type="au" direction="in" name="modifiers"/><arg type="u" direction="in" name="key"/></method>
<method name="Text">${TARGET}<arg type="s" direction="in" name="text"/></method>
</interface></node>`;

// D-Bus error names carry the runtime's error code. Possible means input was
// already sent when the request failed, so its effect is unknown.
const ERROR = {
    permission: `${INTERFACE}.Error.PermissionRequired`,
    target: `${INTERFACE}.Error.TargetUnavailable`,
    invalid: `${INTERFACE}.Error.InvalidArgument`,
    unsupported: `${INTERFACE}.Error.UnsupportedCapability`,
    capture: `${INTERFACE}.Error.CaptureFailed`,
    possible: `${INTERFACE}.Error.Possible`,
};

class Refusal extends Error {
    constructor(name, message) {
        super(message);
        this.dbusName = name;
    }
}

// Clients bind a seat device asynchronously after the seat gains it; input sent
// before that is lost. A new virtual device waits this long before first use.
const DEVICE_SETTLE_MS = 200;
// Keeps a fully covered window painting, as a clone counts as a visible user.
const CAPTURE_CLONE_MS = 4000;
const MAX_CAPTURE_BYTES = 64 * 1024 * 1024;
const DRAG_STEPS = 10;
// Clients see the pointer enter and move before a press, as with a real mouse;
// a press in the same instant as the motion can reach a client that has not
// handled the motion yet.
const SETTLE_MS = 40;
const CLICK_GAP_MS = 30;

const delay = ms => new Promise(resolve => GLib.timeout_add(GLib.PRIORITY_DEFAULT, ms, () => {
    resolve();
    return GLib.SOURCE_REMOVE;
}));

const now = () => GLib.get_monotonic_time();

export default class ComputerUseExtension extends Extension {
    enable() {
        // Executable path -> 'granted' | 'denied', for this login session only.
        this._grants = new Map();
        // Bus unique name -> executable path. Unique names are never reused.
        this._callers = new Map();
        this._asking = new Map();
        this._dialogs = Promise.resolve();
        this._open = null;
        this._clones = new Map();
        this._devices = null;
        this._dbus = Gio.DBusExportedObject.wrapJSObject(IFACE, this);
        this._dbus.export(Gio.DBus.session, OBJECT_PATH);
        this._nameId = Gio.bus_own_name_on_connection(Gio.DBus.session, BUS_NAME, Gio.BusNameOwnerFlags.NONE, null, null);
    }

    disable() {
        // Runs on lock as well: GNOME disables extensions without the
        // unlock-dialog session mode, so nothing is served while locked.
        this._open?.finish(false);
        this._revoke();
        Gio.bus_unown_name(this._nameId);
        this._dbus.unexport();
        this._dbus = null;
        this._grants = null;
        this._callers = null;
        this._asking = null;
    }

    // ---- authorization ----

    async _caller(invocation) {
        const sender = invocation.get_sender();
        let executable = this._callers.get(sender);
        if (!executable) {
            const reply = await Gio.DBus.session.call('org.freedesktop.DBus', '/org/freedesktop/DBus', 'org.freedesktop.DBus',
                'GetConnectionUnixProcessID', new GLib.Variant('(s)', [sender]), new GLib.VariantType('(u)'),
                Gio.DBusCallFlags.NONE, -1, null);
            const [pid] = reply.deepUnpack();
            // The kernel's view of the binary; a caller cannot choose it.
            executable = GLib.file_read_link(`/proc/${pid}/exe`);
            this._callers.set(sender, executable);
        }
        return executable;
    }

    async _authorized(invocation) {
        if (Main.sessionMode.isLocked || Main.sessionMode.isGreeter)
            throw new Refusal(ERROR.target, 'The desktop is locked');
        const executable = await this._caller(invocation);
        if (this._grants.get(executable) !== 'granted')
            throw new Refusal(ERROR.permission, 'Desktop control has not been allowed; call Authorize');
        return executable;
    }

    // One dialog at a time; concurrent requests for the same executable share it.
    _ask(executable) {
        let asking = this._asking.get(executable);
        if (!asking) {
            asking = this._dialogs = this._dialogs.then(() => this._dialog(executable));
            this._asking.set(executable, asking);
            asking.finally(() => this._asking?.delete(executable));
        }
        return asking;
    }

    _dialog(executable) {
        return new Promise(resolve => {
            const dialog = new ModalDialog.ModalDialog({destroyOnClose: true});
            dialog.contentLayout.add_child(new Dialog.MessageDialogContent({
                title: 'Allow desktop control?',
                description: `${executable} wants to see your windows and use the keyboard and pointer on them. ` +
                    'It keeps this permission until you lock the screen or log out, or until you stop it from the top bar.',
            }));
            let answered = false;
            const finish = allowed => {
                if (answered)
                    return;
                answered = true;
                this._open = null;
                dialog.close();
                resolve(allowed);
            };
            dialog.setButtons([
                {label: 'Deny', action: () => finish(false), key: Clutter.KEY_Escape, default: true},
                {label: 'Allow', action: () => finish(true)},
            ]);
            dialog.connect('closed', () => finish(false));
            this._open = {finish};
            // Another modal grab (a different dialog, the lock screen) refuses.
            if (!dialog.open())
                finish(false);
        });
    }

    // Answers the open dialog. Reachable only through org.gnome.Shell.Eval, which
    // GNOME serves in unsafe mode alone: the isolated test sessions.
    _answerForTest(allowed) {
        const open = this._open;
        open?.finish(allowed);
        return Boolean(open);
    }

    _updateIndicator() {
        const granted = [...this._grants].filter(([, status]) => status === 'granted').map(([path]) => path);
        if (granted.length === 0) {
            this._indicator?.destroy();
            this._indicator = null;
            return;
        }
        if (!this._indicator) {
            this._indicator = new PanelMenu.Button(0.0, 'Cherry Computer Use', false);
            this._indicator.add_child(new St.Icon({icon_name: 'preferences-desktop-remote-desktop-symbolic', style_class: 'system-status-icon'}));
            Main.panel.addToStatusArea(this.uuid, this._indicator);
        }
        const menu = this._indicator.menu;
        menu.removeAll();
        for (const path of granted)
            menu.addMenuItem(new PopupMenu.PopupMenuItem(`Desktop control allowed: ${GLib.path_get_basename(path)}`, {reactive: false}));
        menu.addMenuItem(new PopupMenu.PopupSeparatorMenuItem());
        menu.addAction('Stop desktop control', () => this._revoke());
    }

    _revoke() {
        for (const [path, status] of this._grants ?? [])
            if (status === 'granted')
                this._grants.set(path, 'denied');
        this._releaseDevices();
        for (const {clone, timeout} of this._clones.values()) {
            GLib.source_remove(timeout);
            clone.destroy();
        }
        this._clones.clear();
        this._indicator?.destroy();
        this._indicator = null;
    }

    // ---- windows ----

    _actors() {
        return global.get_window_actors().filter(actor => actor.meta_window && !actor.meta_window.is_override_redirect());
    }

    _describe(window, actor, stacking, workspace) {
        const frame = window.get_frame_rect();
        const buffer = window.get_buffer_rect();
        return {
            id: window.get_stable_sequence(),
            pid: window.get_pid(),
            title: window.get_title() ?? '',
            wmClass: window.get_wm_class() ?? '',
            frame: [frame.x, frame.y, frame.width, frame.height],
            // The client surface including client-side shadows: toolkit window
            // coordinates (AT-SPI) start here.
            buffer: [buffer.x, buffer.y, buffer.width, buffer.height],
            focused: window.has_focus(),
            minimized: window.minimized,
            visible: actor.visible && !window.minimized && window.located_on_workspace(workspace),
            scale: actor.get_resource_scale(),
            stacking,
        };
    }

    _windows() {
        const actors = new Map(this._actors().map(actor => [actor.meta_window, actor]));
        const workspace = global.workspace_manager.get_active_workspace();
        return global.display.sort_windows_by_stacking([...actors.keys()])
            .map((window, stacking) => this._describe(window, actors.get(window), stacking, workspace));
    }

    _target(id, pid, title) {
        const actor = this._actors().find(candidate => candidate.meta_window.get_stable_sequence() === id);
        const window = actor?.meta_window;
        if (!window || window.get_pid() !== pid || (window.get_title() ?? '') !== title)
            throw new Refusal(ERROR.target, 'The target window is gone or has changed');
        return {actor, window};
    }

    // The window owning the topmost reactive actor at a point, or null when Shell
    // UI (panel, notification, menu) or the desktop background is there.
    _windowAt(x, y) {
        let actor = global.stage.get_actor_at_pos(Clutter.PickMode.REACTIVE, x, y);
        while (actor && !actor.meta_window)
            actor = actor.get_parent();
        return actor?.meta_window ?? null;
    }

    // Names what is at a point, for refusals: a window's title, or the Shell
    // element (with its style class, such as a notification banner).
    _describeAt(x, y) {
        const window = this._windowAt(x, y);
        if (window)
            return `the window "${window.get_title() ?? ''}"`;
        const actor = global.stage.get_actor_at_pos(Clutter.PickMode.REACTIVE, x, y);
        const names = [];
        for (let current = actor; current && names.length < 3; current = current.get_parent()) {
            const name = current.name || current.style_class || current.constructor.name;
            if (name)
                names.push(name);
        }
        return names.length ? `the Shell element ${names.join(' < ')}` : 'nothing';
    }

    async _capture(id, pid, title) {
        const {actor, window} = this._target(id, pid, title);
        if (window.minimized)
            throw new Refusal(ERROR.target, 'The target window is minimized');
        let entry = this._clones.get(id);
        const fresh = !entry;
        if (entry) {
            GLib.source_remove(entry.timeout);
        } else {
            entry = {clone: new Clutter.Clone({source: actor, opacity: 0, width: 1, height: 1, reactive: false})};
            global.stage.add_child(entry.clone);
            this._clones.set(id, entry);
        }
        entry.timeout = GLib.timeout_add(GLib.PRIORITY_DEFAULT, CAPTURE_CLONE_MS, () => {
            entry.clone.destroy();
            this._clones.delete(id);
            return GLib.SOURCE_REMOVE;
        });
        // A window that was fully covered draws again once the clone maps; give it a frame.
        if (fresh)
            await delay(100);
        const frame = window.get_frame_rect();
        const buffer = window.get_buffer_rect();
        const scale = actor.get_resource_scale();
        const texture = actor.paint_to_content(null)?.get_texture();
        if (!texture || !(scale > 0))
            throw new Refusal(ERROR.capture, 'The window has no content to capture');
        const x = Math.round((frame.x - buffer.x) * scale);
        const y = Math.round((frame.y - buffer.y) * scale);
        const width = Math.round(frame.width * scale);
        const height = Math.round(frame.height * scale);
        if (x < 0 || y < 0 || width <= 0 || height <= 0 || x + width > texture.get_width() || y + height > texture.get_height())
            throw new Refusal(ERROR.capture, 'The window content does not match its frame');
        const stream = Gio.MemoryOutputStream.new_resizable();
        await Shell.Screenshot.composite_to_stream(texture, x, y, width, height, 1, null, 0, 0, 1, stream);
        stream.close(null);
        if (stream.get_data_size() > MAX_CAPTURE_BYTES)
            throw new Refusal(ERROR.capture, 'The window image is too large');
        return [stream.steal_as_bytes(), width, height];
    }

    // ---- input ----

    async _input() {
        if (!this._devices) {
            const seat = Clutter.get_default_backend().get_default_seat();
            this._devices = {
                pointer: seat.create_virtual_device(Clutter.InputDeviceType.POINTER_DEVICE),
                keyboard: seat.create_virtual_device(Clutter.InputDeviceType.KEYBOARD_DEVICE),
                held: {buttons: new Set(), keys: new Set()},
            };
            await delay(DEVICE_SETTLE_MS);
        }
        return this._devices;
    }

    _releaseDevices() {
        if (!this._devices)
            return;
        const {pointer, keyboard, held} = this._devices;
        for (const button of held.buttons)
            pointer.notify_button(now(), button, Clutter.ButtonState.RELEASED);
        for (const key of [...held.keys].reverse())
            keyboard.notify_keyval(now(), key, Clutter.KeyState.RELEASED);
        this._devices = null;
    }

    _button(devices, button, pressed) {
        devices.pointer.notify_button(now(), button, pressed ? Clutter.ButtonState.PRESSED : Clutter.ButtonState.RELEASED);
        if (pressed)
            devices.held.buttons.add(button);
        else
            devices.held.buttons.delete(button);
    }

    _key(devices, keyval, pressed) {
        devices.keyboard.notify_keyval(now(), keyval, pressed ? Clutter.KeyState.PRESSED : Clutter.KeyState.RELEASED);
        if (pressed)
            devices.held.keys.add(keyval);
        else
            devices.held.keys.delete(keyval);
    }

    // Moves the pointer, lets clients see the motion, and confirms the target owns
    // the point right before anything is pressed.
    async _pointAt(devices, target, x, y) {
        const [width, height] = global.display.get_size();
        if (!(x >= 0 && y >= 0 && x < width && y < height))
            throw new Refusal(ERROR.invalid, 'The point is outside the desktop');
        devices.pointer.notify_absolute_motion(now(), x, y);
        await delay(SETTLE_MS);
        const {window} = this._target(...target);
        if (this._windowAt(x, y) !== window)
            throw new Refusal(ERROR.target, `The point (${Math.round(x)}, ${Math.round(y)}) shows ${this._describeAt(x, y)}, not the target; the click would not reach it`);
    }

    _requireFocus(window) {
        if (global.display.focus_window !== window)
            throw new Refusal(ERROR.target, 'The target window does not have keyboard focus; keys would reach another window');
    }

    async _click(target, x, y, button, count) {
        if (![1, 2, 3].includes(button) || count < 1 || count > 3)
            throw new Refusal(ERROR.invalid, 'Unsupported button or click count');
        const devices = await this._input();
        this._target(...target);
        await this._pointAt(devices, target, x, y);
        try {
            for (let i = 0; i < count; i++) {
                if (i > 0)
                    await delay(CLICK_GAP_MS);
                this._button(devices, button, true);
                this._button(devices, button, false);
            }
        } catch (error) {
            throw new Refusal(ERROR.possible, `Pointer input stopped part way: ${error}`);
        }
    }

    async _drag(target, fromX, fromY, toX, toY) {
        const devices = await this._input();
        this._target(...target);
        await this._pointAt(devices, target, fromX, fromY);
        try {
            this._button(devices, Clutter.BUTTON_PRIMARY, true);
            for (let i = 1; i <= DRAG_STEPS; i++) {
                await delay(10);
                devices.pointer.notify_absolute_motion(now(), fromX + (toX - fromX) * i / DRAG_STEPS, fromY + (toY - fromY) * i / DRAG_STEPS);
            }
        } catch (error) {
            throw new Refusal(ERROR.possible, `Pointer input stopped part way: ${error}`);
        } finally {
            if (devices.held.buttons.has(Clutter.BUTTON_PRIMARY))
                this._button(devices, Clutter.BUTTON_PRIMARY, false);
        }
    }

    async _keyChord(target, modifiers, key) {
        const devices = await this._input();
        const {window} = this._target(...target);
        this._requireFocus(window);
        // Mutter derives the modifier state from the pressed keys, as for a keyboard.
        const pressed = [];
        try {
            for (const keyval of [...modifiers, key]) {
                this._key(devices, keyval, true);
                pressed.push(keyval);
            }
        } finally {
            for (const keyval of pressed.reverse())
                this._key(devices, keyval, false);
        }
    }

    async _text(target, text) {
        const devices = await this._input();
        const {window} = this._target(...target);
        this._requireFocus(window);
        // The input method commits any text through text-input, as the on-screen
        // keyboard does. Without a text-input client, only keys the layout surely
        // has are sent; anything else is refused before sending.
        if (Main.inputMethod.currentFocus) {
            Main.inputMethod.commit(text);
            return;
        }
        const keyvals = [...text].map(character => {
            if (character === '\n')
                return Clutter.KEY_Return;
            if (character === '\t')
                return Clutter.KEY_Tab;
            const code = character.codePointAt(0);
            return code >= 0x20 && code <= 0x7e ? code : null;
        });
        if (keyvals.includes(null))
            throw new Refusal(ERROR.unsupported, 'The focused client does not accept input method text, and the text has characters outside ASCII');
        for (const keyval of keyvals) {
            this._key(devices, keyval, true);
            this._key(devices, keyval, false);
        }
    }

    // ---- D-Bus methods ----

    async _serve(invocation, work) {
        try {
            invocation.return_value(await work());
        } catch (error) {
            if (error instanceof Refusal)
                invocation.return_dbus_error(error.dbusName, error.message);
            else
                invocation.return_dbus_error(ERROR.target, String(error));
        }
    }

    GetVersion() {
        return PROTOCOL;
    }

    GetAuthorizationAsync(_params, invocation) {
        this._serve(invocation, async () => {
            const executable = await this._caller(invocation);
            return new GLib.Variant('(s)', [this._grants.get(executable) ?? 'notDetermined']);
        });
    }

    AuthorizeAsync(_params, invocation) {
        this._serve(invocation, async () => {
            if (Main.sessionMode.isLocked || Main.sessionMode.isGreeter)
                throw new Refusal(ERROR.target, 'The desktop is locked');
            const executable = await this._caller(invocation);
            if (this._grants.get(executable) !== 'granted') {
                const allowed = await this._ask(executable);
                this._grants?.set(executable, allowed ? 'granted' : 'denied');
                this._updateIndicator();
            }
            return new GLib.Variant('(s)', [this._grants?.get(executable) ?? 'denied']);
        });
    }

    ListWindowsAsync(_params, invocation) {
        this._serve(invocation, async () => {
            await this._authorized(invocation);
            return new GLib.Variant('(s)', [JSON.stringify(this._windows())]);
        });
    }

    CaptureWindowAsync([id, pid, title], invocation) {
        this._serve(invocation, async () => {
            await this._authorized(invocation);
            const [png, width, height] = await this._capture(id, pid, title);
            return new GLib.Variant('(ayuu)', [png.toArray(), width, height]);
        });
    }

    ActivateAsync([id, pid, title], invocation) {
        this._serve(invocation, async () => {
            await this._authorized(invocation);
            const {window} = this._target(id, pid, title);
            window.activate(global.get_current_time());
            await delay(100);
            return new GLib.Variant('(b)', [global.display.focus_window === window]);
        });
    }

    WindowAtAsync([x, y], invocation) {
        this._serve(invocation, async () => {
            await this._authorized(invocation);
            const window = this._windowAt(x, y);
            const workspace = global.workspace_manager.get_active_workspace();
            const actor = window && this._actors().find(candidate => candidate.meta_window === window);
            return new GLib.Variant('(s)', [JSON.stringify(window && actor ? this._describe(window, actor, -1, workspace) : null)]);
        });
    }

    ClickAsync([id, pid, title, x, y, button, count], invocation) {
        this._serve(invocation, async () => {
            await this._authorized(invocation);
            await this._click([id, pid, title], x, y, button, count);
            return null;
        });
    }

    DragAsync([id, pid, title, fromX, fromY, toX, toY], invocation) {
        this._serve(invocation, async () => {
            await this._authorized(invocation);
            await this._drag([id, pid, title], fromX, fromY, toX, toY);
            return null;
        });
    }

    KeyChordAsync([id, pid, title, modifiers, key], invocation) {
        this._serve(invocation, async () => {
            await this._authorized(invocation);
            await this._keyChord([id, pid, title], modifiers, key);
            return null;
        });
    }

    TextAsync([id, pid, title, text], invocation) {
        this._serve(invocation, async () => {
            await this._authorized(invocation);
            await this._text([id, pid, title], text);
            return null;
        });
    }
}
