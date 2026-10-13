//! XKB keymap lookup for the EIS keyboard. libxkbcommon is loaded at run time:
//! without it the helper still runs and keyboard input reports it as missing.

use std::{ffi::CString, os::raw::c_char};

use xkbcommon_dl::{
    XkbCommon, xkb_context, xkb_context_flags, xkb_keymap, xkb_keymap_compile_flags, xkb_keymap_format,
    xkbcommon_option,
};

pub const SHIFT_L: u32 = 0xffe1;
const RETURN: u32 = 0xff0d;
const TAB: u32 = 0xff09;

/// A key to press: the XKB keycode and whether the level needs Shift.
#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub struct Key {
    pub code: u32,
    pub shift: bool,
}

pub struct Keymap {
    lib: &'static XkbCommon,
    context: *mut xkb_context,
    keymap: *mut xkb_keymap,
    lock: Option<u32>,
}

impl Keymap {
    pub fn parse(text: &[u8]) -> Result<Self, String> {
        let lib = xkbcommon_option().ok_or("libxkbcommon is not installed")?;
        let text = CString::new(text.split(|b| *b == 0).next().unwrap_or_default()).map_err(|e| e.to_string())?;
        unsafe {
            let context = (lib.xkb_context_new)(xkb_context_flags::XKB_CONTEXT_NO_FLAGS);
            if context.is_null() {
                return Err("cannot create an XKB context".into());
            }
            let keymap = (lib.xkb_keymap_new_from_string)(
                context,
                text.as_ptr(),
                xkb_keymap_format::XKB_KEYMAP_FORMAT_TEXT_V1,
                xkb_keymap_compile_flags::XKB_KEYMAP_COMPILE_NO_FLAGS,
            );
            if keymap.is_null() {
                (lib.xkb_context_unref)(context);
                return Err("the compositor sent a keymap libxkbcommon cannot compile".into());
            }
            let lock = (lib.xkb_keymap_mod_get_index)(keymap, c"Lock".as_ptr() as *const c_char);
            Ok(Keymap { lib, context, keymap, lock: (lock != u32::MAX).then_some(lock) })
        }
    }

    /// Whether Caps Lock is among the locked modifiers the compositor reported.
    pub fn caps_locked(&self, locked: u32) -> bool {
        self.lock.is_some_and(|index| index < 32 && locked & (1 << index) != 0)
    }

    fn syms(&self, code: u32, layout: u32, level: u32) -> &[u32] {
        unsafe {
            let mut syms: *const u32 = std::ptr::null();
            let count = (self.lib.xkb_keymap_key_get_syms_by_level)(self.keymap, code, layout, level, &mut syms);
            if count <= 0 || syms.is_null() { &[] } else { std::slice::from_raw_parts(syms, count as usize) }
        }
    }

    /// Finds a key producing the keysym in the active layout at level 1 or 2;
    /// higher levels need layout-specific modifiers and are not attempted.
    fn find(&self, layout: u32, matches: impl Fn(u32) -> bool) -> Option<Key> {
        let (min, max) =
            unsafe { ((self.lib.xkb_keymap_min_keycode)(self.keymap), (self.lib.xkb_keymap_max_keycode)(self.keymap)) };
        for level in 0..2 {
            for code in min..=max {
                let layouts = unsafe { (self.lib.xkb_keymap_num_layouts_for_key)(self.keymap, code) };
                if layouts == 0 {
                    continue;
                }
                let layout = if layout < layouts { layout } else { 0 };
                let levels = unsafe { (self.lib.xkb_keymap_num_levels_for_key)(self.keymap, code, layout) };
                if level < levels && self.syms(code, layout, level).iter().any(|sym| matches(*sym)) {
                    return Some(Key { code, shift: level == 1 });
                }
            }
        }
        None
    }

    pub fn keysym(&self, keysym: u32, layout: u32) -> Option<Key> {
        self.find(layout, |sym| sym == keysym)
    }

    pub fn character(&self, character: char, layout: u32) -> Option<Key> {
        match character {
            '\n' | '\r' => return self.keysym(RETURN, layout),
            '\t' => return self.keysym(TAB, layout),
            _ => {}
        }
        self.find(layout, |sym| unsafe { (self.lib.xkb_keysym_to_utf32)(sym) } == character as u32)
    }
}

impl Drop for Keymap {
    fn drop(&mut self) {
        unsafe {
            (self.lib.xkb_keymap_unref)(self.keymap);
            (self.lib.xkb_context_unref)(self.context);
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    const US: &str = r#"xkb_keymap {
        xkb_keycodes { minimum = 8; maximum = 255; <AC01> = 38; <LFSH> = 50; <RTRN> = 36; <CAPS> = 66; <AE01> = 10; };
        xkb_types { include "complete" };
        xkb_compatibility { include "complete" };
        xkb_symbols {
            key <AC01> { [ a, A ] };
            key <AE01> { [ 1, exclam ] };
            key <LFSH> { [ Shift_L ] };
            key <RTRN> { [ Return ] };
            key <CAPS> { [ Caps_Lock ] };
            modifier_map Shift { <LFSH> };
            modifier_map Lock { <CAPS> };
        };
    };"#;

    #[test]
    fn finds_levels_and_refuses_missing_characters() {
        let Ok(keymap) = Keymap::parse(US.as_bytes()) else {
            eprintln!("libxkbcommon is not installed; skipping");
            return;
        };
        assert_eq!(keymap.character('a', 0), Some(Key { code: 38, shift: false }));
        assert_eq!(keymap.character('A', 0), Some(Key { code: 38, shift: true }));
        assert_eq!(keymap.character('!', 0), Some(Key { code: 10, shift: true }));
        assert_eq!(keymap.character('\n', 0), Some(Key { code: 36, shift: false }));
        assert_eq!(keymap.keysym(SHIFT_L, 0), Some(Key { code: 50, shift: false }));
        assert_eq!(keymap.character('中', 0), None);
        let lock = keymap.lock.expect("Lock modifier");
        assert!(keymap.caps_locked(1 << lock) && !keymap.caps_locked(0));
    }
}
